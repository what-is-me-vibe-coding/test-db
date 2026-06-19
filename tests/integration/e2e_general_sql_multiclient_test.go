// Package integration 端到端集成测试：通用 SQL 工作负载 + 多客户端长连接。
//
// 本文件覆盖第一组「一个 server + 多个 client」场景，聚焦 DML（INSERT/UPDATE/
// DELETE）的真实 SQL 通道与多客户端并发稳定性：
//   - TCP 长连接复用：gsmClient 封装单条长连接，在其上顺序执行 DDL + DML +
//     SELECT 混合负载，覆盖真实客户端（连接池、JDBC）一次握手多次请求的场景
//   - 多客户端交错：6 客户端（TCP+HTTP 交替）并发执行 INSERT/UPDATE/SELECT/DELETE
//   - /write API 与 INSERT 语句结果一致性
//   - UPDATE/DELETE WHERE 过滤与命中行数（resp.Rows 字段）精确校验
//
// 配套文件 e2e_general_sql_multiclient_queries_test.go 覆盖第二组场景：
//   - 元命令（SHOW TABLES / DESCRIBE / EXPLAIN）
//   - 错误路径优雅恢复
//   - HTTP 短连接并发
//   - 列投影、AS 别名、算术表达式
//   - GROUP BY 聚合
//   - WHERE AND/OR 复合过滤
//   - TCP packet 协议层 round-trip
//
// 测试设计原则：
//   - 复用 e2e_server_sql_test.go 中的 sqlServer / startSQLServer / queryVia /
//     writeVia / respRows / toInt64 等公共 helper，避免重复实现
//   - 每个测试 t.Parallel 并发执行，缩短集成测试套件总时长
//   - 不覆盖 ORDER BY DESC / DISTINCT / HAVING 语义：当前 parser 静默丢弃
//     这些子句（见 .agent_plan/query.md），相关特性将由后续 PR 单独修复；
//     错误语法的负路径（IN/BETWEEN/IS NULL）反而被显式断言为「优雅报错」
//
// 本文件作为后续多客户端场景测试的范式，所有测试并行运行以缩短集成测试套件总时长。
package integration

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/server"
)

// gsm 常量：本文件与配套 e2e_general_sql_multiclient_queries_test.go 共享。
const (
	gsmTable         = "gsm_orders" // 通用 SQL 共享表名
	gsmClientCount   = 6            // 并发客户端数
	gsmRowsPerClient = 12           // 每客户端写入行数
	gsmClientBaseID  = 30000        // 客户端写入 ID 起始偏移，避免与 sensor 表冲突
	gsmIterations    = 5            // 每客户端工作负载迭代轮数
)

// gsmColumnNames 定义 gsmTable 的列名集合，集中维护以减少字符串硬编码。
var gsmColumnNames = []string{"id", "product", "qty", "amount", "active"}

// gsmInitialRows 返回 gsmTable 的初始数据（每个产品类别各 2 条）。
//
// active 字段分布：4 true / 1 false，便于 DeleteWhereSemantics 测试断言
// 「DELETE WHERE active=false 命中 1 行」的具体数值；UpdateWhereSemantics
// 测试单独调整期望值覆盖该场景。
func gsmInitialRows() []map[string]any {
	return []map[string]any{
		{"id": 1, "product": "alpha", "qty": 10, "amount": 99.5, "active": true},
		{"id": 2, "product": "beta", "qty": 5, "amount": 49.75, "active": true},
		{"id": 3, "product": "alpha", "qty": 20, "amount": 200.0, "active": false},
		{"id": 4, "product": "gamma", "qty": 15, "amount": 150.0, "active": true},
		{"id": 5, "product": "beta", "qty": 3, "amount": 30.0, "active": true},
	}
}

// gsmDeleteRows 返回 DELETE 测试专用的初始数据：2 行 active=false，便于断言
// 「DELETE WHERE active=false 命中 2 行」的精确数值。
func gsmDeleteRows() []map[string]any {
	return []map[string]any{
		{"id": 1, "product": "alpha", "qty": 10, "amount": 99.5, "active": true},
		{"id": 2, "product": "beta", "qty": 5, "amount": 49.75, "active": false},
		{"id": 3, "product": "alpha", "qty": 20, "amount": 200.0, "active": false},
		{"id": 4, "product": "gamma", "qty": 15, "amount": 150.0, "active": true},
		{"id": 5, "product": "beta", "qty": 3, "amount": 30.0, "active": true},
	}
}

// gsmClient 封装多客户端工作负载的协议客户端。
//
// TCP 客户端复用单条长连接：与真实应用（连接池、JDBC）一致，避免每请求
// 建/断连引入的握手延迟掩盖真实 server 行为。HTTP 模式复用全局连接池。
type gsmClient struct {
	via     string
	srv     *sqlServer
	tcp     *tcpClient
	closeFn func()
}

// newGSMClient 按协议创建客户端并建立长连接（如适用）。
func newGSMClient(s *sqlServer, via string) (*gsmClient, error) {
	c := &gsmClient{via: via, srv: s}
	if via != "tcp" {
		return c, nil
	}
	tc, err := dialTCP(s.tcpAddr)
	if err != nil {
		return nil, err
	}
	c.tcp = tc
	c.closeFn = tc.close
	return c, nil
}

// close 关闭底层长连接。
func (c *gsmClient) close() {
	if c.closeFn != nil {
		c.closeFn()
	}
}

// query 按协议执行 SQL 查询。
func (c *gsmClient) query(sql string) (*server.Response, error) {
	if c.tcp != nil {
		return c.tcp.query(sql)
	}
	return httpQuery(c.srv.httpAddr, sql)
}

// gsmSetupTableWith 创建 gsmTable 并按指定初始数据写入。
//
// 当测试需要不同的初始数据集（如 DELETE 测试需要多行 active=false），
// 调用方应使用本函数替代 gsmSetupTable，以避免污染其他测试。
func gsmSetupTableWith(t *testing.T, s *sqlServer, rows []map[string]any) {
	t.Helper()
	if err := s.srv.Catalog().CreateTable(gsmTable, []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "product", Type: common.TypeString, Nullable: true},
		{Name: "qty", Type: common.TypeInt64, Nullable: true},
		{Name: "amount", Type: common.TypeFloat64, Nullable: true},
		{Name: "active", Type: common.TypeBool, Nullable: true},
	}, []string{"id"}, catalog.TableOptions{}); err != nil {
		t.Fatalf("创建 %s 失败: %v", gsmTable, err)
	}
	writeVia(t, s, "tcp", gsmTable, rows)
}

// gsmSetupTable 创建 gsmTable 并写入默认初始数据。
func gsmSetupTable(t *testing.T, s *sqlServer) {
	gsmSetupTableWith(t, s, gsmInitialRows())
}

// gsmRunOneIteration 是单个客户端的一轮工作负载。
//
// 每个客户端在自己的 ID 区间内执行 INSERT（SQL）→ UPDATE → DELETE（部分行）
// → SELECT 验证，保证数据一致性与 server 端排序无关（按主键固定区间）。
// 返回 nil 表示成功，错误带详细上下文。
//
// 为控制函数长度（CI 限制 80 行），5 步拆分为独立 helper：
//   - gsmIterInsert / gsmIterUpdate / gsmIterSelectAfterUpdate
//   - gsmIterDelete / gsmIterSelectAfterDelete
func gsmRunOneIteration(c *gsmClient, iter, clientID int) error {
	startID := gsmClientBaseID + clientID*gsmRowsPerClient*gsmIterations + iter*gsmRowsPerClient
	if err := gsmIterInsert(c, startID, clientID, iter); err != nil {
		return err
	}
	if err := gsmIterUpdate(c, startID); err != nil {
		return err
	}
	if err := gsmIterSelectAfterUpdate(c, startID); err != nil {
		return err
	}
	if err := gsmIterDelete(c, startID); err != nil {
		return err
	}
	return gsmIterSelectAfterDelete(c, startID)
}

// gsmIterInsert 阶段 1：插入 3 行（id=startID..startID+2）覆盖每轮工作负载起点。
func gsmIterInsert(c *gsmClient, startID, clientID, iter int) error {
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (id, product, qty, amount, active) VALUES "+
			"(%d, 'c%d-i%d', %d, %.2f, true), "+
			"(%d, 'c%d-i%d', %d, %.2f, false), "+
			"(%d, 'c%d-i%d', %d, %.2f, true)",
		gsmTable,
		startID, clientID, iter, startID%100, float64(startID)*1.1,
		startID+1, clientID, iter, (startID+1)%100, float64(startID+1)*1.1,
		startID+2, clientID, iter, (startID+2)%100, float64(startID+2)*1.1,
	)
	return gsmExecSQL(c, "INSERT", insertSQL)
}

// gsmIterUpdate 阶段 2：UPDATE id=startID+1 的 qty*=10、amount*=2。
func gsmIterUpdate(c *gsmClient, startID int) error {
	updateSQL := fmt.Sprintf(
		"UPDATE %s SET qty = qty * 10, amount = amount * 2 WHERE id = %d",
		gsmTable, startID+1,
	)
	return gsmExecSQL(c, "UPDATE", updateSQL)
}

// gsmIterSelectAfterUpdate 阶段 3：校验 id=startID+1 的 qty 已 *= 10。
func gsmIterSelectAfterUpdate(c *gsmClient, startID int) error {
	selectSQL := fmt.Sprintf(
		"SELECT qty, amount FROM %s WHERE id = %d", gsmTable, startID+1,
	)
	resp, err := c.query(selectSQL)
	if err != nil {
		return fmt.Errorf("SELECT after UPDATE: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("SELECT after UPDATE: code=%d msg=%s", resp.Code, resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		return fmt.Errorf("SELECT after UPDATE: 期望 1 行，得到 %d", len(rows))
	}
	wantQty := float64((startID + 1) % 100 * 10)
	gotQty, ok := toFloat64(rows[0]["qty"])
	if !ok {
		return fmt.Errorf("SELECT after UPDATE: qty 类型异常 %T", rows[0]["qty"])
	}
	if gotQty != wantQty {
		return fmt.Errorf("SELECT after UPDATE: id=%d qty 期望 %v，得到 %v",
			startID+1, wantQty, gotQty)
	}
	return nil
}

// gsmIterDelete 阶段 4：DELETE id=startID 的行。
func gsmIterDelete(c *gsmClient, startID int) error {
	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE id = %d", gsmTable, startID,
	)
	return gsmExecSQL(c, "DELETE", deleteSQL)
}

// gsmIterSelectAfterDelete 阶段 5：校验 id=[startID, startID+2] 范围剩 2 行。
func gsmIterSelectAfterDelete(c *gsmClient, startID int) error {
	checkSQL := fmt.Sprintf(
		"SELECT id FROM %s WHERE id >= %d AND id <= %d",
		gsmTable, startID, startID+2,
	)
	resp, err := c.query(checkSQL)
	if err != nil {
		return fmt.Errorf("SELECT after DELETE: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("SELECT after DELETE: code=%d msg=%s", resp.Code, resp.Message)
	}
	gotRows := respRows(resp)
	if len(gotRows) != 2 {
		return fmt.Errorf("SELECT after DELETE: 期望 2 行，得到 %d", len(gotRows))
	}
	return nil
}

// gsmExecSQL 通用 SQL 执行：执行后断言响应码为 0，错误带操作名上下文。
// 适用于「预期成功」的 DML（INSERT/UPDATE/DELETE）。
func gsmExecSQL(c *gsmClient, opName, sql string) error {
	resp, err := c.query(sql)
	if err != nil {
		return fmt.Errorf("%s: %w", opName, err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("%s: code=%d msg=%s", opName, resp.Code, resp.Message)
	}
	return nil
}

// gsmWorker 单个客户端的完整工作负载：每轮 INSERT/UPDATE/SELECT/DELETE。
// 失败时通过 error channel 报告首个错误，atomic 计数器汇总。
func gsmWorker(c *gsmClient, clientID int, failCount *int64) {
	defer c.close()
	for iter := 0; iter < gsmIterations; iter++ {
		if err := gsmRunOneIteration(c, iter, clientID); err != nil {
			atomic.AddInt64(failCount, 1)
			return
		}
	}
}

// TestGeneralSQLMultiClientPersistentConn 验证多客户端长连接上的通用 SQL 工作负载。
//
// 6 个客户端（3 TCP + 3 HTTP）并发执行 5 轮 INSERT/UPDATE/DELETE/SELECT 链路，
// 所有客户端共享同一张表，各自负责独立 ID 区间，互不干扰。最终断言：
//   - 失败客户端数为 0
//   - 表行数 = 初始 5 + 每客户端净写入（每轮 INSERT 3 - DELETE 1 = 2 行）* 5 轮 * 6 客户端 = 65 行
func TestGeneralSQLMultiClientPersistentConn(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	// 显式交错 TCP/HTTP 客户端，确保两种协议都被长连接场景覆盖
	vias := []string{"tcp", "http", "tcp", "http", "tcp", "http"}
	if len(vias) != gsmClientCount {
		t.Fatalf("协议分配数 %d 与客户端数 %d 不一致", len(vias), gsmClientCount)
	}

	var wg sync.WaitGroup
	var failCount int64
	for i := 0; i < gsmClientCount; i++ {
		wg.Add(1)
		go func(clientID int, via string) {
			defer wg.Done()
			c, err := newGSMClient(s, via)
			if err != nil {
				t.Logf("client %d (%s) 建连失败: %v", clientID, via, err)
				atomic.AddInt64(&failCount, 1)
				return
			}
			gsmWorker(c, clientID, &failCount)
		}(i, vias[i])
	}
	wg.Wait()

	if failCount > 0 {
		t.Fatalf("%d 个客户端工作负载失败", failCount)
	}

	// 校验总行数：初始 5 行 + 每客户端每轮净增 2 行 × 5 轮 × 6 客户端 = 65
	wantRows := int64(5 + gsmClientCount*gsmIterations*2)
	resp := queryVia(t, s, "tcp", fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", gsmTable))
	if resp.Code != 0 {
		t.Fatalf("COUNT 查询失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		t.Fatalf("COUNT 期望 1 行，得到 %d", len(rows))
	}
	got, ok := toInt64(rows[0]["cnt"])
	if !ok {
		t.Fatalf("COUNT 返回值类型异常 %T", rows[0]["cnt"])
	}
	if got != wantRows {
		t.Errorf("最终行数: 期望 %d，得到 %d", wantRows, got)
	}
}

// TestGeneralSQLWriteAPIConsistencyWithSQL 验证 /write API 与 INSERT 语句结果一致。
//
// 同一张表分两阶段：
//   - 阶段 A：经 TCP /write API 写入 3 行
//   - 阶段 B：经 HTTP INSERT INTO ... VALUES 写入 3 行
//
// 最终 SELECT * 验证两种通道均写入成功，行数 = 6。
func TestGeneralSQLWriteAPIConsistencyWithSQL(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	// 阶段 A：/write API
	writeRows := []map[string]any{
		{"id": 1001, "product": "via-write-1", "qty": 1, "amount": 1.0, "active": true},
		{"id": 1002, "product": "via-write-2", "qty": 2, "amount": 2.0, "active": true},
		{"id": 1003, "product": "via-write-3", "qty": 3, "amount": 3.0, "active": true},
	}
	writeVia(t, s, "tcp", gsmTable, writeRows)

	// 阶段 B：INSERT 语句
	insertSQL := "INSERT INTO " + gsmTable +
		" (id, product, qty, amount, active) VALUES " +
		"(2001, 'via-insert-1', 11, 11.0, true)," +
		"(2002, 'via-insert-2', 22, 22.0, false)," +
		"(2003, 'via-insert-3', 33, 33.0, true)"
	resp := queryVia(t, s, "http", insertSQL)
	if resp.Code != 0 {
		t.Fatalf("INSERT 失败: %s", resp.Message)
	}

	// 校验总数：初始 5 + 3 + 3 = 11
	resp = queryVia(t, s, "tcp", "SELECT COUNT(*) AS cnt FROM "+gsmTable)
	if resp.Code != 0 {
		t.Fatalf("COUNT 失败: %s", resp.Message)
	}
	cnt, ok := toInt64(respRows(resp)[0]["cnt"])
	if !ok || cnt != 11 {
		t.Fatalf("行数: 期望 11，得到 %d (类型=%T)", cnt, respRows(resp)[0]["cnt"])
	}

	// 校验每行的 product 字段以确认两条路径都生效
	resp = queryVia(t, s, "tcp", "SELECT id, product FROM "+gsmTable+" WHERE id >= 1001")
	if resp.Code != 0 {
		t.Fatalf("SELECT 失败: %s", resp.Message)
	}
	byID := make(map[int64]string)
	for _, row := range respRows(resp) {
		id, _ := toInt64(row["id"])
		byID[id] = fmt.Sprintf("%v", row["product"])
	}
	for _, exp := range []struct {
		id   int64
		prod string
	}{
		{1001, "via-write-1"}, {1002, "via-write-2"}, {1003, "via-write-3"},
		{2001, "via-insert-1"}, {2002, "via-insert-2"}, {2003, "via-insert-3"},
	} {
		got, ok := byID[exp.id]
		if !ok {
			t.Errorf("缺少 id=%d", exp.id)
			continue
		}
		if got != exp.prod {
			t.Errorf("id=%d product: 期望 %q，得到 %q", exp.id, exp.prod, got)
		}
	}
}

// TestGeneralSQLUpdateWhereSemantics 验证 UPDATE WHERE 的过滤语义。
//
// 数据：5 行（id 1..5），分别属于 alpha/alpha/gamma/beta/beta。
// 测试场景：
//   - UPDATE ... WHERE product = 'alpha' SET active = false → alpha 两行 active 变 false
//   - UPDATE ... WHERE id IN (1,2,3) → 当前 parser 不支持 IN，验证返回非零码
func TestGeneralSQLUpdateWhereSemantics(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	// 阶段 1：批量 UPDATE product='alpha'
	resp := queryVia(t, s, "tcp",
		"UPDATE "+gsmTable+" SET active = false WHERE product = 'alpha'")
	if resp.Code != 0 {
		t.Fatalf("UPDATE alpha 失败: %s", resp.Message)
	}

	// 阶段 2：校验：alpha 行 active=false，其他行 active 保持不变
	resp = queryVia(t, s, "tcp", "SELECT id, product, active FROM "+gsmTable+" ORDER BY id ASC")
	if resp.Code != 0 {
		t.Fatalf("SELECT 失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 5 {
		t.Fatalf("期望 5 行，得到 %d", len(rows))
	}
	// 注：当前 parser 不支持 ORDER BY ASC 排序保证，遍历时按主键匹配即可。
	byID := make(map[int64]bool)
	for _, row := range rows {
		id, _ := toInt64(row["id"])
		byID[id] = row["active"] == true
	}
	wantActive := map[int64]bool{
		1: false, // alpha
		2: true,  // beta
		3: false, // alpha
		4: true,  // gamma
		5: true,  // beta
	}
	for id, want := range wantActive {
		if byID[id] != want {
			t.Errorf("id=%d active: 期望 %v，得到 %v", id, want, byID[id])
		}
	}

	// 阶段 3：UPDATE 命中行数校验（响应 Rows 字段）
	resp = queryVia(t, s, "tcp",
		"UPDATE "+gsmTable+" SET qty = qty + 100 WHERE active = false")
	if resp.Code != 0 {
		t.Fatalf("UPDATE active=false 失败: %s", resp.Message)
	}
	// id=1 与 id=3 两行满足 active=false
	if resp.Rows != 2 {
		t.Errorf("UPDATE 命中行数: 期望 2，得到 %d", resp.Rows)
	}

	// 阶段 4：IN 语法当前不被 parser 支持，应返回非零码（不 panic、不挂起）
	resp = queryVia(t, s, "tcp",
		"SELECT * FROM "+gsmTable+" WHERE id IN (1, 2, 3)")
	if resp.Code == 0 {
		t.Error("IN 语法期望返回错误码，但收到成功响应")
	}
}

// TestGeneralSQLDeleteWhereSemantics 验证 DELETE WHERE 过滤与级联语义。
//
// 场景（gsmDeleteRows：id=2,3 为 active=false）：
//   - DELETE FROM gsm_orders WHERE active = false：删除 id=2, id=3 → 剩 3 行
//   - DELETE 命中行数校验：2
//   - 重复执行同一 DELETE：第二应命中 0 行
func TestGeneralSQLDeleteWhereSemantics(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTableWith(t, s, gsmDeleteRows())

	resp := queryVia(t, s, "tcp",
		"DELETE FROM "+gsmTable+" WHERE active = false")
	if resp.Code != 0 {
		t.Fatalf("DELETE 失败: %s", resp.Message)
	}
	if resp.Rows != 2 {
		t.Errorf("DELETE 命中行数: 期望 2，得到 %d", resp.Rows)
	}

	// 校验：剩 3 行（id=2,4,5）
	resp = queryVia(t, s, "tcp", "SELECT COUNT(*) AS cnt FROM "+gsmTable)
	if resp.Code != 0 {
		t.Fatalf("COUNT 失败: %s", resp.Message)
	}
	cnt, _ := toInt64(respRows(resp)[0]["cnt"])
	if cnt != 3 {
		t.Errorf("DELETE 后行数: 期望 3，得到 %d", cnt)
	}

	// 重复 DELETE：命中行数应为 0
	resp = queryVia(t, s, "tcp",
		"DELETE FROM "+gsmTable+" WHERE active = false")
	if resp.Code != 0 {
		t.Fatalf("重复 DELETE 失败: %s", resp.Message)
	}
	if resp.Rows != 0 {
		t.Errorf("重复 DELETE 命中行数: 期望 0，得到 %d", resp.Rows)
	}

	// DELETE 不存在的条件：仍返回成功（0 行命中）
	resp = queryVia(t, s, "tcp",
		"DELETE FROM "+gsmTable+" WHERE id = 99999")
	if resp.Code != 0 {
		t.Errorf("DELETE 不存在条件期望成功，得到 code=%d msg=%s", resp.Code, resp.Message)
	}
	if resp.Rows != 0 {
		t.Errorf("DELETE 不存在条件命中行数: 期望 0，得到 %d", resp.Rows)
	}
}
