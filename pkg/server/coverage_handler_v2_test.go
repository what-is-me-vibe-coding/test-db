package server

import (
	"encoding/json"
	"testing"
)

// --- handleQueryPacket: happy path with valid query request ---

func TestHandleQueryPacket_ValidQuery(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	payload, _ := json.Marshal(QueryRequest{SQL: testSelectAll})
	pkt := NewPacket(PacketQuery, payload)

	resp, err := srv.handleQueryPacket(pkt)
	if err != nil {
		t.Fatalf("handleQueryPacket 失败: %v", err)
	}
	if resp == nil {
		t.Fatal("handleQueryPacket 返回 nil 响应包")
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应包类型 = %d, 期望 %d", resp.Type, PacketResponse)
	}

	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("响应 Code = %d, 期望 0; Message = %q", response.Code, response.Message)
	}
}

// --- handleWritePacket: happy path with valid write request ---

func TestHandleWritePacket_ValidWrite(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	payload, _ := json.Marshal(WriteRequest{
		Table: testTable,
		Rows: []map[string]interface{}{
			{"id": float64(1), testColName: testName},
		},
	})
	pkt := NewPacket(PacketWrite, payload)

	resp, err := srv.handleWritePacket(pkt)
	if err != nil {
		t.Fatalf("handleWritePacket 失败: %v", err)
	}
	if resp == nil {
		t.Fatal("handleWritePacket 返回 nil 响应包")
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应包类型 = %d, 期望 %d", resp.Type, PacketResponse)
	}

	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("响应 Code = %d, 期望 0; Message = %q", response.Code, response.Message)
	}
	if response.Rows != 1 {
		t.Errorf("写入行数 = %d, 期望 1", response.Rows)
	}
}

// --- handlePing: returns correct response with "pong" message ---

func TestHandlePing_ReturnsPong(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	resp, err := srv.handlePing()
	if err != nil {
		t.Fatalf("handlePing 失败: %v", err)
	}
	if resp == nil {
		t.Fatal("handlePing 返回 nil 响应包")
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应包类型 = %d, 期望 %d", resp.Type, PacketResponse)
	}

	var response Response
	if err := json.Unmarshal(resp.Payload, &response); err != nil {
		t.Fatalf("解析心跳响应失败: %v", err)
	}
	if response.Code != 0 {
		t.Errorf("响应 Code = %d, 期望 0", response.Code)
	}
	if response.Message != msgPong {
		t.Errorf("响应 Message = %q, 期望 %q", response.Message, msgPong)
	}
}

// --- handlePacket: default case with unknown packet type ---

func TestHandlePacket_UnknownTypeDefault(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	// Use a packet type that doesn't match any known type
	pkt := NewPacket(99, nil)
	resp, err := srv.handlePacket(pkt)
	if err == nil {
		t.Error("handlePacket(未知类型) 期望返回错误, 得到 nil")
	}
	if resp != nil {
		t.Errorf("handlePacket(未知类型) 响应 = %v, 期望 nil", resp)
	}
}

// --- handlePacket: routes to handleQueryPacket ---

func TestHandlePacket_QueryRoute(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	payload, _ := json.Marshal(QueryRequest{SQL: testSelectAll})
	pkt := NewPacket(PacketQuery, payload)

	resp, err := srv.handlePacket(pkt)
	if err != nil {
		t.Fatalf("handlePacket(Query) 失败: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应包类型 = %d, 期望 %d", resp.Type, PacketResponse)
	}
}

// --- handlePacket: routes to handleWritePacket ---

func TestHandlePacket_WriteRoute(t *testing.T) {
	srv := newTestServerWithTable(t)
	defer func() { _ = srv.Stop() }()

	payload, _ := json.Marshal(WriteRequest{
		Table: testTable,
		Rows:  []map[string]interface{}{{"id": float64(1), testColName: testName}},
	})
	pkt := NewPacket(PacketWrite, payload)

	resp, err := srv.handlePacket(pkt)
	if err != nil {
		t.Fatalf("handlePacket(Write) 失败: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应包类型 = %d, 期望 %d", resp.Type, PacketResponse)
	}
}

// --- handlePacket: routes to handlePing ---

func TestHandlePacket_PingRoute(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	pkt := NewPacket(PacketPing, nil)
	resp, err := srv.handlePacket(pkt)
	if err != nil {
		t.Fatalf("handlePacket(Ping) 失败: %v", err)
	}
	if resp.Type != PacketResponse {
		t.Errorf("响应包类型 = %d, 期望 %d", resp.Type, PacketResponse)
	}
}
