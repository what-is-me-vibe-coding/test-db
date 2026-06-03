package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/query"
)

// --- 指标注册测试 ---

func TestNewMetrics(t *testing.T) {
	m := newMetrics()
	if m == nil {
		t.Fatal("newMetrics 返回 nil")
	}
	if m.registry == nil {
		t.Error("registry 不应为 nil")
	}
	if m.queriesTotal == nil {
		t.Error("queriesTotal 不应为 nil")
	}
	if m.queryDuration == nil {
		t.Error("queryDuration 不应为 nil")
	}
	if m.writesTotal == nil {
		t.Error("writesTotal 不应为 nil")
	}
	if m.memtableSizeBytes == nil {
		t.Error("memtableSizeBytes 不应为 nil")
	}
	if m.segmentCount == nil {
		t.Error("segmentCount 不应为 nil")
	}
}

func TestMetricsRegistryIndependence(t *testing.T) {
	// 验证每个 Server 实例拥有独立的 Registry，不会冲突。
	m1 := newMetrics()
	m2 := newMetrics()

	// 两个 registry 应该是不同的实例。
	if m1.registry == m2.registry {
		t.Error("不同 Server 的 registry 不应共享")
	}
}

// --- recordQuery / recordWrite 测试 ---

func TestRecordQuery(t *testing.T) {
	m := newMetrics()

	m.recordQuery(queryTypeSelect, 100*time.Millisecond)
	m.recordQuery(queryTypeSelect, 50*time.Millisecond)
	m.recordQuery(queryTypeInsert, 10*time.Millisecond)

	// 通过 HTTP 端点验证指标。
	handler := promhttpHandlerFor(m.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `widb_queries_total{type="select"}`) {
		t.Error("metrics 应包含 widb_queries_total{type=\"select\"}")
	}
	if !strings.Contains(body, `widb_queries_total{type="insert"}`) {
		t.Error("metrics 应包含 widb_queries_total{type=\"insert\"}")
	}
}

func TestRecordWrite(t *testing.T) {
	m := newMetrics()

	m.recordWrite()
	m.recordWrite()
	m.recordWrite()

	handler := promhttpHandlerFor(m.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "widb_writes_total 3") {
		t.Errorf("metrics 应包含 widb_writes_total 3, 实际:\n%s", body)
	}
}

// --- updateStorageMetrics 测试 ---

func TestUpdateStorageMetrics(t *testing.T) {
	srv := newTestServer(t)

	srv.updateStorageMetrics()

	handler := promhttpHandlerFor(srv.metrics.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, metricMemtableSize) {
		t.Errorf("metrics 应包含 %s", metricMemtableSize)
	}
	if !strings.Contains(body, `widb_segment_count{level="l0"}`) {
		t.Error("metrics 应包含 widb_segment_count{level=\"l0\"}")
	}
	if !strings.Contains(body, `widb_segment_count{level="total"}`) {
		t.Error("metrics 应包含 widb_segment_count{level=\"total\"}")
	}
}

func TestUpdateStorageMetricsNilStorage(_ *testing.T) {
	// 验证 storage 为 nil 时不 panic。
	srv := &Server{metrics: newMetrics()}
	srv.updateStorageMetrics() // 不应 panic
}

// --- metricsHandler 测试 ---

func TestMetricsHandler(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	handler := srv.metricsHandler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("状态码 = %d, 期望 %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	// Gauge 和 Counter 即使未使用也会输出。
	if !strings.Contains(body, metricWritesTotal) {
		t.Errorf("metrics 应包含 %s", metricWritesTotal)
	}
	if !strings.Contains(body, metricMemtableSize) {
		t.Errorf("metrics 应包含 %s", metricMemtableSize)
	}
	if !strings.Contains(body, metricSegmentCount) {
		t.Errorf("metrics 应包含 %s", metricSegmentCount)
	}
}

func TestMetricsHandlerWrongMethod(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()

	handler := srv.metricsHandler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("状态码 = %d, 期望 %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// --- statementType 测试 ---

func TestStatementType(t *testing.T) {
	tests := []struct {
		name string
		stmt query.Statement
		want string
	}{
		{queryTypeSelect, &query.SelectStatement{}, queryTypeSelect},
		{queryTypeInsert, &query.InsertStatement{}, queryTypeInsert},
		{queryTypeCreateTable, &query.CreateTableStatement{}, queryTypeCreateTable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statementType(tt.stmt)
			if got != tt.want {
				t.Errorf("statementType = %q, 期望 %q", got, tt.want)
			}
		})
	}
}

// --- 查询插桩集成测试 ---

func TestQueryMetricsInstrumentation(t *testing.T) {
	srv := newTestServerWithTable(t)

	// 执行一次查询。
	_, _ = srv.handleQuery(&QueryRequest{SQL: testSelectAll})

	handler := promhttpHandlerFor(srv.metrics.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `widb_queries_total{type="select"}`) {
		t.Error("执行查询后 metrics 应包含 widb_queries_total{type=\"select\"}")
	}
}

func TestWriteMetricsInstrumentation(t *testing.T) {
	srv := newTestServerWithTable(t)

	// 执行一次写入。
	_, _ = srv.handleWrite(&WriteRequest{
		Table: testTable,
		Rows:  []map[string]interface{}{{"id": float64(1), testColName: testName}},
	})

	handler := promhttpHandlerFor(srv.metrics.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, metricWritesTotal) {
		t.Errorf("执行写入后 metrics 应包含 %s", metricWritesTotal)
	}
}

func TestQueryDurationRecorded(t *testing.T) {
	srv := newTestServerWithTable(t)

	// 执行一次查询。
	_, _ = srv.handleQuery(&QueryRequest{SQL: testSelectAll})

	handler := promhttpHandlerFor(srv.metrics.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	// Histogram 输出格式为 widb_query_duration_seconds_count, _sum, _bucket。
	if !strings.Contains(body, `widb_query_duration_seconds_count{type="select"}`) {
		t.Error("执行查询后 metrics 应包含 widb_query_duration_seconds_count{type=\"select\"}")
	}
}

// --- 指标名称验证 ---

func TestMetricNames(t *testing.T) {
	m := newMetrics()

	// 初始化一些指标数据以确保 CounterVec/HistogramVec 输出。
	m.recordQuery(queryTypeSelect, 1*time.Millisecond)
	m.memtableSizeBytes.Set(0)
	m.segmentCount.WithLabelValues(levelL0).Set(0)
	m.segmentCount.WithLabelValues(levelTotal).Set(0)

	handler := promhttpHandlerFor(m.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	expectedNames := []string{
		metricQueriesTotal,
		metricQueryDuration,
		metricWritesTotal,
		metricMemtableSize,
		metricSegmentCount,
	}
	for _, name := range expectedNames {
		if !strings.Contains(body, name) {
			t.Errorf("缺少指标 %q", name)
		}
	}
}

// --- 指标标签验证 ---

func TestMetricLabelValues(t *testing.T) {
	m := newMetrics()

	// 写入不同类型的查询指标。
	m.recordQuery(queryTypeSelect, 10*time.Millisecond)
	m.recordQuery(queryTypeInsert, 5*time.Millisecond)

	// 更新存储指标。
	m.memtableSizeBytes.Set(1024)
	m.segmentCount.WithLabelValues(levelL0).Set(5)
	m.segmentCount.WithLabelValues(levelTotal).Set(10)

	handler := promhttpHandlerFor(m.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `widb_queries_total{type="select"}`) {
		t.Error("widb_queries_total 缺少 type=select 标签")
	}
	if !strings.Contains(body, `widb_queries_total{type="insert"}`) {
		t.Error("widb_queries_total 缺少 type=insert 标签")
	}
	if !strings.Contains(body, `widb_segment_count{level="l0"}`) {
		t.Error("widb_segment_count 缺少 level=l0 标签")
	}
	if !strings.Contains(body, `widb_segment_count{level="total"}`) {
		t.Error("widb_segment_count 缺少 level=total 标签")
	}
}

// --- HTTP /metrics 集成测试 ---

func TestHTTPMetricsEndpointWithPrometheus(t *testing.T) {
	srv := newTestServerWithTable(t)

	// 先执行一些操作以产生指标。
	_, _ = srv.handleQuery(&QueryRequest{SQL: testSelectAll})
	_, _ = srv.handleWrite(&WriteRequest{
		Table: testTable,
		Rows:  []map[string]interface{}{{"id": float64(1), testColName: testName}},
	})

	mux := srv.registerHTTPHandlers()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("状态码 = %d, 期望 %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	expectedMetrics := []string{
		metricQueriesTotal,
		metricQueryDuration,
		metricWritesTotal,
		metricMemtableSize,
		metricSegmentCount,
	}
	for _, name := range expectedMetrics {
		if !strings.Contains(body, name) {
			t.Errorf("/metrics 响应缺少指标 %q", name)
		}
	}
}

// --- 验证 prometheus 默认指标不出现 ---

func TestMetricsEndpointNoDefaultGoMetrics(t *testing.T) {
	srv := newTestServer(t)

	mux := srv.registerHTTPHandlers()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	// 使用自定义 Registry，不应包含 Go 默认指标。
	if strings.Contains(body, "go_goroutines") {
		t.Error("自定义 Registry 不应包含 go_goroutines 默认指标")
	}
}

// --- 验证 segment_count 的 level 标签在 HTTP 响应中 ---

func TestHTTPMetricsSegmentLevelLabels(t *testing.T) {
	srv := newTestServer(t)

	mux := srv.registerHTTPHandlers()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `widb_segment_count{level="l0"}`) {
		t.Error("metrics 应包含 widb_segment_count{level=\"l0\"}")
	}
	if !strings.Contains(body, `widb_segment_count{level="total"}`) {
		t.Error("metrics 应包含 widb_segment_count{level=\"total\"}")
	}
}

// --- 验证 query_duration 的 type 标签在 HTTP 响应中 ---

func TestHTTPMetricsQueryTypeLabels(t *testing.T) {
	srv := newTestServerWithTable(t)

	// 执行一次查询以产生带标签的指标。
	_, _ = srv.handleQuery(&QueryRequest{SQL: testSelectAll})

	mux := srv.registerHTTPHandlers()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `widb_queries_total{type="select"}`) {
		t.Error("metrics 应包含 widb_queries_total{type=\"select\"}")
	}
	// Histogram 输出格式为 _count, _sum, _bucket。
	if !strings.Contains(body, `widb_query_duration_seconds_count{type="select"}`) {
		t.Error("metrics 应包含 widb_query_duration_seconds_count{type=\"select\"}")
	}
}

// --- 验证查询失败不记录查询指标 ---

func TestQueryMetricsOnParseError(t *testing.T) {
	srv := newTestServer(t)

	// 执行一次解析失败的查询。
	resp, err := srv.handleQuery(&QueryRequest{SQL: "INVALID SQL !!!"})
	if err != nil {
		t.Fatalf("handleQuery 失败: %v", err)
	}
	if resp.Code != -1 {
		t.Errorf("响应 Code = %d, 期望 -1", resp.Code)
	}

	// 解析失败时不记录查询指标（因为无法确定查询类型）。
	// 这是预期行为，验证不会 panic 即可。
}

// --- 验证写入失败不记录指标 ---

func TestWriteMetricsOnFailure(t *testing.T) {
	srv := newTestServer(t)

	// 执行一次写入失败的请求（表不存在）。
	resp, err := srv.handleWrite(&WriteRequest{
		Table: "nonexistent",
		Rows:  []map[string]interface{}{{"id": 1}},
	})
	if err != nil {
		t.Fatalf("handleWrite 失败: %v", err)
	}
	if resp.Code != -1 {
		t.Errorf("响应 Code = %d, 期望 -1", resp.Code)
	}

	// 写入失败时不应记录写入指标。
	handler := promhttpHandlerFor(srv.metrics.registry)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "widb_writes_total 1") {
		t.Error("写入失败时不应记录 widb_writes_total")
	}
}
