// Package integration 端到端集成测试：子进程 server + 多客户端"会话式"一般 SQL 混合工作负载。
//
// 本文件补齐既有测试未充分覆盖的"会话维度"组合：把 cmd/server 编译为真实子进程拉起，
// 通过 TCP 长连接与 HTTP 短连接混合的多客户端，每个客户端在专属表上顺序执行一组
// "类应用会话"的通用 SQL（DDL + 批量 DML + 多形式 DQL + UPDATE/DELETE），端到端
// 验证：跨协议一致性 / 并发隔离 / 聚合正确性 / 错误响应优雅。
//
// 与既有测试的区别：
//   - e2e_subproc_general_sql_multiclient_test.go：4 客户端同写 1 表，侧重跨协议一致性。
//   - e2e_subproc_http_keepalive_test.go：单客户端长生命周期 HTTP 持久连接，侧重 keep-alive。
//   - e2e_subproc_mixed_engine_multiclient_test.go：多客户端混用 LSM/memory 引擎。
//
// 本文件是第一份"子进程 server + 多客户端 + 每客户端独立表 + 完整 SQL 会话 +
// 最终跨协议一致性"组合的端到端测试。
//
// 注：仅使用当前 parser 已支持的 SQL 语义。不支持：BETWEEN / IN / IS NULL /
// ORDER BY DESC。
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	mssessClients    = 8
	mssessRowsPerCli = 10
	mssessRounds     = 2
	mssessIDBase     = 7_000_000
	mssessTableFmt   = "mssess_sess_t%d"
	mssessHTTPTO     = 30 * time.Second
)

// mssessTableName 返回 worker 的独立表名。
func mssessTableName(workerID int) string {
	return fmt.Sprintf(mssessTableFmt, workerID)
}

// mssessClientIDRange 返回 worker 的 ID 区间 [lo, hi)。
func mssessClientIDRange(workerID int) (lo, hi int64) {
	lo = int64(mssessIDBase + workerID*mssessRowsPerCli)
	hi = lo + int64(mssessRowsPerCli)
	return
}

// mssessCreateSQL 返回 (table) 对应的建表 DDL。
func mssessCreateSQL(table string) string {
	return fmt.Sprintf(
		"CREATE TABLE %s ("+
			"id INT64 NOT NULL, "+
			"name STRING NULL, "+
			"val FLOAT64 NULL, "+
			"active BOOL NULL, "+
			"label STRING NULL, "+
			"PRIMARY KEY(id))",
		table)
}

// mssessInsertSQL 返回 (table, id, name, val, active, label) 对应的单行 INSERT。
//
// valStr 形如 "1.5" 或 "NULL"；activeStr 形如 "true" / "false" / "NULL"。
func mssessInsertSQL(table string, id int64, name, valStr, activeStr, label string) string {
	return fmt.Sprintf(
		"INSERT INTO %s (id, name, val, active, label) VALUES (%d, '%s', %s, %s, '%s')",
		table, id, name, valStr, activeStr, label)
}

// mssessInitialRow 描述一行 INSERT 数据。每 4 行：1 行 val=NULL，3 行 val 非空；
// active 与 val 配合偶数 = true，奇数 = false。label 在 3 个低基数间循环，便于 GROUP BY。
func mssessInitialRow(workerID, seq int) (id int64, name, valStr, activeStr, label string) {
	id = int64(mssessIDBase + workerID*mssessRowsPerCli + seq)
	name = fmt.Sprintf("w%d-r%d", workerID, seq)
	label = fmt.Sprintf("L-%d", seq%3)
	switch seq % 4 {
	case 0:
		valStr = fmt.Sprintf("%.4f", float64(seq)*1.5)
		activeStr = "true"
	case 1:
		valStr = fmt.Sprintf("%.4f", float64(seq)*2.0)
		activeStr = "false"
	case 2:
		valStr = fmt.Sprintf("%.4f", float64(seq)*0.75)
		activeStr = "true"
	case 3:
		valStr = "NULL"
		activeStr = "false"
	}
	return
}

// mssessPostQueryNoT 通过 HTTP POST /query 执行单条 SQL。
func mssessPostQueryNoT(ctx context.Context, client *http.Client, addr, sql string) (int, string, int, json.RawMessage, error) {
	body, _ := json.Marshal(map[string]string{"sql": sql})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+addr+"/query", &jsonReader{b: body})
	if err != nil {
		return -1, "", 0, nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return -1, "", 0, nil, fmt.Errorf("POST /query 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, "", 0, nil, fmt.Errorf("读取 /query 响应失败: %w", err)
	}
	var out serverQueryResp
	if err := json.Unmarshal(data, &out); err != nil {
		return -1, "", 0, nil, fmt.Errorf("解析 /query 响应失败: %w (raw: %s)", err, data)
	}
	return out.Code, out.Message, out.Rows, out.Data, nil
}

// mssessPostQueryRows 包装 mssessPostQueryNoT：code != 0 时返回 error。
func mssessPostQueryRows(ctx context.Context, client *http.Client, addr, sql string) (int, json.RawMessage, error) {
	code, msg, rows, data, err := mssessPostQueryNoT(ctx, client, addr, sql)
	if err != nil {
		return 0, nil, err
	}
	if code != 0 {
		return 0, data, fmt.Errorf("/query 业务码 %d: %s [sql=%s]", code, msg, sql)
	}
	return rows, data, nil
}

// mssessExtractCountJSON 从 SELECT COUNT(*) 响应中提取整数结果。
func mssessExtractCountJSON(data json.RawMessage) int64 {
	if len(data) == 0 || string(data) == "null" {
		return -1
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return -1
	}
	for _, v := range rows[0] {
		if n, ok := toInt64(v); ok {
			return n
		}
	}
	return -1
}

// mssessExtractCountAny 从 TCP 响应 Data 字段（[]any）中提取 COUNT 整数。
func mssessExtractCountAny(data any) int64 {
	if data == nil {
		return -1
	}
	rows, ok := data.([]any)
	if !ok || len(rows) == 0 {
		return -1
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		return -1
	}
	for _, v := range row {
		if n, ok := toInt64(v); ok {
			return n
		}
	}
	return -1
}

// mssessQueryCases 列出每张表在会话中应跑的多形式 DQL 与期望行数。
//
// 行数期望与 mssessInitialRow 的生成规则保持一致：seq=0/2/4/6/8 active=true，
// seq=1/3/5/7/9 active=false；seq=3/7 val=NULL；seq=0 val=0；其余 val>0。
// 所有 WHERE 子句均在 worker 的 [lo, hi) 区间内，以避免跨 worker 干扰。
func mssessQueryCases(table string, workerID int) []struct {
	sql  string
	want int
} {
	lo, hi := mssessClientIDRange(workerID)
	return []struct {
		sql  string
		want int
	}{
		{fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", table), 1},
		{fmt.Sprintf("SELECT COUNT(*) AS c FROM %s WHERE id >= %d AND id < %d", table, lo, hi), 1},
		{fmt.Sprintf("SELECT * FROM %s WHERE id = %d", table, lo), 1}, // PK 等值
		{fmt.Sprintf("SELECT * FROM %s WHERE name LIKE 'w%d-%%'", table, workerID), mssessRowsPerCli},
		{fmt.Sprintf("SELECT * FROM %s WHERE active = true AND id >= %d", table, lo), 5}, // seq=0,2,4,6,8
		{fmt.Sprintf("SELECT * FROM %s WHERE val > 0 ORDER BY id", table), 7},            // 排除 seq=0/3/7
		{fmt.Sprintf("SELECT * FROM %s ORDER BY id LIMIT 3", table), 3},                  // LIMIT 3
		{fmt.Sprintf("SELECT active, COUNT(*) AS c FROM %s GROUP BY active", table), 2},  // true / false
		{fmt.Sprintf("SELECT MIN(val) AS m, MAX(val) AS x FROM %s", table), 1},
	}
}

// mssessRunHTTPSession 跑完一个 HTTP worker 的全部会话式 SQL。
func mssessRunHTTPSession(ctx context.Context, addr string, workerID int, errCh chan<- error) {
	client := &http.Client{Timeout: mssessHTTPTO}
	defer client.CloseIdleConnections()
	table := mssessTableName(workerID)

	if code, msg, _, _, err := mssessPostQueryNoT(ctx, client, addr, mssessCreateSQL(table)); err != nil {
		errCh <- fmt.Errorf("http worker %d CREATE: %w", workerID, err)
		return
	} else if code != 0 {
		errCh <- fmt.Errorf("http worker %d CREATE 业务失败: %s", workerID, msg)
		return
	}

	for seq := 0; seq < mssessRowsPerCli; seq++ {
		id, name, valStr, activeStr, label := mssessInitialRow(workerID, seq)
		sql := mssessInsertSQL(table, id, name, valStr, activeStr, label)
		if code, msg, _, _, err := mssessPostQueryNoT(ctx, client, addr, sql); err != nil {
			errCh <- fmt.Errorf("http worker %d INSERT seq=%d: %w", workerID, seq, err)
			return
		} else if code != 0 {
			errCh <- fmt.Errorf("http worker %d INSERT seq=%d 业务失败: %s", workerID, seq, msg)
			return
		}
	}

	if !mssessRunHTTPQueries(ctx, client, addr, workerID, errCh) {
		return
	}
	if !mssessRunHTTPUpdates(ctx, client, addr, workerID, errCh) {
		return
	}
	if !mssessRunHTTPDelete(ctx, client, addr, workerID, errCh) {
		return
	}
	if !mssessVerifyHTTPFinalState(ctx, client, addr, workerID, errCh) {
		return
	}
}

// mssessRunHTTPQueries 在 HTTP worker 的表上跑多形式 DQL，验证返回行数。
//
// 返回 true 表示全部成功，false 表示已通过 errCh 上报错误。
func mssessRunHTTPQueries(ctx context.Context, client *http.Client, addr string, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	for i, tc := range mssessQueryCases(table, workerID) {
		rows, data, err := mssessPostQueryRows(ctx, client, addr, tc.sql)
		if err != nil {
			errCh <- fmt.Errorf("http worker %d Q#%d 失败: %w", workerID, i, err)
			return false
		}
		if rows != tc.want {
			errCh <- fmt.Errorf("http worker %d Q#%d rows=%d, 期望 %d (sql=%s, data=%s)",
				workerID, i, rows, tc.want, tc.sql, string(data))
			return false
		}
	}
	return true
}

// mssessRunHTTPUpdates 在 HTTP worker 表上跑 mssessRounds 轮 UPDATE 累加 val。
func mssessRunHTTPUpdates(ctx context.Context, client *http.Client, addr string, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	lo, _ := mssessClientIDRange(workerID)
	for round := 0; round < mssessRounds; round++ {
		targetID := lo + int64(round)
		delta := float64(round+1) * 0.1
		sql := fmt.Sprintf("UPDATE %s SET val = val + %.4f WHERE id = %d", table, delta, targetID)
		rows, _, err := mssessPostQueryRows(ctx, client, addr, sql)
		if err != nil {
			errCh <- fmt.Errorf("http worker %d UPD round=%d: %w", workerID, round, err)
			return false
		}
		if rows != 1 {
			errCh <- fmt.Errorf("http worker %d UPD round=%d rows=%d, 期望 1", workerID, round, rows)
			return false
		}
	}
	return true
}

// mssessRunHTTPDelete 在 HTTP worker 表上删除 1 行（id=lo+mssessRounds）。
func mssessRunHTTPDelete(ctx context.Context, client *http.Client, addr string, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	lo, _ := mssessClientIDRange(workerID)
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = %d", table, lo+int64(mssessRounds))
	rows, _, err := mssessPostQueryRows(ctx, client, addr, sql)
	if err != nil {
		errCh <- fmt.Errorf("http worker %d DELETE: %w", workerID, err)
		return false
	}
	if rows != 1 {
		errCh <- fmt.Errorf("http worker %d DELETE rows=%d, 期望 1", workerID, rows)
		return false
	}
	return true
}

// mssessVerifyHTTPFinalState 校验 HTTP worker 表的最终行数 = mssessRowsPerCli - 1。
func mssessVerifyHTTPFinalState(ctx context.Context, client *http.Client, addr string, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	sql := fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", table)
	rows, data, err := mssessPostQueryRows(ctx, client, addr, sql)
	if err != nil {
		errCh <- fmt.Errorf("http worker %d 最终 COUNT: %w", workerID, err)
		return false
	}
	if rows != 1 {
		errCh <- fmt.Errorf("http worker %d 最终 COUNT rows=%d, 期望 1 (data=%s)", workerID, rows, string(data))
		return false
	}
	if cnt := mssessExtractCountJSON(data); cnt != int64(mssessRowsPerCli-1) {
		errCh <- fmt.Errorf("http worker %d 最终 COUNT = %d, 期望 %d", workerID, cnt, mssessRowsPerCli-1)
		return false
	}
	return true
}

// mssessRunTCPSession 跑完一个 TCP worker 的全部会话式 SQL。
func mssessRunTCPSession(ctx context.Context, addr string, workerID int, errCh chan<- error) {
	tc, err := dialTCP(addr)
	if err != nil {
		errCh <- fmt.Errorf("tcp worker %d 拨号失败: %w", workerID, err)
		return
	}
	defer tc.close()
	table := mssessTableName(workerID)

	if resp, qerr := tc.query(mssessCreateSQL(table)); qerr != nil {
		errCh <- fmt.Errorf("tcp worker %d CREATE: %w", workerID, qerr)
		return
	} else if resp.Code != 0 {
		errCh <- fmt.Errorf("tcp worker %d CREATE 业务失败: %s", workerID, resp.Message)
		return
	}

	for seq := 0; seq < mssessRowsPerCli; seq++ {
		id, name, valStr, activeStr, label := mssessInitialRow(workerID, seq)
		sql := mssessInsertSQL(table, id, name, valStr, activeStr, label)
		if resp, qerr := tc.query(sql); qerr != nil {
			errCh <- fmt.Errorf("tcp worker %d INSERT seq=%d: %w", workerID, seq, qerr)
			return
		} else if resp.Code != 0 {
			errCh <- fmt.Errorf("tcp worker %d INSERT seq=%d 业务失败: %s", workerID, seq, resp.Message)
			return
		} else if resp.Rows != 1 {
			errCh <- fmt.Errorf("tcp worker %d INSERT seq=%d rows=%d, 期望 1", workerID, seq, resp.Rows)
			return
		}
	}

	if !mssessRunTCPQueries(ctx, tc, workerID, errCh) {
		return
	}
	if !mssessRunTCPUpdates(ctx, tc, workerID, errCh) {
		return
	}
	if !mssessRunTCPDelete(ctx, tc, workerID, errCh) {
		return
	}
	if !mssessVerifyTCPFinalState(ctx, tc, workerID, errCh) {
		return
	}
}

// mssessRunTCPQueries 在 TCP worker 的表上跑多形式 DQL。
func mssessRunTCPQueries(ctx context.Context, tc *tcpClient, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	for i, tc2 := range mssessQueryCases(table, workerID) {
		if err := ctx.Err(); err != nil {
			errCh <- fmt.Errorf("tcp worker %d Q#%d ctx: %w", workerID, i, err)
			return false
		}
		resp, qerr := tc.query(tc2.sql)
		if qerr != nil {
			errCh <- fmt.Errorf("tcp worker %d Q#%d: %w", workerID, i, qerr)
			return false
		}
		if resp.Code != 0 {
			errCh <- fmt.Errorf("tcp worker %d Q#%d 业务失败: %s", workerID, i, resp.Message)
			return false
		}
		if resp.Rows != tc2.want {
			errCh <- fmt.Errorf("tcp worker %d Q#%d rows=%d, 期望 %d (data=%v)",
				workerID, i, resp.Rows, tc2.want, resp.Data)
			return false
		}
	}
	return true
}

// mssessRunTCPUpdates 在 TCP worker 表上跑 mssessRounds 轮 UPDATE 累加 val。
func mssessRunTCPUpdates(_ context.Context, tc *tcpClient, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	lo, _ := mssessClientIDRange(workerID)
	for round := 0; round < mssessRounds; round++ {
		targetID := lo + int64(round)
		delta := float64(round+1) * 0.1
		sql := fmt.Sprintf("UPDATE %s SET val = val + %.4f WHERE id = %d", table, delta, targetID)
		resp, qerr := tc.query(sql)
		if qerr != nil {
			errCh <- fmt.Errorf("tcp worker %d UPD round=%d: %w", workerID, round, qerr)
			return false
		}
		if resp.Code != 0 || resp.Rows != 1 {
			errCh <- fmt.Errorf("tcp worker %d UPD round=%d code=%d rows=%d msg=%s",
				workerID, round, resp.Code, resp.Rows, resp.Message)
			return false
		}
	}
	return true
}

// mssessRunTCPDelete 在 TCP worker 表上删除 1 行。
func mssessRunTCPDelete(_ context.Context, tc *tcpClient, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	lo, _ := mssessClientIDRange(workerID)
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = %d", table, lo+int64(mssessRounds))
	resp, qerr := tc.query(sql)
	if qerr != nil {
		errCh <- fmt.Errorf("tcp worker %d DELETE: %w", workerID, qerr)
		return false
	}
	if resp.Code != 0 || resp.Rows != 1 {
		errCh <- fmt.Errorf("tcp worker %d DELETE code=%d rows=%d msg=%s",
			workerID, resp.Code, resp.Rows, resp.Message)
		return false
	}
	return true
}

// mssessVerifyTCPFinalState 校验 TCP worker 表的最终行数 = mssessRowsPerCli - 1。
func mssessVerifyTCPFinalState(_ context.Context, tc *tcpClient, workerID int, errCh chan<- error) bool {
	table := mssessTableName(workerID)
	sql := fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", table)
	resp, qerr := tc.query(sql)
	if qerr != nil {
		errCh <- fmt.Errorf("tcp worker %d 最终 COUNT: %w", workerID, qerr)
		return false
	}
	if resp.Code != 0 {
		errCh <- fmt.Errorf("tcp worker %d 最终 COUNT 业务失败: %s", workerID, resp.Message)
		return false
	}
	if resp.Rows != 1 {
		errCh <- fmt.Errorf("tcp worker %d 最终 COUNT rows=%d, 期望 1 (data=%v)",
			workerID, resp.Rows, resp.Data)
		return false
	}
	if cnt := mssessExtractCountAny(resp.Data); cnt != int64(mssessRowsPerCli-1) {
		errCh <- fmt.Errorf("tcp worker %d 最终 COUNT = %d, 期望 %d", workerID, cnt, mssessRowsPerCli-1)
		return false
	}
	return true
}

// mssessRunWorkers 启动 4 TCP + 4 HTTP worker 并等待完成。
func mssessRunWorkers(t *testing.T, s *subprocServer) (errs []error, tcpOK, httpOK int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	errCh := make(chan error, mssessClients*16)
	for i := 0; i < mssessClients; i++ {
		i := i
		isTCP := i%2 == 0
		wg.Add(1)
		go func() {
			defer wg.Done()
			if isTCP {
				mssessRunTCPSession(ctx, s.tcpAddr, i, errCh)
				atomic.AddInt64(&tcpOK, 1)
			} else {
				mssessRunHTTPSession(ctx, s.httpAddr, i, errCh)
				atomic.AddInt64(&httpOK, 1)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		errs = append(errs, e)
	}
	return
}

// mssessCheckErrorPaths 验证子进程对错误 SQL 返回非零 code，且错误注入后连接未坏。
func mssessCheckErrorPaths(t *testing.T, s *subprocServer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := &http.Client{Timeout: mssessHTTPTO}
	defer client.CloseIdleConnections()
	table := mssessTableName(0)
	lo, _ := mssessClientIDRange(0)

	cases := []struct{ name, sql string }{
		{"重复主键", fmt.Sprintf("INSERT INTO %s (id, name) VALUES (%d, 'dup')", table, lo)},
		{"未知列", fmt.Sprintf("SELECT bad_col FROM %s", table)},
		{"错误语法", "THIS IS NOT SQL"},
		{"UPDATE 主键冲突", fmt.Sprintf("UPDATE %s SET id = %d WHERE id = %d", table, lo+1, lo)},
	}
	for _, tc := range cases {
		code, msg, _, _, err := mssessPostQueryNoT(ctx, client, s.httpAddr, tc.sql)
		if err != nil {
			t.Fatalf("错误路径 %q 请求失败: %v", tc.name, err)
		}
		if code == 0 {
			t.Errorf("错误路径 %q 应返回非零 code, 实际为 0 (msg=%s)", tc.name, msg)
		}
	}

	sql := fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", table)
	if code, msg, _, data, err := mssessPostQueryNoT(ctx, client, s.httpAddr, sql); err != nil {
		t.Fatalf("错误注入后正常查询失败: %v", err)
	} else if code != 0 {
		t.Fatalf("错误注入后正常查询业务失败: %s", msg)
	} else if got := mssessExtractCountJSON(data); got != int64(mssessRowsPerCli-1) {
		t.Errorf("错误注入后 COUNT = %d, 期望 %d", got, mssessRowsPerCli-1)
	}
}

// mssessCheckCrossProtocolConsistency 通过 TCP 与 HTTP 各读一次全部表的全集 ID。
func mssessCheckCrossProtocolConsistency(t *testing.T, s *subprocServer) {
	t.Helper()
	expected := mssessExpectedAllIDs()
	sort.Slice(expected, func(i, j int) bool { return expected[i] < expected[j] })

	gotHTTP := mssessReadAllIDsHTTP(t, s.httpAddr)
	if !int64SliceEqual(gotHTTP, expected) {
		t.Errorf("HTTP 跨表 id 集合与期望不一致\n期望: %v\n实际: %v", expected, gotHTTP)
	}
	gotTCP := mssessReadAllIDsTCP(t, s.tcpAddr)
	if !int64SliceEqual(gotTCP, expected) {
		t.Errorf("TCP 跨表 id 集合与期望不一致\n期望: %v\n实际: %v", expected, gotTCP)
	}
}

// mssessExpectedAllIDs 返回所有 worker 写满后再删 1 行后的 id 全集。
func mssessExpectedAllIDs() []int64 {
	out := make([]int64, 0, mssessClients*(mssessRowsPerCli-1))
	for w := 0; w < mssessClients; w++ {
		lo, _ := mssessClientIDRange(w)
		for seq := 0; seq < mssessRowsPerCli; seq++ {
			if seq == mssessRounds {
				continue
			}
			out = append(out, lo+int64(seq))
		}
	}
	return out
}

// mssessReadAllIDsHTTP 通过 HTTP /query 跨表读取所有 id 集合。
func mssessReadAllIDsHTTP(t *testing.T, addr string) []int64 {
	t.Helper()
	client := &http.Client{Timeout: mssessHTTPTO}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out := make([]int64, 0, mssessClients*(mssessRowsPerCli-1))
	for w := 0; w < mssessClients; w++ {
		table := mssessTableName(w)
		sql := fmt.Sprintf("SELECT id FROM %s ORDER BY id", table)
		_, data, err := mssessPostQueryRows(ctx, client, addr, sql)
		if err != nil {
			t.Fatalf("HTTP 读取 %s 失败: %v", table, err)
		}
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatalf("解析 HTTP 响应失败: %v (raw: %s)", err, string(data))
		}
		for _, r := range rows {
			id, ok := toInt64(r["id"])
			if !ok {
				t.Fatalf("id 字段不是整数: %v", r)
			}
			out = append(out, id)
		}
	}
	return out
}

// mssessReadAllIDsTCP 通过 TCP 跨表读取所有 id 集合（每张表一条新连接）。
func mssessReadAllIDsTCP(t *testing.T, addr string) []int64 {
	t.Helper()
	out := make([]int64, 0, mssessClients*(mssessRowsPerCli-1))
	for w := 0; w < mssessClients; w++ {
		tc, err := dialTCP(addr)
		if err != nil {
			t.Fatalf("TCP 拨号失败: %v", err)
		}
		table := mssessTableName(w)
		sql := fmt.Sprintf("SELECT id FROM %s ORDER BY id", table)
		resp, qerr := tc.query(sql)
		tc.close()
		if qerr != nil {
			t.Fatalf("TCP 读取 %s 失败: %v", table, qerr)
		}
		if resp == nil || resp.Code != 0 {
			t.Fatalf("TCP 读取 %s 业务失败: %v", table, resp)
		}
		rows, ok := resp.Data.([]any)
		if !ok {
			t.Fatalf("TCP 响应 Data 不是 []any: %T", resp.Data)
		}
		for _, r := range rows {
			row, ok := r.(map[string]any)
			if !ok {
				t.Fatalf("TCP 行不是 map: %T", r)
			}
			id, ok := toInt64(row["id"])
			if !ok {
				t.Fatalf("id 字段不是整数: %v", row)
			}
			out = append(out, id)
		}
	}
	return out
}

// TestSubprocMixedSQLSessions 主测试：子进程 + 多客户端"会话式"一般 SQL 端到端。
func TestSubprocMixedSQLSessions(t *testing.T) {
	dir := t.TempDir()
	s, log := startSubprocessServer(t,
		allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	t.Cleanup(func() {
		if s != nil {
			stopSubprocessServer(t, s)
		}
		if t.Failed() {
			t.Logf("子进程日志:\n%s", log.String())
		}
	})

	if hp := httpHealthHit(t, s.httpAddr); hp.StatusCode != 200 {
		_ = hp.Body.Close()
		t.Fatalf("/health 状态码 = %d, want 200", hp.StatusCode)
	} else {
		_ = hp.Body.Close()
	}

	errs, tcpOK, httpOK := mssessRunWorkers(t, s)
	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("worker 错误: %v", e)
		}
		t.FailNow()
	}
	if got := atomic.LoadInt64(&tcpOK); got != int64(mssessClients/2) {
		t.Errorf("TCP worker 完成数 = %d, 期望 %d", got, mssessClients/2)
	}
	if got := atomic.LoadInt64(&httpOK); got != int64(mssessClients/2) {
		t.Errorf("HTTP worker 完成数 = %d, 期望 %d", got, mssessClients/2)
	}

	mssessCheckCrossProtocolConsistency(t, s)
	mssessCheckErrorPaths(t, s)

	sendSignalToSubprocess(t, s, syscall.SIGTERM)
	code, err := waitForSubprocessExit(t, s, subprocStopTimeout)
	if err != nil && code != 0 {
		t.Errorf("子进程退出码 = %d, 期望 0; err = %v", code, err)
	}
}

// TestSubprocMixedSQLSessionsAggregates 验证 8 客户端并发写完后，跨表聚合与
// SHOW TABLES 列表与期望一致。
func TestSubprocMixedSQLSessionsAggregates(t *testing.T) {
	dir := t.TempDir()
	s, log := startSubprocessServer(t,
		allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	t.Cleanup(func() {
		if s != nil {
			stopSubprocessServer(t, s)
		}
		if t.Failed() {
			t.Logf("子进程日志:\n%s", log.String())
		}
	})

	errs, _, _ := mssessRunWorkers(t, s)
	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("worker 错误: %v", e)
		}
		t.FailNow()
	}

	wantTotal := int64(mssessClients * (mssessRowsPerCli - 1))
	if gotTotal := mssessCrossTableCountHTTP(t, s.httpAddr); gotTotal != wantTotal {
		t.Errorf("跨表 COUNT = %d, 期望 %d", gotTotal, wantTotal)
	}

	if gotTables := mssessListMSSessTables(t, s.httpAddr); len(gotTables) != mssessClients {
		t.Errorf("SHOW TABLES 包含 %d 张 mssess 表, 期望 %d (tables=%v)",
			len(gotTables), mssessClients, gotTables)
	}

	sendSignalToSubprocess(t, s, syscall.SIGTERM)
	code, err := waitForSubprocessExit(t, s, subprocStopTimeout)
	if err != nil && code != 0 {
		t.Errorf("子进程退出码 = %d, 期望 0; err = %v", code, err)
	}
}

// mssessCrossTableCountHTTP 跨表累加 COUNT(*) 并返回总和。
func mssessCrossTableCountHTTP(t *testing.T, addr string) int64 {
	t.Helper()
	client := &http.Client{Timeout: mssessHTTPTO}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var total int64
	for w := 0; w < mssessClients; w++ {
		table := mssessTableName(w)
		sql := fmt.Sprintf("SELECT COUNT(*) AS c FROM %s", table)
		_, data, err := mssessPostQueryRows(ctx, client, addr, sql)
		if err != nil {
			t.Fatalf("跨表 COUNT %s 失败: %v", table, err)
		}
		c := mssessExtractCountJSON(data)
		if c < 0 {
			t.Fatalf("跨表 COUNT %s 解析失败: %s", table, string(data))
		}
		total += c
	}
	return total
}

// mssessListMSSessTables 通过 SHOW TABLES 列出所有 mssess_sess_t* 表名。
func mssessListMSSessTables(t *testing.T, addr string) []string {
	t.Helper()
	client := &http.Client{Timeout: mssessHTTPTO}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := mssessPostQueryRows(ctx, client, addr, "SHOW TABLES")
	if err != nil {
		t.Fatalf("SHOW TABLES 失败: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("解析 SHOW TABLES 失败: %v (raw: %s)", err, string(data))
	}
	out := make([]string, 0, len(rows))
	prefix := "mssess_sess_t"
	for _, r := range rows {
		name, _ := r["table"].(string)
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// jsonReader 是 io.Reader 的最小实现，避免在 import 列表中暴露 bytes。
type jsonReader struct {
	b []byte
	i int
}

func (r *jsonReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
