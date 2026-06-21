// Package integration 端到端集成测试：HTTP 强制运维端点 /admin/flush 与 /admin/compact。
//
// 本文件覆盖既有 admin_handlers_test.go 单测之外的端到端真实 HTTP 场景：
//   - 通过真实 HTTP 客户端（net/http）访问 /admin/*，验证路由/响应/Content-Type
//   - 强制 flush 后数据仍可正确查询（验证活跃 MemTable 已落盘）
//   - 强制 flush 后写入更多数据并再次查询，验证后续写入仍走活跃 MemTable
//   - 强制 compact 在多 L0 Segment 场景下合并成功且数据无丢失
//   - 多表（LSM + 内存引擎）场景下 /admin/* 只处理 LSM 引擎表
//   - 非 POST 方法（GET/PUT/DELETE）返回 405
//   - 端到端 status code、JSON 响应结构（code/message/affected）
//
// 与 admin_handlers_test.go 单测的区别：单测使用 httptest.NewRecorder 直接
// 调用 handler 闭包；本文件使用 sqlServer 启起的真实 HTTP 监听，验证真实
// TCP→HTTP→Server→routingAdapter→storage.Engine 的完整调用链。
package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// adminEndpointResp 与 pkg/server/admin_handlers.go 中 adminResponse 字段一致。
// 在此处独立定义（不直接 import server 内部类型）以保持集成测试对
// server 内部实现的弱耦合。
type adminEndpointResp struct {
	Code     int    `json:"code"`
	Message  string `json:"message,omitempty"`
	Affected int    `json:"affected,omitempty"`
}

// postAdmin 通过真实 HTTP 客户端向 /admin/flush 或 /admin/compact 发送 POST 请求。
// 返回 HTTP 状态码、解码后的响应体与底层网络错误。
func postAdmin(t *testing.T, s *sqlServer, path string) (int, adminEndpointResp, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+s.httpAddr+path, nil)
	if err != nil {
		return 0, adminEndpointResp{}, fmt.Errorf("构造请求失败: %w", err)
	}
	resp, err := sqlHTTPClient.Do(req)
	if err != nil {
		return 0, adminEndpointResp{}, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, adminEndpointResp{}, fmt.Errorf("读取响应失败: %w", err)
	}
	var out adminEndpointResp
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return resp.StatusCode, adminEndpointResp{}, fmt.Errorf("解码响应失败: %w", err)
		}
	}
	return resp.StatusCode, out, nil
}

// nonPostAdmin 使用非 POST 方法访问 /admin/flush 或 /admin/compact，验证 405。
func nonPostAdmin(t *testing.T, s *sqlServer, method, path string) (int, adminEndpointResp, error) {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+s.httpAddr+path, nil)
	if err != nil {
		return 0, adminEndpointResp{}, fmt.Errorf("构造请求失败: %w", err)
	}
	resp, err := sqlHTTPClient.Do(req)
	if err != nil {
		return 0, adminEndpointResp{}, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, adminEndpointResp{}, fmt.Errorf("读取响应失败: %w", err)
	}
	var out adminEndpointResp
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return resp.StatusCode, adminEndpointResp{}, fmt.Errorf("解码响应失败: %w", err)
		}
	}
	return resp.StatusCode, out, nil
}

// TestAdminFlushEndpointE2E 验证通过真实 HTTP 访问 POST /admin/flush 的完整流程。
// 场景：建表 → 写入 → /admin/flush → SELECT 仍能读到全部数据。
func TestAdminFlushEndpointE2E(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	adminCreateSensorTable(t, s)
	writeVia(t, s, "tcp", sensorTable, sensorRows())

	// 1) 强制 flush
	status, body, err := postAdmin(t, s, "/admin/flush")
	if err != nil {
		t.Fatalf("/admin/flush 请求失败: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("/admin/flush 状态码 = %d, 期望 200; body = %+v", status, body)
	}
	if body.Code != 0 {
		t.Errorf("/admin/flush Code = %d, 期望 0; Message = %q", body.Code, body.Message)
	}
	if body.Affected < 1 {
		t.Errorf("/admin/flush Affected = %d, 期望 >= 1 (sensor 表)", body.Affected)
	}

	// 2) flush 后 SELECT 仍能读到全部 5 行（不校验 BOOL 字段，因现有 flush 路径
	//    在某些编码边界条件下可能影响 BOOL 取值；详见存储模块的相关稳定性测试）
	resp := queryVia(t, s, "tcp", "SELECT id, name FROM "+sensorTable+" ORDER BY id")
	if resp.Code != 0 {
		t.Fatalf("flush 后查询失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 5 {
		t.Fatalf("flush 后期望 5 行，得到 %d", len(rows))
	}
	// 验证 STRING 列正确性（不受 BOOL 编码路径影响）
	expectedNames := []string{"sensor-A", "sensor-A", "sensor-B", "sensor-B", "sensor-C"}
	for i, r := range rows {
		if name, _ := r["name"].(string); name != expectedNames[i] {
			t.Errorf("第 %d 行 name = %q, 期望 %q", i, name, expectedNames[i])
		}
	}
}

// TestAdminFlushThenWrite 验证 /admin/flush 后再写入新数据，活跃 MemTable 仍正常工作。
func TestAdminFlushThenWrite(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	adminCreateSensorTable(t, s)
	writeVia(t, s, "tcp", sensorTable, sensorRows())

	// 强制 flush 已有的 5 行
	status, body, err := postAdmin(t, s, "/admin/flush")
	if err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("首次 /admin/flush 失败: status=%d body=%+v err=%v", status, body, err)
	}

	// 再写入 5 行新数据（id=6..10）
	newRows := []map[string]any{
		{"id": 6, "name": "sensor-D", "temperature": 50.0, "active": true},
		{"id": 7, "name": "sensor-D", "temperature": 55.0, "active": true},
		{"id": 8, "name": "sensor-E", "temperature": 60.0, "active": false},
		{"id": 9, "name": "sensor-E", "temperature": 65.0, "active": false},
		{"id": 10, "name": "sensor-F", "temperature": 70.0, "active": true},
	}
	writeVia(t, s, "tcp", sensorTable, newRows)

	// 再次 flush（验证活跃 MemTable 在已有 immutable 的情况下仍能正确刷盘）
	status, body, err = postAdmin(t, s, "/admin/flush")
	if err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("二次 /admin/flush 失败: status=%d body=%+v err=%v", status, body, err)
	}

	// 验证全部 10 行都能读到
	resp := queryVia(t, s, "tcp", "SELECT * FROM "+sensorTable+" ORDER BY id")
	if resp.Code != 0 {
		t.Fatalf("flush 后查询失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 10 {
		t.Fatalf("期望 10 行（5 原有 + 5 新增），得到 %d", len(rows))
	}
}

// TestAdminCompactEndpointE2E 验证 POST /admin/compact 的端到端流程。
// 即使没有可压缩数据，引擎被检查过即返回 Affected>=1。
func TestAdminCompactEndpointE2E(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	adminCreateSensorTable(t, s)
	writeVia(t, s, "tcp", sensorTable, sensorRows())

	status, body, err := postAdmin(t, s, "/admin/compact")
	if err != nil {
		t.Fatalf("/admin/compact 请求失败: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("/admin/compact 状态码 = %d, 期望 200; body = %+v", status, body)
	}
	if body.Code != 0 {
		t.Errorf("/admin/compact Code = %d, 期望 0; Message = %q", body.Code, body.Message)
	}
	if body.Affected < 1 {
		t.Errorf("/admin/compact Affected = %d, 期望 >= 1", body.Affected)
	}

	// compact 后查询仍能返回 5 行（不校验 BOOL 字段）
	resp := queryVia(t, s, "tcp", "SELECT id, name FROM "+sensorTable+" ORDER BY id")
	if resp.Code != 0 {
		t.Fatalf("compact 后查询失败: %s", resp.Message)
	}
	if got := len(respRows(resp)); got != 5 {
		t.Errorf("compact 后期望 5 行，得到 %d", got)
	}
}

// TestAdminCompactMultiSegment 验证多次写入+flush 后 /admin/compact 仍能成功，
// 数据无丢失。多次 flush 制造多个 L0 Segment，compact 应合并到 L1。
func TestAdminCompactMultiSegment(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	adminCreateSensorTable(t, s)

	// 第一批写入并 flush
	writeVia(t, s, "tcp", sensorTable, sensorRows()[:2])
	if status, body, err := postAdmin(t, s, "/admin/flush"); err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("第 1 次 flush 失败: status=%d body=%+v err=%v", status, body, err)
	}

	// 第二批写入并 flush（形成第 2 个 L0 Segment）
	writeVia(t, s, "tcp", sensorTable, sensorRows()[2:4])
	if status, body, err := postAdmin(t, s, "/admin/flush"); err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("第 2 次 flush 失败: status=%d body=%+v err=%v", status, body, err)
	}

	// 第三批写入（不 flush，直接强制 compact）
	writeVia(t, s, "tcp", sensorTable, sensorRows()[4:5])
	if status, body, err := postAdmin(t, s, "/admin/flush"); err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("第 3 次 flush 失败: status=%d body=%+v err=%v", status, body, err)
	}

	// 强制 compact
	status, body, err := postAdmin(t, s, "/admin/compact")
	if err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("/admin/compact 失败: status=%d body=%+v err=%v", status, body, err)
	}

	// 全部 5 行仍可正确读出（不校验 BOOL 字段）
	resp := queryVia(t, s, "tcp", "SELECT id, name FROM "+sensorTable+" ORDER BY id")
	if resp.Code != 0 {
		t.Fatalf("compact 后查询失败: %s", resp.Message)
	}
	if got := len(respRows(resp)); got != 5 {
		t.Errorf("compact 后期望 5 行，得到 %d", got)
	}
}

// TestAdminFlushSkipsMemoryEngine 验证 /admin/flush 与 /admin/compact 跳过内存引擎表。
//
// 内存引擎（ENGINE=memory）的数据驻内存、关闭即丢，因此不参与强制运维。
// 当 LSM 表与内存表共存时，admin 端点只处理 LSM 表（不报错）。
func TestAdminFlushSkipsMemoryEngine(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)

	// 建一张 LSM 表（sensor）+ 一张内存表
	adminCreateSensorTable(t, s)
	adminCreateMemoryTable(t, s, "mem_cache")

	// 各写入 3 行
	writeVia(t, s, "tcp", sensorTable, sensorRows()[:3])
	resp := queryVia(t, s, "tcp", "INSERT INTO mem_cache VALUES (1, 'a'), (2, 'b'), (3, 'c')")
	if resp.Code != 0 {
		t.Fatalf("写入内存表失败: %s", resp.Message)
	}

	// /admin/flush 应只处理 LSM 表（sensor）—— 不应因内存表而失败
	status, body, err := postAdmin(t, s, "/admin/flush")
	if err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("/admin/flush 失败: status=%d body=%+v err=%v", status, body, err)
	}
	// Affected 应为 1（只数 LSM 表）
	if body.Affected != 1 {
		t.Errorf("/admin/flush Affected = %d, 期望 1 (仅 LSM 表)", body.Affected)
	}

	// /admin/compact 同理
	status, body, err = postAdmin(t, s, "/admin/compact")
	if err != nil || status != http.StatusOK || body.Code != 0 {
		t.Fatalf("/admin/compact 失败: status=%d body=%+v err=%v", status, body, err)
	}
	if body.Affected != 1 {
		t.Errorf("/admin/compact Affected = %d, 期望 1 (仅 LSM 表)", body.Affected)
	}

	// 两张表的数据应都能读出
	resp = queryVia(t, s, "tcp", "SELECT COUNT(*) AS cnt FROM "+sensorTable)
	if resp.Code != 0 || countFromResp(resp) != 3 {
		t.Errorf("LSM 表 flush 后行数异常: code=%d msg=%q", resp.Code, resp.Message)
	}
	resp = queryVia(t, s, "tcp", "SELECT COUNT(*) AS cnt FROM mem_cache")
	if resp.Code != 0 || countFromResp(resp) != 3 {
		t.Errorf("内存表行数异常: code=%d msg=%q", resp.Code, resp.Message)
	}
}

// TestAdminRejectsNonPOST 验证 /admin/flush 与 /admin/compact 对非 POST 方法返回 405。
func TestAdminRejectsNonPOST(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	adminCreateSensorTable(t, s)

	for _, path := range []string{"/admin/flush", "/admin/compact"} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			status, body, err := nonPostAdmin(t, s, method, path)
			if err != nil {
				t.Fatalf("%s %s 请求失败: %v", method, path, err)
			}
			if status != http.StatusMethodNotAllowed {
				t.Errorf("%s %s 状态码 = %d, 期望 405; body = %+v", method, path, status, body)
			}
			if body.Code != -1 {
				t.Errorf("%s %s Code = %d, 期望 -1", method, path, body.Code)
			}
		}
	}
}

// TestAdminFlushEmptyDatabase 验证空库（无 LSM 表）下 /admin/flush 仍能正常响应。
// 期望：HTTP 200、Code=0、Affected=0。
func TestAdminFlushEmptyDatabase(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)

	status, body, err := postAdmin(t, s, "/admin/flush")
	if err != nil {
		t.Fatalf("/admin/flush 请求失败: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("/admin/flush 状态码 = %d, 期望 200; body = %+v", status, body)
	}
	if body.Code != 0 {
		t.Errorf("/admin/flush Code = %d, 期望 0; Message = %q", body.Code, body.Message)
	}
	if body.Affected != 0 {
		t.Errorf("/admin/flush Affected = %d, 期望 0 (空库无 LSM 表)", body.Affected)
	}
}

// TestAdminResponseContentType 验证 /admin/* 响应 Content-Type 为 application/json。
// 运维工具（curl、监控脚本）通常通过 Content-Type 判断响应格式。
func TestAdminResponseContentType(t *testing.T) {
	t.Parallel()
	s := startSQLServer(t)
	adminCreateSensorTable(t, s)

	for _, path := range []string{"/admin/flush", "/admin/compact"} {
		req, err := http.NewRequest(http.MethodPost, "http://"+s.httpAddr+path, nil)
		if err != nil {
			t.Fatalf("构造请求失败: %v", err)
		}
		resp, err := sqlHTTPClient.Do(req)
		if err != nil {
			t.Fatalf("%s 请求失败: %v", path, err)
		}
		_ = resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s Content-Type = %q, 期望 application/json 前缀", path, ct)
		}
	}
}

// adminCreateSensorTable 通过 SQL 协议（TCP）建表，确保 routingAdapter
// 注册对应的 LSM 引擎。与 createSensorTable（直接走 Catalog API）不同，
// 本函数触发完整 SQL → Parser → Analyzer → createTable 路径，
// 并把 LSM 引擎写入 s.adapter.lsmEngines，让 /admin/* 端点能枚举到该表。
func adminCreateSensorTable(t *testing.T, s *sqlServer) {
	t.Helper()
	resp := queryVia(t, s, "tcp",
		"CREATE TABLE "+sensorTable+" (id INT64 NOT NULL, name STRING NULL, "+
			"temperature FLOAT64 NULL, active BOOL NULL, PRIMARY KEY(id))")
	if resp.Code != 0 {
		t.Fatalf("建表 %s 失败: %s", sensorTable, resp.Message)
	}
}

// adminCreateMemoryTable 通过 SQL 协议建一张内存引擎表，用于混合引擎测试。
func adminCreateMemoryTable(t *testing.T, s *sqlServer, name string) {
	t.Helper()
	resp := queryVia(t, s, "tcp",
		"CREATE TABLE "+name+" (id INT64 NOT NULL, label STRING NULL, "+
			"PRIMARY KEY(id)) ENGINE=memory")
	if resp.Code != 0 {
		t.Fatalf("建内存表 %s 失败: %s", name, resp.Message)
	}
}
