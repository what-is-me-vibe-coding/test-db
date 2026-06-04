package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/server"
)

// --- executeTCP 边界测试 ---

func TestCLIExecuteTCPWriteFailAfterConnect(t *testing.T) {
	tcpAddr, httpAddr := startServer(t)
	c := newCLI(tcpAddr, httpAddr, testModeTCP)
	defer c.close()
	if err := c.connect(); err != nil {
		t.Fatalf("connect 失败: %v", err)
	}
	// Close the underlying connection but keep c.conn non-nil so reconnect is NOT attempted
	_ = c.conn.Close()
	// c.conn is still set (non-nil), so executeTCP will try to write on the closed conn
	_, err := c.execute(testSQL)
	if err == nil {
		t.Error("期望写入失败错误")
	}
	if !strings.Contains(err.Error(), "发送请求失败") {
		t.Errorf("错误应包含 '发送请求失败': %v", err)
	}
	if c.conn != nil {
		t.Error("写入失败后 conn 应被置为 nil")
	}
}

func TestCLIExecuteTCPResponseUnmarshalFail(t *testing.T) {
	// Start a mock TCP server that sends a valid packet with corrupted JSON payload
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen 失败: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read the client's request packet (discard it)
		header := make([]byte, server.HeaderSize)
		if _, err := conn.Read(header); err != nil {
			return
		}
		length := binary.BigEndian.Uint32(header[7:11])
		if length > 0 {
			payload := make([]byte, length)
			if _, err := conn.Read(payload); err != nil {
				return
			}
		}

		// Send back a valid packet with corrupted (non-JSON) payload
		corruptedPayload := []byte("this is not json!!!")
		respPkt := server.NewPacket(server.PacketResponse, corruptedPayload)
		_, _ = conn.Write(respPkt.Encode())
	}()

	c := newCLI(ln.Addr().String(), "127.0.0.1:1", testModeTCP)
	defer c.close()
	_, execErr := c.execute("SELECT 1")
	if execErr == nil {
		t.Error("期望解析响应失败错误")
	}
	if !strings.Contains(execErr.Error(), "解析响应失败") {
		t.Errorf("错误应包含 '解析响应失败': %v", execErr)
	}
}

// --- runInteractive 补充测试 ---

func TestCLIRunInteractiveUseTCP(t *testing.T) {
	tcpAddr, httpAddr := startServer(t)
	c := newCLI(tcpAddr, httpAddr, testModeHTTP) // start in HTTP mode
	defer c.close()
	out, _ := runInt(c, "\\use TCP\n\\q\n")
	if !strings.Contains(out, "已切换到 TCP 模式") {
		t.Errorf("输出应包含 TCP 模式切换信息: %q", out)
	}
	if c.mode != testModeTCP {
		t.Errorf("模式应为 tcp, 实际: %s", c.mode)
	}
}

func TestCLIRunInteractiveQuitAlias(t *testing.T) {
	tcpAddr, httpAddr := startServer(t)
	c := newCLI(tcpAddr, httpAddr, testModeTCP)
	defer c.close()
	out, err := runInt(c, "\\quit\n")
	if err != nil && err.Error() != "EOF" {
		t.Errorf("不应返回非 EOF 错误: %v", err)
	}
	if !strings.Contains(out, "再见!") {
		t.Errorf("输出应包含 '再见!': %q", out)
	}
}

// --- runCLI -mode 标志测试 ---

func TestRunCLIModeFlagTCP(t *testing.T) {
	tcpAddr, httpAddr := startServer(t)
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{testFlagTCP, tcpAddr, testFlagHTTP, httpAddr, testFlagMode, "TCP", "-e", testSQL}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, stderr: %s", code, stderr.String())
	}
	if stdout.String() == "" {
		t.Error("stdout 不应为空")
	}
}

func TestRunCLIModeFlagHTTP(t *testing.T) {
	tcpAddr, httpAddr := startServer(t)
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{testFlagTCP, tcpAddr, testFlagHTTP, httpAddr, testFlagMode, "HTTP", "-e", testSQL}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, stderr: %s", code, stderr.String())
	}
	if stdout.String() == "" {
		t.Error("stdout 不应为空")
	}
}
