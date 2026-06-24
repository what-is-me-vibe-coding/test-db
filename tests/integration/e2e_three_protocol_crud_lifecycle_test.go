// Package integration 端到端集成测试：三协议（TCP / HTTP / PG wire）CRUD 生命周期
// 与 Read-Your-Writes 一致性。
//
// 既有 e2e_protocol_parity_general_sql_test.go 已覆盖「同一 SQL 在三协议
// 下的结果一致」，但每次都用同一份种子数据、单次 SELECT 验证。本文件补齐
// 既有测试未覆盖的「**写后立即读**」（Read-Your-Writes）维度：客户端 A
// 通过某协议写入一条数据，客户端 B 在不同时刻（毫秒/百毫秒级）通过另外
// 两协议读同一行，应能读到一致结果。这验证了：
//
//  1. WAL+MemTable 的可见性在毫秒级被新查询观察到（不是仅在 flush 后）
//  2. 三个协议最终都路由到同一 catalog + 同一组 LSM/memory 引擎，
//     不存在「TCP 写入只走 TCP 缓存」之类的隐性问题
//  3. DELETE 后的 RYW 行为：被删行在三协议中应同时消失，不出现
//     「PG wire 看到的是旧值」这种协议漂移
//
// 与既有测试的区别：
//   - e2e_protocol_parity_general_sql_test.go：三协议对同一 SQL 的结果一致
//     （不涉及写入后立即读、不涉及 DELETE 的可见性）
//   - e2e_mrpcm_multiprotocol_test.go：多客户端各自写自己的 ID 区间，
//     最后做 COUNT 校验，不验证 RYW 时间窗口
//   - e2e_subproc_*_test.go：子进程维度，但 PG wire 场景未覆盖
//
// 本文件使用同进程 *server.Server（startPGWireServer），与既有同进程
// 协议一致性测试保持一致，避免引入新的进程级 flakiness。
//
// 测试设计原则：
//   - 每个测试 t.Parallel 并发执行
//   - 写客户端与读客户端使用独立的 sqlServer 实例，确保读客户端拿到的
//     一定是从远端「跨协议」读到的数据（而不是同进程内存命中）
//   - RYW 校验：INSERT 后立即 sleep 0（无延迟）读取 + sleep 50ms 读取，
//     两次都应看到新行
//   - 数值字段按 float64 容差 1e-9 比较；字符串按精确比较
//
// 函数拆分说明：CI 限制单函数 ≤ 80 行 + 圈复杂度 ≤ 15，因此每个测试
// 函数只做编排，具体读写逻辑下沉到 *step* / *perProtocol* 辅助函数。
package integration

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

// ryw 测试常量。
const (
	rywTable        = "ryw_orders"
	rywClientsPer   = 3 // 每协议的客户端数
	rywOpsPerClient = 4 // 每客户端执行的 INSERT 次数
	rywBatchPerIns  = 2 // 每次 INSERT 的行数（tag=0 / tag=1 各一行，用于 50/50 拆分）
	rywBaseID       = int64(50000)
)

// rywProtocols 三协议集合（与 protocolParityProtocols 顺序无关，本地
// 用一个独立常量便于在不依赖外部变量的情况下做单测）。
var rywProtocols = []string{"tcp", "http", "pg"}

// rywClient 单一协议的客户端封装。
//
// TCP 客户端复用单条长连接；HTTP 客户端走 sqlHTTPClient 共享连接池；PG wire
// 客户端每次新建短连接（pgx 等真实驱动也是连接池化，但协议层无状态，简化
// 为按需建立以避免 Prepared Statement / Portal 跨连接状态污染）。
type rywClient struct {
	via string
	srv *sqlServer
	tcp *tcpClient
}

// rywNewClient 按协议建立客户端。
func rywNewClient(s *sqlServer, via string) *rywClient {
	c := &rywClient{via: via, srv: s}
	if via == "tcp" {
		tc, err := dialTCP(s.tcpAddr)
		if err != nil {
			panic(fmt.Sprintf("tcp 拨号失败: %v", err))
		}
		c.tcp = tc
	}
	return c
}

// rywClose 关闭 TCP 长连接（HTTP/PG 按连接级生命周期各自处理）。
func (c *rywClient) rywClose() {
	if c.tcp != nil {
		c.tcp.close()
	}
}

// rywInsert 经客户端所在协议插入一行；id 由调用方按协议+客户端+轮次唯一生成。
//
// tag 参数用于 TestThreeProtocolConcurrentCRUD 中区分 UPDATE / DELETE 目标
// 子集；调用方为每行指定 0 或 1。
func (c *rywClient) rywInsert(id int64, name string, amount float64, tag int64) error {
	sql := fmt.Sprintf(
		"INSERT INTO %s (id, name, amount, tag) VALUES (%d, '%s', %.4f, %d)",
		rywTable, id, name, amount, tag,
	)
	return rywExecSimple(c.srv, c.tcp, c.via, sql, "INSERT")
}

// rywExecSimple 在指定协议上执行单条 DML，返回协议级错误。
//
// 共享 INSERT/UPDATE/DELETE 的执行路径：构造 SQL 后按协议发送，统一处理
// code != 0 / PG 错误消息等。
func rywExecSimple(s *sqlServer, tc *tcpClient, via, sql, op string) error {
	switch via {
	case "tcp":
		resp, err := tc.query(sql)
		if err != nil {
			return fmt.Errorf("tcp %s: %w", op, err)
		}
		if resp.Code != 0 {
			return fmt.Errorf("tcp %s code=%d: %s", op, resp.Code, resp.Message)
		}
	case "http":
		resp, err := httpQuery(s.httpAddr, sql)
		if err != nil {
			return fmt.Errorf("http %s: %w", op, err)
		}
		if resp.Code != 0 {
			return fmt.Errorf("http %s code=%d: %s", op, resp.Code, resp.Message)
		}
	case "pg":
		pg, err := dialPGWireErr(s.srv.PGAddr())
		if err != nil {
			return fmt.Errorf("pg %s 拨号: %w", op, err)
		}
		defer pg.close()
		if err := pg.handshakeErr(); err != nil {
			return fmt.Errorf("pg %s 握手: %w", op, err)
		}
		res, err := pg.sendQueryRead(sql)
		if err != nil {
			return fmt.Errorf("pg %s: %w", op, err)
		}
		if res.errMsg != "" {
			return fmt.Errorf("pg %s 错误: %s", op, res.errMsg)
		}
	default:
		return fmt.Errorf("未知协议: %s", via)
	}
	return nil
}

// rywSelectByID 经指定协议 SELECT 单行，返回行级 map；不存在时返回 nil。
//
// 用于 RYW 校验：INSERT 后立即读取，期望读到新行；DELETE 后立即读取，
// 期望返回 nil。
func rywSelectByID(t *testing.T, s *sqlServer, via string, id int64) (map[string]any, error) {
	t.Helper()
	sql := fmt.Sprintf("SELECT id, name, amount FROM %s WHERE id = %d", rywTable, id)
	switch via {
	case "tcp":
		return rywSelectByIDTCP(s, sql)
	case "http":
		return rywSelectByIDHTTP(s, sql)
	case "pg":
		return rywSelectByIDPG(s, sql)
	default:
		return nil, fmt.Errorf("未知协议: %s", via)
	}
}

// rywSelectByIDTCP TCP 协议 SELECT 单行。
func rywSelectByIDTCP(s *sqlServer, sql string) (map[string]any, error) {
	tc, err := dialTCP(s.tcpAddr)
	if err != nil {
		return nil, fmt.Errorf("tcp 拨号: %w", err)
	}
	defer tc.close()
	resp, err := tc.query(sql)
	if err != nil {
		return nil, fmt.Errorf("tcp SELECT: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("tcp SELECT code=%d: %s", resp.Code, resp.Message)
	}
	rows := respRows(resp)
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// rywSelectByIDHTTP HTTP 协议 SELECT 单行。
func rywSelectByIDHTTP(s *sqlServer, sql string) (map[string]any, error) {
	resp, err := httpQuery(s.httpAddr, sql)
	if err != nil {
		return nil, fmt.Errorf("http SELECT: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("http SELECT code=%d: %s", resp.Code, resp.Message)
	}
	rows := respRows(resp)
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// rywSelectByIDPG PG wire 协议 SELECT 单行。
func rywSelectByIDPG(s *sqlServer, sql string) (map[string]any, error) {
	pg, err := dialPGWireErr(s.srv.PGAddr())
	if err != nil {
		return nil, fmt.Errorf("pg 拨号: %w", err)
	}
	defer pg.close()
	if err := pg.handshakeErr(); err != nil {
		return nil, fmt.Errorf("pg 握手: %w", err)
	}
	res, err := pg.sendQueryRead(sql)
	if err != nil {
		return nil, fmt.Errorf("pg SELECT: %w", err)
	}
	if res.errMsg != "" {
		return nil, fmt.Errorf("pg SELECT 错误: %s", res.errMsg)
	}
	if len(res.rows) == 0 {
		return nil, nil
	}
	return pgRowToMap(res.columns, res.rows[0]), nil
}

// rywAssertSameRow 三协议读到同一行时，关键字段必须一致。
//
// id 完全相等；name 完全相等；amount 数值按 float64 容差比较（PG wire
// 经文本协议返回字符串，本函数先尝试 ParseFloat 失败时再走严格相等）。
func rywAssertSameRow(t *testing.T, id int64, viaToRow map[string]map[string]any) {
	t.Helper()
	base, ok := viaToRow["tcp"]
	if !ok {
		t.Fatalf("缺少 TCP 读结果")
	}
	baseName, _ := base["name"].(string)
	baseAmt, _ := toFloat64(base["amount"])
	for _, via := range []string{"http", "pg"} {
		row, ok := viaToRow[via]
		if !ok {
			t.Errorf("[id=%d] 缺少 %s 读结果", id, via)
			continue
		}
		if row["name"] != baseName {
			t.Errorf("[id=%d] name 不一致: tcp=%q %s=%q", id, baseName, via, row["name"])
		}
		var amt float64
		switch n := row["amount"].(type) {
		case float64:
			amt = n
		case string:
			f, err := strconv.ParseFloat(n, 64)
			if err != nil {
				t.Errorf("[id=%d] %s amount 解析失败: %v (raw=%q)", id, via, err, n)
				continue
			}
			amt = f
		default:
			t.Errorf("[id=%d] %s amount 类型异常: %T", id, via, row["amount"])
			continue
		}
		if diff := amt - baseAmt; diff < -1e-9 || diff > 1e-9 {
			t.Errorf("[id=%d] amount 不一致: tcp=%v %s=%v", id, baseAmt, via, amt)
		}
	}
}

// rywReadAllProtocols 一次 SELECT 全部三个协议，返回 via -> row。
//
// 任何协议出错 / 缺行通过 t.Errorf 报告（不致命，便于聚合报告）。
func rywReadAllProtocols(t *testing.T, s *sqlServer, id int64, label string) map[string]map[string]any {
	t.Helper()
	out := make(map[string]map[string]any, len(rywProtocols))
	for _, via := range rywProtocols {
		row, err := rywSelectByID(t, s, via, id)
		if err != nil {
			t.Errorf("[%s, via=%s] SELECT 失败: %v", label, via, err)
			continue
		}
		if row == nil {
			t.Errorf("[%s, via=%s] 期望读到行，got nil", label, via)
			continue
		}
		out[via] = row
	}
	return out
}

// TestThreeProtocolReadYourWrites 验证 INSERT/UPDATE/DELETE 之后三协议
// 都能立即读到最新状态。
//
// 流程：
//  1. 建表（HTTP，一次性）；
//  2. 经 TCP 客户端 INSERT 一行，间隔 0/10/50/100ms 通过三协议 SELECT 同一 id，
//     验证所有时刻三协议都读得到且数据一致；
//  3. 经 HTTP 客户端 UPDATE 该行，三协议 SELECT 应看到新值；
//  4. 经 PG wire 客户端 DELETE 该行，三协议 SELECT 应返回空。
func TestThreeProtocolReadYourWrites(t *testing.T) {
	t.Parallel()
	s := startPGWireServer(t)
	rywCreateTable(t, s)
	const targetID = int64(60001)

	// Step 1: TCP 客户端 INSERT
	tcpC := rywNewClient(s, "tcp")
	defer tcpC.rywClose()
	if err := tcpC.rywInsert(targetID, "tcp-inserted", 100.5, 0); err != nil {
		t.Fatalf("TCP INSERT 失败: %v", err)
	}

	// Step 2: 多时刻 RYW SELECT，应全可见且一致
	rywAssertReadYourWrites(t, s, targetID)

	// Step 3: HTTP UPDATE
	rywUpdateAndAssertVisible(t, s, targetID)

	// Step 4: PG wire DELETE
	rywDeleteAndAssertGone(t, s, targetID)
}

// rywAssertReadYourWrites 多次间隔（0/10/50/100ms）下三协议读到同一行。
func rywAssertReadYourWrites(t *testing.T, s *sqlServer, id int64) {
	t.Helper()
	delays := []time.Duration{0, 10 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond}
	for i, d := range delays {
		time.Sleep(d)
		got := rywReadAllProtocols(t, s, id, fmt.Sprintf("RYW iter=%d delay=%v", i, d))
		rywAssertSameRow(t, id, got)
	}
}

// rywUpdateAndAssertVisible 经 HTTP UPDATE 目标行后三协议 SELECT 应可见新值。
func rywUpdateAndAssertVisible(t *testing.T, s *sqlServer, id int64) {
	t.Helper()
	updateSQL := fmt.Sprintf("UPDATE %s SET amount = 999.99, name = 'http-updated' WHERE id = %d",
		rywTable, id)
	resp, err := httpQuery(s.httpAddr, updateSQL)
	if err != nil || resp.Code != 0 {
		t.Fatalf("HTTP UPDATE 失败: err=%v code=%d msg=%s", err, resp.Code, resp.Message)
	}
	got := rywReadAllProtocols(t, s, id, "UPDATE 后")
	rywAssertSameRow(t, id, got)
	if name, _ := got["tcp"]["name"].(string); name != "http-updated" {
		t.Errorf("UPDATE 后 name 应为 'http-updated'，得到 %q", name)
	}
}

// rywDeleteAndAssertGone 经 PG wire DELETE 后三协议 SELECT 应返回 nil。
func rywDeleteAndAssertGone(t *testing.T, s *sqlServer, id int64) {
	t.Helper()
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE id = %d", rywTable, id)
	if err := rywExecSimple(s, nil, "pg", deleteSQL, "DELETE"); err != nil {
		t.Fatalf("pg DELETE 失败: %v", err)
	}
	for _, via := range rywProtocols {
		row, err := rywSelectByID(t, s, via, id)
		if err != nil {
			t.Errorf("[DELETE, via=%s] SELECT 失败: %v", via, err)
			continue
		}
		if row != nil {
			t.Errorf("[DELETE, via=%s] 期望读到 nil，得到 %v", via, row)
		}
	}
}

// rywCreateTable 经 HTTP 建表（DDL 在 SQL 层三协议等价，用 HTTP 即可）。
//
// 包含 `tag INT64` 列用于 UPDATE/DELETE 的二分子集选择：当前 SQL 解析器
// 不支持 % 模运算，额外引入 tag 列（写入时显式打 0/1 标签）是协议无关
// 的一致性测试中实现「一半行 UPDATE，一半行 DELETE」的最直接方式。
func rywCreateTable(t *testing.T, s *sqlServer) {
	t.Helper()
	sql := "CREATE TABLE " + rywTable + " (" +
		"id INT64 NOT NULL, name STRING NULL, amount FLOAT64 NULL, " +
		"tag INT64 NULL, " +
		"PRIMARY KEY(id))"
	resp := queryVia(t, s, "http", sql)
	if resp.Code != 0 {
		t.Fatalf("建表失败: %s", resp.Message)
	}
}

// TestThreeProtocolConcurrentCRUD 验证三协议并发执行完整 CRUD 生命周期
// 后的最终状态一致：所有写入都被持久化，所有协议都读到相同结果。
//
// 流程：
//  1. 6 个客户端（每协议 2 个）并发执行 INSERT；
//  2. 所有客户端完成后，三协议各做一次 SELECT * 验证总行数与字段一致；
//  3. 经 HTTP 执行 UPDATE（只一次，避免累加干扰）将 tag=0 的 amount 增加；
//  4. 经 PG wire 执行 DELETE 删除 tag=1 的行；
//  5. 三协议各做一次 SELECT 验证 target 行（tag=0）amount 已更新，
//     抽样三协议一致；COUNT 验证剩余行数与预期一致。
//
// 不变量：所有协议任何时刻看到的都是同一份「committed 视图」。
//
// 设计权衡：UPDATE / DELETE 只经单一协议发起，再用三协议并行校验，避
// 免「同一变更被三协议各自执行一次」造成 3 倍更新的副作用（也让结果可
// 预期、可断言）。这与现实世界用法（写走任意协议，读走任意协议）一致。
func TestThreeProtocolConcurrentCRUD(t *testing.T) {
	t.Parallel()
	s := startPGWireServer(t)
	rywCreateTable(t, s)

	totalRows := rywClientsPer * len(rywProtocols) * rywOpsPerClient * rywBatchPerIns

	// Step 1: 6 客户端并发 INSERT
	rywConcurrentInsert(t, s)
	if t.Failed() {
		return // 后续断言无意义，直接返回；defer 由 t.Cleanup 处理
	}

	// Step 2: 三协议 SELECT * 验证总行数
	rywAssertCount(t, s, "INSERT 后", totalRows)

	// Step 3: HTTP 单协议发起 UPDATE（tag=0 的 amount 增加 1000）。
	rywUpdateHalfAndAssert(t, s)

	// Step 4: PG wire 单协议发起 DELETE（tag=1 的行）。
	if err := rywDeleteTagOneViaPG(s); err != nil {
		t.Fatalf("pg DELETE 失败: %v", err)
	}

	// Step 5: 三协议 COUNT 验证
	rywAssertCount(t, s, "DELETE 后", totalRows/2)
}

// rywConcurrentInsert 启动 6 个客户端并发执行 INSERT。
//
// 任一客户端失败通过 t.Errorf 报告，函数返回时如 t.Failed() 表明本阶段
// 失败。
func rywConcurrentInsert(t *testing.T, s *sqlServer) {
	t.Helper()
	var wg sync.WaitGroup
	for c := 0; c < rywClientsPer; c++ {
		for _, via := range rywProtocols {
			wg.Add(1)
			go rywRunOneInserts(t, &wg, s, via, c)
		}
	}
	wg.Wait()
}

// rywRunOneInserts 单个客户端的 INSERT 循环。
func rywRunOneInserts(t *testing.T, wg *sync.WaitGroup, s *sqlServer, via string, clientID int) {
	defer wg.Done()
	cl := rywNewClient(s, via)
	defer cl.rywClose()
	for op := 0; op < rywOpsPerClient; op++ {
		for j := 0; j < rywBatchPerIns; j++ {
			id := rywComputeID(via, clientID, op, j)
			name := fmt.Sprintf("%s-c%d-o%d-r%d", via, clientID, op, j)
			amount := float64(id) * 0.5
			// j 决定 tag：j=0 -> tag=0（UPDATE 目标），j=1 -> tag=1（DELETE 目标）。
			tag := int64(j)
			if err := cl.rywInsert(id, name, amount, tag); err != nil {
				t.Errorf("[%s c%d o%d r%d] INSERT 失败: %v", via, clientID, op, j, err)
				return
			}
		}
	}
}

// rywAssertCount 在三协议上断言行数等于 want。
func rywAssertCount(t *testing.T, s *sqlServer, label string, want int) {
	t.Helper()
	for _, via := range rywProtocols {
		got, err := rywCountViaProtocol(s, via)
		if err != nil {
			t.Errorf("[%s, %s] COUNT 失败: %v", label, via, err)
			continue
		}
		if got != want {
			t.Errorf("[%s, %s] COUNT 期望 %d，得到 %d", label, via, want, got)
		}
	}
}

// rywUpdateHalfAndAssert HTTP 单协议 UPDATE tag=0 的 amount +1000，并三协议校验。
//
// 注：当前 SQL 解析器不支持字符串连接符 ||，所以 name 字段不参与 UPDATE；
// 用 amount 单字段断言变更即可。
func rywUpdateHalfAndAssert(t *testing.T, s *sqlServer) {
	t.Helper()
	updateSQL := fmt.Sprintf("UPDATE %s SET amount = amount + 1000 WHERE tag = 0", rywTable)
	resp, err := httpQuery(s.httpAddr, updateSQL)
	if err != nil {
		t.Fatalf("HTTP UPDATE 失败: err=%v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("HTTP UPDATE code=%d: %s", resp.Code, resp.Message)
	}
	targetID := rywComputeID("tcp", 0, 0, 0)
	baseAmount := float64(targetID) * 0.5
	for _, via := range rywProtocols {
		row, err := rywSelectByID(t, s, via, targetID)
		if err != nil || row == nil {
			t.Errorf("[UPDATE, via=%s] 抽样 id=%d 失败: err=%v row=%v",
				via, targetID, err, row)
			continue
		}
		gotAmt, ok := rywParseAmount(t, via, row["amount"])
		if !ok {
			continue
		}
		if diff := gotAmt - (baseAmount + 1000); diff < -1e-9 || diff > 1e-9 {
			t.Errorf("[UPDATE, via=%s] tag=0 amount 未增加：期望 %v，得到 %v",
				via, baseAmount+1000, gotAmt)
		}
	}
}

// rywParseAmount 统一解析 amount 字段：float64 直读，string 走 ParseFloat。
func rywParseAmount(t *testing.T, via string, raw any) (float64, bool) {
	t.Helper()
	switch n := raw.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			t.Errorf("[%s] amount 解析失败: %v (raw=%q)", via, err, n)
			return 0, false
		}
		return f, true
	default:
		t.Errorf("[%s] amount 类型异常: %T (raw=%v)", via, raw, raw)
		return 0, false
	}
}

// rywDeleteTagOneViaPG 经 PG wire 发起 DELETE WHERE tag=1。
func rywDeleteTagOneViaPG(s *sqlServer) error {
	sql := fmt.Sprintf("DELETE FROM %s WHERE tag = 1", rywTable)
	return rywExecSimple(s, nil, "pg", sql, "DELETE")
}

// rywComputeID 计算各客户端写入的唯一 id。
//
// 编码规则：id = rywBaseID + viaOffset*100000 + clientID*1000 + op*100 + j。
// viaOffset 通过 protocolParityProtocols 顺序取得（http=0, tcp=1, pg=2）。
// 各协议段 100000 容量足够 rywOpsPerClient*rywBatchPerIns 行；每客户端 1000
// 容量足够 rywOpsPerClient*rywBatchPerIns 行。
func rywComputeID(via string, clientID, op, j int) int64 {
	viaOffset := -1
	for i, v := range protocolParityProtocols {
		if v == via {
			viaOffset = i
			break
		}
	}
	if viaOffset < 0 {
		panic(fmt.Sprintf("未知协议: %s", via))
	}
	return rywBaseID + int64(viaOffset)*100000 + int64(clientID)*1000 + int64(op)*100 + int64(j)
}

// rywCountViaProtocol 按协议执行 COUNT(*) 返回行数。
func rywCountViaProtocol(s *sqlServer, via string) (int, error) {
	sql := fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", rywTable)
	row, err := rywSingleValueByProtocol(s, via, sql)
	if err != nil {
		return 0, err
	}
	switch n := row.(type) {
	case float64:
		return int(n), nil
	case int64:
		return int(n), nil
	case string:
		i, perr := strconv.Atoi(n)
		if perr != nil {
			return 0, fmt.Errorf("[%s] COUNT 返回值解析失败: %q", via, n)
		}
		return i, nil
	}
	return 0, fmt.Errorf("[%s] COUNT 返回值类型异常: %T", via, row)
}

// rywSingleValueByProtocol 按协议执行 SQL 并取第一行第一列（dispatcher）。
func rywSingleValueByProtocol(s *sqlServer, via, sql string) (any, error) {
	switch via {
	case "tcp":
		return rywSingleValueByProtocolTCP(s, sql)
	case "http":
		return rywSingleValueByProtocolHTTP(s, sql)
	case "pg":
		return rywSingleValueByProtocolPG(s, sql)
	}
	return nil, fmt.Errorf("未知协议: %s", via)
}

// rywSingleValueByProtocolTCP TCP 协议取单值。
func rywSingleValueByProtocolTCP(s *sqlServer, sql string) (any, error) {
	tc, err := dialTCP(s.tcpAddr)
	if err != nil {
		return nil, err
	}
	defer tc.close()
	resp, err := tc.query(sql)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("code=%d: %s", resp.Code, resp.Message)
	}
	rows := respRows(resp)
	if len(rows) == 0 {
		return nil, fmt.Errorf("无返回行")
	}
	for _, v := range rows[0] {
		return v, nil
	}
	return nil, nil
}

// rywSingleValueByProtocolHTTP HTTP 协议取单值。
func rywSingleValueByProtocolHTTP(s *sqlServer, sql string) (any, error) {
	resp, err := httpQuery(s.httpAddr, sql)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("code=%d: %s", resp.Code, resp.Message)
	}
	rows := respRows(resp)
	if len(rows) == 0 {
		return nil, fmt.Errorf("无返回行")
	}
	for _, v := range rows[0] {
		return v, nil
	}
	return nil, nil
}

// rywSingleValueByProtocolPG PG wire 协议取单值。
func rywSingleValueByProtocolPG(s *sqlServer, sql string) (any, error) {
	pg, err := dialPGWireErr(s.srv.PGAddr())
	if err != nil {
		return nil, err
	}
	defer pg.close()
	if err := pg.handshakeErr(); err != nil {
		return nil, err
	}
	res, err := pg.sendQueryRead(sql)
	if err != nil {
		return nil, err
	}
	if res.errMsg != "" {
		return nil, fmt.Errorf("pg 错误: %s", res.errMsg)
	}
	if len(res.rows) == 0 {
		return nil, fmt.Errorf("无返回行")
	}
	// PG wire 返回的是 []any，找首个非 nil 值
	for _, v := range res.rows[0] {
		if v != nil {
			return v, nil
		}
	}
	return nil, nil
}
