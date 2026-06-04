package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// --- isClosedConnErr tests ---

func TestIsClosedConnErr_EOF(t *testing.T) {
	if !isClosedConnErr(io.EOF) {
		t.Error("isClosedConnErr(io.EOF) = false, want true")
	}
}

func TestIsClosedConnErr_OpErrorTimeout(t *testing.T) {
	// Use a real net.Error that reports Timeout() = true
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(-1 * time.Second))
	_, readErr := bufio.NewReader(conn).ReadByte()

	if readErr == nil {
		t.Fatal("expected a deadline error, got nil")
	}

	// The error from a timed-out read should be a *net.OpError with Timeout()=true
	if !isClosedConnErr(readErr) {
		t.Errorf("isClosedConnErr(timeout error) = false, want true; err=%T: %v", readErr, readErr)
	}
}

func TestIsClosedConnErr_OpErrorNotTimeout(t *testing.T) {
	opErr := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("some error")}
	if isClosedConnErr(opErr) {
		t.Error("isClosedConnErr(non-timeout OpError) = true, want false")
	}
}

func TestIsClosedConnErr_OtherError(t *testing.T) {
	if isClosedConnErr(errors.New("random error")) {
		t.Error("isClosedConnErr(random error) = true, want false")
	}
}

// --- handleTCPConn tests ---

func TestHandleTCPConn_ValidQueryPacket(t *testing.T) {
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

	queryPayload, _ := json.Marshal(QueryRequest{SQL: testSelectAll})
	queryPkt := NewPacket(PacketQuery, queryPayload)
	if _, err := conn.Write(queryPkt.Encode()); err != nil {
		t.Fatalf("write query packet failed: %v", err)
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
	if response.Code != 0 {
		t.Errorf("response Code = %d, want 0; Message = %q", response.Code, response.Message)
	}
}

func TestHandleTCPConn_InvalidPacket(t *testing.T) {
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
	defer func() { _ = conn.Close() }()

	// Write garbage bytes that will fail DecodePacket (bad magic)
	garbage := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if _, err := conn.Write(garbage); err != nil {
		t.Fatalf("write garbage failed: %v", err)
	}

	// Server should close the connection after decode error.
	// Set a deadline so we don't block forever.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1024)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		// If we got data, that's unexpected but not a failure; the important
		// thing is the server didn't panic.
		t.Log("server sent data before closing; that's acceptable")
	}
}

func TestHandleTCPConn_ServerShutdown(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.tcpListener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial TCP failed: %v", err)
	}

	// Send a ping to confirm connection works
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

	// Close client connection so the server's handleTCPConn loop
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

// --- serveHTTP tests ---

func TestServeHTTP_GracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		TCPAddr:  testListenAddr,
		HTTPAddr: testListenAddr,
		DataDir:  dir,
	}
	registry := prometheus.NewRegistry()
	srv, err := NewServer(cfg, WithMetricsRegistry(registry))
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Verify HTTP is serving
	baseURL := "http://" + srv.httpListener.Addr().String()
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Gracefully stop the server; serveHTTP should exit cleanly via <-s.done
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// After stop, HTTP should be unavailable
	_, err = http.Get(baseURL + "/health")
	if err == nil {
		t.Error("expected error after server stop, got nil")
	}
}

// --- handlePacket tests ---

func TestHandlePacket_UnknownType(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	pkt := NewPacket(255, nil)
	resp, err := srv.handlePacket(pkt)
	if err == nil {
		t.Error("handlePacket(unknown type) expected error, got nil")
	}
	if resp != nil {
		t.Errorf("handlePacket(unknown type) resp = %v, want nil", resp)
	}
	if err.Error() != "未知的包类型: 255" {
		t.Errorf("error = %q, want %q", err.Error(), "未知的包类型: 255")
	}
}

// --- NewServer default MaxMemTableSize test ---

func TestNewServer_DefaultMaxMemTableSize(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		TCPAddr:  testListenAddr,
		HTTPAddr: testListenAddr,
		DataDir:  dir,
		// MaxMemTableSize is 0, should default to 64MB
	}
	registry := prometheus.NewRegistry()
	srv, err := NewServer(cfg, WithMetricsRegistry(registry))
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	const expected int64 = 64 * 1024 * 1024
	if srv.cfg.MaxMemTableSize != expected {
		t.Errorf("MaxMemTableSize = %d, want %d", srv.cfg.MaxMemTableSize, expected)
	}
}

func TestNewServer_CustomMaxMemTableSize(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		TCPAddr:         testListenAddr,
		HTTPAddr:        testListenAddr,
		DataDir:         dir,
		MaxMemTableSize: 128 * 1024 * 1024,
	}
	registry := prometheus.NewRegistry()
	srv, err := NewServer(cfg, WithMetricsRegistry(registry))
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	if srv.cfg.MaxMemTableSize != 128*1024*1024 {
		t.Errorf("MaxMemTableSize = %d, want %d", srv.cfg.MaxMemTableSize, 128*1024*1024)
	}
}
