package pgwire

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgproto3/v2"
)

// serverVersion 是报告给客户端的 PostgreSQL 版本字符串。
const serverVersion = "15.0 (widb)"

// processIDCounter 生成单调递增的进程 ID（用于 BackendKeyData）。
var processIDCounter uint32

// sslNegotiationResponse 是对 SSLRequest 的单字节 'N' 响应，表示不支持 SSL。
type sslNegotiationResponse struct{}

func (sslNegotiationResponse) Backend() {}

// Encode 将 'N' 字节追加到 dst。
func (sslNegotiationResponse) Encode(dst []byte) ([]byte, error) {
	return append(dst, 'N'), nil
}

// Decode 是 BackendMessage 接口的空实现（此消息仅由服务端发送，无需解码）。
func (sslNegotiationResponse) Decode([]byte) error { return nil }

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

// connHandler 处理单个 PG wire 连接的完整生命周期。
type connHandler struct {
	backend      *pgproto3.Backend
	executor     SQLExecutor
	conn         net.Conn
	idleTimeout  time.Duration
	writeTimeout time.Duration

	extMu      sync.Mutex                 // 保护 extStmts / extPortals / extErr 的并发访问
	extStmts   map[string]*extendedStmt   // prepared statement 名 → SQL
	extPortals map[string]*extendedPortal // portal 名 → SQL
	extErr     error                      // extended query 模式下的错误状态；非 nil 时丢弃后续消息直到 Sync
}

// newConnHandler 创建一个新的连接处理器。
// conn 用于设置读写截止时间，idleTimeout 为单次读取空闲超时，writeTimeout 为单次写入超时。
func newConnHandler(backend *pgproto3.Backend, executor SQLExecutor, conn net.Conn, idleTimeout, writeTimeout time.Duration) *connHandler {
	return &connHandler{
		backend:      backend,
		executor:     executor,
		conn:         conn,
		idleTimeout:  idleTimeout,
		writeTimeout: writeTimeout,
		extStmts:     make(map[string]*extendedStmt),
		extPortals:   make(map[string]*extendedPortal),
	}
}

// serve 运行连接生命周期：启动握手 → 查询循环。
func (h *connHandler) serve() {
	if err := h.handleStartup(); err != nil {
		log.Printf("pgwire: startup failed: %v", err)
		return
	}
	h.queryLoop()
}

// setReadDeadline 在配置了空闲超时时设置下次读取的截止时间。
func (h *connHandler) setReadDeadline() {
	if h.idleTimeout > 0 {
		_ = h.conn.SetReadDeadline(time.Now().Add(h.idleTimeout))
	}
}

// send 发送一条后端消息，并在配置了写超时时设置写截止时间。
// 返回发送错误，便于调用方感知连接断开（修复 review #4：不再静默丢弃发送错误）。
func (h *connHandler) send(msg pgproto3.BackendMessage) error {
	if h.writeTimeout > 0 {
		_ = h.conn.SetWriteDeadline(time.Now().Add(h.writeTimeout))
	}
	return h.backend.Send(msg)
}

// handleStartup 处理启动握手，包括 SSL 协商和认证。
func (h *connHandler) handleStartup() error {
	h.setReadDeadline()
	msg, err := h.backend.ReceiveStartupMessage()
	if err != nil {
		return fmt.Errorf("receive startup: %w", err)
	}
	switch m := msg.(type) {
	case *pgproto3.StartupMessage:
		_ = m
		return h.sendStartupResponse()
	case *pgproto3.SSLRequest:
		return h.handleSSLNegotiation()
	case *pgproto3.GSSEncRequest:
		return h.handleSSLNegotiation()
	default:
		return fmt.Errorf("unexpected startup message: %T", msg)
	}
}

// handleSSLNegotiation 拒绝 SSL/GSS 加密，然后接收真正的 StartupMessage。
func (h *connHandler) handleSSLNegotiation() error {
	if err := h.send(sslNegotiationResponse{}); err != nil {
		return fmt.Errorf("send ssl response: %w", err)
	}
	h.setReadDeadline()
	msg, err := h.backend.ReceiveStartupMessage()
	if err != nil {
		return fmt.Errorf("receive startup after ssl: %w", err)
	}
	if _, ok := msg.(*pgproto3.StartupMessage); !ok {
		return fmt.Errorf("expected startup message, got %T", msg)
	}
	return h.sendStartupResponse()
}

// sendStartupResponse 发送认证成功后的初始消息序列。
func (h *connHandler) sendStartupResponse() error {
	if err := h.send(&pgproto3.AuthenticationOk{}); err != nil {
		return fmt.Errorf("send auth ok: %w", err)
	}
	if err := h.sendParameterStatuses(); err != nil {
		return err
	}
	pid := atomic.AddUint32(&processIDCounter, 1)
	if err := h.send(&pgproto3.BackendKeyData{
		ProcessID: pid,
		SecretKey: pid,
	}); err != nil {
		return fmt.Errorf("send backend key data: %w", err)
	}
	return h.sendReadyForQuery()
}

// sendParameterStatuses 发送客户端期望的参数状态。
func (h *connHandler) sendParameterStatuses() error {
	params := []struct{ name, value string }{
		{"server_version", serverVersion},
		{"client_encoding", "UTF8"},
		{"server_encoding", "UTF8"},
		{"DateStyle", "ISO, MDY"},
		{"TimeZone", "UTC"},
		{"standard_conforming_strings", "on"},
		{"integer_datetimes", "on"},
	}
	for _, p := range params {
		if err := h.send(&pgproto3.ParameterStatus{
			Name: p.name, Value: p.value,
		}); err != nil {
			return fmt.Errorf("send parameter %s: %w", p.name, err)
		}
	}
	return nil
}

// queryLoop 接收并处理客户端消息，直到连接关闭或 Terminate。
// 每次接收前重置读截止时间，空闲超时后自动关闭连接（修复 review #2）。
func (h *connHandler) queryLoop() {
	for {
		h.setReadDeadline()
		msg, err := h.backend.Receive()
		if err != nil {
			return
		}
		if !h.dispatchMessage(msg) {
			return
		}
	}
}

// dispatchMessage 分发消息到对应处理器，返回 false 表示应终止连接。
func (h *connHandler) dispatchMessage(msg pgproto3.FrontendMessage) bool {
	switch m := msg.(type) {
	case *pgproto3.Query:
		h.handleQuery(m.String)
		return true
	case *pgproto3.Terminate:
		return false
	case *pgproto3.Sync:
		// Sync 结束一个 extended query 周期：清除错误状态并发送 ReadyForQuery。
		h.extMu.Lock()
		h.extErr = nil
		h.extMu.Unlock()
		_ = h.sendReadyForQuery()
		return true
	case *pgproto3.Parse:
		// extended query 模式下，处于错误状态时丢弃消息直到 Sync。
		if h.consumeExtError() {
			return true
		}
		h.handleParse(m)
		return true
	case *pgproto3.Bind:
		if h.consumeExtError() {
			return true
		}
		h.handleBind(m)
		return true
	case *pgproto3.Describe:
		if h.consumeExtError() {
			return true
		}
		h.handleDescribe(m)
		return true
	case *pgproto3.Execute:
		if h.consumeExtError() {
			return true
		}
		h.handleExecute(m)
		return true
	case *pgproto3.Close:
		// Close 不受错误状态影响（按 PG 协议，即使在错误状态也应处理 Close）。
		h.handleClose(m)
		return true
	case *pgproto3.Flush:
		// 强制刷新缓冲区；当前实现每次 Send 即刷，无需特殊处理。
		return true
	default:
		return true
	}
}

// consumeExtError 检查并返回当前是否处于 extended query 错误状态。
// 当 extErr 非 nil 时返回 true，表示应丢弃后续消息；并在首次被错误 Sync
// 之外的客户端读取时保留错误供可能的 ErrorResponse 报告。
// 实现逻辑：若 extErr 非 nil，返回 true；否则返回 false。
func (h *connHandler) consumeExtError() bool {
	h.extMu.Lock()
	defer h.extMu.Unlock()
	return h.extErr != nil
}

// setExtError 记录 extended query 错误状态。
// 错误状态会在下一次 Sync 时被清除（PG 协议要求）。
func (h *connHandler) setExtError(err error) {
	h.extMu.Lock()
	h.extErr = err
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
		// prepared statement: ParameterDescription + (RowDescription | NoData)
		h.extMu.Lock()
		stmt, ok := h.extStmts[m.Name]
		var paramOIDs []uint32
		if ok {
			// 若 SQL 中包含 $1/$2/... 占位符（简化：仅检查 $N 出现次数），返回对应 OID 列表。
			// 真实协议下应解析参数类型；此处无法静态分析，使用 unspecified OID=0。
			paramOIDs = extractParamOIDs(stmt.sql)
		}
		h.extMu.Unlock()
		if !ok {
			err := fmt.Errorf("prepared statement %q 不存在", m.Name)
			h.sendError(err)
			h.setExtError(err)
			return
		}
		if err := h.send(&pgproto3.ParameterDescription{ParameterOIDs: paramOIDs}); err != nil {
			log.Printf("pgwire: send parameter description: %v", err)
			return
		}
		// 是否返回行的 SQL 类别只能通过执行判断；简化实现：始终返回 NoData，
		// 在 Execute 时若实际为 SELECT/EXPLAIN/SHOW TABLES，则追加 RowDescription。
		if err := h.send(&pgproto3.NoData{}); err != nil {
			log.Printf("pgwire: send no data: %v", err)
		}
	case 'P':
		// portal: 仅返回 (RowDescription | NoData)
		h.extMu.Lock()
		_, ok := h.extPortals[m.Name]
		h.extMu.Unlock()
		if !ok {
			err := fmt.Errorf("portal %q 不存在", m.Name)
			h.sendError(err)
			h.setExtError(err)
			return
		}
		// 同上：返回 NoData，RowDescription 在 Execute 时按需补发。
		if err := h.send(&pgproto3.NoData{}); err != nil {
			log.Printf("pgwire: send no data: %v", err)
		}
	default:
		err := fmt.Errorf("describe 类型 %c 不支持", m.ObjectType)
		h.sendError(err)
		h.setExtError(err)
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
		// 按 PG 规范，Execute 错误不会污染 extended query 错误状态，
		// 仅在 Parse/Bind/Describe 失败时才进入错误状态。
		return
	}
	h.sendExtendedResult(result, int(m.MaxRows))
}

// sendExtendedResult 与 Simple Query 路径相同：补发 RowDescription（如有），
// 然后发送 DataRow* + CommandComplete。
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
	if !strings.Contains(sql, "$") {
		return nil
	}
	maxN := 0
	for i := 0; i < len(sql)-1; i++ {
		if sql[i] != '$' {
			continue
		}
		n := 0
		hasDigit := false
		for j := i + 1; j < len(sql) && sql[j] >= '0' && sql[j] <= '9'; j++ {
			n = n*10 + int(sql[j]-'0')
			hasDigit = true
		}
		if hasDigit && n > maxN {
			maxN = n
		}
	}
	if maxN == 0 {
		return nil
	}
	oIDs := make([]uint32, maxN)
	return oIDs
}

// handleQuery 执行 SQL 查询并发送结果。
func (h *connHandler) handleQuery(sql string) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		if err := h.send(&pgproto3.EmptyQueryResponse{}); err != nil {
			log.Printf("pgwire: send empty query response: %v", err)
			return
		}
		_ = h.sendReadyForQuery()
		return
	}
	result, err := h.executor.ExecuteSQL(sql)
	if err != nil {
		h.sendError(err)
		_ = h.sendReadyForQuery()
		return
	}
	if result.IsQuery {
		h.sendQueryResult(result)
	} else {
		if err := h.send(&pgproto3.CommandComplete{
			CommandTag: []byte(result.CommandTag),
		}); err != nil {
			log.Printf("pgwire: send command complete: %v", err)
			return
		}
	}
	_ = h.sendReadyForQuery()
}

// sendQueryResult 发送结果集（RowDescription + DataRow* + CommandComplete）。
// 使用 result.CommandTag 作为命令标签，避免对 INSERT/UPDATE...RETURNING 等带结果集
// 的写操作错误地返回 "SELECT N" 标签（修复 review #1）。
func (h *connHandler) sendQueryResult(result *SQLResult) {
	// 优先使用 Schema 列类型生成准确的 RowDescription（修复 DATE/TIMESTAMP/INT
	// 被错误推断为 TEXT 的问题）；Schema 类型缺失时回退到按行值推断。
	types := columnTypesFromSchema(result.Columns, result.ColumnTypes)
	if types == nil {
		types = inferColumnTypes(result.Columns, result.Rows)
	}
	if err := h.send(buildRowDescription(result.Columns, types)); err != nil {
		log.Printf("pgwire: send row description: %v", err)
		return
	}
	for _, row := range result.Rows {
		if err := h.send(buildDataRow(row, result.Columns)); err != nil {
			log.Printf("pgwire: send data row: %v", err)
			return
		}
	}
	tag := result.CommandTag
	if tag == "" {
		tag = fmt.Sprintf("SELECT %d", len(result.Rows))
	}
	if err := h.send(&pgproto3.CommandComplete{CommandTag: []byte(tag)}); err != nil {
		log.Printf("pgwire: send command complete: %v", err)
	}
}

// sendError 发送错误响应。
func (h *connHandler) sendError(err error) {
	if err := h.send(&pgproto3.ErrorResponse{
		Severity: "ERROR",
		Code:     "XX000",
		Message:  err.Error(),
	}); err != nil {
		log.Printf("pgwire: send error response: %v", err)
	}
}

// sendReadyForQuery 发送 ReadyForQuery 消息（空闲状态）。
func (h *connHandler) sendReadyForQuery() error {
	return h.send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
}
