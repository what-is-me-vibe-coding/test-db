package pgwire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
)

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
