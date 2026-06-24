// Package integration 端到端集成测试：子进程 server + 多客户端 + /admin/stats 一致性。
//
// 本文件补齐既有测试未覆盖的「多客户端并发 + /admin/stats 端点」组合：把
// cmd/server 编译为子进程拉起，使用 TCP + HTTP 多客户端在多张表上并发执行
// DML（INSERT/UPDATE），随后通过真实 HTTP 客户端访问 GET /admin/stats，
// 验证：
//
//  1. /admin/stats 端点在子进程 server 模式下可访问、返回 200
//  2. 顶层 summary（total_tables/total_rows）与并发工作负载结果一致
//  3. 每张表的 row_count 与该表实际写入行数一致
//  4. 跨协议（TCP + HTTP）多客户端并发写入后，统计量精确（无重复计数、无丢失）
//  5. /admin/flush 触发后，summary/tables 仍然正确
//
// 与既有测试的区别：
//   - e2e_admin_stats_endpoint_test.go：in-process server，未跨进程、未多客户端
//   - e2e_subproc_general_sql_multiclient_test.go：未访问 /admin/stats
//   - e2e_subproc_long_session_test.go：访问 /admin/flush 但未访问 /admin/stats
//   - e2e_subproc_http_keepalive_test.go：访问 /admin/flush 但未覆盖 stats 一致性
//
// 设计要点：
//  1. 复用 e2e_subproc_smoke_test.go 的 buildSubprocBinary /
//     startSubprocessServer / stopSubprocessServer / allocateEphemeralPort
//     等 helper。
//  2. 并发客户端使用同进程 e2e_server_sql_test.go 的 dialTCP / tcpClient
//     协议（PacketQuery + JSON payload），HTTP 端点直连 /write 与 /admin/*。
//  3. 每客户端使用独立 ID 区间避免主键冲突；最终统计量使用 summary.TotalRows
//     与逐表 RowCount 双断言。
//  4. 失败时打印子进程日志便于诊断（subprocLog.String）。
package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// subprocAdminStats 测试常量。
const (
	subprocASClientCount    = 4              // 并发客户端数（2 TCP + 2 HTTP）
	subprocASRowsPerClient  = 25             // 每客户端写入行数
	subprocASTableA         = "subproc_as_a" // 主表 A
	subprocASTableB         = "subproc_as_b" // 主表 B
	subprocASBaseID         = 900000         // 客户端 ID 起始偏移
	subprocASRequestTimeout = 5 * time.Second
)

// subprocASRow 描述单次 INSERT 的行内容，避免与其它测试的 sensorRows 冲突。
type subprocASRow struct {
	ID   int64   `json:"id"`
	Name string  `json:"name"`
	Val  float64 `json:"val"`
}

// subprocASBuildRows 为 clientID 生成 [base, base+rowsPerClient) 区间的行，
// Name 形如 "client-N-step-M" 便于后续 SELECT 验证写入分布。
func subprocASBuildRows(clientID int) []subprocASRow {
	rows := make([]subprocASRow, 0, subprocASRowsPerClient)
	base := int64(subprocASBaseID) + int64(clientID)*int64(subprocASRowsPerClient)
	for i := 0; i < subprocASRowsPerClient; i++ {
		rows = append(rows, subprocASRow{
			ID:   base + int64(i),
			Name: fmt.Sprintf("client-%d-step-%d", clientID, i),
			Val:  float64(clientID)*100.0 + float64(i),
		})
	}
	return rows
}

// subprocASCreateTables 通过 TCP 客户端执行 CREATE TABLE 建两张 LSM 表。
// TCP 通道覆盖完整的 SQL 解析与执行路径，与 /write 的 HTTP DML 路径形成对比。
func subprocASCreateTables(t *testing.T, srv *subprocServer) {
	t.Helper()
	conn, err := dialTCP(srv.tcpAddr)
	if err != nil {
		t.Fatalf("建表 TCP 拨号失败: %v", err)
	}
	defer conn.close()
	for _, name := range []string{subprocASTableA, subprocASTableB} {
		ddl := "CREATE TABLE " + name +
			" (id INT64 NOT NULL, name STRING NULL, val FLOAT64 NULL, PRIMARY KEY(id))"
		resp, err := conn.query(ddl)
		if err != nil {
			t.Fatalf("建表 %s 失败: %v", name, err)
		}
		if resp.Code != 0 {
			t.Fatalf("建表 %s 业务码 %d: %s", name, resp.Code, resp.Message)
		}
	}
}

// subprocASWriteRowsHTTP 走 HTTP /write 端点写入 rows；worker goroutine 内
// 不调用 t.Fatal/t.Errorf，通过返回值上报错误。
func subprocASWriteRowsHTTP(httpAddr, table string, rows []subprocASRow) error {
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("序列化 rows 失败: %w", err)
	}
	body := fmt.Sprintf(`{"table":%q,"rows":%s}`, table, string(payload))
	req, err := http.NewRequest(http.MethodPost, "http://"+httpAddr+"/write", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造 /write 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: subprocASRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST /write 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/write 状态码 %d, body=%s", resp.StatusCode, string(respBody))
	}
	var decoded struct {
		Code    int    `json:"code"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return fmt.Errorf("解析 /write 响应失败: %w, body=%s", err, string(respBody))
	}
	if decoded.Code != 0 {
		return fmt.Errorf("/write 业务码 %d: %s", decoded.Code, decoded.Message)
	}
	return nil
}

// subprocASWriteRowsTCP 通过 TCP + SQL INSERT 写入 rows；供偶数之外的客户端使用。
// 失败返回 error，由调用 goroutine 汇总到 errCh。
func subprocASWriteRowsTCP(tcpAddr string, tables []string, rows []subprocASRow) error {
	conn, err := dialTCP(tcpAddr)
	if err != nil {
		return fmt.Errorf("TCP 拨号失败: %w", err)
	}
	defer conn.close()
	for _, tbl := range tables {
		for _, r := range rows {
			insertSQL := fmt.Sprintf(
				"INSERT INTO %s (id, name, val) VALUES (%d, %q, %v)",
				tbl, r.ID, r.Name, r.Val,
			)
			resp, err := conn.query(insertSQL)
			if err != nil {
				return fmt.Errorf("INSERT %s 失败 [%s]: %w", tbl, insertSQL, err)
			}
			if resp.Code != 0 {
				return fmt.Errorf("INSERT %s 业务码 %d: %s", tbl, resp.Code, resp.Message)
			}
		}
	}
	return nil
}

// subprocASRunMultiClient 派发 clientCount 个客户端（半数 HTTP、半数 TCP），
// 每个客户端往两张测试表各写 rowsPerClient 行；通过 error channel 汇总失败。
func subprocASRunMultiClient(srv *subprocServer) error {
	tables := []string{subprocASTableA, subprocASTableB}
	errCh := make(chan error, subprocASClientCount)
	var wg sync.WaitGroup
	wg.Add(subprocASClientCount)
	for cid := 0; cid < subprocASClientCount; cid++ {
		clientID := cid
		rows := subprocASBuildRows(clientID)
		go func() {
			defer wg.Done()
			// 偶数客户端走 HTTP /write，奇数客户端走 TCP + SQL INSERT。
			if clientID%2 == 0 {
				for _, tbl := range tables {
					if err := subprocASWriteRowsHTTP(srv.httpAddr, tbl, rows); err != nil {
						errCh <- fmt.Errorf("HTTP client %d 写 %s 失败: %w", clientID, tbl, err)
						return
					}
				}
				return
			}
			if err := subprocASWriteRowsTCP(srv.tcpAddr, tables, rows); err != nil {
				errCh <- fmt.Errorf("TCP client %d 失败: %w", clientID, err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// subprocASFetchStats 通过真实 HTTP 客户端访问 GET /admin/stats 并返回原始 body。
// 失败时返回 error 便于 worker goroutine 上报。
func subprocASFetchStats(httpAddr string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, "http://"+httpAddr+"/admin/stats", nil)
	if err != nil {
		return 0, nil, fmt.Errorf("构造 /admin/stats 请求失败: %w", err)
	}
	client := &http.Client{Timeout: subprocASRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("GET /admin/stats 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, body, fmt.Errorf("读取 /admin/stats 响应失败: %w", err)
	}
	return resp.StatusCode, body, nil
}

// subprocASDecodeStats 解码 /admin/stats 响应 body。
func subprocASDecodeStats(t *testing.T, body []byte) subprocASStatsResponse {
	t.Helper()
	var stats subprocASStatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("解析 /admin/stats 响应失败: %v, body=%s", err, string(body))
	}
	if stats.Code != 0 {
		t.Fatalf("/admin/stats 业务码 = %d, message=%q", stats.Code, stats.Message)
	}
	return stats
}

// subprocASStatsResponse 描述 /admin/stats 响应的解码结构，与
// e2e_admin_stats_endpoint_test.go 中的结构保持一致（独立定义以维持
// 集成测试对 server 内部实现的弱耦合）。
type subprocASStatsResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Summary struct {
		TotalTables   int   `json:"total_tables"`
		LSMTables     int   `json:"lsm_tables"`
		MemoryTables  int   `json:"memory_tables"`
		TotalSegments int   `json:"total_segments"`
		TotalRows     int64 `json:"total_rows"`
	} `json:"summary"`
	Tables []struct {
		Name     string `json:"name"`
		Engine   string `json:"engine"`
		RowCount int64  `json:"row_count"`
	} `json:"tables"`
}

// subprocASAssertTables 断言 stats 中每张测试表的 RowCount == wantRowsPerTable，
// engine == "lsm"。
func subprocASAssertTables(t *testing.T, stats subprocASStatsResponse, wantRowsPerTable int64) {
	t.Helper()
	rowByTable := make(map[string]int64, 2)
	engineByTable := make(map[string]string, 2)
	for _, item := range stats.Tables {
		rowByTable[item.Name] = item.RowCount
		engineByTable[item.Name] = item.Engine
	}
	for _, name := range []string{subprocASTableA, subprocASTableB} {
		got, ok := rowByTable[name]
		if !ok {
			t.Errorf("/admin/stats 缺表 %q; 现有: %v", name, rowByTable)
			continue
		}
		if got != wantRowsPerTable {
			t.Errorf("表 %q RowCount = %d, 期望 %d", name, got, wantRowsPerTable)
		}
		if engineByTable[name] != "lsm" {
			t.Errorf("表 %q engine = %q, 期望 lsm", name, engineByTable[name])
		}
	}
}

// subprocASAssertSummaryAndTables 一次性断言 summary 与 tables 的一致性。
func subprocASAssertSummaryAndTables(t *testing.T, stats subprocASStatsResponse, wantTables int, wantTotalRows, wantRowsPerTable int64) {
	t.Helper()
	if stats.Summary.TotalTables != wantTables {
		t.Errorf("Summary.TotalTables = %d, 期望 %d", stats.Summary.TotalTables, wantTables)
	}
	if stats.Summary.TotalRows != wantTotalRows {
		t.Errorf("Summary.TotalRows = %d, 期望 %d", stats.Summary.TotalRows, wantTotalRows)
	}
	subprocASAssertTables(t, stats, wantRowsPerTable)
}

// subprocASFlushServer 通过 HTTP POST /admin/flush 触发 server 刷盘。
func subprocASFlushServer(t *testing.T, httpAddr string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+httpAddr+"/admin/flush", strings.NewReader(""))
	if err != nil {
		t.Fatalf("构造 /admin/flush 请求失败: %v", err)
	}
	client := &http.Client{Timeout: subprocASRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/flush 失败: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/admin/flush 状态码 = %d", resp.StatusCode)
	}
}

// TestSubprocAdminStatsMultiClient 验证子进程 server 模式下多客户端并发写入
// 后，GET /admin/stats 端点返回的 summary/tables 字段与真实工作负载一致：
//
//  1. 启动子进程 server，建 2 张 LSM 表
//  2. 4 个客户端（2 TCP + 2 HTTP）各写 25 行 × 2 表 = 50 行 / 客户端
//  3. 调用 /admin/stats 验证 summary.total_rows == 200、total_tables == 2
//  4. 验证每张表的 row_count == 100
//  5. 调用 /admin/flush 后再次访问 /admin/stats，统计量不变
func TestSubprocAdminStatsMultiClient(t *testing.T) {
	t.Parallel()
	tcpPort := allocateEphemeralPort(t)
	httpPort := allocateEphemeralPort(t)
	dataDir := t.TempDir()
	srv, _ := startSubprocessServer(t, tcpPort, httpPort, dataDir,
		"-max-memtable", "65536",
	)
	defer stopSubprocessServer(t, srv)

	// 阶段 1：通过 TCP 建 2 张表。
	subprocASCreateTables(t, srv)

	// 阶段 2：4 个客户端并发写入 25 行 × 2 表。
	if err := subprocASRunMultiClient(srv); err != nil {
		t.Fatalf("并发客户端失败: %v", err)
	}

	// 阶段 3：调用 /admin/stats 并断言 summary/tables。
	const wantTotalRows = int64(subprocASClientCount) * int64(subprocASRowsPerClient) * 2
	const wantRowsPerTable = int64(subprocASClientCount) * int64(subprocASRowsPerClient)
	status, body, err := subprocASFetchStats(srv.httpAddr)
	if err != nil {
		t.Fatalf("GET /admin/stats 失败: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("/admin/stats 状态码 = %d, body = %s", status, string(body))
	}
	stats := subprocASDecodeStats(t, body)
	subprocASAssertSummaryAndTables(t, stats, 2, wantTotalRows, wantRowsPerTable)

	// 阶段 4：/admin/flush 后再读一次 stats，统计量应保持。
	subprocASFlushServer(t, srv.httpAddr)
	status2, body2, err := subprocASFetchStats(srv.httpAddr)
	if err != nil {
		t.Fatalf("flush 后 GET /admin/stats 失败: %v", err)
	}
	if status2 != http.StatusOK {
		t.Fatalf("flush 后 /admin/stats 状态码 = %d, body=%s", status2, string(body2))
	}
	stats2 := subprocASDecodeStats(t, body2)
	subprocASAssertSummaryAndTables(t, stats2, 2, wantTotalRows, wantRowsPerTable)
}
