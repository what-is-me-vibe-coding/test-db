package server

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 辅助类型：transientErrListener
// ---------------------------------------------------------------------------

// transientErrListener 包装 net.Listener，在第 1 次 Accept() 时注入瞬态错误。
// 使用 atomic 计数器确保并发安全。
type transientErrListener struct {
	net.Listener
	err       error
	callCount int32
}

func (l *transientErrListener) Accept() (net.Conn, error) {
	if atomic.AddInt32(&l.callCount, 1) == 1 {
		return nil, l.err
	}
	return l.Listener.Accept()
}

// ---------------------------------------------------------------------------
// acceptTCP: 瞬态错误继续重试路径（88.9% → 100%）
// ---------------------------------------------------------------------------

// startAcceptTCPOnly 手动启动 acceptTCP goroutine，不调用 Start()。
// 这样可以在 goroutine 启动之前设置自定义的 tcpListener，避免 DATA RACE。
func startAcceptTCPOnly(srv *Server, ln net.Listener) {
	srv.tcpListener = ln
	srv.wg.Add(1)
	go srv.acceptTCP()
}

// TestAcceptTCP_TransientErrorContinue 测试 acceptTCP 遇到瞬态错误后继续重试。
// 覆盖 tcp_handler.go:28-30 的瞬态错误 continue 路径。
// 关键：在 acceptTCP goroutine 启动之前设置好包装的监听器，避免 DATA RACE。
func TestAcceptTCP_TransientErrorContinue(t *testing.T) {
	realLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}

	// 包装为注入瞬态错误的监听器
	wrappedLn := &transientErrListener{
		Listener: realLn,
		err: &net.OpError{
			Op:  testOpAccept,
			Net: testNetTCP,
			Err: errors.New("resource temporarily unavailable"),
		},
	}

	srv := newTestServer(t)
	// 在 acceptTCP goroutine 启动之前设置监听器（无竞态条件）
	startAcceptTCPOnly(srv, wrappedLn)

	// 第一次 Accept() 返回瞬态错误，acceptTCP 会 continue 重试
	// 第二次 Accept() 返回真实连接
	conn, err := net.DialTimeout("tcp", realLn.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	// 发送 ping 确保连接正常处理
	pingPkt := NewPacket(PacketPing, nil)
	if _, err := conn.Write(pingPkt.Encode()); err != nil {
		t.Fatalf("写入 Ping 包失败: %v", err)
	}
	resp, err := DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应类型 = %d, 期望 %d", resp.Type, PacketResponse)
	}

	// 必须在 Stop() 之前关闭连接，否则 handleTCPConn 会阻塞在读取上
	_ = conn.Close()
	_ = srv.Stop()
}

// TestAcceptTCP_TooManyOpenFiles 测试 "too many open files" 瞬态错误后继续重试。
func TestAcceptTCP_TooManyOpenFiles(t *testing.T) {
	realLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}

	wrappedLn := &transientErrListener{
		Listener: realLn,
		err: &net.OpError{
			Op:  testOpAccept,
			Net: testNetTCP,
			Err: errors.New("too many open files"),
		},
	}

	srv := newTestServer(t)
	startAcceptTCPOnly(srv, wrappedLn)

	conn, err := net.DialTimeout("tcp", realLn.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	pingPkt := NewPacket(PacketPing, nil)
	if _, err := conn.Write(pingPkt.Encode()); err != nil {
		t.Fatalf("写入 Ping 包失败: %v", err)
	}
	_, err = DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	// 必须在 Stop() 之前关闭连接，否则 handleTCPConn 会阻塞在读取上
	_ = conn.Close()
	_ = srv.Stop()
}

// TestAcceptTCP_TimeoutError 测试 acceptTCP 遇到超时 OpError 后继续重试。
// 覆盖 isTransientAcceptErr 中 OpError.Timeout() 返回 true 的路径。
func TestAcceptTCP_TimeoutError(t *testing.T) {
	realLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}

	// 使用 handler_test.go 中已有的 timeoutError 类型
	wrappedLn := &transientErrListener{
		Listener: realLn,
		err: &net.OpError{
			Op:  testOpAccept,
			Net: testNetTCP,
			Err: timeoutError{},
		},
	}

	srv := newTestServer(t)
	startAcceptTCPOnly(srv, wrappedLn)

	conn, err := net.DialTimeout("tcp", realLn.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	pingPkt := NewPacket(PacketPing, nil)
	if _, err := conn.Write(pingPkt.Encode()); err != nil {
		t.Fatalf("写入 Ping 包失败: %v", err)
	}
	_, err = DecodePacket(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	// 必须在 Stop() 之前关闭连接，否则 handleTCPConn 会阻塞在读取上
	_ = conn.Close()
	_ = srv.Stop()
}

// ---------------------------------------------------------------------------
// handleTCPConn: isClosedConnErr(io.EOF) 路径
// ---------------------------------------------------------------------------

// TestHandleTCPConn_PipeEOF 使用 net.Pipe 直接测试 isClosedConnErr(io.EOF) 路径。
func TestHandleTCPConn_PipeEOF(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	serverConn, clientConn := net.Pipe()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		pingPkt := NewPacket(PacketPing, nil)
		if _, err := clientConn.Write(pingPkt.Encode()); err != nil {
			return
		}
		_, err := DecodePacket(bufio.NewReader(clientConn))
		if err != nil {
			return
		}
		_ = clientConn.Close() // 关闭连接，使服务器收到 io.EOF
	}()

	srv.wg.Add(1)
	srv.handleTCPConn(serverConn)
	<-clientDone
}

// TestHandleTCPConn_PipeDecodeError 测试非关闭连接的解码错误路径。
// 覆盖 tcp_handler.go:74-75 的非 isClosedConnErr 解码错误路径。
func TestHandleTCPConn_PipeDecodeError(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	serverConn, clientConn := net.Pipe()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		// 发送无效数据（Magic 不匹配）
		_, _ = clientConn.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x01, 0xAA})
		_ = clientConn.Close()
	}()

	srv.wg.Add(1)
	srv.handleTCPConn(serverConn)
	<-clientDone
}

// ---------------------------------------------------------------------------
// handleTCPConn: isClosedConnErr(net.ErrClosed) 路径
// ---------------------------------------------------------------------------

// mockNetErrConn 是一个模拟 net.Conn 的类型，在 Write 后使 Read 返回指定错误。
// 不使用 net.Pipe，避免同步阻塞问题。
type mockNetErrConn struct {
	readBuf  []byte
	readPos  int
	readErr  error
	wrote    bool
	writeBuf []byte
}

func (c *mockNetErrConn) Read(b []byte) (int, error) {
	if c.wrote && c.readErr != nil {
		return 0, c.readErr
	}
	if c.readPos >= len(c.readBuf) {
		return 0, io.EOF
	}
	n := copy(b, c.readBuf[c.readPos:])
	c.readPos += n
	return n, nil
}

func (c *mockNetErrConn) Write(b []byte) (int, error) {
	c.writeBuf = append(c.writeBuf, b...)
	c.wrote = true
	return len(b), nil
}

func (c *mockNetErrConn) Close() error                       { return nil }
func (c *mockNetErrConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *mockNetErrConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *mockNetErrConn) SetDeadline(_ time.Time) error      { return nil }
func (c *mockNetErrConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *mockNetErrConn) SetWriteDeadline(_ time.Time) error { return nil }

// TestHandleTCPConn_PipeNetErrClosed 测试服务器写入响应后 Read 返回 net.ErrClosed，
// 触发 isClosedConnErr(errors.Is(err, net.ErrClosed)) 返回 true 的路径。
// 覆盖 tcp_handler.go:71-73 的 isClosedConnErr 返回 true 路径。
func TestHandleTCPConn_PipeNetErrClosed(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	// 构造 ping 包数据
	pingPkt := NewPacket(PacketPing, nil)
	pingData := pingPkt.Encode()

	// 创建模拟连接：第一次 Read 返回 ping 数据，Write 后 Read 返回 net.ErrClosed
	mockConn := &mockNetErrConn{
		readBuf: pingData,
		readErr: net.ErrClosed,
	}

	srv.wg.Add(1)
	// handleTCPConn 处理完 ping 后写入响应（设置 wrote 标志），
	// 下次循环读取时 Read 返回 net.ErrClosed，DecodePacket 包装该错误，
	// isClosedConnErr 通过 errors.Is 检测到 net.ErrClosed 返回 true
	srv.handleTCPConn(mockConn)
}

// ---------------------------------------------------------------------------
// protocol.go: 超大负载错误路径
// ---------------------------------------------------------------------------

// TestDecodePacket_OversizedPayload 测试解码超过最大负载大小的包。
func TestDecodePacket_OversizedPayload(t *testing.T) {
	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(header[0:4], Magic)
	binary.BigEndian.PutUint16(header[4:6], ProtocolVersion)
	header[6] = PacketQuery
	binary.BigEndian.PutUint32(header[7:11], MaxPacketSize+1)

	_, err := DecodePacket(bytes.NewReader(header))
	if err == nil {
		t.Error("期望返回超大负载错误，但成功解码")
	}
	if err != nil && !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("错误信息应包含 'exceeds maximum'，实际: %v", err)
	}
}
