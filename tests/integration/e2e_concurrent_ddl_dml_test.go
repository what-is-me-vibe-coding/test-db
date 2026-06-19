// Package integration 端到端集成测试：并发 DDL + DML 混合工作负载。
//
// 补充既有集成测试未覆盖的「并发 DDL+DML」场景。既有所有多客户端测试均由
// 单个 setup 客户端预先建表后再并发执行 DML，未验证 DDL（CREATE/DROP TABLE）
// 与 DML（INSERT/SELECT/UPDATE/DELETE）并发交错时的正确性。本文件覆盖：
//   - 启动一个 server，预创建一张共享 LSM 表并写入初始数据
//   - DDL 客户端：多轮 CREATE TABLE → INSERT → SELECT → DROP TABLE，操作各自独立的临时表
//   - 写入客户端：向共享表写入唯一 ID 区间的行并读回校验
//   - 混合客户端：在共享表上执行 INSERT → UPDATE → DELETE 全 DML 链路
//   - 聚合客户端：并发执行 COUNT/SUM/AVG/MIN/MAX/GROUP BY，仅校验查询成功
//   - 最终校验：共享表行数、临时表已全部清理、UPDATE 结果生效、聚合结果一致性
//
// 直接验证「一个 server + 多个 client + 一般 SQL + DDL/DML 并发」的稳定性，
// 可捕获 catalog 锁、表引擎注销、并发扫描等路径的潜在缺陷。
package integration

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/server"
)

// cdm 角色枚举。
type cdmRole int

const (
	cdmRoleDDL    cdmRole = 0 // DDL 客户端：建/删独立临时表
	cdmRoleWriter cdmRole = 1 // 写入客户端：向共享表写入唯一 ID 区间
	cdmRoleMixed  cdmRole = 2 // 混合 DML 客户端：INSERT→UPDATE→DELETE
	cdmRoleAgg    cdmRole = 3 // 聚合客户端：并发聚合查询
)

// cdm 常量：并发 DDL+DML 工作负载参数。
const (
	cdmTotalClients   = 10 // 并发客户端总数
	cdmDDLClientCount = 3  // DDL 客户端数
	cdmWriterCount    = 3  // 写入客户端数
	cdmMixedCount     = 2  // 混合 DML 客户端数
	cdmAggCount       = 2  // 聚合客户端数
	cdmAggIterations  = 4  // 聚合客户端迭代次数
	cdmDDLRounds      = 3  // DDL 客户端建表/删表轮数
	cdmRowsPerWriter  = 10 // 每个写入客户端写入行数
	cdmRowsPerMixed   = 8  // 每个混合客户端初始写入行数
	cdmWriterBaseID   = 10000
	cdmMixedBaseID    = 20000
	cdmSharedTable    = "shared_evt"
	cdmUpdatedAmount  = 999.0 // 混合客户端 UPDATE 后 amount 的期望值
	cdmFloatEpsilon   = 1e-9  // 浮点数比较容差，避免 == 直接判等引入不稳定
)

// cdmRoleSpec 描述单个客户端的角色与协议。
type cdmRoleSpec struct {
	role cdmRole
	via  string // "tcp" 或 "http"
}

// cdmRoles 显式分配每个客户端的角色与协议。
//
// 使用显式数组而非取模分配，确保每种角色均被 TCP 与 HTTP 协议覆盖，
// 避免取模导致的角色与协议覆盖不均衡。cdmAssertRoleCounts 会校验分布一致性。
var cdmRoles = []cdmRoleSpec{
	{role: cdmRoleDDL, via: "http"},    // client 0
	{role: cdmRoleWriter, via: "tcp"},  // client 1
	{role: cdmRoleMixed, via: "tcp"},   // client 2
	{role: cdmRoleAgg, via: "http"},    // client 3
	{role: cdmRoleDDL, via: "tcp"},     // client 4
	{role: cdmRoleWriter, via: "http"}, // client 5
	{role: cdmRoleMixed, via: "http"},  // client 6
	{role: cdmRoleAgg, via: "tcp"},     // client 7
	{role: cdmRoleDDL, via: "tcp"},     // client 8
	{role: cdmRoleWriter, via: "tcp"},  // client 9
}

// cdmSharedRows 返回共享表的初始数据。
func cdmSharedRows() []map[string]any {
	return []map[string]any{
		{"id": 1, "category": "alpha", "amount": 10.5, "active": true},
		{"id": 2, "category": "beta", "amount": 20.0, "active": false},
		{"id": 3, "category": "alpha", "amount": 15.0, "active": true},
		{"id": 4, "category": "gamma", "amount": 30.0, "active": true},
	}
}

// cdmSetupSharedTable 创建共享 LSM 表并写入初始数据。
func cdmSetupSharedTable(t *testing.T, s *sqlServer) {
	t.Helper()
	err := s.srv.Catalog().CreateTable(cdmSharedTable, []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "category", Type: common.TypeString, Nullable: true},
		{Name: "amount", Type: common.TypeFloat64, Nullable: true},
		{Name: "active", Type: common.TypeBool, Nullable: true},
	}, []string{"id"}, catalog.TableOptions{})
	if err != nil {
		t.Fatalf("创建共享表失败: %v", err)
	}
	writeVia(t, s, "tcp", cdmSharedTable, cdmSharedRows())
}

// cdmClient 封装并发客户端使用的协议连接。
//
// TCP 模式复用单条长连接，避免每请求新建/断开连接造成的短连接风暴；
// HTTP 模式复用全局 sqlHTTPClient 的连接池。每个 goroutine 创建一个客户端，
// 在整个工作负载期间复用，结束后通过 close 释放。
type cdmClient struct {
	srv *sqlServer
	tcp *tcpClient // 非 nil 时使用长连接 TCP
}

// newCDMClient 按协议创建客户端。TCP 客户端建立一条长连接供整个工作负载复用。
func newCDMClient(s *sqlServer, via string) (*cdmClient, error) {
	c := &cdmClient{srv: s}
	if via == "http" {
		return c, nil
	}
	tc, err := dialTCP(s.tcpAddr)
	if err != nil {
		return nil, err
	}
	c.tcp = tc
	return c, nil
}

// close 释放底层连接（仅 TCP 长连接需要显式关闭）。
func (c *cdmClient) close() {
	if c.tcp != nil {
		c.tcp.close()
	}
}

// query 按客户端协议执行查询。
func (c *cdmClient) query(sql string) (*server.Response, error) {
	if c.tcp != nil {
		return c.tcp.query(sql)
	}
	return httpQuery(c.srv.httpAddr, sql)
}

// write 按客户端协议写入数据。
func (c *cdmClient) write(table string, rows []map[string]any) (*server.Response, error) {
	if c.tcp != nil {
		return c.tcp.write(table, rows)
	}
	return httpWrite(c.srv.httpAddr, table, rows)
}

// checkResp 统一校验响应：resp 为 nil、err 非 nil 或 resp.Code != 0 时返回带 name 的错误，
// 减少各工作负载中重复的错误格式化代码。
func checkResp(name string, resp *server.Response, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if resp == nil {
		return fmt.Errorf("%s: nil response", name)
	}
	if resp.Code != 0 {
		return fmt.Errorf("%s: code=%d msg=%s", name, resp.Code, resp.Message)
	}
	return nil
}

// cdmAssertRoleCounts 校验 cdmRoles 的角色分布与常量定义一致，
// 防止显式数组与计数常量不同步（即原 review 指出的「角色分配与常量定义不匹配」）。
func cdmAssertRoleCounts(t *testing.T) {
	t.Helper()
	got := map[cdmRole]int{}
	for _, sp := range cdmRoles {
		got[sp.role]++
	}
	want := map[cdmRole]int{
		cdmRoleDDL:    cdmDDLClientCount,
		cdmRoleWriter: cdmWriterCount,
		cdmRoleMixed:  cdmMixedCount,
		cdmRoleAgg:    cdmAggCount,
	}
	for r, w := range want {
		if got[r] != w {
			t.Fatalf("角色 %d 数量: 期望 %d 得到 %d", r, w, got[r])
		}
	}
	if len(cdmRoles) != cdmTotalClients {
		t.Fatalf("客户端总数: 期望 %d 得到 %d", cdmTotalClients, len(cdmRoles))
	}
}

// cdmMixedBaseIDAt 返回第 mixedIdx（从 0 开始）个混合客户端的 base 行 ID。
//
// 集中此计算可保证 cdmMixedClientWork 写入的 base ID 与 cdmMixedBaseIDs 校验时
// 取出的 base ID 完全一致，避免「分配与校验漂移」。
func cdmMixedBaseIDAt(mixedIdx int) int {
	return cdmMixedBaseID + mixedIdx*cdmRowsPerMixed
}

// cdmMixedBaseIDs 返回所有混合客户端的 base 行 ID，用于最终校验 UPDATE 结果。
//
// 使用「在 cdmRoles 中遇到 cdmRoleMixed 的次序」作为乘数（mixedIdx * cdmRowsPerMixed），
// 而非全局下标 i。这样即使调整 cdmRoles 顺序，混合客户端的 base ID 分配依然稳定。
func cdmMixedBaseIDs() []int {
	var ids []int
	mixedIdx := 0
	for _, sp := range cdmRoles {
		if sp.role != cdmRoleMixed {
			continue
		}
		ids = append(ids, cdmMixedBaseIDAt(mixedIdx))
		mixedIdx++
	}
	return ids
}

// cdmDDLClientWork 执行 DDL 客户端工作负载。
//
// 每轮创建一张独立的临时表，写入若干行、查询校验、再删除，并验证删除后查询失败。
// 所有操作仅作用于以 clientID 命名的独立表，不与其他客户端冲突。
func cdmDDLClientWork(c *cdmClient, clientID int) error {
	for round := 0; round < cdmDDLRounds; round++ {
		table := fmt.Sprintf("tmp_ddl_%d_%d", clientID, round)
		ddl := fmt.Sprintf("CREATE TABLE %s (id INT64 NOT NULL, "+
			"val STRING NULL, PRIMARY KEY(id))", table)
		resp, err := c.query(ddl)
		if err := checkResp(fmt.Sprintf("建表 %s", table), resp, err); err != nil {
			return err
		}
		rows := []map[string]any{
			{"id": round*10 + 1, "val": fmt.Sprintf("v-%d-%d", clientID, round)},
			{"id": round*10 + 2, "val": fmt.Sprintf("v-%d-%d", clientID, round)},
		}
		if err := cdmWriteAndVerify(c, table, rows); err != nil {
			return err
		}
		if err := cdmDropAndVerify(c, table); err != nil {
			return err
		}
	}
	return nil
}

// cdmWriteAndVerify 写入行并校验 COUNT 与点查结果。
func cdmWriteAndVerify(c *cdmClient, table string, rows []map[string]any) error {
	wresp, err := c.write(table, rows)
	if err := checkResp(fmt.Sprintf("写入 %s", table), wresp, err); err != nil {
		return err
	}
	cresp, err := c.query(fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", table))
	if err := checkResp(fmt.Sprintf("COUNT %s", table), cresp, err); err != nil {
		return err
	}
	rs := respRows(cresp)
	if len(rs) == 0 {
		return fmt.Errorf("COUNT %s: 响应无数据行", table)
	}
	cnt, _ := toInt64(rs[0]["cnt"])
	if cnt != int64(len(rows)) {
		return fmt.Errorf("COUNT %s: 期望 %d 得到 %d", table, len(rows), cnt)
	}
	return nil
}

// cdmDropAndVerify 删除表并验证删除后查询返回「表不存在」错误。
//
// 区分错误类型：网络层错误（连接断开、server 崩溃）不得视为「表已不可用」，
// 必须返回失败以暴露真实系统故障；仅当 server 返回非零 Code（表不存在）时才视为通过。
func cdmDropAndVerify(c *cdmClient, table string) error {
	dresp, err := c.query("DROP TABLE " + table)
	if err := checkResp(fmt.Sprintf("DROP %s", table), dresp, err); err != nil {
		return err
	}
	qresp, err := c.query(fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", table))
	if err != nil {
		return fmt.Errorf("DROP %s 后查询网络错误（不应视为表已删除）: %w", table, err)
	}
	if qresp.Code == 0 {
		return fmt.Errorf("DROP %s 后查询仍成功", table)
	}
	return nil // resp.Code != 0 表示表不存在，符合预期
}

// cdmWriterClientWork 执行写入客户端工作负载：向共享表写入唯一 ID 区间的行并读回。
func cdmWriterClientWork(c *cdmClient, clientID int) error {
	base := cdmWriterBaseID + clientID*cdmRowsPerWriter
	rows := make([]map[string]any, cdmRowsPerWriter)
	for i := 0; i < cdmRowsPerWriter; i++ {
		id := base + i
		rows[i] = map[string]any{
			"id":       id,
			"category": fmt.Sprintf("w-%d", clientID),
			"amount":   float64(id) * 0.5,
			"active":   i%2 == 0,
		}
	}
	wresp, err := c.write(cdmSharedTable, rows)
	if err := checkResp("写入共享表", wresp, err); err != nil {
		return err
	}
	qresp, err := c.query(fmt.Sprintf("SELECT * FROM %s WHERE id >= %d AND id < %d",
		cdmSharedTable, base, base+cdmRowsPerWriter))
	if err := checkResp("读回写入区间", qresp, err); err != nil {
		return err
	}
	if got := len(respRows(qresp)); got != cdmRowsPerWriter {
		return fmt.Errorf("写入区间行数: 期望 %d 得到 %d", cdmRowsPerWriter, got)
	}
	return nil
}

// cdmMixedClientWork 执行混合 DML 客户端工作负载：
// 写入 → UPDATE 一行 → DELETE 一行 → 读回校验剩余行数。
//
// mixedIdx 是该客户端在 cdmRoles 中「混合角色序号」（从 0 开始），
// 而非全局 clientID。base ID 通过 cdmMixedBaseIDAt(mixedIdx) 计算，
// 与 cdmMixedBaseIDs 校验时使用的公式保持一致，避免分配与校验漂移。
//
// UPDATE 将区间首行 amount 改为 cdmUpdatedAmount，最终校验阶段会验证该值生效。
func cdmMixedClientWork(c *cdmClient, mixedIdx int) error {
	base := cdmMixedBaseIDAt(mixedIdx)
	rows := make([]map[string]any, cdmRowsPerMixed)
	for i := 0; i < cdmRowsPerMixed; i++ {
		id := base + i
		rows[i] = map[string]any{
			"id":       id,
			"category": fmt.Sprintf("m-%d", mixedIdx),
			"amount":   float64(id),
			"active":   true,
		}
	}
	wresp, err := c.write(cdmSharedTable, rows)
	if err := checkResp("混合写入", wresp, err); err != nil {
		return err
	}
	uresp, err := c.query(fmt.Sprintf("UPDATE %s SET amount = %d WHERE id = %d",
		cdmSharedTable, int(cdmUpdatedAmount), base))
	if err := checkResp(fmt.Sprintf("UPDATE id=%d", base), uresp, err); err != nil {
		return err
	}
	if uresp.Rows != 1 {
		return fmt.Errorf("UPDATE id=%d: rows=%d 期望 1", base, uresp.Rows)
	}
	dresp, err := c.query(fmt.Sprintf("DELETE FROM %s WHERE id = %d",
		cdmSharedTable, base+1))
	if err := checkResp(fmt.Sprintf("DELETE id=%d", base+1), dresp, err); err != nil {
		return err
	}
	if dresp.Rows != 1 {
		return fmt.Errorf("DELETE id=%d: rows=%d 期望 1", base+1, dresp.Rows)
	}
	want := cdmRowsPerMixed - 1
	qresp, err := c.query(fmt.Sprintf("SELECT * FROM %s WHERE id >= %d AND id < %d",
		cdmSharedTable, base, base+cdmRowsPerMixed))
	if err := checkResp("混合读回", qresp, err); err != nil {
		return err
	}
	if got := len(respRows(qresp)); got != want {
		return fmt.Errorf("混合区间剩余行数: 期望 %d 得到 %d", want, got)
	}
	return nil
}

// cdmAggregateClientWork 执行聚合客户端工作负载：
// 多轮运行 COUNT/SUM/AVG/MIN/MAX/GROUP BY，仅校验查询成功（不校验精确值，
// 因并发写入下计数会变化；精确校验在最终验证阶段进行）。
func cdmAggregateClientWork(c *cdmClient, _ int) error {
	queries := []string{
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", cdmSharedTable),
		fmt.Sprintf("SELECT category, COUNT(*) AS cnt, SUM(amount) AS s, "+
			"AVG(amount) AS a, MIN(amount) AS mn, MAX(amount) AS mx "+
			"FROM %s GROUP BY category", cdmSharedTable),
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s WHERE active = true",
			cdmSharedTable),
		fmt.Sprintf("SELECT id, amount FROM %s WHERE amount > 0 LIMIT 5",
			cdmSharedTable),
	}
	for i := 0; i < cdmAggIterations; i++ {
		for _, sql := range queries {
			resp, err := c.query(sql)
			if err := checkResp("聚合查询 ["+sql+"]", resp, err); err != nil {
				return err
			}
		}
	}
	return nil
}

// cdmRunClient 按角色分发客户端工作负载。
//
// 对混合角色单独传入 mixedIdx（混合序号）以保证写入与校验的 base ID 计算一致。
func cdmRunClient(c *cdmClient, clientID int, role cdmRole, mixedIdx int) error {
	switch role {
	case cdmRoleDDL:
		return cdmDDLClientWork(c, clientID)
	case cdmRoleWriter:
		return cdmWriterClientWork(c, clientID)
	case cdmRoleMixed:
		return cdmMixedClientWork(c, mixedIdx)
	default:
		return cdmAggregateClientWork(c, clientID)
	}
}

// TestConcurrentDDLAndDML 验证一个 server 下多客户端并发执行
// DDL + DML 混合工作负载的正确性。
//
// 10 个客户端按 cdmRoles 显式分配角色与协议（3 DDL / 3 写入 / 2 混合 / 2 聚合），
// TCP 客户端复用长连接，HTTP 客户端复用连接池。所有客户端错误经互斥锁保护的
// 切片完整收集（而非仅保留最后一个），最终校验共享表行数、临时表清理、
// UPDATE 结果生效与聚合一致性。
func TestConcurrentDDLAndDML(t *testing.T) {
	cdmAssertRoleCounts(t)
	s := startSQLServer(t)
	cdmSetupSharedTable(t, s)

	var mu sync.Mutex
	errs := make([]string, 0, cdmTotalClients)
	recordErr := func(clientID int, sp cdmRoleSpec, err error) {
		mu.Lock()
		errs = append(errs, fmt.Sprintf("client %d (%s, role %d): %v",
			clientID, sp.via, sp.role, err))
		mu.Unlock()
	}

	var wg sync.WaitGroup
	mixedIdx := 0 // 累计到当前位置为止的混合角色客户端数
	for i, spec := range cdmRoles {
		wg.Add(1)
		// 捕获循环变量与当前 mixedIdx，避免闭包共享
		clientID, sp, mi := i, spec, mixedIdx
		if sp.role == cdmRoleMixed {
			mixedIdx++
		}
		go func() {
			defer wg.Done()
			c, err := newCDMClient(s, sp.via)
			if err != nil {
				recordErr(clientID, sp, fmt.Errorf("建连: %w", err))
				return
			}
			defer c.close()
			if err := cdmRunClient(c, clientID, sp.role, mi); err != nil {
				recordErr(clientID, sp, err)
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("%d 个客户端失败:\n%s", len(errs), strings.Join(errs, "\n"))
	}
	cdmVerifyFinalState(t, s)
}

// cdmVerifyFinalState 校验全部客户端完成后的最终状态。
//
// 校验项：
//   - 共享表行数 = 初始 4 行 + 写入客户端 cdmWriterCount*cdmRowsPerWriter
//   - 混合客户端 cdmMixedCount*(cdmRowsPerMixed-1)
//   - 所有 DDL 临时表已删除（catalog 快照中无 tmp_ddl_* 表）
//   - 混合客户端 UPDATE 生效：每个 base 行 amount 为 cdmUpdatedAmount
//   - 聚合查询结果与实际行数一致（GROUP BY 分组数 >= 3，SUM 非空）
func cdmVerifyFinalState(t *testing.T, s *sqlServer) {
	t.Helper()
	wantShared := int64(len(cdmSharedRows()) +
		cdmWriterCount*cdmRowsPerWriter +
		cdmMixedCount*(cdmRowsPerMixed-1))
	resp := queryVia(t, s, "tcp",
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", cdmSharedTable))
	if resp.Code != 0 {
		t.Fatalf("最终 COUNT 失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) == 0 {
		t.Fatalf("最终 COUNT: 响应无数据行")
	}
	got, _ := toInt64(rows[0]["cnt"])
	if got != wantShared {
		t.Errorf("共享表行数: 期望 %d 得到 %d", wantShared, got)
	}
	cdmVerifyTmpTablesDropped(t, s)
	cdmVerifyUpdates(t, s)
	cdmVerifyAggregates(t, s)
}

// cdmVerifyTmpTablesDropped 校验所有 DDL 临时表已从 catalog 清理。
//
// 通过 SHOW TABLES SQL 走 server 端而非直接读取 catalog snapshot，更端到端
// （验证的是 server 暴露的元数据视图，而非内部数据结构），且解除了测试对
// pkg/catalog 内部 API 的依赖。
func cdmVerifyTmpTablesDropped(t *testing.T, s *sqlServer) {
	t.Helper()
	resp := queryVia(t, s, "tcp", "SHOW TABLES")
	if resp.Code != 0 {
		t.Fatalf("SHOW TABLES 失败: %s", resp.Message)
	}
	for _, row := range respRows(resp) {
		// SHOW TABLES 返回的列名是 "table"（见 server.handleShowTables）。
		name, _ := row["table"].(string)
		if strings.HasPrefix(name, "tmp_ddl_") {
			t.Errorf("临时表未删除: %s", name)
		}
	}
}

// cdmVerifyUpdates 校验混合客户端 UPDATE 结果生效：每个 base 行 amount 应为
// cdmUpdatedAmount。原实现仅校验行数而忽略 UPDATE，可能掩盖 UPDATE 未生效的缺陷。
func cdmVerifyUpdates(t *testing.T, s *sqlServer) {
	t.Helper()
	for _, bid := range cdmMixedBaseIDs() {
		uresp := queryVia(t, s, "tcp",
			fmt.Sprintf("SELECT amount FROM %s WHERE id = %d", cdmSharedTable, bid))
		if uresp.Code != 0 {
			t.Errorf("UPDATE 校验 id=%d 查询失败: %s", bid, uresp.Message)
			continue
		}
		row := firstRow(uresp)
		if row == nil {
			t.Errorf("UPDATE 校验 id=%d: 行不存在", bid)
			continue
		}
		amt, _ := toFloat64(row["amount"])
		if math.Abs(amt-cdmUpdatedAmount) >= cdmFloatEpsilon {
			t.Errorf("UPDATE 校验 id=%d: amount 期望 %v 得到 %v",
				bid, cdmUpdatedAmount, row["amount"])
		}
	}
}

// cdmVerifyAggregates 校验最终聚合结果：GROUP BY 分组数与 SUM 非空。
func cdmVerifyAggregates(t *testing.T, s *sqlServer) {
	t.Helper()
	gresp := queryVia(t, s, "tcp",
		fmt.Sprintf("SELECT category, COUNT(*) AS cnt FROM %s "+
			"GROUP BY category", cdmSharedTable))
	if gresp.Code != 0 {
		t.Fatalf("最终 GROUP BY 失败: %s", gresp.Message)
	}
	if got := len(respRows(gresp)); got < 3 {
		t.Errorf("GROUP BY 分组数: 期望 >=3 得到 %d", got)
	}
	sresp := queryVia(t, s, "tcp",
		fmt.Sprintf("SELECT SUM(amount) AS s FROM %s", cdmSharedTable))
	if sresp.Code != 0 {
		t.Fatalf("最终 SUM 失败: %s", sresp.Message)
	}
	srows := respRows(sresp)
	if len(srows) == 0 {
		t.Fatalf("最终 SUM: 响应无数据行")
	}
	if srows[0]["s"] == nil {
		t.Errorf("SUM(amount): 期望非 nil，得到 nil")
	}
}

// firstRow 返回响应的首行，无行时返回 nil。
func firstRow(resp *server.Response) map[string]any {
	rows := respRows(resp)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}
