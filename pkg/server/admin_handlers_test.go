package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newAdminTestServer 构造一个已注册 users 表（带 LSM 引擎）的 Server。
// 使用 handleQuery("create table...") 走完整路径，从而触发 storage.Engine
// 注册到 routingAdapter.lsmEngines，确保 /admin/* 端点能枚举到该表。
func newAdminTestServer(t *testing.T) *Server {
	t.Helper()
	srv := newTestServer(t)
	resp, err := srv.handleQuery(&QueryRequest{
		SQL: "create table users (id int64 not null, name string, primary key(id))",
	})
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("创建表返回错误: code=%d message=%q", resp.Code, resp.Message)
	}
	return srv
}

// adminRequest 触发一个 admin handler 并返回响应解码结果。
func adminRequest(t *testing.T, srv *Server, method, path string) (int, adminResponse) {
	t.Helper()
	mux := srv.registerHTTPHandlers()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	var body adminResponse
	if rec.Body.Len() > 0 {
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("解码响应失败: %v", err)
		}
	}
	return res.StatusCode, body
}

// TestAdminFlushEndpointSuccess 验证 POST /admin/flush 在 LSM 引擎上返回成功。
// 即使 MemTable 为空，调用也应被识别为「已处理」并返回 Affected>=1。
func TestAdminFlushEndpointSuccess(t *testing.T) {
	srv := newAdminTestServer(t)

	status, body := adminRequest(t, srv, http.MethodPost, "/admin/flush")
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200; body = %+v", status, body)
	}
	if body.Code != 0 {
		t.Errorf("响应 Code = %d, 期望 0; Message = %q", body.Code, body.Message)
	}
	if body.Message != adminMsgFlushOK {
		t.Errorf("响应 Message = %q, 期望 %q", body.Message, adminMsgFlushOK)
	}
	if body.Affected < 1 {
		t.Errorf("响应 Affected = %d, 期望 >= 1", body.Affected)
	}
}

// TestAdminFlushRejectsNonPOST 验证 /admin/flush 拒绝非 POST 方法。
func TestAdminFlushRejectsNonPOST(t *testing.T) {
	srv := newAdminTestServer(t)

	status, body := adminRequest(t, srv, http.MethodGet, "/admin/flush")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 405; body = %+v", status, body)
	}
	if body.Code != -1 {
		t.Errorf("响应 Code = %d, 期望 -1", body.Code)
	}
}

// TestAdminCompactEndpointSuccess 验证 POST /admin/compact 返回成功。
// 即使没有可压缩数据，也应返回 Affected>=1（已检查过引擎）。
func TestAdminCompactEndpointSuccess(t *testing.T) {
	srv := newAdminTestServer(t)

	status, body := adminRequest(t, srv, http.MethodPost, "/admin/compact")
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200; body = %+v", status, body)
	}
	if body.Code != 0 {
		t.Errorf("响应 Code = %d, 期望 0; Message = %q", body.Code, body.Message)
	}
	if body.Message != adminMsgCompactOK {
		t.Errorf("响应 Message = %q, 期望 %q", body.Message, adminMsgCompactOK)
	}
	if body.Affected < 1 {
		t.Errorf("响应 Affected = %d, 期望 >= 1", body.Affected)
	}
}

// TestAdminCompactRejectsNonPOST 验证 /admin/compact 拒绝非 POST 方法。
func TestAdminCompactRejectsNonPOST(t *testing.T) {
	srv := newAdminTestServer(t)

	status, body := adminRequest(t, srv, http.MethodPut, "/admin/compact")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 405; body = %+v", status, body)
	}
	if body.Code != -1 {
		t.Errorf("响应 Code = %d, 期望 -1", body.Code)
	}
}

// TestAdminFlushPersistsData 验证强制 flush 后数据仍可通过查询读到。
// 覆盖场景：admin flush 在 MemTable 非空时确实把数据持久化为 Segment。
func TestAdminFlushPersistsData(t *testing.T) {
	srv := newAdminTestServer(t)

	// 写一行数据
	if _, err := srv.handleWrite(&WriteRequest{
		Table: "users",
		Rows:  []map[string]any{{"id": int64(1), testColName: "alice"}},
	}); err != nil {
		t.Fatalf("handleWrite 失败: %v", err)
	}

	// 强制 flush
	if status, body := adminRequest(t, srv, http.MethodPost, "/admin/flush"); status != http.StatusOK {
		t.Fatalf("flush 失败: status=%d body=%+v", status, body)
	}

	// 通过查询读出来
	resp, err := srv.handleQuery(&QueryRequest{SQL: "select id, name from users where id = 1"})
	if err != nil {
		t.Fatalf("handleQuery 失败: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("查询 Code=%d Message=%q", resp.Code, resp.Message)
	}
	rows, ok := resp.Data.([]map[string]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("查询结果 rows=%v", resp.Data)
	}
	if rows[0][testColName] != "alice" {
		t.Errorf("flush 后数据丢失: name=%v", rows[0][testColName])
	}
}

// TestAdminEndpointsRegistered 验证 /admin/flush 与 /admin/compact
// 都已被 registerHTTPHandlers 注册（路由表漂移保护）。
func TestAdminEndpointsRegistered(t *testing.T) {
	srv := newAdminTestServer(t)
	mux := srv.registerHTTPHandlers()
	for _, path := range []string{"/admin/flush", "/admin/compact"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		_, pattern := mux.Handler(req)
		if pattern == "" {
			t.Errorf("路由 %s 未注册", path)
		}
	}
}
