package server

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// --- handleTCPConn: handlePacket error response path ---

func TestHandleTCPConn_HandlePacketError(t *testing.T) {
	srv := newTestServer(t) // no table created
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send a PacketWrite with invalid payload (not valid WriteRequest JSON).
	// This will cause handlePacket -> handleWritePacket to return an error
	// (json unmarshal failure), which triggers the error response path in
	// handleTCPConn (Code: -1).
	invalidWritePkt := NewPacket(PacketWrite, []byte("not json"))
	if _, err := conn.Write(invalidWritePkt.Encode()); err != nil {
		t.Fatalf("write invalid write packet failed: %v", err)
	}

	resp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if resp.Type != PacketResponse {
		t.Errorf("response type = %d, want %d", resp.Type, PacketResponse)
	}

	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if response.Code != -1 {
		t.Errorf("response Code = %d, want -1; Message = %q", response.Code, response.Message)
	}
}

// --- handleTCPConn: write then query round-trip ---

func TestHandleTCPConn_WriteThenQuery(t *testing.T) {
	srv := newTestServerWithTable(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Write a row
	writePayload, _ := json.Marshal(WriteRequest{
		Table: testTable,
		Rows: []map[string]interface{}{
			{"id": float64(1), testColName: testName, "score": 88.0},
		},
	})
	writePkt := NewPacket(PacketWrite, writePayload)
	if _, err := conn.Write(writePkt.Encode()); err != nil {
		t.Fatalf("write packet failed: %v", err)
	}

	writeResp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("decode write response failed: %v", err)
	}
	var wr Response
	if err := json.Unmarshal(writeResp.Payload, &wr); err != nil {
		t.Fatalf("unmarshal write response failed: %v", err)
	}
	if wr.Code != 0 {
		t.Fatalf("write Code = %d, want 0; Message = %q", wr.Code, wr.Message)
	}

	// Query the data back
	queryPayload, _ := json.Marshal(QueryRequest{SQL: testSelectAll})
	queryPkt := NewPacket(PacketQuery, queryPayload)
	if _, err := conn.Write(queryPkt.Encode()); err != nil {
		t.Fatalf("write query packet failed: %v", err)
	}

	queryResp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("decode query response failed: %v", err)
	}
	var qr Response
	if err := json.Unmarshal(queryResp.Payload, &qr); err != nil {
		t.Fatalf("unmarshal query response failed: %v", err)
	}
	if qr.Code != 0 {
		t.Errorf("query Code = %d, want 0; Message = %q", qr.Code, qr.Message)
	}
}

// --- handleTCPConn: ping then query on same connection ---

func TestHandleTCPConn_PingThenQuery(t *testing.T) {
	srv := newTestServerWithTable(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send ping first
	pingPkt := NewPacket(PacketPing, nil)
	if _, err := conn.Write(pingPkt.Encode()); err != nil {
		t.Fatalf("write ping failed: %v", err)
	}
	pingResp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("decode ping response failed: %v", err)
	}
	var pr Response
	if err := json.Unmarshal(pingResp.Payload, &pr); err != nil {
		t.Fatalf("unmarshal ping response failed: %v", err)
	}
	if pr.Code != 0 || pr.Message != msgPong {
		t.Errorf("ping response Code=%d Message=%q, want Code=0 Message='pong'", pr.Code, pr.Message)
	}

	// Then send a query on the same connection
	queryPayload, _ := json.Marshal(QueryRequest{SQL: testSelectAll})
	queryPkt := NewPacket(PacketQuery, queryPayload)
	if _, err := conn.Write(queryPkt.Encode()); err != nil {
		t.Fatalf("write query packet failed: %v", err)
	}
	queryResp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("decode query response failed: %v", err)
	}
	var qr Response
	if err := json.Unmarshal(queryResp.Payload, &qr); err != nil {
		t.Fatalf("unmarshal query response failed: %v", err)
	}
	if qr.Code != 0 {
		t.Errorf("query Code = %d, want 0; Message = %q", qr.Code, qr.Message)
	}
}

// --- handleTCPConn: write to non-existent table returns error response ---

func TestHandleTCPConn_WriteToNonExistentTable(t *testing.T) {
	srv := newTestServer(t) // no table
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send a valid WriteRequest JSON but for a table that doesn't exist.
	// handleWrite will return an error (table not found), which triggers
	// the error response path in handleTCPConn.
	writePayload, _ := json.Marshal(WriteRequest{
		Table: "nonexistent", //nolint:goconst
		Rows: []map[string]interface{}{
			{"id": float64(1), "name": testTableName},
		},
	})
	writePkt := NewPacket(PacketWrite, writePayload)
	if _, err := conn.Write(writePkt.Encode()); err != nil {
		t.Fatalf("write packet failed: %v", err)
	}

	resp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if response.Code != -1 {
		t.Errorf("response Code = %d, want -1; Message = %q", response.Code, response.Message)
	}
}

// --- Start 错误路径测试 ---

func TestStartTCPAddrInUse(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	time.Sleep(50 * time.Millisecond)

	srv2 := newTestServer(t)
	srv2.cfg.TCPAddr = srv.tcpListener.Addr().String()

	err := srv2.Start()
	if err == nil {
		_ = srv2.Stop()
		t.Error("期望 TCP 端口冲突错误，但启动成功")
	}
	if !strings.Contains(err.Error(), "listen tcp") {
		t.Errorf("错误信息应包含 'listen tcp'，实际: %v", err)
	}
}

func TestTCPConnectionDuringShutdown(t *testing.T) {
	srv := newTestServerWithTable(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Establish a TCP connection
	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}

	// Send a ping to confirm the connection works
	pingPkt := NewPacket(PacketPing, nil)
	if _, err := conn.Write(pingPkt.Encode()); err != nil {
		t.Fatalf("write ping failed: %v", err)
	}
	resp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("read ping response failed: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("ping response type = %d, want %d", resp.Type, PacketResponse)
	}

	// Close the client connection so the server's handleTCPConn loop
	// hits io.EOF on the next read and exits cleanly.
	_ = conn.Close()

	// Stop the server; should complete quickly since the TCP handler
	// will see the closed connection.
	done := make(chan error, 1)
	go func() {
		done <- srv.Stop()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server shutdown timed out")
	}
}

func TestTCPMultipleRequestResponse(t *testing.T) {
	srv := newTestServerWithTable(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send multiple ping requests on the same connection and verify all get responses
	for i := 0; i < 5; i++ {
		pingPkt := NewPacket(PacketPing, nil)
		if _, err := conn.Write(pingPkt.Encode()); err != nil {
			t.Fatalf("write ping #%d failed: %v", i, err)
		}

		resp, err := DecodePacket(bufio.NewReader(conn))
		if err != nil {
			t.Fatalf("read response #%d failed: %v", i, err)
		}

		if resp.Type != PacketResponse {
			t.Errorf("response #%d type = %d, want %d", i, resp.Type, PacketResponse)
		}

		var response Response
		if err := json.Unmarshal(resp.Payload, &response); err != nil {
			t.Fatalf("unmarshal response #%d failed: %v", i, err)
		}
		if response.Code != 0 {
			t.Errorf("response #%d Code = %d, want 0", i, response.Code)
		}
		if response.Message != msgPong {
			t.Errorf("response #%d Message = %q, want %q", i, response.Message, msgPong)
		}
	}
}

// TestTCPReadDeadlineError tests that handleTCPConn exits gracefully when
// the server shuts down while a TCP connection is idle. This exercises the
// done channel path in the handleTCPConn loop.
func TestTCPReadDeadlineError(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}

	// Leave the connection idle (don't send any data).
	// Call Stop() to trigger the done channel path in handleTCPConn.
	// Close the client connection concurrently to unblock the server's read.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}()

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestTCPServerShutdownDuringConnection tests that the server shuts down
// cleanly when Stop() is called while a TCP connection is active.
func TestTCPServerShutdownDuringConnection(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}

	// Send a ping to confirm the connection works
	pingPkt := NewPacket(PacketPing, nil)
	if _, err := conn.Write(pingPkt.Encode()); err != nil {
		t.Fatalf("write ping failed: %v", err)
	}
	resp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("read ping response failed: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("ping response type = %d, want %d", resp.Type, PacketResponse)
	}

	// Stop the server while the connection is still active.
	// Close the client connection concurrently to unblock the server.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- srv.Stop()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server shutdown timed out")
	}
}

// TestTCPWriteToClosedConnection tests that handleTCPConn handles write
// errors gracefully when the client closes the connection mid-request.
func TestTCPWriteToClosedConnection(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}

	// Send a ping and immediately close the connection.
	// The server may fail to write the response, testing the write error path.
	pingPkt := NewPacket(PacketPing, nil)
	if _, err := conn.Write(pingPkt.Encode()); err != nil {
		t.Fatalf("write ping failed: %v", err)
	}
	_ = conn.Close()

	// Give the server time to process the closed connection
	time.Sleep(200 * time.Millisecond)

	// Verify the server is still running and accepting new connections
	conn2, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("server not accepting new connections after write error: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	pingPkt2 := NewPacket(PacketPing, nil)
	if _, err := conn2.Write(pingPkt2.Encode()); err != nil {
		t.Fatalf("write ping on second connection failed: %v", err)
	}
	resp2, err := DecodePacket(bufio.NewReader(conn2))
	if err != nil {
		t.Fatalf("read ping response on second connection failed: %v", err)
	}
	if resp2.Type != PacketResponse {
		t.Errorf("second ping response type = %d, want %d", resp2.Type, PacketResponse)
	}
}

// --- handlePacket 错误路径直接测试 ---

// TestHandleQueryPacketUnmarshalError 测试 handleQueryPacket 在无效 JSON 载荷时返回错误。
func TestHandleQueryPacketUnmarshalError(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	pkt := NewPacket(PacketQuery, []byte("{{invalid json"))
	_, err := srv.handleQueryPacket(pkt)
	if err == nil {
		t.Error("期望无效 JSON 返回错误，但返回 nil")
	}
}

// TestHandleWritePacketUnmarshalError 测试 handleWritePacket 在无效 JSON 载荷时返回错误。
func TestHandleWritePacketUnmarshalError(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	pkt := NewPacket(PacketWrite, []byte("{{invalid json"))
	_, err := srv.handleWritePacket(pkt)
	if err == nil {
		t.Error("期望无效 JSON 返回错误，但返回 nil")
	}
}

// TestHandlePacketUnknownType 测试 handlePacket 在未知包类型时返回错误。
func TestHandlePacketUnknownTypeDirect(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	pkt := NewPacket(255, nil)
	_, err := srv.handlePacket(pkt)
	if err == nil {
		t.Error("期望未知包类型返回错误，但返回 nil")
	}
}
