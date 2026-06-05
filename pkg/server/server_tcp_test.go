package server

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

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
