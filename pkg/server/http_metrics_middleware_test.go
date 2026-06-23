package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// httpMetricSample 用于在 gather 后按 (endpoint, method, status) 索引计数 / 耗时。
type httpMetricSample struct {
	endpoint string
	method   string
	status   string
}

// gatherHTTPMetrics 抓取 registry 中 widb_http_* 指标，转换为可比较的样本。
// 计数取 label 组合唯一时的累加值；耗时取 histogram sum 字段近似。
func gatherHTTPMetrics(t *testing.T, reg *prometheus.Registry) (counts map[httpMetricSample]float64, durations map[httpMetricSample]float64) {
	t.Helper()
	counts = map[httpMetricSample]float64{}
	durations = map[httpMetricSample]float64{}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather 失败: %v", err)
	}
	for _, mf := range mfs {
		switch mf.GetName() {
		case "widb_http_requests_total":
			for _, m := range mf.GetMetric() {
				sample := httpMetricSample{
					endpoint: labelValue(m.Label, "endpoint"),
					method:   labelValue(m.Label, "method"),
					status:   labelValue(m.Label, "status"),
				}
				if c := m.GetCounter(); c != nil {
					counts[sample] = c.GetValue()
				}
			}
		case "widb_http_request_duration_seconds":
			for _, m := range mf.GetMetric() {
				sample := httpMetricSample{
					endpoint: labelValue(m.Label, "endpoint"),
					method:   labelValue(m.Label, "method"),
				}
				if h := m.GetHistogram(); h != nil {
					durations[sample] = h.GetSampleSum()
				}
			}
		}
	}
	return counts, durations
}

func labelValue(labels []*dto.LabelPair, key string) string {
	for _, l := range labels {
		if l.GetName() == key {
			return l.GetValue()
		}
	}
	return ""
}

// withFreshServer 创建一个使用独立 registry 的 Server，绕开全局指标污染。
func withFreshServer(t *testing.T) (*Server, *prometheus.Registry) {
	t.Helper()
	dir := t.TempDir()
	reg := prometheus.NewRegistry()
	srv, err := NewServer(Config{
		TCPAddr:  "127.0.0.1:0",
		HTTPAddr: "127.0.0.1:0",
		DataDir:  dir,
	}, WithMetricsRegistry(reg))
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	return srv, reg
}

// TestHTTPMiddlewareIncrementsCounter 验证 /query 200 响应后
// widb_http_requests_total{endpoint="/query",method="POST",status="2xx"} 自增 1。
func TestHTTPMiddlewareIncrementsCounter(t *testing.T) {
	srv, reg := withFreshServer(t)
	// 建表使 SELECT * FROM users 解析为有效计划
	if err := srv.catalog.CreateTable("users", []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
	}, []string{"id"}, catalog.TableOptions{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	mux := srv.registerHTTPHandlers()

	body := `{"sql":"SELECT * FROM users"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	counts, _ := gatherHTTPMetrics(t, reg)
	key := httpMetricSample{endpoint: "/query", method: "POST", status: "2xx"}
	if counts[key] < 1 {
		t.Errorf("/query POST 2xx 计数 = %v, 期望 >= 1", counts[key])
	}
}

// TestHTTPMiddlewareRecords404Status 验证未匹配路径汇聚到 "other" 标签，
// 且状态码归一为 4xx。
func TestHTTPMiddlewareRecords404Status(t *testing.T) {
	srv, reg := withFreshServer(t)
	mux := srv.registerHTTPHandlers()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 %d", w.Code, http.StatusNotFound)
	}

	counts, _ := gatherHTTPMetrics(t, reg)
	key := httpMetricSample{endpoint: "other", method: "GET", status: "4xx"}
	if counts[key] < 1 {
		t.Errorf("other GET 4xx 计数 = %v, 期望 >= 1", counts[key])
	}
}

// TestHTTPMiddlewareRecordsMethodNotAllowed 验证 /query 收到 GET 时计入 4xx 桶。
func TestHTTPMiddlewareRecordsMethodNotAllowed(t *testing.T) {
	srv, reg := withFreshServer(t)
	mux := srv.registerHTTPHandlers()

	req := httptest.NewRequest(http.MethodGet, "/query", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 %d", w.Code, http.StatusMethodNotAllowed)
	}

	counts, _ := gatherHTTPMetrics(t, reg)
	key := httpMetricSample{endpoint: "/query", method: "GET", status: "4xx"}
	if counts[key] < 1 {
		t.Errorf("/query GET 4xx 计数 = %v, 期望 >= 1", counts[key])
	}
}

// TestHTTPMiddlewareRecordsDuration 验证耗时直方图 sum 大于 0。
// 通过多个端点调用保证所有桶都至少有 1 次 observe。
func TestHTTPMiddlewareRecordsDuration(t *testing.T) {
	srv, reg := withFreshServer(t)
	if err := srv.catalog.CreateTable("users", []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
	}, []string{"id"}, catalog.TableOptions{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	mux := srv.registerHTTPHandlers()

	// 触发 /query（POST）、/health（GET）、/other（GET 404）
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(`{"sql":"SELECT * FROM users"}`)))
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	_, durations := gatherHTTPMetrics(t, reg)
	for _, ep := range []httpMetricSample{
		{endpoint: "/query", method: "POST"},
		{endpoint: "/health", method: "GET"},
		{endpoint: "other", method: "GET"},
	} {
		if durations[ep] <= 0 {
			t.Errorf("%v 耗时 sum = %v, 期望 > 0", ep, durations[ep])
		}
	}
}

// TestHTTPMiddlewareNoCardinalityExplosion 验证随机路径不会产生新标签组合。
// 攻击者若能撑爆 endpoint 标签基数，prometheus 就会 OOM。
func TestHTTPMiddlewareNoCardinalityExplosion(t *testing.T) {
	srv, reg := withFreshServer(t)
	mux := srv.registerHTTPHandlers()

	// 1000 个随机路径
	for i := 0; i < 1000; i++ {
		path := fmt.Sprintf("/random/%d", i)
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	counts, _ := gatherHTTPMetrics(t, reg)
	total := float64(0)
	for k, v := range counts {
		if k.endpoint == "other" {
			total += v
		}
	}
	if total < 1000 {
		t.Errorf("other 标签累计计数 = %v, 期望 >= 1000（所有随机路径应汇聚到 other）", total)
	}
	for k := range counts {
		if k.endpoint != "other" && k.endpoint != "/query" && k.endpoint != "/write" &&
			k.endpoint != "/health" && k.endpoint != "/admin/flush" && k.endpoint != "/admin/compact" {
			t.Errorf("出现未预期 endpoint 标签 %q，会撑爆基数", k.endpoint)
		}
	}
}

// TestHTTPMiddlewarePanicRecoversAndRecords 验证 handler panic 仍能写入 5xx 计数后继续抛错。
func TestHTTPMiddlewarePanicRecoversAndRecords(t *testing.T) {
	srv, reg := withFreshServer(t)
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	wrapped := srv.httpMetricsMiddleware("/panic", boom)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("panic 应被中间件重新抛出")
		}
		counts, _ := gatherHTTPMetrics(t, reg)
		key := httpMetricSample{endpoint: "/panic", method: "GET", status: "5xx"}
		if counts[key] < 1 {
			t.Errorf("panic 后 /panic GET 5xx 计数 = %v, 期望 >= 1", counts[key])
		}
	}()
	wrapped(w, req)
}

// TestStatusClass 验证状态码归一函数对边界值的处理。
func TestStatusClass(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{200, "2xx"},
		{201, "2xx"},
		{299, "2xx"},
		{301, "3xx"},
		{400, "4xx"},
		{404, "4xx"},
		{499, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{0, "5xx"},
		{99, "5xx"},
		{1000, "5xx"},
	}
	for _, c := range cases {
		if got := statusClass(c.in); got != c.want {
			t.Errorf("statusClass(%d) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestStatusWriterCapturesCode 验证 statusWriter 能正确捕获 WriteHeader 与默认 200。
func TestStatusWriterCapturesCode(t *testing.T) {
	t.Run("explicit WriteHeader", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sw := &statusWriter{ResponseWriter: rec, status: 200}
		sw.WriteHeader(404)
		if sw.status != 404 {
			t.Errorf("status = %d, 期望 404", sw.status)
		}
		if rec.Code != 404 {
			t.Errorf("recorder Code = %d, 期望 404", rec.Code)
		}
	})

	t.Run("implicit Write defaults to 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sw := &statusWriter{ResponseWriter: rec, status: 200}
		if _, err := sw.Write([]byte("ok")); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
		if sw.status != http.StatusOK {
			t.Errorf("status = %d, 期望 %d", sw.status, http.StatusOK)
		}
	})

	t.Run("double WriteHeader is idempotent", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sw := &statusWriter{ResponseWriter: rec, status: 200}
		sw.WriteHeader(201)
		sw.WriteHeader(500) // 第二次调用应被忽略
		if sw.status != 201 {
			t.Errorf("status = %d, 期望 201", sw.status)
		}
	})
}

// TestHTTPMiddlewareNilMetrics 验证 metrics 字段为 nil 时中间件不会 panic。
// 模拟未初始化 metrics 的早期启动场景。
func TestHTTPMiddlewareNilMetrics(t *testing.T) {
	srv := &Server{} // 故意不初始化 metrics
	mw := srv.httpMetricsMiddleware("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	})
	rec := httptest.NewRecorder()
	mw(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Code = %d, 期望 200", rec.Code)
	}
}

// TestHTTPMetricsEndpointNotRecorded 验证 /metrics 自身不被记录（避免自递归）。
// 通过连续两次抓取并比较 /metrics 抓取次数是否增加 0 来检测。
func TestHTTPMetricsEndpointNotRecorded(t *testing.T) {
	srv, reg := withFreshServer(t)
	mux := srv.registerHTTPHandlers()

	for i := 0; i < 3; i++ {
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
	}

	counts, _ := gatherHTTPMetrics(t, reg)
	if c, ok := counts[httpMetricSample{endpoint: "/metrics", method: "GET", status: "2xx"}]; ok && c > 0 {
		t.Errorf("/metrics 自身被记录 %v 次，期望 0（自递归防护）", c)
	}
}

// TestHTTPMiddlewareElapsedTime 验证耗时观测值与实际请求耗时的量级一致。
// 故意 sleep 50ms 触发明显延迟。
func TestHTTPMiddlewareElapsedTime(t *testing.T) {
	srv, reg := withFreshServer(t)
	slow := srv.httpMetricsMiddleware("/slow", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	})
	rec := httptest.NewRecorder()
	slow(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))

	_, durations := gatherHTTPMetrics(t, reg)
	got := durations[httpMetricSample{endpoint: "/slow", method: "GET"}]
	if got < 0.05 {
		t.Errorf("耗时 = %v 秒, 期望 >= 0.05（sleep 50ms）", got)
	}
	if got > 1.0 {
		t.Errorf("耗时 = %v 秒, 期望 < 1.0（不应异常拉长）", got)
	}
}

// TestHTTPMetricsHandlerConcurrent 验证并发请求下计数与耗时的累加正确。
func TestHTTPMetricsHandlerConcurrent(t *testing.T) {
	srv, reg := withFreshServer(t)
	mux := srv.registerHTTPHandlers()

	const n = 50
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}

	counts, _ := gatherHTTPMetrics(t, reg)
	got := counts[httpMetricSample{endpoint: "/health", method: "GET", status: "2xx"}]
	if got < n {
		t.Errorf("并发后 /health GET 2xx 计数 = %v, 期望 >= %d", got, n)
	}
}

// TestHTTPNotFoundHandler 验证 httpNotFound 行为：JSON 错误响应 + 404 状态码。
func TestHTTPNotFoundHandler(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	srv.httpNotFound(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Code = %d, 期望 %d", rec.Code, http.StatusNotFound)
	}
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	if resp.Code != -1 {
		t.Errorf("resp.Code = %d, 期望 -1", resp.Code)
	}
	if !strings.Contains(resp.Message, "/foo/bar") {
		t.Errorf("resp.Message = %q, 应包含路径", resp.Message)
	}
}

// TestHTTPMetricsRecordedOnError 验证业务 handler 返回 4xx 时指标仍正确落盘。
func TestHTTPMetricsRecordedOnError(t *testing.T) {
	srv, reg := withFreshServer(t)
	mux := srv.registerHTTPHandlers()

	// /query 收到非法 JSON 应返回 400
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString("not json"))
	mux.ServeHTTP(httptest.NewRecorder(), req)

	counts, _ := gatherHTTPMetrics(t, reg)
	if counts[httpMetricSample{endpoint: "/query", method: "POST", status: "4xx"}] < 1 {
		t.Errorf("/query POST 4xx 计数缺失")
	}
}
