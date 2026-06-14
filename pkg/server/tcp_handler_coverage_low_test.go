package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
)

const (
	v14OpAccept    = "accept"
	v14OpRead      = "read"
	v14Nonexistent = "nonexistent"
	testNameIOEOF  = "io.EOF"
	testNameNilErr = "nil error"
)

// ---------------------------------------------------------------------------
// isTransientAcceptErr: comprehensive tests
// ---------------------------------------------------------------------------

func TestIsTransientAcceptErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"non-OpError", errors.New("some error"), false},
		{testNameNilErr, nil, false},
		{"OpError with timeout", &net.OpError{Op: v14OpAccept, Net: testNetTCP, Err: timeoutError{}}, true},
		{"OpError resource temporarily unavailable", &net.OpError{Op: v14OpAccept, Net: testNetTCP, Err: errors.New("resource temporarily unavailable")}, true},
		{"OpError too many open files", &net.OpError{Op: v14OpAccept, Net: testNetTCP, Err: errors.New("too many open files")}, true},
		{"OpError other message", &net.OpError{Op: v14OpAccept, Net: testNetTCP, Err: errors.New("connection refused")}, false},
		{"OpError partial match resource", &net.OpError{Op: v14OpAccept, Net: testNetTCP, Err: errors.New("accept: resource temporarily unavailable - retry")}, true},
		{"OpError partial match too many", &net.OpError{Op: v14OpAccept, Net: testNetTCP, Err: errors.New("socket: too many open files in system")}, true},
		{"OpError empty message", &net.OpError{Op: v14OpAccept, Net: testNetTCP, Err: errors.New("")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientAcceptErr(tt.err); got != tt.want {
				t.Errorf("isTransientAcceptErr() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isClosedConnErr: comprehensive tests
// ---------------------------------------------------------------------------

func TestIsClosedConnErr_Table(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{testNameIOEOF, io.EOF, true},
		{"net.ErrClosed", net.ErrClosed, true},
		{"wrapped net.ErrClosed", fmt.Errorf("wrapped: %w", net.ErrClosed), true},
		{"double-wrapped net.ErrClosed", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", net.ErrClosed)), true},
		{"timeout OpError", &net.OpError{Op: v14OpRead, Net: testNetTCP, Err: timeoutError{}}, true},
		{"non-timeout OpError", &net.OpError{Op: v14OpRead, Net: testNetTCP, Err: errors.New("connection reset")}, false},
		{"arbitrary error", errors.New("some error"), false},
		{testNameNilErr, nil, false},
		{"OpError empty Err", &net.OpError{Op: v14OpRead, Net: testNetTCP, Err: errors.New("")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClosedConnErr(tt.err); got != tt.want {
				t.Errorf("isClosedConnErr() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handlePacket: unknown packet type paths
// ---------------------------------------------------------------------------

func TestHandlePacket_UnknownTypes(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name    string
		pktType uint8
		payload []byte
	}{
		{"type 0", 0, nil},
		{"type 4", 4, nil},
		{"type 255", 255, nil},
		{"type 99 with payload", 99, []byte(`{"test": true}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := NewPacket(tt.pktType, tt.payload)
			resp, err := srv.handlePacket(pkt)
			if err == nil {
				t.Error("unknown packet type should return error")
			}
			if resp != nil {
				t.Errorf("resp = %v, want nil", resp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleQueryPacket: invalid JSON payload paths
// ---------------------------------------------------------------------------

func TestHandleQueryPacket_BadJSON(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name    string
		payload []byte
	}{
		{"empty", []byte{}},
		{"partial JSON", []byte("{")},
		{"number", []byte("42")},
		{"array", []byte(`[1,2,3]`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := NewPacket(PacketQuery, tt.payload)
			resp, err := srv.handleQueryPacket(pkt)
			if err == nil {
				t.Error("invalid JSON should return error")
			}
			if resp != nil {
				t.Errorf("resp = %v, want nil", resp)
			}
		})
	}
}

// "null" is valid JSON and unmarshals into QueryRequest as an empty struct,
// producing QueryRequest{SQL: ""} which fails at SQL parse (Code=-1, no Go error).
func TestHandleQueryPacket_NullPayload(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	pkt := NewPacket(PacketQuery, []byte("null"))
	resp, err := srv.handleQueryPacket(pkt)
	if err != nil {
		t.Fatalf("null payload should unmarshal: %v", err)
	}
	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Code != -1 {
		t.Errorf("Code = %d, want -1", response.Code)
	}
}

// ---------------------------------------------------------------------------
// handleQueryPacket: valid query returning error response from handleQuery
// ---------------------------------------------------------------------------
// Note: handleQuery never returns a non-nil Go error; errors are encoded as
// Response{Code: -1}. The error path in handleQueryPacket where handleQuery
// returns a non-nil error is currently unreachable without code changes.

func TestHandleQueryPacket_ErrorResponses(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{"parse error", testInvalidSQL},
		{"table not exist", testSelectNonexistent},
		{"empty SQL", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t)
			defer func() { _ = srv.Stop() }()

			payload, _ := json.Marshal(QueryRequest{SQL: tt.sql})
			pkt := NewPacket(PacketQuery, payload)
			resp, err := srv.handleQueryPacket(pkt)
			if err != nil {
				t.Fatalf("handleQueryPacket should not return Go error: %v", err)
			}
			var response Response
			if err := json.Unmarshal(resp.Payload, &response); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if response.Code != -1 {
				t.Errorf("Code = %d, want -1", response.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleWritePacket: invalid JSON payload paths
// ---------------------------------------------------------------------------

func TestHandleWritePacket_BadJSON(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name    string
		payload []byte
	}{
		{"empty", []byte{}},
		{"partial JSON", []byte("{")},
		{"number", []byte("42")},
		{"string", []byte(`"hello"`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := NewPacket(PacketWrite, tt.payload)
			resp, err := srv.handleWritePacket(pkt)
			if err == nil {
				t.Error("invalid JSON should return error")
			}
			if resp != nil {
				t.Errorf("resp = %v, want nil", resp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleWritePacket: valid write returning error response from handleWrite
// ---------------------------------------------------------------------------
// Note: handleWrite never returns a non-nil Go error; errors are encoded as
// Response{Code: -1}. The error path in handleWritePacket where handleWrite
// returns a non-nil error is currently unreachable.

func TestHandleWritePacket_ErrorResponses(t *testing.T) {
	tests := []struct {
		name  string
		table string
		rows  []map[string]interface{}
	}{
		{"table not exist", v14Nonexistent, []map[string]interface{}{{"id": float64(1)}}},
		{"missing primary key", testTable, []map[string]interface{}{{testColName: testName}}},
		{"type mismatch", testTable, []map[string]interface{}{{"id": float64(1), testColName: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServerWithTable(t)
			defer func() { _ = srv.Stop() }()

			payload, _ := json.Marshal(WriteRequest{Table: tt.table, Rows: tt.rows})
			pkt := NewPacket(PacketWrite, payload)
			resp, err := srv.handleWritePacket(pkt)
			if err != nil {
				t.Fatalf("handleWritePacket should not return Go error: %v", err)
			}
			var response Response
			if err := json.Unmarshal(resp.Payload, &response); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if response.Code != -1 {
				t.Errorf("Code = %d, want -1", response.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handlePing: response content verification
// ---------------------------------------------------------------------------
// Note: handlePing always constructs Response{Code:0, Message:"pong"}, which
// is always marshallable. The json.Marshal error path is unreachable.

func TestHandlePing_ResponseContent(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	resp, err := srv.handlePing()
	if err != nil {
		t.Fatalf("handlePing failed: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("type = %d, want %d", resp.Type, PacketResponse)
	}
	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("Code = %d, want 0", response.Code)
	}
	if response.Message != msgPong {
		t.Errorf("Message = %q, want %q", response.Message, msgPong)
	}
}

// ---------------------------------------------------------------------------
// handlePacket: routing verification
// ---------------------------------------------------------------------------

func TestHandlePacket_RouteInvalidPayloads(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	// Query route with invalid payload
	if _, err := srv.handlePacket(NewPacket(PacketQuery, []byte("bad"))); err == nil {
		t.Error("PacketQuery with bad payload should return error")
	}
	// Write route with invalid payload
	if _, err := srv.handlePacket(NewPacket(PacketWrite, []byte("bad"))); err == nil {
		t.Error("PacketWrite with bad payload should return error")
	}
	// Ping route succeeds
	resp, err := srv.handlePacket(NewPacket(PacketPing, nil))
	if err != nil {
		t.Fatalf("PacketPing should not error: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("ping resp type = %d, want %d", resp.Type, PacketResponse)
	}
}

// ---------------------------------------------------------------------------
// handleQueryPacket / handleWritePacket: closed storage
// ---------------------------------------------------------------------------

func TestHandleQueryPacket_ClosedStorage(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	_ = srv.storage.Close()
	payload, _ := json.Marshal(QueryRequest{SQL: testSelectAll})
	pkt := NewPacket(PacketQuery, payload)
	resp, err := srv.handleQueryPacket(pkt)
	if err != nil {
		t.Logf("closed storage returned Go error: %v", err)
	}
	if resp != nil {
		var response Response
		if unmarshalErr := json.Unmarshal(resp.Payload, &response); unmarshalErr == nil {
			t.Logf("closed storage: Code=%d, Message=%q", response.Code, response.Message)
		}
	}
}

func TestHandleWritePacket_ClosedStorage(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	_ = srv.storage.Close()
	payload, _ := json.Marshal(WriteRequest{
		Table: testTable,
		Rows:  []map[string]interface{}{{"id": float64(1), testColName: testName}},
	})
	pkt := NewPacket(PacketWrite, payload)
	resp, err := srv.handleWritePacket(pkt)
	if err != nil {
		t.Logf("closed storage returned Go error: %v", err)
	}
	if resp != nil {
		var response Response
		if unmarshalErr := json.Unmarshal(resp.Payload, &response); unmarshalErr == nil {
			if response.Code != -1 {
				t.Errorf("Code = %d, want -1 (closed storage)", response.Code)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// handleQueryPacket: 有效查询、无效 JSON、查询执行错误路径（80.0% → >90%）
// ---------------------------------------------------------------------------

// TestHandleQueryPacket_ValidQueryV6 测试 handleQueryPacket 处理有效 SQL 查询。
func TestHandleQueryPacket_ValidQueryV6(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	// 先写入一条数据
	writePayload, _ := json.Marshal(WriteRequest{
		Table: testTable,
		Rows:  []map[string]interface{}{{"id": float64(1), testColName: testName}},
	})
	writePkt := NewPacket(PacketWrite, writePayload)
	writeResp, err := srv.handleWritePacket(writePkt)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	var writeResponse Response
	if err := json.Unmarshal(writeResp.Payload, &writeResponse); err != nil {
		t.Fatalf("解析写入响应失败: %v", err)
	}
	if writeResponse.Code != 0 {
		t.Fatalf("写入返回错误: Code=%d, Message=%q", writeResponse.Code, writeResponse.Message)
	}

	// 发送有效查询
	queryPayload, _ := json.Marshal(QueryRequest{SQL: testSelectAll})
	pkt := NewPacket(PacketQuery, queryPayload)
	resp, err := srv.handleQueryPacket(pkt)
	if err != nil {
		t.Fatalf("handleQueryPacket 有效查询失败: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应类型 = %d，期望 %d", resp.Type, PacketResponse)
	}
	if resp.Magic != Magic {
		t.Errorf("响应 Magic = 0x%08x，期望 0x%08x", resp.Magic, Magic)
	}
	if resp.Version != ProtocolVersion {
		t.Errorf("响应 Version = %d，期望 %d", resp.Version, ProtocolVersion)
	}

	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("解析查询响应失败: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("响应 Code = %d，期望 0，Message = %q", response.Code, response.Message)
	}
	if response.Rows != 1 {
		t.Errorf("响应 Rows = %d，期望 1", response.Rows)
	}
}

// TestHandleQueryPacket_InvalidJSONVariantsV6 测试 handleQueryPacket 收到各种无效 JSON 时的错误返回。
func TestHandleQueryPacket_InvalidJSONVariantsV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name    string
		payload []byte
	}{
		{testPlainText, []byte("hello world")},
		{"不完整JSON对象", []byte(`{"sql":`)},
		{testJSONArray, []byte(`[1,2,3]`)},
		{"JSON数字", []byte("42")},
		{"JSON布尔值", []byte("true")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := NewPacket(PacketQuery, tt.payload)
			resp, err := srv.handleQueryPacket(pkt)
			if err == nil {
				t.Error("期望返回 JSON 反序列化错误，得到 nil")
			}
			if resp != nil {
				t.Errorf("期望 resp 为 nil，得到 %v", resp)
			}
		})
	}
}

// TestHandleQueryPacket_QueryExecutionErrorV6 测试 handleQueryPacket 查询执行错误时的响应。
// handleQuery 将错误封装为 Response{Code:-1}，不会返回 Go error。
func TestHandleQueryPacket_QueryExecutionErrorV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name string
		sql  string
	}{
		{"SQL解析错误", testInvalidSQL},
		{"查询不存在的表", "SELECT * FROM nonexistent_v6_table"},
		{"空SQL", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := json.Marshal(QueryRequest{SQL: tt.sql})
			pkt := NewPacket(PacketQuery, payload)
			resp, err := srv.handleQueryPacket(pkt)
			if err != nil {
				t.Fatalf("handleQueryPacket 不应返回 Go 错误: %v", err)
			}
			var response Response
			if err := json.Unmarshal(resp.Payload, &response); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if response.Code != -1 {
				t.Errorf("响应 Code = %d，期望 -1", response.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleWritePacket: 有效写入、无效 JSON、写入执行错误路径（80.0% → >90%）
// ---------------------------------------------------------------------------

// TestHandleWritePacket_ValidWriteV6 测试 handleWritePacket 处理有效写入请求。
func TestHandleWritePacket_ValidWriteV6(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	writePayload, _ := json.Marshal(WriteRequest{
		Table: testTable,
		Rows: []map[string]interface{}{
			{"id": float64(1), testColName: testName},
			{"id": float64(2), testColName: testNameBob},
		},
	})
	pkt := NewPacket(PacketWrite, writePayload)
	resp, err := srv.handleWritePacket(pkt)
	if err != nil {
		t.Fatalf("handleWritePacket 有效写入失败: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应类型 = %d，期望 %d", resp.Type, PacketResponse)
	}

	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("解析写入响应失败: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("响应 Code = %d，期望 0，Message = %q", response.Code, response.Message)
	}
	if response.Rows != 2 {
		t.Errorf("写入行数 = %d，期望 2", response.Rows)
	}
}

// TestHandleWritePacket_InvalidJSONVariantsV6 测试 handleWritePacket 收到各种无效 JSON 时的错误返回。
func TestHandleWritePacket_InvalidJSONVariantsV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name    string
		payload []byte
	}{
		{testPlainText, []byte("hello world")},
		{"不完整JSON对象", []byte(`{"table":`)},
		{testJSONArray, []byte(`[1,2,3]`)},
		{"JSON数字", []byte("42")},
		{"JSON字符串", []byte(`"hello"`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := NewPacket(PacketWrite, tt.payload)
			resp, err := srv.handleWritePacket(pkt)
			if err == nil {
				t.Error("期望返回 JSON 反序列化错误，得到 nil")
			}
			if resp != nil {
				t.Errorf("期望 resp 为 nil，得到 %v", resp)
			}
		})
	}
}

// TestHandleWritePacket_WriteExecutionErrorV6 测试 handleWritePacket 写入执行错误时的响应。
func TestHandleWritePacket_WriteExecutionErrorV6(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name  string
		table string
		rows  []map[string]interface{}
	}{
		{"表不存在", "nonexistent_v6_table", []map[string]interface{}{{"id": float64(1)}}},
		{"缺失主键", testTable, []map[string]interface{}{{testColName: testName}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := json.Marshal(WriteRequest{Table: tt.table, Rows: tt.rows})
			pkt := NewPacket(PacketWrite, payload)
			resp, err := srv.handleWritePacket(pkt)
			if err != nil {
				t.Fatalf("handleWritePacket 不应返回 Go 错误: %v", err)
			}
			var response Response
			if err := json.Unmarshal(resp.Payload, &response); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if response.Code != -1 {
				t.Errorf("响应 Code = %d，期望 -1", response.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handlePing: 心跳响应验证（80.0% → >90%）
// ---------------------------------------------------------------------------

// TestHandlePing_ResponseFieldsV6 测试 handlePing 返回的响应字段完整性。
func TestHandlePing_ResponseFieldsV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	resp, err := srv.handlePing()
	if err != nil {
		t.Fatalf("handlePing 失败: %v", err)
	}

	// 验证协议头字段
	if resp.Magic != Magic {
		t.Errorf("Magic = 0x%08x，期望 0x%08x", resp.Magic, Magic)
	}
	if resp.Version != ProtocolVersion {
		t.Errorf("Version = %d，期望 %d", resp.Version, ProtocolVersion)
	}
	if resp.Type != PacketResponse {
		t.Errorf("Type = %d，期望 %d", resp.Type, PacketResponse)
	}

	// 验证 Payload 内容
	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("解析 ping 响应失败: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("Code = %d，期望 0", response.Code)
	}
	if response.Message != msgPong {
		t.Errorf("Message = %q，期望 %q", response.Message, msgPong)
	}
	if response.Data != nil {
		t.Errorf("Data = %v，期望 nil", response.Data)
	}
	if response.Rows != 0 {
		t.Errorf("Rows = %d，期望 0", response.Rows)
	}
}

// ---------------------------------------------------------------------------
// handlePacket: 未知包类型路由验证
// ---------------------------------------------------------------------------

// TestHandlePacket_UnknownTypeVariantsV6 测试 handlePacket 对多种未知包类型的错误处理。
func TestHandlePacket_UnknownTypeVariantsV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	tests := []struct {
		name    string
		pktType uint8
	}{
		{"类型0", 0},
		{"类型4", 4},
		{"类型100", 100},
		{"类型255", 255},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := NewPacket(tt.pktType, nil)
			resp, err := srv.handlePacket(pkt)
			if err == nil {
				t.Error("未知包类型应返回错误")
			}
			if resp != nil {
				t.Errorf("期望 resp 为 nil，得到 %v", resp)
			}
		})
	}
}

// TestHandlePacket_RouteQueryV6 测试 handlePacket 正确路由查询请求。
func TestHandlePacket_RouteQueryV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	payload, _ := json.Marshal(QueryRequest{SQL: testSelectNonexistent})
	pkt := NewPacket(PacketQuery, payload)
	resp, err := srv.handlePacket(pkt)
	if err != nil {
		t.Fatalf("handlePacket(PacketQuery) 不应返回 Go 错误: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应类型 = %d，期望 %d", resp.Type, PacketResponse)
	}
}

// TestHandlePacket_RouteWriteV6 测试 handlePacket 正确路由写入请求。
func TestHandlePacket_RouteWriteV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	payload, _ := json.Marshal(WriteRequest{
		Table: v14Nonexistent,
		Rows:  []map[string]interface{}{{"id": float64(1)}},
	})
	pkt := NewPacket(PacketWrite, payload)
	resp, err := srv.handlePacket(pkt)
	if err != nil {
		t.Fatalf("handlePacket(PacketWrite) 不应返回 Go 错误: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应类型 = %d，期望 %d", resp.Type, PacketResponse)
	}
}

// TestHandlePacket_RoutePingV6 测试 handlePacket 正确路由心跳请求。
func TestHandlePacket_RoutePingV6(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	pkt := NewPacket(PacketPing, nil)
	resp, err := srv.handlePacket(pkt)
	if err != nil {
		t.Fatalf("handlePacket(PacketPing) 失败: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应类型 = %d，期望 %d", resp.Type, PacketResponse)
	}
}
