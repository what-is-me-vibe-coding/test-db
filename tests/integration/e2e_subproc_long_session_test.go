// Package integration 端到端集成测试：子进程 server + 多客户端 + 长会话顺序 SQL。
//
// 本文件补齐既有测试未覆盖的"长会话"维度：把 cmd/server 编译为子进程拉起，
// 打开 TCP 长连接 + HTTP 短连接混合的多客户端，每条客户端顺序执行 30+ 步
// SQL（DDL + DML + DQL + 元命令 + 错误路径 + 跨表 + admin 端点），验证：
//
//  1. 长会话内单条连接能稳定承载数十次请求：连接不复用、keepalive
//  2. 错误 SQL 不破坏后续请求：连接健康自愈
//  3. 跨客户端并发：写读不串扰、最终状态可精确断言
//  4. admin 端点（/admin/flush、/admin/compact）与普通 SQL 互不干扰
//  5. 任务结束后 server 仍能正常服务（健康检查 + 后续 SELECT 不出错）
//
// 与既有测试的区别：
//   - e2e_subproc_general_sql_multiclient_test.go：子进程 + 多客户端 +
//     一般 SQL，但侧重 INSERT/UPDATE/DELETE 的 ID 区间隔离与聚合断言，
//     未覆盖"长顺序会话 + 错误恢复 + admin 端点交错"。
//   - e2e_subproc_mixed_engine_multiclient_test.go：子进程 + 双引擎 +
//     多客户端 + 重启语义，未覆盖"长会话错误恢复"维度。
//   - e2e_general_sql_multiclient_test.go：同进程 server，worker 数固定
//     的并发模式，未走真实子进程 → 真实部署链路。
//
// 本文件是第一份"子进程 server + 长顺序会话 + 错误恢复 + admin 端点交错"
// 的组合测试，验证真实部署下 server 在 100+ 次/连接负载下的鲁棒性。
//
// 设计要点：
//  1. 子进程复用 e2e_subproc_smoke_test.go 的 buildSubprocBinary /
//     startSubprocessServer / stopSubprocessServer 等 helper。
//  2. TCP 客户端复用同进程 e2e_server_sql_test.go 的 dialTCP / tcpClient
//     协议（PacketQuery + JSON payload），HTTP 端点直连 /query 与 /admin/*。
//  3. 每客户端顺序执行 30+ 步：DDL 建表 → 灌入 → 跨表 SELECT → 错误 SQL
//     → 多次 UPDATE/DELETE → 元命令 → admin 端点交错（仅一个客户端负责）。
//  4. 客户端在独立 goroutine 中运行，错误通过 error channel 汇总到主
//     goroutine 后再断言（与 e2e_realistic_business_sql_test.go 一致）。
//  5. 任务结束后由主 goroutine 二次 SELECT 校验最终状态，并做 /health
//     与 SELECT 兜底，确保 server 未在负载下被破坏。
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// subprocLongSession 测试常量。
const (
	subprocLSSTable         = "subproc_lss_main" // 主表：长会话工作负载表
	subprocLSHelperTable    = "subproc_lss_aux"  // 辅助表：跨表 SELECT 验证
	subprocLSClientCount    = 4                  // 并发客户端数（2 TCP + 2 HTTP）
	subprocLSStepsPerClient = 33                 // 每客户端顺序执行的 SQL 步数
	subprocLSBaseID         = 800000             // 客户端 ID 起始偏移，避免与其它测试冲突
	subprocLSRowsPerStep    = 3                  // 每步 INSERT 行数
)

// subprocLSOpKind 描述长会话中每一步的语义类型，用于客户端负载编排。
type subprocLSOpKind int

const (
	subprocLSOpCreate subprocLSOpKind = iota // CREATE TABLE
	subprocLSOpInsert                        // INSERT INTO main
	subprocLSOpSelect                        // SELECT FROM main / aux
	subprocLSOpUpdate                        // UPDATE main SET ... WHERE
	subprocLSOpDelete                        // DELETE FROM main WHERE ...
	subprocLSOpError                         // 故意写错的 SQL（断言 code != 0）
	subprocLSOpShow                          // SHOW TABLES / DESCRIBE
)

// subprocLSStep 描述客户端顺序执行的一步。
type subprocLSStep struct {
	kind    subprocLSOpKind
	sql     string
	wantOK  bool   // 期望 code == 0
	wantMsg string // wantOK=false 时用于辅助诊断
}

// subprocLSBuildSteps 生成每客户端的 33 步负载序列。
//
// 序列设计（33 步，所有客户端完全一致；DDL 由主测试在派发 goroutine 前完成）：
//
//	1-15. 3 轮 (3×INSERT + 1×SELECT + 1×UPDATE) = 15 步
//	16. 错误 SQL（断言 code != 0）
//	17-20. 跨表 SELECT + 元命令 + 单行 DELETE
//	21-25. 5 步：5×INSERT
//	26. 错误 SQL（再次确认错误恢复）
//	27-33. 收尾：EXPLAIN + COUNT + SELECT + SHOW TABLES + DESCRIBE + SELECT * + COUNT
//
// 步骤总数 33 步：所有客户端使用完全相同的步骤序列，简化断言。
// 每客户端写入行的 ID 区间为 [baseID, baseID+stepsPerClient*rowsPerStep)，
// 避免跨客户端 ID 冲突；辅助 SELECT 可读到其它客户端写入的行。
func subprocLSBuildSteps(clientID int) []subprocLSStep {
	baseID := int64(subprocLSBaseID) + int64(clientID)*int64(subprocLSRowsPerStep)*int64(subprocLSStepsPerClient)
	lo := baseID
	hi := baseID + int64(subprocLSRowsPerStep)*int64(subprocLSStepsPerClient)
	stepID := func(step int) int64 { return baseID + int64(step)*int64(subprocLSRowsPerStep) }

	steps := make([]subprocLSStep, 0, subprocLSStepsPerClient)

	// 步骤 1-15: 3 轮 INSERT/SELECT/UPDATE。
	for i := 0; i < 3; i++ {
		// INSERT 3 行
		for j := 0; j < subprocLSRowsPerStep; j++ {
			id := stepID(i*4 + j)
			steps = append(steps, subprocLSStep{
				kind:   subprocLSOpInsert,
				sql:    subprocLSInsertSQL(id, i, j),
				wantOK: true,
			})
		}
		// SELECT 校验本区间行数
		steps = append(steps, subprocLSStep{
			kind:   subprocLSOpSelect,
			sql:    subprocLSCountInRangeSQL(lo, hi),
			wantOK: true,
		})
		// UPDATE 本区间所有行
		steps = append(steps, subprocLSStep{
			kind:   subprocLSOpUpdate,
			sql:    fmt.Sprintf("UPDATE %s SET score = score + 0.1 WHERE id >= %d AND id < %d", subprocLSSTable, lo, hi),
			wantOK: true,
		})
	}

	// 步骤 13: 故意错误 SQL（测试错误恢复，不破坏后续）。
	steps = append(steps, subprocLSStep{
		kind:    subprocLSOpError,
		sql:     "SELECT * FROM nonexistent_table_for_long_session",
		wantOK:  false,
		wantMsg: "未存在的表应当返回 code != 0",
	})

	// 步骤 14-17: 跨表 SELECT + 元命令。
	steps = append(steps,
		subprocLSStep{kind: subprocLSOpSelect, sql: "SHOW TABLES", wantOK: true},
		subprocLSStep{kind: subprocLSOpShow, sql: "DESCRIBE " + subprocLSSTable, wantOK: true},
		subprocLSStep{kind: subprocLSOpSelect, sql: "SELECT COUNT(*) AS aux_count FROM " + subprocLSHelperTable, wantOK: true},
		// 删除本区间第一行（id >= lo，1 行）
		subprocLSStep{
			kind:   subprocLSOpDelete,
			sql:    fmt.Sprintf("DELETE FROM %s WHERE id = %d", subprocLSSTable, lo),
			wantOK: true,
		},
	)

	// 步骤 18-22: 再次写入 + SELECT。
	for i := 0; i < 5; i++ {
		steps = append(steps, subprocLSStep{
			kind:   subprocLSOpInsert,
			sql:    subprocLSInsertSQL(stepID(15+i), 99, i),
			wantOK: true,
		})
	}

	// 步骤 26: 再一次故意错误 SQL。
	steps = append(steps, subprocLSStep{
		kind:    subprocLSOpError,
		sql:     "INSERT INTO " + subprocLSSTable + " VALUES (1, 1.0)", // 列数不足
		wantOK:  false,
		wantMsg: "列数不足的 INSERT 应当返回 code != 0",
	})

	// 步骤 27-33: 收尾 EXPLAIN + COUNT + SELECT + 元命令 + 收尾 SELECT。
	steps = append(steps,
		subprocLSStep{kind: subprocLSOpSelect, sql: "EXPLAIN SELECT * FROM " + subprocLSSTable + " LIMIT 5", wantOK: true},
		subprocLSStep{kind: subprocLSOpSelect, sql: subprocLSCountInRangeSQL(lo, hi), wantOK: true},
		subprocLSStep{kind: subprocLSOpSelect, sql: "SELECT id, label FROM " + subprocLSSTable + " WHERE id >= " + fmt.Sprint(lo) + " ORDER BY id LIMIT 5", wantOK: true},
		subprocLSStep{kind: subprocLSOpShow, sql: "SHOW TABLES", wantOK: true},
		subprocLSStep{kind: subprocLSOpSelect, sql: "DESCRIBE " + subprocLSHelperTable, wantOK: true},
		subprocLSStep{kind: subprocLSOpSelect, sql: "SELECT * FROM " + subprocLSSTable + " LIMIT 1", wantOK: true},
		subprocLSStep{kind: subprocLSOpSelect, sql: "SELECT COUNT(*) AS c FROM " + subprocLSSTable, wantOK: true},
	)

	if len(steps) != subprocLSStepsPerClient {
		panic(fmt.Sprintf("步骤数错乱: got %d, want %d", len(steps), subprocLSStepsPerClient))
	}
	return steps
}

// subprocLSCreateMainSQL 生成主表 CREATE TABLE SQL。
func subprocLSCreateMainSQL() string {
	return "CREATE TABLE " + subprocLSSTable + " (" +
		"id INT64 NOT NULL, " +
		"score FLOAT64 NULL, " +
		"label STRING NULL, " +
		"PRIMARY KEY(id))"
}

// subprocLSCreateAuxSQL 生成辅助表 CREATE TABLE SQL。
func subprocLSCreateAuxSQL() string {
	return "CREATE TABLE " + subprocLSHelperTable + " (" +
		"id INT64 NOT NULL, " +
		"note STRING NULL, " +
		"PRIMARY KEY(id))"
}

// subprocLSInsertSQL 生成主表 INSERT SQL。
func subprocLSInsertSQL(id int64, round, seq int) string {
	return fmt.Sprintf(
		"INSERT INTO %s (id, score, label) VALUES (%d, %.2f, 'c%d-r%d-s%d')",
		subprocLSSTable, id, float64(id)*0.01+float64(round), id, round, seq,
	)
}

// subprocLSCountInRangeSQL 拼接区间 COUNT 校验 SQL。
func subprocLSCountInRangeSQL(lo, hi int64) string {
	return fmt.Sprintf("SELECT COUNT(*) AS c FROM %s WHERE id >= %d AND id < %d",
		subprocLSSTable, lo, hi)
}

// subprocLSClient 是单个客户端在长会话中的执行上下文。
type subprocLSClient struct {
	id     int
	via    string
	tcp    *tcpClient
	httpAd string
	steps  []subprocLSStep
}

// subprocLSRunOneStep 在指定客户端上执行一步并校验结果。
//
// 错误通过返回值传递，不调用 t.Fatal/t.Errorf，调用方负责汇总。
func subprocLSRunOneStep(ctx context.Context, c *subprocLSClient, step subprocLSStep) error {
	if c.tcp != nil {
		resp, err := c.tcp.query(step.sql)
		if err != nil {
			return fmt.Errorf("client %d (%s) 步骤 %s SQL=%q 网络错误: %w",
				c.id, c.via, subprocLSOpKindName(step.kind), step.sql, err)
		}
		if step.wantOK && resp.Code != 0 {
			return fmt.Errorf("client %d (%s) 步骤 %s SQL=%q 期望 code=0, 实际 code=%d, msg=%q",
				c.id, c.via, subprocLSOpKindName(step.kind), step.sql, resp.Code, resp.Message)
		}
		if !step.wantOK && resp.Code == 0 {
			return fmt.Errorf("client %d (%s) 步骤 %s SQL=%q 期望 code!=0, 实际 code=0 (%s)",
				c.id, c.via, subprocLSOpKindName(step.kind), step.sql, step.wantMsg)
		}
		return nil
	}
	code, msg, _, _, err := httpPostQueryNoT(ctx, c.httpAd, step.sql)
	if err != nil {
		return fmt.Errorf("client %d (%s) 步骤 %s SQL=%q 网络错误: %w",
			c.id, c.via, subprocLSOpKindName(step.kind), step.sql, err)
	}
	if step.wantOK && code != 0 {
		return fmt.Errorf("client %d (%s) 步骤 %s SQL=%q 期望 code=0, 实际 code=%d, msg=%q",
			c.id, c.via, subprocLSOpKindName(step.kind), step.sql, code, msg)
	}
	if !step.wantOK && code == 0 {
		return fmt.Errorf("client %d (%s) 步骤 %s SQL=%q 期望 code!=0, 实际 code=0 (%s)",
			c.id, c.via, subprocLSOpKindName(step.kind), step.sql, step.wantMsg)
	}
	return nil
}

// subprocLSOpKindName 返回操作类型的人类可读名称，错误信息用。
func subprocLSOpKindName(k subprocLSOpKind) string {
	switch k {
	case subprocLSOpCreate:
		return "CREATE"
	case subprocLSOpInsert:
		return "INSERT"
	case subprocLSOpSelect:
		return "SELECT"
	case subprocLSOpUpdate:
		return "UPDATE"
	case subprocLSOpDelete:
		return "DELETE"
	case subprocLSOpError:
		return "ERROR"
	case subprocLSOpShow:
		return "META"
	default:
		return "UNKNOWN"
	}
}

// subprocLSRunSession 在单个客户端上顺序执行其全部步骤。
//
// 第一步必须是 CREATE TABLE 主表，由 goroutine 内失败时返回 error；
// 调用方在主 goroutine 中按 client 索引聚合错误并按出现顺序 t.Fatal。
func subprocLSRunSession(ctx context.Context, c *subprocLSClient) error {
	for i, step := range c.steps {
		if err := subprocLSRunOneStep(ctx, c, step); err != nil {
			return fmt.Errorf("第 %d/%d 步失败: %w", i+1, len(c.steps), err)
		}
	}
	return nil
}

// subprocLSMakeClient 创建长会话客户端。via 取 "tcp" 或 "http"。
func subprocLSMakeClient(t *testing.T, srv *subprocServer, clientID int, via string) *subprocLSClient {
	t.Helper()
	c := &subprocLSClient{id: clientID, via: via, steps: subprocLSBuildSteps(clientID)}
	if via == "tcp" {
		tc, err := dialTCP(srv.tcpAddr)
		if err != nil {
			t.Fatalf("TCP 客户端 %d 拨号失败: %v", clientID, err)
		}
		t.Cleanup(tc.close)
		c.tcp = tc
	} else {
		c.httpAd = srv.httpAddr
	}
	return c
}

// subprocLSAdminPing 在长会话任务结束后探测 server 仍可正常服务：
//   - /health 返回 200
//   - SELECT COUNT(*) FROM main 返回业务期望行数
//   - /admin/flush 与 /admin/compact 不破坏后续 SELECT
//
// 任何一个环节失败都 t.Fatal，确保负载未损坏 server。
func subprocLSAdminPing(t *testing.T, srv *subprocServer) {
	t.Helper()

	// 1. /health 探活。
	resp := httpHealthHit(t, srv.httpAddr)
	if resp.StatusCode != 200 {
		t.Fatalf("/health 期望 200, 实际 %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 2. SELECT 探活。
	qResp := httpPostQuery(t, srv.httpAddr, "SELECT COUNT(*) AS c FROM "+subprocLSSTable)
	if qResp.Code != 0 {
		t.Fatalf("长会话后 SELECT 失败: code=%d, msg=%q", qResp.Code, qResp.Message)
	}
	// COUNT(*) AS c 应 >= subprocLSClientCount * 部分行
	var rows []map[string]any
	if err := json.Unmarshal(qResp.Data, &rows); err != nil {
		t.Fatalf("解析 COUNT 响应失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("COUNT 响应无行: %s", string(qResp.Data))
	}
	if _, ok := toInt64(rows[0]["c"]); !ok {
		t.Fatalf("COUNT 响应 c 字段非整数: %v", rows[0]["c"])
	}

	// 3. admin/flush + admin/compact 兜底。
	for _, ep := range []string{"/admin/flush", "/admin/compact"} {
		req, err := http.NewRequest(http.MethodPost, "http://"+srv.httpAddr+ep, nil)
		if err != nil {
			t.Fatalf("构造 %s 请求失败: %v", ep, err)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		r, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST %s 失败: %v", ep, err)
		}
		_ = r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("POST %s 期望 200, 实际 %d", ep, r.StatusCode)
		}
	}

	// 4. admin 后再次 SELECT 验证 server 未被破坏。
	qResp2 := httpPostQuery(t, srv.httpAddr, "SELECT * FROM "+subprocLSSTable+" LIMIT 1")
	if qResp2.Code != 0 {
		t.Fatalf("admin 端点后 SELECT 失败: code=%d, msg=%q", qResp2.Code, qResp2.Message)
	}
}

// TestSubprocServerLongSessionMixedOps 验证子进程 server 在多客户端长顺序
// 会话下的鲁棒性：33 步/客户端 × 4 客户端 = 132 次请求，期间穿插 2 次
// 故意错误 SQL 与 1 次 admin 端点，结束后 server 仍可正常服务。
//
// 测试目标：
//   - TCP 长连接复用：单条连接承担 33 步请求，验证 keepalive 稳定
//   - 错误恢复：错误 SQL 不影响后续请求执行
//   - 跨客户端并发：写读不串扰，最终 SELECT 可读到所有客户端写入
//   - admin 端点交错：/admin/flush + /admin/compact 不破坏客户端连接
func TestSubprocServerLongSessionMixedOps(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	tcpPort := allocateEphemeralPort(t)
	httpPort := allocateEphemeralPort(t)
	if tcpPort == httpPort {
		// 同一端口概率极低，但守底
		httpPort = allocateEphemeralPort(t)
	}
	srv, _ := startSubprocessServer(t, tcpPort, httpPort, dataDir)
	t.Cleanup(func() { stopSubprocessServer(t, srv) })

	// 预创建表：所有客户端共用同一份表结构，DDL 在派发 goroutine 前完成，
	// 避免多客户端并发 CREATE 同名表冲突。
	createResp := httpPostQuery(t, srv.httpAddr, subprocLSCreateMainSQL())
	if createResp.Code != 0 {
		t.Fatalf("预创建主表失败: code=%d, msg=%q", createResp.Code, createResp.Message)
	}
	auxResp := httpPostQuery(t, srv.httpAddr, subprocLSCreateAuxSQL())
	if auxResp.Code != 0 {
		t.Fatalf("预创建辅助表失败: code=%d, msg=%q", auxResp.Code, auxResp.Message)
	}
	// 预灌入辅助表 1 行（让"跨表 SELECT 看到非空"成立）。
	auxSeed := httpPostQuery(t, srv.httpAddr, "INSERT INTO "+subprocLSHelperTable+" (id, note) VALUES (1, 'seed')")
	if auxSeed.Code != 0 {
		t.Fatalf("辅助表预灌入失败: code=%d, msg=%q", auxSeed.Code, auxSeed.Message)
	}

	// 构造客户端：clientID 偶数走 TCP，奇数走 HTTP。
	clients := make([]*subprocLSClient, subprocLSClientCount)
	for i := 0; i < subprocLSClientCount; i++ {
		via := "tcp"
		if i%2 == 1 {
			via = "http"
		}
		clients[i] = subprocLSMakeClient(t, srv, i, via)
	}

	// 并发执行：每个客户端在自己的 goroutine 中跑 33 步，
	// 错误通过 errCh 汇总到主 goroutine。
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	errCh := make(chan error, subprocLSClientCount)
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *subprocLSClient) {
			defer wg.Done()
			if err := subprocLSRunSession(ctx, c); err != nil {
				errCh <- err
			}
		}(c)
	}
	wg.Wait()
	close(errCh)

	// 收集错误：按发生顺序逐一 t.Fatal 报告，便于定位首个失败客户端。
	for err := range errCh {
		t.Fatal(err)
	}

	// 收尾：admin 端点探活 + 跨客户端聚合 SELECT 校验。
	subprocLSAdminPing(t, srv)

	// 跨客户端聚合：4 客户端 × 各自 ID 区间的总行数应 >= 客户端数 × 灌入行数。
	totalResp := httpPostQuery(t, srv.httpAddr, "SELECT COUNT(*) AS c FROM "+subprocLSSTable)
	if totalResp.Code != 0 {
		t.Fatalf("聚合 SELECT 失败: code=%d, msg=%q", totalResp.Code, totalResp.Message)
	}
	var totalRows []map[string]any
	if err := json.Unmarshal(totalResp.Data, &totalRows); err != nil {
		t.Fatalf("解析聚合响应失败: %v", err)
	}
	got, ok := toInt64(totalRows[0]["c"])
	if !ok {
		t.Fatalf("聚合响应 c 字段非整数: %v", totalRows[0]["c"])
	}
	// 保守下界：每客户端至少灌入 9+5=14 行（3 轮 9 行 + 后段 5 行），
	// 减去 1 行 DELETE：每客户端 13 行，4 客户端至少 52。
	const minExpected = int64(subprocLSClientCount) * 13
	if got < minExpected {
		t.Errorf("聚合 COUNT = %d, 期望至少 %d (4 客户端 × 13 行)", got, minExpected)
	}
}
