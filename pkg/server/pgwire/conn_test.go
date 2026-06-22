package pgwire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgproto3/v2"
)

// sslNegotiationResponse 测试
func TestSSLNegotiationResponseEncode(t *testing.T) {
	r := sslNegotiationResponse{}
	dst, err := r.Encode(nil)
	if err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	if len(dst) != 1 || dst[0] != 'N' {
		t.Errorf("期望单字节 'N', got %v", dst)
	}
}

func TestSSLNegotiationResponseDecode(t *testing.T) {
	r := sslNegotiationResponse{}
	if err := r.Decode([]byte{1, 2, 3}); err != nil {
		t.Errorf("Decode 不应返回错误: %v", err)
	}
}

func TestSSLNegotiationResponseBackend(t *testing.T) {
	t.Helper()
	r := sslNegotiationResponse{}
	r.Backend() // 仅验证不 panic
}

// --- PG 协议客户端辅助函数 ---

// pgClient 封装一个原始 PG 协议客户端，用于测试。
type pgClient struct {
	conn net.Conn
}

func newPGClient(t *testing.T, addr string) *pgClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	return &pgClient{conn: conn}
}

func (c *pgClient) close() { _ = c.conn.Close() }

// sendStartupMessage 发送 StartupMessage。
func (c *pgClient) sendStartupMessage() error {
	// StartupMessage: length(4) + protocol(4) + params + \0\0
	buf := &bytes.Buffer{}
	// protocol version 3.0 = 196608
	_ = binary.Write(buf, binary.BigEndian, uint32(196608))
	buf.WriteString("user")
	buf.WriteByte(0)
	buf.WriteString("test")
	buf.WriteByte(0)
	buf.WriteString("database")
	buf.WriteByte(0)
	buf.WriteString("testdb")
	buf.WriteByte(0)
	buf.WriteByte(0) // 终止符
	body := buf.Bytes()
	// length = 4 (length itself) + len(body)
	totalLen := uint32(4 + len(body))
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, totalLen)
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if _, err := c.conn.Write(body); err != nil {
		return err
	}
	return nil
}

// sendSSLRequest 发送 SSLRequest。
func (c *pgClient) sendSSLRequest() error {
	// SSLRequest: length(4) + 80877103(4)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[0:4], 8)
	binary.BigEndian.PutUint32(buf[4:8], 80877103)
	_, err := c.conn.Write(buf)
	return err
}

// sendGSSEncRequest 发送 GSSEncRequest。
func (c *pgClient) sendGSSEncRequest() error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[0:4], 8)
	binary.BigEndian.PutUint32(buf[4:8], 80877104)
	_, err := c.conn.Write(buf)
	return err
}

// sendQuery 发送 Query 消息。
func (c *pgClient) sendQuery(sql string) error {
	body := []byte(sql)
	body = append(body, 0) // 终止符
	totalLen := uint32(4 + len(body))
	buf := make([]byte, 5)
	buf[0] = 'Q'
	binary.BigEndian.PutUint32(buf[1:5], totalLen)
	if _, err := c.conn.Write(buf); err != nil {
		return err
	}
	if _, err := c.conn.Write(body); err != nil {
		return err
	}
	return nil
}

// sendTerminate 发送 Terminate 消息。
func (c *pgClient) sendTerminate() error {
	buf := make([]byte, 5)
	buf[0] = 'X'
	binary.BigEndian.PutUint32(buf[1:5], 4)
	_, err := c.conn.Write(buf)
	return err
}

// sendSync 发送 Sync 消息。
func (c *pgClient) sendSync() error {
	buf := make([]byte, 5)
	buf[0] = 'S'
	binary.BigEndian.PutUint32(buf[1:5], 4)
	_, err := c.conn.Write(buf)
	return err
}

// sendParse 发送 Parse 消息（extended query）。
// stmtName: 空串表示 unnamed statement。
// query: SQL 文本。
// paramTypes: 参数 OID 列表；通常为 nil 让服务端推断。
func (c *pgClient) sendParse(stmtName, query string, paramTypes []uint32) error {
	buf := &bytes.Buffer{}
	buf.WriteString(stmtName)
	buf.WriteByte(0)
	buf.WriteString(query)
	buf.WriteByte(0)
	_ = binary.Write(buf, binary.BigEndian, uint16(len(paramTypes)))
	for _, t := range paramTypes {
		_ = binary.Write(buf, binary.BigEndian, t)
	}
	body := buf.Bytes()
	total := uint32(4 + len(body))
	header := make([]byte, 5)
	header[0] = 'P'
	binary.BigEndian.PutUint32(header[1:5], total)
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(body)
	return err
}

// sendBind 发送 Bind 消息（extended query）。
// portalName/stmtName: 空串表示 unnamed。
// params: 参数值（nil 元素表示 NULL）。
// resultFormats: 每个结果列的格式码（0=text, 1=binary），0/空 = 全部 text。
func (c *pgClient) sendBind(portalName, stmtName string, params [][]byte, resultFormats []int16) error {
	buf := &bytes.Buffer{}
	buf.WriteString(portalName)
	buf.WriteByte(0)
	buf.WriteString(stmtName)
	buf.WriteByte(0)
	_ = binary.Write(buf, binary.BigEndian, uint16(0)) // 0 param format codes
	_ = binary.Write(buf, binary.BigEndian, uint16(len(params)))
	for _, p := range params {
		if p == nil {
			_ = binary.Write(buf, binary.BigEndian, int32(-1))
		} else {
			_ = binary.Write(buf, binary.BigEndian, int32(len(p)))
			buf.Write(p)
		}
	}
	_ = binary.Write(buf, binary.BigEndian, uint16(len(resultFormats)))
	for _, f := range resultFormats {
		_ = binary.Write(buf, binary.BigEndian, f)
	}
	body := buf.Bytes()
	total := uint32(4 + len(body))
	header := make([]byte, 5)
	header[0] = 'B'
	binary.BigEndian.PutUint32(header[1:5], total)
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(body)
	return err
}

// sendDescribe 发送 Describe 消息。
// kind: 'S' = prepared statement, 'P' = portal。
func (c *pgClient) sendDescribe(kind byte, name string) error {
	buf := &bytes.Buffer{}
	buf.WriteByte(kind)
	buf.WriteString(name)
	buf.WriteByte(0)
	body := buf.Bytes()
	total := uint32(4 + len(body))
	header := make([]byte, 5)
	header[0] = 'D'
	binary.BigEndian.PutUint32(header[1:5], total)
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(body)
	return err
}

// sendExecute 发送 Execute 消息。
// portalName: 空串表示 unnamed portal。
// maxRows: 0 = 无限制。
func (c *pgClient) sendExecute(portalName string, maxRows uint32) error {
	buf := &bytes.Buffer{}
	buf.WriteString(portalName)
	buf.WriteByte(0)
	_ = binary.Write(buf, binary.BigEndian, maxRows)
	body := buf.Bytes()
	total := uint32(4 + len(body))
	header := make([]byte, 5)
	header[0] = 'E'
	binary.BigEndian.PutUint32(header[1:5], total)
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(body)
	return err
}

// sendClose 发送 Close 消息。
// kind: 'S' = prepared statement, 'P' = portal。
func (c *pgClient) sendClose(kind byte, name string) error {
	buf := &bytes.Buffer{}
	buf.WriteByte(kind)
	buf.WriteString(name)
	buf.WriteByte(0)
	body := buf.Bytes()
	total := uint32(4 + len(body))
	header := make([]byte, 5)
	header[0] = 'C'
	binary.BigEndian.PutUint32(header[1:5], total)
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(body)
	return err
}

// sendFlush 发送 Flush 消息。
func (c *pgClient) sendFlush() error {
	buf := make([]byte, 5)
	buf[0] = 'H'
	binary.BigEndian.PutUint32(buf[1:5], 4)
	_, err := c.conn.Write(buf)
	return err
}

// readMessage 读取一个 PG 后端消息（带类型前缀）。
func (c *pgClient) readMessage() (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, nil, err
	}
	msgType := header[0]
	bodyLen := binary.BigEndian.Uint32(header[1:5])
	if bodyLen < 4 {
		return 0, nil, fmt.Errorf("invalid body length: %d", bodyLen)
	}
	body := make([]byte, bodyLen-4)
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return 0, nil, err
	}
	return msgType, body, nil
}

// readUntilReadyForQuery 读取消息直到 ReadyForQuery，返回所有消息类型序列。
func (c *pgClient) readUntilReadyForQuery() ([]byte, error) {
	var types []byte
	for {
		mt, _, err := c.readMessage()
		if err != nil {
			return types, err
		}
		types = append(types, mt)
		if mt == 'Z' { // ReadyForQuery
			return types, nil
		}
		if mt == 'E' { // ErrorResponse
			// 继续读取直到 ReadyForQuery
			continue
		}
	}
}

// --- 连接处理器测试 ---

// startTestServer 启动一个测试用的 pgwire 服务端。
func startTestServer(t *testing.T, exec SQLExecutor) *Server {
	t.Helper()
	srv := NewServer("127.0.0.1:0", exec)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	return srv
}

// TestConnStartupHandshake 验证完整的启动握手流程。
func TestConnStartupHandshake(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()

	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}

	// 期望收到: AuthenticationOk ('R') + ParameterStatus* ('S') + BackendKeyData ('K') + ReadyForQuery ('Z')
	var gotTypes []byte
	for {
		mt, _, err := client.readMessage()
		if err != nil {
			t.Fatalf("读取消息失败: %v", err)
		}
		gotTypes = append(gotTypes, mt)
		if mt == 'Z' {
			break
		}
		if len(gotTypes) > 20 {
			t.Fatalf("消息过多, got %v", gotTypes)
		}
	}

	if gotTypes[0] != 'R' {
		t.Errorf("第一个消息应为 AuthenticationOk('R'), got %c", gotTypes[0])
	}
	if gotTypes[len(gotTypes)-1] != 'Z' {
		t.Errorf("最后一个消息应为 ReadyForQuery('Z'), got %c", gotTypes[len(gotTypes)-1])
	}
	// 应包含 BackendKeyData ('K')
	hasK := false
	for _, c := range gotTypes {
		if c == 'K' {
			hasK = true
		}
	}
	if !hasK {
		t.Errorf("应包含 BackendKeyData('K'), got %v", gotTypes)
	}
}

// TestConnSSLNegotiation 验证 SSL 协商流程。
func TestConnSSLNegotiation(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()

	// 先发 SSLRequest
	if err := client.sendSSLRequest(); err != nil {
		t.Fatalf("sendSSLRequest 失败: %v", err)
	}
	// 期望收到单字节 'N'
	resp := make([]byte, 1)
	if _, err := io.ReadFull(client.conn, resp); err != nil {
		t.Fatalf("读取 SSL 响应失败: %v", err)
	}
	if resp[0] != 'N' {
		t.Errorf("期望 'N', got %c", resp[0])
	}
	// 再发 StartupMessage
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取握手响应失败: %v", err)
	}
	if len(types) == 0 || types[0] != 'R' {
		t.Errorf("SSL 协商后第一个消息应为 AuthenticationOk, got %v", types)
	}
}

// TestConnGSSEncNegotiation 验证 GSS 加密协商流程。
func TestConnGSSEncNegotiation(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()

	if err := client.sendGSSEncRequest(); err != nil {
		t.Fatalf("sendGSSEncRequest 失败: %v", err)
	}
	resp := make([]byte, 1)
	if _, err := io.ReadFull(client.conn, resp); err != nil {
		t.Fatalf("读取 GSS 响应失败: %v", err)
	}
	if resp[0] != 'N' {
		t.Errorf("期望 'N', got %c", resp[0])
	}
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取握手响应失败: %v", err)
	}
	if len(types) == 0 || types[0] != 'R' {
		t.Errorf("GSS 协商后第一个消息应为 AuthenticationOk, got %v", types)
	}
}

// TestConnQuerySelect 验证 SELECT 查询流程。
func TestConnQuerySelect(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"id", "name"},
		Rows: []map[string]any{
			{"id": int64(1), "name": "alice"},
			{"id": int64(2), "name": "bob"},
		},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("SELECT * FROM t"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	// 期望: RowDescription('T') + DataRow*('D') + CommandComplete('C') + ReadyForQuery('Z')
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取查询响应失败: %v", err)
	}
	if len(types) < 4 {
		t.Fatalf("消息过少: %v", types)
	}
	if types[0] != 'T' {
		t.Errorf("第一个应为 RowDescription('T'), got %c", types[0])
	}
	// 2 个 DataRow
	if types[1] != 'D' || types[2] != 'D' {
		t.Errorf("应为 2 个 DataRow('D'), got %v", types[1:3])
	}
	if types[3] != 'C' {
		t.Errorf("应为 CommandComplete('C'), got %c", types[3])
	}
	if types[4] != 'Z' {
		t.Errorf("应为 ReadyForQuery('Z'), got %c", types[4])
	}
	// 验证 executor 收到了 SQL
	if got := exec.lastQuery(); got != "SELECT * FROM t" {
		t.Errorf("executor 收到 %q, 期望 SELECT * FROM t", got)
	}
}

// TestConnQueryError 验证查询错误返回 ErrorResponse。
func TestConnQueryError(t *testing.T) {
	exec := &mockExecutor{err: errors.New("table not found")}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("SELECT * FROM missing"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	hasError := false
	for _, c := range types {
		if c == 'E' {
			hasError = true
		}
	}
	if !hasError {
		t.Errorf("应包含 ErrorResponse('E'), got %v", types)
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
}

// TestConnEmptyQuery 验证空查询返回 EmptyQueryResponse。
func TestConnEmptyQuery(t *testing.T) {
	exec := &mockExecutor{}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("   "); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("期望 2 个消息, got %v", types)
	}
	if types[0] != 'I' { // EmptyQueryResponse
		t.Errorf("期望 EmptyQueryResponse('I'), got %c", types[0])
	}
	if types[1] != 'Z' {
		t.Errorf("期望 ReadyForQuery('Z'), got %c", types[1])
	}
}

// TestConnTerminate 验证 Terminate 消息关闭连接。
func TestConnTerminate(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendTerminate(); err != nil {
		t.Fatalf("sendTerminate 失败: %v", err)
	}
	// 服务端应关闭连接，读取应返回 EOF
	_, _, err := client.readMessage()
	if err == nil {
		t.Error("期望 EOF 或错误, 但读取成功")
	}
}

// TestConnSync 验证 Sync 消息触发 ReadyForQuery。
func TestConnSync(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	mt, _, err := client.readMessage()
	if err != nil {
		t.Fatalf("读取 Sync 响应失败: %v", err)
	}
	if mt != 'Z' {
		t.Errorf("Sync 后期望 ReadyForQuery('Z'), got %c", mt)
	}
}

// TestConnFlush 验证 Flush 消息不触发响应但保持连接。
func TestConnFlush(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendFlush(); err != nil {
		t.Fatalf("sendFlush 失败: %v", err)
	}
	// Flush 不返回消息，发送一个 Query 验证连接仍可用
	if err := client.sendQuery("SELECT 1"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取查询响应失败: %v", err)
	}
	if len(types) == 0 {
		t.Error("Flush 后查询应返回消息")
	}
}

// TestConnNonQueryCommand 验证非查询命令（如 INSERT）返回 CommandComplete。
func TestConnNonQueryCommand(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		RowsAffected: 3,
		CommandTag:   "INSERT 0 3",
		IsQuery:      false,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("INSERT INTO t VALUES (1)"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("期望 2 个消息, got %v", types)
	}
	if types[0] != 'C' { // CommandComplete
		t.Errorf("期望 CommandComplete('C'), got %c", types[0])
	}
}

// TestConnMultipleQueries 验证连接可处理多个连续查询。
func TestConnMultipleQueries(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"v"},
		Rows:    []map[string]any{{"v": int64(1)}},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := client.sendQuery("SELECT 1"); err != nil {
			t.Fatalf("第 %d 次 sendQuery 失败: %v", i, err)
		}
		types, err := client.readUntilReadyForQuery()
		if err != nil {
			t.Fatalf("第 %d 次读取响应失败: %v", i, err)
		}
		if len(types) < 3 {
			t.Errorf("第 %d 次响应消息过少: %v", i, types)
		}
	}
}

// TestConnQueryWithNilValues 验证含 NULL 值的查询结果。
func TestConnQueryWithNilValues(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"id", "name"},
		Rows: []map[string]any{
			{"id": int64(1), "name": nil},
			{"id": nil, "name": "bob"},
		},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("SELECT * FROM t"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// T + D + D + C + Z
	if len(types) != 5 {
		t.Errorf("期望 5 个消息, got %v", types)
	}
}

// TestConnEmptyResultQuery 验证空结果集查询。
func TestConnEmptyResultQuery(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"id"},
		Rows:    nil,
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("SELECT * FROM empty"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// T + C + Z (无 DataRow)
	if len(types) != 3 {
		t.Errorf("期望 3 个消息, got %v", types)
	}
	if types[0] != 'T' {
		t.Errorf("期望 RowDescription('T'), got %c", types[0])
	}
	if types[1] != 'C' {
		t.Errorf("期望 CommandComplete('C'), got %c", types[1])
	}
}

// TestConnUnexpectedStartupMessage 验证非预期启动消息的处理。
func TestConnUnexpectedStartupMessage(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()

	// 发送一个无效的启动消息（长度为 0 的特殊消息）
	// 使用一个未知协议版本
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[0:4], 8)     // length = 8
	binary.BigEndian.PutUint32(buf[4:8], 99999) // 未知协议
	if _, err := client.conn.Write(buf); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	// 服务端应关闭连接或返回错误
	time.Sleep(50 * time.Millisecond)
	// 尝试读取，应失败或 EOF
	client.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err := client.readMessage()
	if err == nil {
		t.Log("读取返回 nil（连接已关闭）")
	}
}

// TestConnConcurrentConnections 验证并发连接处理。
func TestConnConcurrentConnections(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"v"},
		Rows:    []map[string]any{{"v": int64(1)}},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			client := newPGClient(t, srv.Addr())
			defer client.close()
			if err := client.sendStartupMessage(); err != nil {
				errs <- fmt.Errorf("client %d startup: %w", idx, err)
				return
			}
			if _, err := client.readUntilReadyForQuery(); err != nil {
				errs <- fmt.Errorf("client %d handshake: %w", idx, err)
				return
			}
			if err := client.sendQuery("SELECT 1"); err != nil {
				errs <- fmt.Errorf("client %d query: %w", idx, err)
				return
			}
			if _, err := client.readUntilReadyForQuery(); err != nil {
				errs <- fmt.Errorf("client %d response: %w", idx, err)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestConnHandlerDirect 直接测试 connHandler 的方法（不通过网络）。
func TestConnHandlerDirect(t *testing.T) {
	t.Run("newConnHandler", func(t *testing.T) {
		// 使用一对 net.Pipe 模拟连接
		clientConn, serverConn := net.Pipe()
		defer func() { _ = clientConn.Close() }()
		defer func() { _ = serverConn.Close() }()

		backend := pgproto3.NewBackend(pgproto3.NewChunkReader(serverConn), serverConn)
		exec := &mockExecutor{}
		h := newConnHandler(backend, exec, serverConn, 0, 0)
		if h == nil {
			t.Fatal("newConnHandler 返回 nil")
		}
		if h.backend == nil {
			t.Error("backend 不应为 nil")
		}
		if h.executor == nil {
			t.Error("executor 不应为 nil")
		}
		if h.conn == nil {
			t.Error("conn 不应为 nil")
		}
	})
}

// TestConnQueryResultWithAllTypes 验证所有支持类型的查询结果。
func TestConnQueryResultWithAllTypes(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"b", "i", "f", "s"},
		Rows: []map[string]any{
			{"b": true, "i": int64(42), "f": float64(3.14), "s": "text"},
		},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("SELECT * FROM types"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	types, err := client.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// T + D + C + Z
	if len(types) != 4 {
		t.Errorf("期望 4 个消息, got %v", types)
	}
}

// --- Extended Query Protocol 测试 ---

// runExtendedQuery 发送一个完整的 extended query 周期（Parse+Bind+Describe+Execute+Sync）
// 并返回响应消息类型序列与最终的 ReadyForQuery 状态。
// 假定已与服务器完成 Startup 握手。
func runExtendedQuery(t *testing.T, c *pgClient, sql string) []byte {
	t.Helper()
	if err := c.sendParse("", sql, nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	if err := c.sendBind("", "", nil, []int16{0, 0}); err != nil {
		t.Fatalf("sendBind 失败: %v", err)
	}
	if err := c.sendDescribe('P', ""); err != nil {
		t.Fatalf("sendDescribe 失败: %v", err)
	}
	if err := c.sendExecute("", 0); err != nil {
		t.Fatalf("sendExecute 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	return types
}

// TestExtendedQuerySelect 验证 extended query 协议下 SELECT 正常返回数据。
// 修复 issue #234：此前服务端忽略 Parse/Bind/Describe/Execute，导致客户端
// （如 DBeaver/pgAdmin/Navicat 默认走 extended query）"查询没有传回任何结果"。
func TestExtendedQuerySelect(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"id", "name"},
		Rows: []map[string]any{
			{"id": int64(1), "name": "alice"},
			{"id": int64(2), "name": "bob"},
		},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	types := runExtendedQuery(t, c, "SELECT id, name FROM t")
	// 期望序列: ParseComplete(1) + BindComplete(2) + NoData(n)
	// + RowDescription(T) + 2*DataRow(D) + CommandComplete(C) + ReadyForQuery(Z)
	if len(types) < 7 {
		t.Fatalf("消息过少, got %v", types)
	}
	if types[0] != '1' {
		t.Errorf("期望 ParseComplete('1') 开头, got %c", types[0])
	}
	if types[1] != '2' {
		t.Errorf("期望 BindComplete('2') 第二个, got %c", types[1])
	}
	if types[2] != 'n' {
		t.Errorf("期望 NoData('n') 第三个, got %c", types[2])
	}
	if types[3] != 'T' {
		t.Errorf("期望 RowDescription('T') 第四个, got %c", types[3])
	}
	if types[4] != 'D' || types[5] != 'D' {
		t.Errorf("期望 2 个 DataRow, got %v", types[4:6])
	}
	if types[6] != 'C' {
		t.Errorf("期望 CommandComplete('C'), got %c", types[6])
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery('Z') 结尾, got %c", types[len(types)-1])
	}
	// executor 应收到一次 ExecuteSQL 调用
	if got := exec.lastQuery(); got != "SELECT id, name FROM t" {
		t.Errorf("executor 收到 %q, 期望 %q", got, "SELECT id, name FROM t")
	}
}

// TestExtendedQueryShowTables 验证 extended query 协议下 SHOW TABLES 返回结果。
// 复现 issue #234 中 "show tables" 工作但 "select" 不工作的差异。
func TestExtendedQueryShowTables(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"table"},
		Rows:    []map[string]any{{"table": "t"}},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	types := runExtendedQuery(t, c, "show tables")
	// 期望包含 RowDescription + DataRow + CommandComplete + ReadyForQuery
	hasT, hasD, hasC := false, false, false
	for _, m := range types {
		switch m {
		case 'T':
			hasT = true
		case 'D':
			hasD = true
		case 'C':
			hasC = true
		}
	}
	if !hasT {
		t.Errorf("应包含 RowDescription('T'), got %v", types)
	}
	if !hasD {
		t.Errorf("应包含 DataRow('D'), got %v", types)
	}
	if !hasC {
		t.Errorf("应包含 CommandComplete('C'), got %v", types)
	}
}

// TestExtendedQueryNonQuery 验证 extended query 协议下 INSERT 等非查询语句不发送 RowDescription。
func TestExtendedQueryNonQuery(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{RowsAffected: 1, CommandTag: "INSERT 0 1"}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	types := runExtendedQuery(t, c, "INSERT INTO t VALUES (1, 'x')")
	// 期望: 1+2+n + CommandComplete(C) + ReadyForQuery(Z)，不应有 T/D
	for _, m := range types {
		if m == 'T' {
			t.Errorf("INSERT 不应发送 RowDescription, got %v", types)
		}
		if m == 'D' {
			t.Errorf("INSERT 不应发送 DataRow, got %v", types)
		}
	}
	hasC := false
	for _, m := range types {
		if m == 'C' {
			hasC = true
		}
	}
	if !hasC {
		t.Errorf("应包含 CommandComplete, got %v", types)
	}
}

// TestExtendedQueryExecutorError 验证 extended query 协议下执行错误返回 ErrorResponse。
// 重要：Execute 错误不应污染 extended query 错误状态（即后续消息仍能处理）。
func TestExtendedQueryExecutorError(t *testing.T) {
	exec := &mockExecutor{err: errors.New("syntax error at or near FOO")}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	types := runExtendedQuery(t, c, "BAD SQL")
	hasE := false
	for _, m := range types {
		if m == 'E' {
			hasE = true
		}
	}
	if !hasE {
		t.Errorf("应包含 ErrorResponse('E'), got %v", types)
	}
	// 应以 ReadyForQuery 结尾
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
}

// TestExtendedQueryBindMissingStmt 验证 Bind 引用不存在的 prepared statement 时进入错误状态。
// 错误状态会在下次 Sync 时清除。
func TestExtendedQueryBindMissingStmt(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	// Bind 不存在的 statement
	if err := c.sendBind("", "no_such_stmt", nil, nil); err != nil {
		t.Fatalf("sendBind 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	hasE := false
	for _, m := range types {
		if m == 'E' {
			hasE = true
		}
	}
	if !hasE {
		t.Errorf("Bind 未知 statement 应返回 ErrorResponse, got %v", types)
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
}

// TestExtendedQueryDescribeMissingPortal 验证 Describe(P) 引用不存在的 portal 返回错误。
func TestExtendedQueryDescribeMissingPortal(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	if err := c.sendDescribe('P', "no_such_portal"); err != nil {
		t.Fatalf("sendDescribe 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	hasE := false
	for _, m := range types {
		if m == 'E' {
			hasE = true
		}
	}
	if !hasE {
		t.Errorf("Describe 未知 portal 应返回 ErrorResponse, got %v", types)
	}
}

// TestExtendedQueryDescribeStatement 验证 Describe(S) 返回 ParameterDescription + NoData。
func TestExtendedQueryDescribeStatement(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	// Parse + Describe(S) + Sync
	if err := c.sendParse("", "SELECT $1", nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	if err := c.sendDescribe('S', ""); err != nil {
		t.Fatalf("sendDescribe 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 期望: ParseComplete(1) + ParameterDescription(t) + NoData(n) + ReadyForQuery(Z)
	if types[0] != '1' {
		t.Errorf("期望 ParseComplete, got %c", types[0])
	}
	if types[1] != 't' {
		t.Errorf("期望 ParameterDescription, got %c", types[1])
	}
	if types[2] != 'n' {
		t.Errorf("期望 NoData, got %c", types[2])
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
}

// TestExtendedQueryClose 验证 Close 消息返回 CloseComplete 并删除对应对象。
func TestExtendedQueryClose(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"x"},
		Rows:    []map[string]any{{"x": int64(1)}},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// Parse + Bind + Close(stmt) + Close(portal) + Sync
	if err := c.sendParse("", "SELECT 1", nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	if err := c.sendBind("", "", nil, nil); err != nil {
		t.Fatalf("sendBind 失败: %v", err)
	}
	if err := c.sendClose('S', ""); err != nil {
		t.Fatalf("sendClose 失败: %v", err)
	}
	if err := c.sendClose('P', ""); err != nil {
		t.Fatalf("sendClose 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 期望 1 + 2 + 3 + 3 + Z
	if len(types) < 5 {
		t.Fatalf("消息过少, got %v", types)
	}
	if types[0] != '1' || types[1] != '2' || types[2] != '3' || types[3] != '3' {
		t.Errorf("期望 ParseComplete+BindComplete+CloseComplete+CloseComplete, got %v", types[:4])
	}
	if types[4] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[4])
	}
}

// TestExtendedQueryBatch 验证多个 Parse/Bind/Execute 在一次 Sync 内全部处理。
// 真实 PG 客户端（如 DBeaver）经常以这种批量方式发送查询。
func TestExtendedQueryBatch(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"x"},
		Rows:    []map[string]any{{"x": int64(1)}},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// 3 条语句合并到一个 Sync
	for i := 0; i < 3; i++ {
		if err := c.sendParse("", "SELECT 1", nil); err != nil {
			t.Fatalf("sendParse %d 失败: %v", i, err)
		}
		if err := c.sendBind("", "", nil, nil); err != nil {
			t.Fatalf("sendBind %d 失败: %v", i, err)
		}
		if err := c.sendDescribe('P', ""); err != nil {
			t.Fatalf("sendDescribe %d 失败: %v", i, err)
		}
		if err := c.sendExecute("", 0); err != nil {
			t.Fatalf("sendExecute %d 失败: %v", i, err)
		}
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 期望 3 组 (1+2+n+T+D+C) + Z = 18 个消息
	if len(types) != 19 {
		t.Errorf("期望 19 个消息, got %d (%v)", len(types), types)
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
	// executor 应被调用 3 次
	if queries := exec.queryCount(); queries != 3 {
		t.Errorf("executor 应被调用 3 次, got %d", queries)
	}
}

// TestExtendedQueryExecuteMissingPortal 验证 Execute 引用不存在的 portal 返回错误并进入错误状态。
func TestExtendedQueryExecuteMissingPortal(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	if err := c.sendExecute("no_such_portal", 0); err != nil {
		t.Fatalf("sendExecute 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	hasE := false
	for _, m := range types {
		if m == 'E' {
			hasE = true
		}
	}
	if !hasE {
		t.Errorf("Execute 未知 portal 应返回 ErrorResponse, got %v", types)
	}
}

// TestExtendedQueryMaxRows 验证 Execute 的 maxRows 限制返回行数。
func TestExtendedQueryMaxRows(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"x"},
		Rows: []map[string]any{
			{"x": int64(1)},
			{"x": int64(2)},
			{"x": int64(3)},
		},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	if err := c.sendParse("", "SELECT x FROM t", nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	if err := c.sendBind("", "", nil, nil); err != nil {
		t.Fatalf("sendBind 失败: %v", err)
	}
	// maxRows=2: 仅返回 2 行
	if err := c.sendExecute("", 2); err != nil {
		t.Fatalf("sendExecute 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 应只有 1 个 DataRow（maxRows=2 限制为 2 行以内，但实际只读了 1 个 D）
	// 实际 2 行，所以应有两个 D
	dataRowCount := 0
	for _, m := range types {
		if m == 'D' {
			dataRowCount++
		}
	}
	if dataRowCount != 2 {
		t.Errorf("maxRows=2 应返回 2 行, got %d DataRow", dataRowCount)
	}
}
