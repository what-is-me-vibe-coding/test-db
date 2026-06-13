package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// httpQuery: 错误 HTTP 方法、JSON 解码错误、handleQuery 错误、非零响应码
// ---------------------------------------------------------------------------

// TestCoverageLowHandlerV7_HttpQuery_ErrorPaths 测试 httpQuery 的各种错误路径。
// 使用表驱动测试覆盖：错误 HTTP 方法、JSON 解码错误、非零响应码。
func TestCoverageLowHandlerV7_HttpQuery_ErrorPaths(t *testing.T) {
	srv := newTestServerV7WithTable(t)

	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		wantCode   int
	}{
		{
			name:       "错误HTTP方法_GET",
			method:     http.MethodGet,
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   -1,
		},
		{
			name:       "错误HTTP方法_PUT",
			method:     http.MethodPut,
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   -1,
		},
		{
			name:       "错误HTTP方法_DELETE",
			method:     http.MethodDelete,
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   -1,
		},
		{
			name:       "JSON解码错误_无效JSON",
			method:     http.MethodPost,
			body:       "<<<不是json>>>",
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "JSON解码错误_空请求体",
			method:     http.MethodPost,
			body:       "",
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "JSON解码错误_不完整JSON",
			method:     http.MethodPost,
			body:       "{",
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "非零响应码_无效SQL",
			method:     http.MethodPost,
			body:       testInvalidSQLBody,
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "非零响应码_查询不存在的表",
			method:     http.MethodPost,
			body:       `{"sql":"SELECT * FROM nonexistent_v7"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "正常查询_零响应码",
			method:     http.MethodPost,
			body:       benchSelectAllSQL,
			wantStatus: http.StatusOK,
			wantCode:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body string
			if tt.method == http.MethodPost {
				body = tt.body
			}
			req := httptest.NewRequest(tt.method, "/query", strings.NewReader(body))
			w := httptest.NewRecorder()
			srv.httpQuery(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("状态码 = %d，期望 %d", w.Code, tt.wantStatus)
			}

			var resp Response
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if resp.Code != tt.wantCode {
				t.Errorf("响应 Code = %d，期望 %d，Message = %q", resp.Code, tt.wantCode, resp.Message)
			}
		})
	}
}

// TestCoverageLowHandlerV7_HttpQuery_HandleQueryError 测试 httpQuery 中 handleQuery 返回错误时的行为。
// 注意：当前 handleQuery 实现始终返回 nil error（错误通过 Response.Code 传递），
// 因此 httpQuery 中的 `if err != nil` 分支（返回 HTTP 500）在当前实现中不可达。
// 此测试验证：handleQuery 返回非零 Code 的 Response 时，httpQuery 正确返回 HTTP 400。
func TestCoverageLowHandlerV7_HttpQuery_HandleQueryError(t *testing.T) {
	srv := newTestServerV7(t)

	// 发送无效 SQL，handleQuery 返回 Response{Code: -1}, nil（而非 Go error）
	// httpQuery 应将非零 Code 的 Response 映射为 HTTP 400
	body := testInvalidSQLBody
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.httpQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 %d", w.Code, http.StatusBadRequest)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != -1 {
		t.Errorf("响应 Code = %d，期望 -1", resp.Code)
	}
}

// ---------------------------------------------------------------------------
// httpWrite: 错误 HTTP 方法、JSON 解码错误、handleWrite 错误、非零响应码
// ---------------------------------------------------------------------------

// TestCoverageLowHandlerV7_HttpWrite_ErrorPaths 测试 httpWrite 的各种错误路径。
// 使用表驱动测试覆盖：错误 HTTP 方法、JSON 解码错误、非零响应码。
func TestCoverageLowHandlerV7_HttpWrite_ErrorPaths(t *testing.T) {
	srv := newTestServerV7WithTable(t)

	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		wantCode   int
	}{
		{
			name:       "错误HTTP方法_GET",
			method:     http.MethodGet,
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   -1,
		},
		{
			name:       "错误HTTP方法_PUT",
			method:     http.MethodPut,
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   -1,
		},
		{
			name:       "错误HTTP方法_PATCH",
			method:     http.MethodPatch,
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   -1,
		},
		{
			name:       "JSON解码错误_无效JSON",
			method:     http.MethodPost,
			body:       "<<<不是json>>>",
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "JSON解码错误_空请求体",
			method:     http.MethodPost,
			body:       "",
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "JSON解码错误_不完整JSON",
			method:     http.MethodPost,
			body:       "{",
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "非零响应码_表不存在",
			method:     http.MethodPost,
			body:       `{"table":"nonexistent_v7","rows":[{"id":1}]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "非零响应码_缺少主键",
			method:     http.MethodPost,
			body:       `{"table":"users","rows":[{"name":"alice"}]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "非零响应码_类型不匹配",
			method:     http.MethodPost,
			body:       `{"table":"users","rows":[{"id":1,"name":true}]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   -1,
		},
		{
			name:       "正常写入_零响应码",
			method:     http.MethodPost,
			body:       testWriteAliceBody,
			wantStatus: http.StatusOK,
			wantCode:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body string
			if tt.method == http.MethodPost {
				body = tt.body
			}
			req := httptest.NewRequest(tt.method, "/write", strings.NewReader(body))
			w := httptest.NewRecorder()
			srv.httpWrite(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("状态码 = %d，期望 %d，Body = %s", w.Code, tt.wantStatus, w.Body.String())
			}

			var resp Response
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if resp.Code != tt.wantCode {
				t.Errorf("响应 Code = %d，期望 %d，Message = %q", resp.Code, tt.wantCode, resp.Message)
			}
		})
	}
}

// TestCoverageLowHandlerV7_HttpWrite_HandleWriteError 测试 httpWrite 中 handleWrite 返回错误时的行为。
// 注意：当前 handleWrite 实现始终返回 nil error（错误通过 Response.Code 传递），
// 因此 httpWrite 中的 `if err != nil` 分支（返回 HTTP 500）在当前实现中不可达。
// 此测试验证：handleWrite 返回非零 Code 的 Response 时，httpWrite 正确返回 HTTP 400。
func TestCoverageLowHandlerV7_HttpWrite_HandleWriteError(t *testing.T) {
	srv := newTestServerV7(t)

	// 写入不存在的表，handleWrite 返回 Response{Code: -1}, nil（而非 Go error）
	// httpWrite 应将非零 Code 的 Response 映射为 HTTP 400
	body := `{"table":"nonexistent_v7","rows":[{"id":1}]}`
	req := httptest.NewRequest(http.MethodPost, "/write", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.httpWrite(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 %d", w.Code, http.StatusBadRequest)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != -1 {
		t.Errorf("响应 Code = %d，期望 -1", resp.Code)
	}
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

// newTestServerV7WithTable 创建用于 V7 覆盖率测试的服务器，并注册 users 表。
func newTestServerV7WithTable(t *testing.T) *Server {
	t.Helper()

	srv := newTestServerV7(t)

	err := srv.catalog.CreateTable(testTable, []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColScore, Type: common.TypeFloat64, Nullable: true},
	}, []string{"id"}, catalog.TableOptions{})
	if err != nil {
		t.Fatalf("CreateTable 失败: %v", err)
	}

	return srv
}
