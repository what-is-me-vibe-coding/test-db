package pgwire

import (
	"fmt"
	"log"

	"github.com/jackc/pgproto3/v2"
)

// extendedStmt 保存一个已 Parse 的 prepared statement。
// 简化实现：仅缓存 SQL 文本。Parse 时若原 statement 同名已存在则替换。
// 内部使用空串 "" 表示 "unnamed statement"，对应客户端常用的
// Parse("", query, ...) 调用模式。
type extendedStmt struct {
	sql string
}

// extendedPortal 保存一个已 Bind 的 portal。
// 简化实现：缓存关联的 SQL 文本（自 prepared statement 复制）。
// 忽略 Bind 携带的参数值，因为我们底层的 SQL 解析器不支持占位符；
// 实际查询时直接使用 prepared statement 的 SQL 文本执行。
// 内部使用空串 "" 表示 "unnamed portal"。
type extendedPortal struct {
	sql string
}

// dispatchExtended 将 extended query 协议消息（Parse/Bind/Describe/Execute/Close）
// 路由到对应处理器。
// 返回 true 表示已处理（可能为静默忽略，例如错误状态期间）。
func (h *connHandler) dispatchExtended(msg pgproto3.FrontendMessage) bool {
	switch m := msg.(type) {
	case *pgproto3.Parse:
		// 错误状态期间丢弃消息直到 Sync。
		if h.inExtError() {
			return true
		}
		h.handleParse(m)
	case *pgproto3.Bind:
		if h.inExtError() {
			return true
		}
		h.handleBind(m)
	case *pgproto3.Describe:
		if h.inExtError() {
			return true
		}
		h.handleDescribe(m)
	case *pgproto3.Execute:
		if h.inExtError() {
			return true
		}
		h.handleExecute(m)
	case *pgproto3.Close:
		// Close 不受错误状态影响（按 PG 协议）。
		h.handleClose(m)
	case *pgproto3.Sync:
		// Sync 结束一个 extended query 周期：清除错误状态并发送 ReadyForQuery。
		h.clearExtError()
		if err := h.sendReadyForQuery(); err != nil {
			log.Printf("pgwire: send ready for query: %v", err)
		}
	default:
		// 未识别的消息类型，由调用方负责处理
		return false
	}
	return true
}

// inExtError 检查是否处于 extended query 错误状态。
// 错误状态会在下一次 Sync 时被清除（PG 协议要求）。
func (h *connHandler) inExtError() bool {
	h.extMu.Lock()
	defer h.extMu.Unlock()
	return h.extErr != nil
}

// setExtError 记录 extended query 错误状态。
func (h *connHandler) setExtError(err error) {
	h.extMu.Lock()
	h.extErr = err
	h.extMu.Unlock()
}

// clearExtError 清除 extended query 错误状态。
func (h *connHandler) clearExtError() {
	h.extMu.Lock()
	h.extErr = nil
	h.extMu.Unlock()
}

// handleParse 处理 Parse 消息：缓存 SQL 文本到 prepared statement 映射。
// 空 SQL 也被允许（PG 规范），但会替换同名已存在的 statement。
func (h *connHandler) handleParse(m *pgproto3.Parse) {
	h.extMu.Lock()
	h.extStmts[m.Name] = &extendedStmt{sql: m.Query}
	h.extMu.Unlock()
	if err := h.send(&pgproto3.ParseComplete{}); err != nil {
		log.Printf("pgwire: send parse complete: %v", err)
	}
}

// handleBind 处理 Bind 消息：将 prepared statement 的 SQL 复制到 portal。
// 简化实现：忽略参数值（pgwire 内 SQL 解析器不支持占位符）。
// 若引用的 prepared statement 不存在，则进入错误状态。
func (h *connHandler) handleBind(m *pgproto3.Bind) {
	h.extMu.Lock()
	stmt, ok := h.extStmts[m.PreparedStatement]
	if !ok {
		h.extMu.Unlock()
		err := fmt.Errorf("prepared statement %q 不存在", m.PreparedStatement)
		h.sendError(err)
		h.setExtError(err)
		return
	}
	h.extPortals[m.DestinationPortal] = &extendedPortal{sql: stmt.sql}
	h.extMu.Unlock()
	if err := h.send(&pgproto3.BindComplete{}); err != nil {
		log.Printf("pgwire: send bind complete: %v", err)
	}
}

// handleDescribe 处理 Describe 消息：返回 parameter description 与 RowDescription/NoData。
// 简化实现：因我们不解析参数类型，ParameterDescription 始终返回空列表；
// RowDescription/NoData 在 Execute 时才确定（此时仅返回 NoData）。
func (h *connHandler) handleDescribe(m *pgproto3.Describe) {
	switch m.ObjectType {
	case 'S':
		h.handleDescribeStatement(m.Name)
	case 'P':
		h.handleDescribePortal(m.Name)
	default:
		err := fmt.Errorf("describe 类型 %c 不支持", m.ObjectType)
		h.sendError(err)
		h.setExtError(err)
	}
}

// handleDescribeStatement 处理 Describe(S)：先查 statement，
// 然后发送 ParameterDescription + NoData。
func (h *connHandler) handleDescribeStatement(name string) {
	h.extMu.Lock()
	stmt, ok := h.extStmts[name]
	if !ok {
		h.extMu.Unlock()
		err := fmt.Errorf("prepared statement %q 不存在", name)
		h.sendError(err)
		h.setExtError(err)
		return
	}
	// 简化：仅检查 $N 占位符数量用于 ParameterDescription。
	paramOIDs := extractParamOIDs(stmt.sql)
	h.extMu.Unlock()
	if err := h.send(&pgproto3.ParameterDescription{ParameterOIDs: paramOIDs}); err != nil {
		log.Printf("pgwire: send parameter description: %v", err)
		return
	}
	// 行结构在 Execute 时才确定；此处返回 NoData，Execute 时按需补发 RowDescription。
	if err := h.send(&pgproto3.NoData{}); err != nil {
		log.Printf("pgwire: send no data: %v", err)
	}
}

// handleDescribePortal 处理 Describe(P)：先查 portal，
// 然后发送 NoData（RowDescription 在 Execute 时按需补发）。
func (h *connHandler) handleDescribePortal(name string) {
	h.extMu.Lock()
	_, ok := h.extPortals[name]
	h.extMu.Unlock()
	if !ok {
		err := fmt.Errorf("portal %q 不存在", name)
		h.sendError(err)
		h.setExtError(err)
		return
	}
	if err := h.send(&pgproto3.NoData{}); err != nil {
		log.Printf("pgwire: send no data: %v", err)
	}
}

// handleClose 处理 Close 消息：从对应映射中删除 prepared statement 或 portal，
// 并发送 CloseComplete。Close 始终成功响应，即使对象不存在（按 PG 规范）。
func (h *connHandler) handleClose(m *pgproto3.Close) {
	h.extMu.Lock()
	switch m.ObjectType {
	case 'S':
		delete(h.extStmts, m.Name)
	case 'P':
		delete(h.extPortals, m.Name)
	}
	h.extMu.Unlock()
	if err := h.send(&pgproto3.CloseComplete{}); err != nil {
		log.Printf("pgwire: send close complete: %v", err)
	}
}

// handleExecute 处理 Execute 消息：从 portal 取出 SQL，由 executor 执行。
// 对于返回结果集的查询，在 DataRow 之前先补发 RowDescription。
// maxRows>0 时限制返回行数；portal 不存在则进入错误状态。
func (h *connHandler) handleExecute(m *pgproto3.Execute) {
	h.extMu.Lock()
	portal, ok := h.extPortals[m.Portal]
	if !ok {
		h.extMu.Unlock()
		err := fmt.Errorf("portal %q 不存在", m.Portal)
		h.sendError(err)
		h.setExtError(err)
		return
	}
	sql := portal.sql
	h.extMu.Unlock()

	result, err := h.executor.ExecuteSQL(sql)
	if err != nil {
		// 执行错误不发 ErrorResponse 之前的描述消息（PG 规范）。
		h.sendError(err)
		// 按 PG 规范，Execute 错误不会污染 extended query 错误状态。
		return
	}
	h.sendExtendedResult(result, int(m.MaxRows))
}

// sendExtendedResult 发送 extended query 协议的查询结果。
// 对于查询：先补发 RowDescription（如缺），再发 DataRow*，最后发 CommandComplete。
// 对于非查询：仅发 CommandComplete。
// maxRows 限制返回的最大行数（0 表示无限制）。
func (h *connHandler) sendExtendedResult(result *SQLResult, maxRows int) {
	if result.IsQuery {
		types := columnTypesFromSchema(result.Columns, result.ColumnTypes)
		if types == nil {
			types = inferColumnTypes(result.Columns, result.Rows)
		}
		if err := h.send(buildRowDescription(result.Columns, types)); err != nil {
			log.Printf("pgwire: send row description: %v", err)
			return
		}
		rowLimit := len(result.Rows)
		if maxRows > 0 && maxRows < rowLimit {
			rowLimit = maxRows
		}
		for i := 0; i < rowLimit; i++ {
			row := result.Rows[i]
			if err := h.send(buildDataRow(row, result.Columns)); err != nil {
				log.Printf("pgwire: send data row: %v", err)
				return
			}
		}
	}
	tag := result.CommandTag
	if tag == "" {
		if result.IsQuery {
			tag = fmt.Sprintf("SELECT %d", len(result.Rows))
		} else {
			tag = "OK"
		}
	}
	if err := h.send(&pgproto3.CommandComplete{CommandTag: []byte(tag)}); err != nil {
		log.Printf("pgwire: send command complete: %v", err)
	}
}

// extractParamOIDs 从 SQL 文本中提取 $N 占位符数量（最大 N）并返回对应数量的
// OID=0（unspecified）。返回 nil 表示无占位符。
// 简化实现：widb 的 SQL 解析器不支持占位符，因此参数总是被忽略；
// 此函数仅用于 ParameterDescription 满足 PG 客户端对 Describe(S) 的期望。
func extractParamOIDs(sql string) []uint32 {
	maxN := scanMaxPlaceholder(sql)
	if maxN == 0 {
		return nil
	}
	oIDs := make([]uint32, maxN)
	return oIDs
}

// scanMaxPlaceholder 在 SQL 中扫描 $N 占位符并返回最大的 N。
// 若未找到占位符返回 0。
func scanMaxPlaceholder(sql string) int {
	maxN := 0
	for i := 0; i < len(sql)-1; i++ {
		if sql[i] != '$' {
			continue
		}
		n, ok := parseDigits(sql, i+1)
		if ok && n > maxN {
			maxN = n
		}
	}
	return maxN
}

// parseDigits 从 s[start] 开始解析连续的数字字符，返回数值与是否解析成功。
func parseDigits(s string, start int) (int, bool) {
	if start >= len(s) {
		return 0, false
	}
	n := 0
	hasDigit := false
	for j := start; j < len(s) && s[j] >= '0' && s[j] <= '9'; j++ {
		n = n*10 + int(s[j]-'0')
		hasDigit = true
	}
	return n, hasDigit
}
