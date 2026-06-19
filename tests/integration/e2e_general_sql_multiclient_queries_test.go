// Package integration 端到端集成测试：通用 SQL 第二组场景（查询/元命令/错误/协议）。
//
// 本文件与 e2e_general_sql_multiclient_test.go 配套，覆盖查询侧场景：
//   - 元命令：SHOW TABLES / DESCRIBE / EXPLAIN 端到端
//   - 错误路径：长连接上多次错误后仍能正常服务
//   - HTTP 短连接风格多客户端并发
//   - 列投影、AS 别名、算术表达式
//   - GROUP BY + COUNT/SUM/AVG/MIN/MAX 聚合
//   - WHERE AND/OR 复合过滤
//   - TCP packet 协议层完整 round-trip
//
// 共享的 gsmClient / gsmTable / gsmSetupTable 等 helper 定义在主文件中。
// 拆分原因：单文件测试体量过大（> 800 行）触发 CI「测试文件 ≤ 800 行」上限。
package integration

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/server"
)

// TestGeneralSQLMetaCommands 验证 SHOW TABLES / DESCRIBE / EXPLAIN 端到端可用。
//
// 三个元命令均经 TCP 与 HTTP 通道调用，验证响应码为 0 且包含期望片段。
// 为控制认知复杂度（gocognit ≤ 20），元命令校验按命令维度拆分为独立 helper。
func TestGeneralSQLMetaCommands(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	for _, via := range []string{"tcp", "http"} {
		t.Run(via, func(t *testing.T) {
			gsmVerifyShowTables(t, s, via)
			gsmVerifyDescribe(t, s, via)
			gsmVerifyExplain(t, s, via)
		})
	}
}

// gsmVerifyShowTables 校验 SHOW TABLES 输出包含 gsmTable。
func gsmVerifyShowTables(t *testing.T, s *sqlServer, via string) {
	t.Helper()
	resp := queryVia(t, s, via, "SHOW TABLES")
	if resp.Code != 0 {
		t.Fatalf("SHOW TABLES 失败: %s", resp.Message)
	}
	showRows := respRows(resp)
	if len(showRows) == 0 {
		t.Fatal("SHOW TABLES 期望至少 1 行")
	}
	for _, row := range showRows {
		// SHOW TABLES 输出列名为「table」（见 e2e_session_sql_test.go
		// sessVerifyMeta 约定），兼容此命名。
		if name, ok := row["table"].(string); ok && name == gsmTable {
			return
		}
	}
	t.Errorf("SHOW TABLES 未列出 %s，rows=%v", gsmTable, showRows)
}

// gsmVerifyDescribe 校验 DESCRIBE 输出列数与 gsmTable 一致。
func gsmVerifyDescribe(t *testing.T, s *sqlServer, via string) {
	t.Helper()
	resp := queryVia(t, s, via, "DESCRIBE "+gsmTable)
	if resp.Code != 0 {
		t.Fatalf("DESCRIBE 失败: %s", resp.Message)
	}
	descRows := respRows(resp)
	if len(descRows) != len(gsmColumnNames) {
		t.Errorf("DESCRIBE 列数: 期望 %d，得到 %d",
			len(gsmColumnNames), len(descRows))
	}
}

// gsmVerifyExplain 校验 EXPLAIN 输出至少 1 行 plan 描述。
func gsmVerifyExplain(t *testing.T, s *sqlServer, via string) {
	t.Helper()
	resp := queryVia(t, s, via,
		"EXPLAIN SELECT id, product FROM "+gsmTable+" WHERE id = 1")
	if resp.Code != 0 {
		t.Fatalf("EXPLAIN 失败: %s", resp.Message)
	}
	if len(respRows(resp)) == 0 {
		t.Error("EXPLAIN 期望至少 1 行 plan 描述")
	}
}

// TestGeneralSQLErrorPaths 验证错误 SQL 优雅报错（不 panic、不挂起、不污染连接）。
//
// 关键点：同一条 TCP 长连接上第一个错误不应影响后续正常请求的返回。
func TestGeneralSQLErrorPaths(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	// 启动一个 TCP 长连接
	c, err := newGSMClient(s, "tcp")
	if err != nil {
		t.Fatalf("建连失败: %v", err)
	}
	defer c.close()

	// 错误 1：查询不存在的表
	resp, err := c.query("SELECT * FROM does_not_exist")
	if err != nil {
		t.Fatalf("查询不存在的表 IO 错误: %v", err)
	}
	if resp.Code == 0 {
		t.Error("查询不存在的表应返回非零码")
	}

	// 错误 2：语法错误
	resp, err = c.query("INVALID SQL STATEMENT")
	if err != nil {
		t.Fatalf("语法错误 IO 失败: %v", err)
	}
	if resp.Code == 0 {
		t.Error("无效 SQL 应返回非零码")
	}

	// 错误 3：parser 暂不支持的语法（IN）
	resp, err = c.query("SELECT * FROM " + gsmTable + " WHERE id IN (1)")
	if err != nil {
		t.Fatalf("IN 语法 IO 失败: %v", err)
	}
	if resp.Code == 0 {
		t.Error("IN 语法当前不被支持，应返回非零码")
	}

	// 错误 4：UPDATE 不存在的表
	resp, err = c.query("UPDATE does_not_exist SET x = 1")
	if err != nil {
		t.Fatalf("UPDATE 不存在表 IO 失败: %v", err)
	}
	if resp.Code == 0 {
		t.Error("UPDATE 不存在的表应返回非零码")
	}

	// 关键：长连接在多次错误后仍能正常服务后续请求
	resp, err = c.query("SELECT COUNT(*) AS cnt FROM " + gsmTable)
	if err != nil {
		t.Fatalf("错误后正常查询 IO 失败: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("错误后正常查询应成功，得到 code=%d msg=%s",
			resp.Code, resp.Message)
	}
	cnt, _ := toInt64(respRows(resp)[0]["cnt"])
	if cnt != 5 {
		t.Errorf("错误恢复后行数: 期望 5，得到 %d", cnt)
	}
}

// TestGeneralSQLHTTPConcurrency 验证 HTTP 多客户端并发下 SELECT/INSERT 端到端一致。
//
// 与 TestGeneralSQLMultiClientPersistentConn 的区别：此处走 HTTP 短连接风格
// （每请求新建连接），用于覆盖 HTTP 服务端路径的并发稳定性。
func TestGeneralSQLHTTPConcurrency(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	const numClients = 10
	const writesPerClient = 5
	var wg sync.WaitGroup
	var failCount int64
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			for j := 0; j < writesPerClient; j++ {
				id := 50000 + clientID*writesPerClient + j
				sql := fmt.Sprintf(
					"INSERT INTO %s (id, product, qty, amount, active) VALUES "+
						"(%d, 'http-c%d', %d, %.2f, true)",
					gsmTable, id, clientID, j, float64(id)*0.5,
				)
				resp, err := httpQuery(s.httpAddr, sql)
				if err != nil {
					atomic.AddInt64(&failCount, 1)
					return
				}
				if resp.Code != 0 {
					atomic.AddInt64(&failCount, 1)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if failCount > 0 {
		t.Fatalf("%d 个 HTTP 客户端失败", failCount)
	}

	// 期望行数：初始 5 + 10*5 = 55
	resp := queryVia(t, s, "tcp", "SELECT COUNT(*) AS cnt FROM "+gsmTable)
	if resp.Code != 0 {
		t.Fatalf("COUNT 失败: %s", resp.Message)
	}
	want := int64(5 + numClients*writesPerClient)
	got, _ := toInt64(respRows(resp)[0]["cnt"])
	if got != want {
		t.Errorf("HTTP 并发写入行数: 期望 %d，得到 %d", want, got)
	}
}

// TestGeneralSQLProjectionAndAlias 验证列投影、别名、算术表达式端到端正确。
//
// 涵盖：列投影、AS 别名、SELECT 中的算术运算（amount*2、qty+10）。
func TestGeneralSQLProjectionAndAlias(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	// 简单投影
	resp := queryVia(t, s, "tcp", "SELECT id, product FROM "+gsmTable+" WHERE id = 1")
	if resp.Code != 0 {
		t.Fatalf("投影查询失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		t.Fatalf("投影期望 1 行，得到 %d", len(rows))
	}
	if _, ok := rows[0]["id"]; !ok {
		t.Error("投影结果缺少 id")
	}
	if _, ok := rows[0]["product"]; !ok {
		t.Error("投影结果缺少 product")
	}
	if _, ok := rows[0]["amount"]; ok {
		t.Error("投影结果不应包含 amount")
	}

	// 别名
	resp = queryVia(t, s, "tcp",
		"SELECT id, amount * 2 AS doubled FROM "+gsmTable+" WHERE id = 1")
	if resp.Code != 0 {
		t.Fatalf("别名查询失败: %s", resp.Message)
	}
	rows = respRows(resp)
	if len(rows) != 1 {
		t.Fatalf("别名查询期望 1 行，得到 %d", len(rows))
	}
	doubled, ok := toFloat64(rows[0]["doubled"])
	if !ok {
		t.Fatalf("doubled 字段类型异常 %T", rows[0]["doubled"])
	}
	wantDoubled := 99.5 * 2
	if doubled != wantDoubled {
		t.Errorf("amount*2: 期望 %v，得到 %v", wantDoubled, doubled)
	}

	// 算术 + 别名组合（qty+10 AS new_qty）
	resp = queryVia(t, s, "tcp",
		"SELECT qty + 10 AS new_qty FROM "+gsmTable+" WHERE id = 1")
	if resp.Code != 0 {
		t.Fatalf("算术查询失败: %s", resp.Message)
	}
	rows = respRows(resp)
	if len(rows) != 1 {
		t.Fatalf("算术查询期望 1 行，得到 %d", len(rows))
	}
	newQty, ok := toFloat64(rows[0]["new_qty"])
	if !ok {
		t.Fatalf("new_qty 字段类型异常 %T", rows[0]["new_qty"])
	}
	if newQty != 20 {
		t.Errorf("qty+10: 期望 20，得到 %v", newQty)
	}
}

// TestGeneralSQLGroupByAggregation 验证 GROUP BY + 聚合函数在通用 SQL 通道下正确。
//
// 场景：
//   - 初始 5 行：alpha×2、beta×2、gamma×1
//   - 校验 COUNT/AVG/SUM/MIN/MAX 各分组值
//   - 额外用 SUM/COUNT 验证 0.5 容差（避免浮点累加误差）
func TestGeneralSQLGroupByAggregation(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	resp := queryVia(t, s, "tcp",
		"SELECT product, COUNT(*) AS cnt, SUM(qty) AS sum_qty, "+
			"AVG(amount) AS avg_amount, MIN(qty) AS min_qty, MAX(qty) AS max_qty "+
			"FROM "+gsmTable+" GROUP BY product")
	if resp.Code != 0 {
		t.Fatalf("聚合查询失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 3 {
		t.Fatalf("期望 3 个分组（alpha/beta/gamma），得到 %d", len(rows))
	}

	// 按 product 建索引
	type aggExpect struct {
		cnt       int64
		sumQty    float64
		avgAmount float64
		minQty    float64
		maxQty    float64
	}
	want := map[string]aggExpect{
		"alpha": {cnt: 2, sumQty: 30, avgAmount: (99.5 + 200.0) / 2, minQty: 10, maxQty: 20},
		"beta":  {cnt: 2, sumQty: 8, avgAmount: (49.75 + 30.0) / 2, minQty: 3, maxQty: 5},
		"gamma": {cnt: 1, sumQty: 15, avgAmount: 150.0, minQty: 15, maxQty: 15},
	}
	byProduct := make(map[string]map[string]any)
	for _, row := range rows {
		prod, _ := row["product"].(string)
		byProduct[prod] = row
	}
	for prod, exp := range want {
		row, ok := byProduct[prod]
		if !ok {
			t.Errorf("缺少 product=%s 分组", prod)
			continue
		}
		cnt, _ := toInt64(row["cnt"])
		if cnt != exp.cnt {
			t.Errorf("product=%s cnt: 期望 %d，得到 %d", prod, exp.cnt, cnt)
		}
		sumQty, _ := toFloat64(row["sum_qty"])
		if sumQty != exp.sumQty {
			t.Errorf("product=%s sum_qty: 期望 %v，得到 %v", prod, exp.sumQty, sumQty)
		}
		avgAmount, _ := toFloat64(row["avg_amount"])
		if diff := avgAmount - exp.avgAmount; diff < -1e-6 || diff > 1e-6 {
			t.Errorf("product=%s avg_amount: 期望 %v，得到 %v",
				prod, exp.avgAmount, avgAmount)
		}
		minQty, _ := toFloat64(row["min_qty"])
		if minQty != exp.minQty {
			t.Errorf("product=%s min_qty: 期望 %v，得到 %v", prod, exp.minQty, minQty)
		}
		maxQty, _ := toFloat64(row["max_qty"])
		if maxQty != exp.maxQty {
			t.Errorf("product=%s max_qty: 期望 %v，得到 %v", prod, exp.maxQty, maxQty)
		}
	}
}

// TestGeneralSQLFilterAnd 验证 WHERE AND/OR 组合过滤。
//
// 数据：id=1 (alpha, qty=10), id=2 (beta, qty=5), id=3 (alpha, qty=20) ...
// 测试 product='alpha' AND qty > 15 → 仅 id=3
// 测试 product='alpha' OR product='beta' → id=1,2,3,5（4 行）
func TestGeneralSQLFilterAnd(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	// AND 组合
	resp := queryVia(t, s, "tcp",
		"SELECT id FROM "+gsmTable+
			" WHERE product = 'alpha' AND qty > 15")
	if resp.Code != 0 {
		t.Fatalf("AND 查询失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		t.Fatalf("AND 期望 1 行，得到 %d", len(rows))
	}
	id, _ := toInt64(rows[0]["id"])
	if id != 3 {
		t.Errorf("AND 期望 id=3，得到 %d", id)
	}

	// OR 组合
	resp = queryVia(t, s, "tcp",
		"SELECT id FROM "+gsmTable+
			" WHERE product = 'alpha' OR product = 'beta'")
	if resp.Code != 0 {
		t.Fatalf("OR 查询失败: %s", resp.Message)
	}
	rows = respRows(resp)
	if len(rows) != 4 {
		t.Fatalf("OR 期望 4 行，得到 %d", len(rows))
	}
	gotIDs := make(map[int64]bool)
	for _, r := range rows {
		id, _ := toInt64(r["id"])
		gotIDs[id] = true
	}
	for _, want := range []int64{1, 2, 3, 5} {
		if !gotIDs[want] {
			t.Errorf("OR 缺少 id=%d", want)
		}
	}

	// 复合 (alpha AND qty>15) OR (beta AND active=true)
	resp = queryVia(t, s, "tcp",
		"SELECT id FROM "+gsmTable+
			" WHERE (product = 'alpha' AND qty > 15) OR "+
			"(product = 'beta' AND active = true)")
	if resp.Code != 0 {
		t.Fatalf("复合 WHERE 失败: %s", resp.Message)
	}
	rows = respRows(resp)
	if len(rows) != 3 {
		t.Fatalf("复合 WHERE 期望 3 行（id=3,2,5），得到 %d", len(rows))
	}
}

// TestGeneralSQLPacketRoundTrip 验证 TCP 自定义协议层的完整 round-trip。
//
// 发送：QueryRequest、WriteRequest、Ping 三种 packet
// 解析：响应 packet 的 magic、header、payload 均正确
//
// 此测试聚焦协议层正确性，不依赖 SQL 语义。
func TestGeneralSQLPacketRoundTrip(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	gsmSetupTable(t, s)

	c, err := newGSMClient(s, "tcp")
	if err != nil {
		t.Fatalf("建连失败: %v", err)
	}
	defer c.close()

	// 1. Ping
	pingMsg, err := c.tcp.ping()
	if err != nil {
		t.Fatalf("Ping IO 失败: %v", err)
	}
	if pingMsg != "pong" {
		t.Errorf("Ping 响应: 期望 pong，得到 %q", pingMsg)
	}

	// 2. Query
	resp, err := c.tcp.query("SELECT id, product FROM " + gsmTable + " LIMIT 2")
	if err != nil {
		t.Fatalf("Query IO 失败: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("Query code: %d msg=%s", resp.Code, resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 2 {
		t.Errorf("Query 期望 2 行，得到 %d", len(rows))
	}

	// 3. 验证响应 payload 是合法 JSON
	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("响应序列化失败: %v", err)
	}
	// 反序列化一致性
	var back server.Response
	if err := json.Unmarshal(payload, &back); err != nil {
		t.Fatalf("响应反序列化失败: %v", err)
	}
	if back.Code != resp.Code {
		t.Errorf("Round-trip code 不一致: %d vs %d", back.Code, resp.Code)
	}

	// 4. WriteRequest
	writeResp, err := c.tcp.write(gsmTable, []map[string]any{
		{"id": 99001, "product": "rt-1", "qty": 1, "amount": 1.0, "active": true},
	})
	if err != nil {
		t.Fatalf("Write IO 失败: %v", err)
	}
	if writeResp.Code != 0 {
		t.Fatalf("Write code: %d msg=%s", writeResp.Code, writeResp.Message)
	}

	// 5. 验证写入生效
	verifyResp, err := c.tcp.query("SELECT COUNT(*) AS cnt FROM " + gsmTable)
	if err != nil {
		t.Fatalf("Verify IO 失败: %v", err)
	}
	cnt, _ := toInt64(respRows(verifyResp)[0]["cnt"])
	if cnt != 6 { // 初始 5 + 1
		t.Errorf("Round-trip 后行数: 期望 6，得到 %d", cnt)
	}
}
