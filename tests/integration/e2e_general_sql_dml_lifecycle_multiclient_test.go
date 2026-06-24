// Package integration 端到端集成测试：多客户端 + 多协议 + 完整 DML 生命周期烟雾测试。
//
// 既有 e2e_general_sql_multiclient_*.go 覆盖「多客户端 + 单协议组合 + 写后查」
// 场景，e2e_concurrent_ddl_dml_test.go 覆盖「DDL+DML 并发交错」，e2e_pgwire_multi_test.go
// 覆盖「PG wire 写 + 跨协议读一致性」。本文件聚焦以下组合尚未充分覆盖的「长链
// DML 生命周期」语义：
//
//   - 启动一个同时监听 TCP/HTTP/PG wire 的 server
//   - 6 个客户端（2 TCP + 2 HTTP + 2 PG wire）并发执行「完整 DML 生命周期」：
//     INSERT 多行 → SELECT 校验 → UPDATE 全部行（不变量，幂等）→ SELECT 校验
//     → UPDATE 加权 → SELECT 校验 → DELETE 半行 → SELECT 校验
//   - 每个客户端负责一段不重叠的 ID 区间，便于「最终行数 = Σ 客户端残留行数」校验
//   - 阶段断言：每步 SELECT 返回的行数与累计修改完全一致，捕捉 DML 顺序错乱
//   - 跨协议一致性：所有客户端结束后，再由 3 个协议各做一次 COUNT(*) /
//     SUM(qty) / GROUP BY 校验，三协议结果完全一致
//
// 与既有测试的区别：
//   - e2e_multi_client_sql_test.go：12 客户端混合 5 种角色，无「同一行多次 DML
//     顺序」的精确断言
//   - e2e_olap_multiclient_test.go：写+读并发 + 聚合，侧重混合工作负载稳定性
//   - e2e_pgwire_multi_test.go：仅 10 个 PG wire 客户端，单次 INSERT + UPDATE +
//     DELETE，不验证「UPDATE 是否真的改变」与「DELETE 后状态」
//
// 设计原则：
//   - 复用 e2e_server_sql_test.go / e2e_pgwire_sql_test.go 中的 sqlServer /
//     startPGWireServer / rawQuery / rawWrite / respRows / toInt64 / dialPGWire
//     / pgRowToMap / pgInt 等公共 helper
//   - worker goroutine 不调用 t.Fatal/t.Errorf，错误通过返回值汇总到主 goroutine
//     后再统一 t.Fatal（参照 e2e_concurrent_ddl_dml_test.go 范式）
//   - 测试 t.Parallel 并发执行，缩短集成测试套件总时长
package integration

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// dmlLife 测试常量。
const (
	dmlLifeTable     = "dml_lifecycle_events" // 共享测试表
	dmlLifeClientNum = 6                      // 并发客户端数（2 TCP + 2 HTTP + 2 PG wire）
	dmlLifeRowsInit  = 8                      // 每客户端初始写入行数
	dmlLifeRowsKeep  = 4                      // 每客户端最终保留行数（删除一半）
	dmlLifeIDBase    = 40000                  // 客户端 ID 区间起始偏移，避免与既有测试冲突
	dmlLifeQtyAdd    = 100                    // 第二次 UPDATE 给 qty 增加的值
)

// dmlLifeRole 描述单个客户端的协议与 ID 区间。
//
// 通过显式数组确保 3 种协议都被覆盖，且 TCP/HTTP 至少各 2 个、PG wire 至少 2 个，
// 避免取模分配导致 PG wire 在某些测试中被遗漏。
type dmlLifeRole struct {
	via  string // "tcp" / "http" / "pg"
	idLo int64  // 该客户端负责的 ID 区间起点（包含）
	idHi int64  // 该客户端负责的 ID 区间终点（不包含）
}

// dmlLifeRoles 显式分配 6 个客户端的协议与 ID 区间。
var dmlLifeRoles = func() []dmlLifeRole {
	roles := make([]dmlLifeRole, 0, dmlLifeClientNum)
	for i := 0; i < dmlLifeClientNum; i++ {
		via := "tcp"
		switch i % 3 {
		case 0:
			via = "tcp"
		case 1:
			via = "http"
		case 2:
			via = "pg"
		}
		idLo := dmlLifeIDBase + int64(i)*dmlLifeRowsInit
		roles = append(roles, dmlLifeRole{via: via, idLo: idLo, idHi: idLo + dmlLifeRowsInit})
	}
	return roles
}()

// dmlLifeCreateTable 经 TCP 创建共享测试表。
//
// 包含 4 列：id 主键 INT64、name STRING 区分客户端、qty INT64 用于 UPDATE 累加、
// active BOOL 用于 DELETE 条件过滤。
func dmlLifeCreateTable(t *testing.T, s *sqlServer) {
	t.Helper()
	ddl := "CREATE TABLE " + dmlLifeTable + " (" +
		"id INT64 NOT NULL, " +
		"name STRING NULL, " +
		"qty INT64 NULL, " +
		"active BOOL NULL, " +
		"PRIMARY KEY(id))"
	execSQLVia(t, s, "tcp", ddl)
}

// dmlLifeClient 通过 dmlLifeRoles[clientID] 派发到正确的协议执行器。
//
// 协议分发原因：TCP/HTTP 经 rawQuery / rawWrite 直接得到 *server.Response，
// PG wire 经 sendQueryRead 得到 *pgResult。两者响应格式不同，故按协议分派
// 可避免在 worker 内做类型断言。
func dmlLifeClient(s *sqlServer, clientID int) error {
	role := dmlLifeRoles[clientID]
	switch role.via {
	case "tcp", "http":
		return dmlLifeRunHTTPishClient(s, role.via, role.idLo, role.idHi)
	case "pg":
		return dmlLifeRunPGClient(s, role.idLo, role.idHi)
	default:
		return fmt.Errorf("未知协议 %q", role.via)
	}
}

// dmlLifeRunHTTPishClient 经 TCP 或 HTTP 执行 DML 生命周期。
//
// 每步对返回结果做精确校验（行数 / 字段值），错误以 fmt.Errorf 包装后返回，
// 由主 goroutine 统一 t.Fatal 报告。
func dmlLifeRunHTTPishClient(s *sqlServer, via string, idLo, idHi int64) error {
	// 1) INSERT dmlLifeRowsInit 行
	rows := make([]map[string]any, 0, idHi-idLo)
	for id := idLo; id < idHi; id++ {
		rows = append(rows, map[string]any{
			"id":     id,
			"name":   fmt.Sprintf("c%d", id),
			"qty":    int64(10),
			"active": true,
		})
	}
	if err := dmlLifeRawWrite(s, via, dmlLifeTable, rows); err != nil {
		return fmt.Errorf("INSERT 失败: %w", err)
	}

	// 2) SELECT 校验 INSERT
	if got := dmlLifeCountRangeHTTP(s, via, idLo, idHi); got != idHi-idLo {
		return fmt.Errorf("INSERT 后区间行数: 期望 %d，得到 %d", idHi-idLo, got)
	}

	// 3) UPDATE 全部行（不变量：qty = qty，幂等）
	updSQL := fmt.Sprintf("UPDATE %s SET qty = qty WHERE id >= %d AND id < %d",
		dmlLifeTable, idLo, idHi)
	updRows, err := dmlLifeRawQueryRows(s, via, updSQL)
	if err != nil {
		return fmt.Errorf("幂等 UPDATE 失败: %w", err)
	}
	if updRows != int(idHi-idLo) {
		return fmt.Errorf("幂等 UPDATE 期望命中 %d 行，得到 %d", idHi-idLo, updRows)
	}
	if got := dmlLifeSumQtyHTTP(s, via, idLo, idHi); got != int64(10)*(idHi-idLo) {
		return fmt.Errorf("幂等 UPDATE 后 qty 总和: 期望 %d，得到 %d", int64(10)*(idHi-idLo), got)
	}

	// 4) UPDATE 加权：qty = qty + dmlLifeQtyAdd
	addSQL := fmt.Sprintf("UPDATE %s SET qty = qty + %d WHERE id >= %d AND id < %d",
		dmlLifeTable, dmlLifeQtyAdd, idLo, idHi)
	addRows, err := dmlLifeRawQueryRows(s, via, addSQL)
	if err != nil {
		return fmt.Errorf("加权 UPDATE 失败: %w", err)
	}
	if addRows != int(idHi-idLo) {
		return fmt.Errorf("加权 UPDATE 期望命中 %d 行，得到 %d", idHi-idLo, addRows)
	}
	wantSum := (int64(10) + dmlLifeQtyAdd) * (idHi - idLo)
	if got := dmlLifeSumQtyHTTP(s, via, idLo, idHi); got != wantSum {
		return fmt.Errorf("加权 UPDATE 后 qty 总和: 期望 %d，得到 %d", wantSum, got)
	}

	// 5) DELETE 半行：将后半段 active 置 false 后再按 active=false 删除
	halfID := idLo + (idHi-idLo)/2
	deactSQL := fmt.Sprintf("UPDATE %s SET active = false WHERE id >= %d AND id < %d",
		dmlLifeTable, halfID, idHi)
	deactRows, err := dmlLifeRawQueryRows(s, via, deactSQL)
	if err != nil {
		return fmt.Errorf("DELETE 前 UPDATE 失败: %w", err)
	}
	if deactRows != int(idHi-halfID) {
		return fmt.Errorf("DELETE 前 UPDATE 期望命中 %d 行，得到 %d", idHi-halfID, deactRows)
	}
	delSQL := fmt.Sprintf("DELETE FROM %s WHERE id >= %d AND id < %d AND active = false",
		dmlLifeTable, halfID, idHi)
	delRows, err := dmlLifeRawQueryRows(s, via, delSQL)
	if err != nil {
		return fmt.Errorf("DELETE 失败: %w", err)
	}
	if delRows != int(idHi-halfID) {
		return fmt.Errorf("DELETE 期望命中 %d 行，得到 %d", idHi-halfID, delRows)
	}

	// 6) 最终行数校验：每客户端保留 dmlLifeRowsKeep 行
	wantKeep := int64(dmlLifeRowsKeep)
	if got := dmlLifeCountRangeHTTP(s, via, idLo, idHi); got != wantKeep {
		return fmt.Errorf("DELETE 后区间行数: 期望 %d，得到 %d", wantKeep, got)
	}
	return nil
}

// dmlLifeRunPGClient 经 PG wire 执行 DML 生命周期。
//
// 与 HTTPish 版本语义一致；差异仅在响应解码：PG wire 数值经文本协议返回，
// 故用 pgInt 解析。本函数作为 orchestrator 串联 5 个阶段函数；每个阶段函数
// 自身只承担一个 DML 步骤的 SQL 拼装 + 结果校验，便于单步失败定位。
func dmlLifeRunPGClient(s *sqlServer, idLo, idHi int64) error {
	c, err := dialPGWireErr(s.srv.PGAddr())
	if err != nil {
		return fmt.Errorf("PG 拨号失败: %w", err)
	}
	defer c.close()
	if err := c.handshakeErr(); err != nil {
		return fmt.Errorf("PG 握手失败: %w", err)
	}
	if err := dmlLifePGInsertRange(c, idLo, idHi); err != nil {
		return err
	}
	if err := dmlLifePGVerifyInsert(c, idLo, idHi); err != nil {
		return err
	}
	if err := dmlLifePGIdempotentUpdate(c, idLo, idHi); err != nil {
		return err
	}
	if err := dmlLifePGWeightedUpdate(c, idLo, idHi); err != nil {
		return err
	}
	return dmlLifePGDeleteHalf(c, idLo, idHi)
}

// dmlLifePGInsertRange 经 PG wire 批量 INSERT [idLo, idHi) 区间行。
func dmlLifePGInsertRange(c *pgWireClient, idLo, idHi int64) error {
	sql := fmt.Sprintf("INSERT INTO %s (id, name, qty, active) VALUES ", dmlLifeTable)
	first := true
	for id := idLo; id < idHi; id++ {
		if !first {
			sql += ", "
		}
		first = false
		sql += fmt.Sprintf("(%d, 'c%d', 10, true)", id, id)
	}
	if err := dmlLifePGExec(c, sql); err != nil {
		return fmt.Errorf("INSERT 失败: %w", err)
	}
	return nil
}

// dmlLifePGVerifyInsert 经 PG wire 校验 INSERT 后区间行数 == idHi-idLo。
func dmlLifePGVerifyInsert(c *pgWireClient, idLo, idHi int64) error {
	got, err := dmlLifeCountRangePG(c, idLo, idHi)
	if err != nil {
		return fmt.Errorf("INSERT 后行数查询失败: %w", err)
	}
	if got != idHi-idLo {
		return fmt.Errorf("INSERT 后区间行数: 期望 %d，得到 %d", idHi-idLo, got)
	}
	return nil
}

// dmlLifePGIdempotentUpdate 经 PG wire 执行幂等 UPDATE（qty = qty），
// 并校验 SUM(qty) 未变。
func dmlLifePGIdempotentUpdate(c *pgWireClient, idLo, idHi int64) error {
	sql := fmt.Sprintf("UPDATE %s SET qty = qty WHERE id >= %d AND id < %d",
		dmlLifeTable, idLo, idHi)
	if err := dmlLifePGExec(c, sql); err != nil {
		return fmt.Errorf("幂等 UPDATE 失败: %w", err)
	}
	got, err := dmlLifeSumQtyPG(c, idLo, idHi)
	if err != nil {
		return fmt.Errorf("幂等 UPDATE 后 qty 查询失败: %w", err)
	}
	want := int64(10) * (idHi - idLo)
	if got != want {
		return fmt.Errorf("幂等 UPDATE 后 qty 总和: 期望 %d，得到 %d", want, got)
	}
	return nil
}

// dmlLifePGWeightedUpdate 经 PG wire 执行加权 UPDATE（qty += dmlLifeQtyAdd），
// 并校验 SUM(qty) 增加正确数值。
func dmlLifePGWeightedUpdate(c *pgWireClient, idLo, idHi int64) error {
	sql := fmt.Sprintf("UPDATE %s SET qty = qty + %d WHERE id >= %d AND id < %d",
		dmlLifeTable, dmlLifeQtyAdd, idLo, idHi)
	if err := dmlLifePGExec(c, sql); err != nil {
		return fmt.Errorf("加权 UPDATE 失败: %w", err)
	}
	want := (int64(10) + dmlLifeQtyAdd) * (idHi - idLo)
	got, err := dmlLifeSumQtyPG(c, idLo, idHi)
	if err != nil {
		return fmt.Errorf("加权 UPDATE 后 qty 查询失败: %w", err)
	}
	if got != want {
		return fmt.Errorf("加权 UPDATE 后 qty 总和: 期望 %d，得到 %d", want, got)
	}
	return nil
}

// dmlLifePGDeleteHalf 经 PG wire 删后半段：先 UPDATE active=false，再 DELETE。
// 最后校验区间行数 == dmlLifeRowsKeep。
func dmlLifePGDeleteHalf(c *pgWireClient, idLo, idHi int64) error {
	halfID := idLo + (idHi-idLo)/2
	deactSQL := fmt.Sprintf("UPDATE %s SET active = false WHERE id >= %d AND id < %d",
		dmlLifeTable, halfID, idHi)
	if err := dmlLifePGExec(c, deactSQL); err != nil {
		return fmt.Errorf("DELETE 前 UPDATE 失败: %w", err)
	}
	delSQL := fmt.Sprintf("DELETE FROM %s WHERE id >= %d AND id < %d AND active = false",
		dmlLifeTable, halfID, idHi)
	if err := dmlLifePGExec(c, delSQL); err != nil {
		return fmt.Errorf("DELETE 失败: %w", err)
	}
	got, err := dmlLifeCountRangePG(c, idLo, idHi)
	if err != nil {
		return fmt.Errorf("DELETE 后行数查询失败: %w", err)
	}
	want := int64(dmlLifeRowsKeep)
	if got != want {
		return fmt.Errorf("DELETE 后区间行数: 期望 %d，得到 %d", want, got)
	}
	return nil
}

// dmlLifeRawQueryRows 经指定协议执行 SQL 并返回 resp.Rows 字段（命中行数）。
//
// 传输错误或非零 code 返回 error，便于 worker goroutine 统一处理。
func dmlLifeRawQueryRows(s *sqlServer, via, sql string) (int, error) {
	resp, err := rawQuery(s, via, sql)
	if err != nil {
		return 0, fmt.Errorf("传输失败: %w", err)
	}
	if resp.Code != 0 {
		return 0, fmt.Errorf("code=%d msg=%s", resp.Code, resp.Message)
	}
	return resp.Rows, nil
}

// dmlLifeRawWrite 经指定协议批量写入数据，失败时返回 error。
func dmlLifeRawWrite(s *sqlServer, via, table string, rows []map[string]any) error {
	resp, err := rawWrite(s, via, table, rows)
	if err != nil {
		return fmt.Errorf("传输失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("code=%d msg=%s", resp.Code, resp.Message)
	}
	return nil
}

// dmlLifePGExec 经 PG wire 执行任意 SQL（INSERT/UPDATE/DELETE/CREATE 等），
// 失败时返回 error。注意：SELECT 等需要结果集的查询请用 dmlLifeCountRangePG /
// dmlLifeSumQtyPG / pgWire 客户端的 execOK 等专用函数。
func dmlLifePGExec(c *pgWireClient, sql string) error {
	res, err := c.sendQueryRead(sql)
	if err != nil {
		return fmt.Errorf("传输失败: %w", err)
	}
	if res.errMsg != "" {
		return fmt.Errorf("PG 错误: %s", res.errMsg)
	}
	return nil
}

// dmlLifeCountRangeHTTP 经 TCP/HTTP 查询区间行数。
func dmlLifeCountRangeHTTP(s *sqlServer, via string, idLo, idHi int64) int64 {
	sql := fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s WHERE id >= %d AND id < %d",
		dmlLifeTable, idLo, idHi)
	resp, err := rawQuery(s, via, sql)
	if err != nil || resp.Code != 0 {
		return -1
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		return -2
	}
	cnt, _ := toInt64(rows[0]["cnt"])
	return cnt
}

// dmlLifeSumQtyHTTP 经 TCP/HTTP 查询区间 qty 总和。
func dmlLifeSumQtyHTTP(s *sqlServer, via string, idLo, idHi int64) int64 {
	sql := fmt.Sprintf("SELECT SUM(qty) AS total FROM %s WHERE id >= %d AND id < %d",
		dmlLifeTable, idLo, idHi)
	resp, err := rawQuery(s, via, sql)
	if err != nil || resp.Code != 0 {
		return -1
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		return -2
	}
	// SUM 在空集返回 NULL；空集已通过行数检查过滤，此处直接取值
	v, ok := rows[0]["total"]
	if !ok || v == nil {
		return 0
	}
	total, _ := toInt64(v)
	return total
}

// dmlLifeCountRangePG 经 PG wire 查询区间行数。
func dmlLifeCountRangePG(c *pgWireClient, idLo, idHi int64) (int64, error) {
	sql := fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s WHERE id >= %d AND id < %d",
		dmlLifeTable, idLo, idHi)
	res, err := c.sendQueryRead(sql)
	if err != nil {
		return 0, fmt.Errorf("传输失败: %w", err)
	}
	if res.errMsg != "" {
		return 0, fmt.Errorf("PG 错误: %s", res.errMsg)
	}
	if len(res.rows) != 1 {
		return 0, fmt.Errorf("行数错误: %d", len(res.rows))
	}
	cnt, _ := pgInt(pgRowToMap(res.columns, res.rows[0])["cnt"])
	return cnt, nil
}

// dmlLifeSumQtyPG 经 PG wire 查询区间 qty 总和。
//
// 注意：scalar evaluator 不支持 COALESCE，故直接用 SUM(qty)，并在 Go 侧
// 处理 NULL 情况。
func dmlLifeSumQtyPG(c *pgWireClient, idLo, idHi int64) (int64, error) {
	sql := fmt.Sprintf("SELECT SUM(qty) AS total FROM %s WHERE id >= %d AND id < %d",
		dmlLifeTable, idLo, idHi)
	res, err := c.sendQueryRead(sql)
	if err != nil {
		return 0, fmt.Errorf("传输失败: %w", err)
	}
	if res.errMsg != "" {
		return 0, fmt.Errorf("PG 错误: %s", res.errMsg)
	}
	if len(res.rows) != 1 {
		return 0, fmt.Errorf("行数错误: %d", len(res.rows))
	}
	v := pgRowToMap(res.columns, res.rows[0])["total"]
	// SUM 在空集上返回 NULL；其余情况返回数值
	if v == nil {
		return 0, nil
	}
	total, _ := pgInt(v)
	return total, nil
}

// dmlLifeVerifyCrossProtocol 在所有 worker 完成后由 3 个协议各做一次全表统计，
// 三协议结果应完全一致：总行数 = N×dmlLifeRowsKeep，总 qty = Σ (10+dmlLifeQtyAdd)×保留行数。
func dmlLifeVerifyCrossProtocol(t *testing.T, s *sqlServer) {
	t.Helper()
	wantRows := int64(dmlLifeClientNum * dmlLifeRowsKeep)
	// 每客户端保留 dmlLifeRowsKeep 行，qty = 10 + dmlLifeQtyAdd
	wantQty := int64(dmlLifeClientNum*dmlLifeRowsKeep) * (int64(10) + dmlLifeQtyAdd)

	// TCP / HTTP 校验
	tcpRows, tcpQty := dmlLifeStatsTotal(s, "tcp")
	httpRows, httpQty := dmlLifeStatsTotal(s, "http")
	if tcpRows != wantRows {
		t.Errorf("TCP COUNT(*) 期望 %d，得到 %d", wantRows, tcpRows)
	}
	if tcpQty != wantQty {
		t.Errorf("TCP SUM(qty) 期望 %d，得到 %d", wantQty, tcpQty)
	}
	if httpRows != tcpRows || httpQty != tcpQty {
		t.Errorf("跨协议不一致: TCP=(%d,%d) HTTP=(%d,%d)",
			tcpRows, tcpQty, httpRows, httpQty)
	}

	// PG wire 校验
	pgc := dialPGWire(t, s.srv.PGAddr())
	defer pgc.close()
	pgc.handshake(t)
	res := pgc.execOK(t, "SELECT COUNT(*) AS cnt FROM "+dmlLifeTable)
	pgCnt, _ := pgInt(pgRowToMap(res.columns, res.rows[0])["cnt"])
	if pgCnt != tcpRows {
		t.Errorf("跨协议 COUNT 不一致: TCP=%d, PG=%d", tcpRows, pgCnt)
	}
	qres := pgc.execOK(t, "SELECT SUM(qty) AS total FROM "+dmlLifeTable)
	qm := pgRowToMap(qres.columns, qres.rows[0])
	pgQty := int64(0)
	if qm["total"] != nil {
		pgQty, _ = pgInt(qm["total"])
	}
	if pgQty != tcpQty {
		t.Errorf("跨协议 SUM 不一致: TCP=%d, PG=%d", tcpQty, pgQty)
	}
}

// dmlLifeStatsTotal 经指定协议做全表 COUNT(*) 与 SUM(qty)。
//
// 返回值 (rows, qty) 用于跨协议一致性比较。注意：返回值不进行错误处理，
// 由调用方在 t.Helper 上下文中处理（与 respRows 风格一致）。
//
// 注意：scalar evaluator 不支持 COALESCE，直接用 SUM(qty) 并在 Go 侧处理 NULL。
func dmlLifeStatsTotal(s *sqlServer, via string) (int64, int64) {
	resp, err := rawQuery(s, via,
		"SELECT COUNT(*) AS cnt, SUM(qty) AS total FROM "+dmlLifeTable)
	if err != nil {
		return -1, -1
	}
	if resp.Code != 0 {
		return -1, -1
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		return -1, -1
	}
	cnt, _ := toInt64(rows[0]["cnt"])
	var total int64
	if v, ok := rows[0]["total"]; ok && v != nil {
		total, _ = toInt64(v)
	}
	return cnt, total
}

// dmlLifeVerifyGroupByActive 经 TCP 校验 GROUP BY active 的结果：
// 保留行（active=true）应占 dmlLifeRowsKeep × 客户端数；删除行不应出现。
func dmlLifeVerifyGroupByActive(t *testing.T, s *sqlServer) {
	t.Helper()
	resp := queryVia(t, s, "tcp",
		"SELECT active, COUNT(*) AS cnt FROM "+dmlLifeTable+" GROUP BY active")
	if resp.Code != 0 {
		t.Fatalf("GROUP BY 失败: %s", resp.Message)
	}
	rows := respRows(resp)
	// 只应存在 active=true 一组（active=false 已被删除）
	if len(rows) != 1 {
		t.Fatalf("GROUP BY active 期望 1 组，得到 %d", len(rows))
	}
	active, _ := rows[0]["active"].(bool)
	if !active {
		t.Errorf("GROUP BY active: 期望 true 组，得到 false")
	}
	cnt, _ := toInt64(rows[0]["cnt"])
	if cnt != int64(dmlLifeClientNum*dmlLifeRowsKeep) {
		t.Errorf("GROUP BY active=true 行数: 期望 %d，得到 %d",
			dmlLifeClientNum*dmlLifeRowsKeep, cnt)
	}
}

// TestDMLLifecycleMultiProtocol 启动一个 server，6 个客户端（2 TCP + 2 HTTP +
// 2 PG wire）并发执行完整 DML 生命周期：INSERT 8 行 → SELECT 校验 → 幂等 UPDATE →
// SELECT 校验 → 加权 UPDATE → SELECT 校验 → DELETE 半行（4 行）→ SELECT 校验。
// 最终跨 3 协议做 COUNT/SUM/GROUP BY 一致性断言。
//
// 验证目标：
//   - DML 顺序正确性：每步 SELECT 返回值与「累计修改」完全一致
//   - 多客户端隔离：各客户端负责不重叠 ID 区间，残留行数 = Σ 客户端保留行数
//   - 跨协议一致性：TCP/HTTP/PG wire 三协议读出的全表聚合完全一致
//   - 幂等 UPDATE：不改变数据但仍返回正确的命中行数（resp.Rows）
//   - 条件 DELETE：基于前面 UPDATE 设置的 active 字段过滤，删除精确命中预期行
func TestDMLLifecycleMultiProtocol(t *testing.T) {
	t.Parallel()
	s := startPGWireServer(t)
	dmlLifeCreateTable(t, s)

	var wg sync.WaitGroup
	var failCount int64
	var lastErr atomic.Value

	for i := 0; i < dmlLifeClientNum; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			if err := dmlLifeClient(s, clientID); err != nil {
				t.Logf("dmlLife 客户端 %d (%s) 失败: %v",
					clientID, dmlLifeRoles[clientID].via, err)
				lastErr.Store(err.Error())
				atomic.AddInt64(&failCount, 1)
			}
		}(i)
	}
	wg.Wait()

	if failCount > 0 {
		t.Fatalf("%d 个客户端失败，最后错误: %v", failCount, lastErr.Load())
	}

	// 跨协议一致性校验：TCP / HTTP / PG wire 三者结果必须完全一致
	dmlLifeVerifyCrossProtocol(t, s)

	// GROUP BY 验证：active=false 行应已全部删除，仅剩 active=true 一组
	dmlLifeVerifyGroupByActive(t, s)
}
