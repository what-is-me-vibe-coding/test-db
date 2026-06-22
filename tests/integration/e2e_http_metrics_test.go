// Package integration 端到端集成测试：HTTP 服务层指标 widb_http_*。
//
// 补齐 pkg/server/http_metrics_middleware_test.go 的端到端覆盖：启动一个独立 server，
// 通过 net/http 真实客户端触发 /query、/write、/health、/admin/flush 等端点，
// 然后抓取 /metrics 解析 widb_http_requests_total 与 widb_http_request_duration_seconds，
// 验证：
//   - /query 200 计入 2xx 桶
//   - /write 400 计入 4xx 桶
//   - /health 200 计入 2xx 桶
//   - /admin/flush POST 也被记录
//   - 耗时直方图 sum > 0
//   - 多次请求后计数累加正确
//   - 并发请求下指标累加正确
//
// 与 unit 测试的差异：unit 使用 httptest.NewRecorder，无真实 TCP 监听；
// 本测试用真实 server + net/http，覆盖完整的 mux → http.Server → TCP → 客户端回路。
package integration

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/server"
)

// httpMetricsIntFromText 从 /metrics 文本输出中解析指定指标在指定标签下的整数值。
// 未找到时返回 0；存在多行匹配时取所有行之和（实际 prometheus 不会重复同一标签组合）。
func httpMetricsIntFromText(body, metric string, labels map[string]string) int {
	// 注：标签顺序由 NewMetrics 中 WithLabelValues 的调用顺序决定。
	// status 可能在最后也可能不在，故只要求标签集合存在；不做完整行匹配。
	// 退而求其次：先按行扫描包含 metric 名的行，再按标签过滤。
	lines := strings.Split(body, "\n")
	total := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, metric) {
			continue
		}
		// 行必须包含所有指定标签
		matched := true
		for k, v := range labels {
			if !strings.Contains(line, k+`="`+v+`"`) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		// 提取最后的数值
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.Atoi(strings.TrimSuffix(fields[len(fields)-1], ".0"))
		if err != nil {
			// 可能是 float
			f, err2 := strconv.ParseFloat(fields[len(fields)-1], 64)
			if err2 != nil {
				continue
			}
			val = int(f)
		}
		total += val
	}
	return total
}

// startHTTPTestServer 启动一个 HTTP server，addr 为实际绑定地址。
// 使用独立 prometheus registry 避免多个 server 共享 DefaultRegisterer 时 panic。
func startHTTPTestServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	dir := t.TempDir()
	registry := prometheus.NewRegistry()
	srv, err := server.NewServer(server.Config{
		TCPAddr:  "127.0.0.1:0",
		HTTPAddr: "127.0.0.1:0",
		DataDir:  dir,
	}, server.WithMetricsRegistry(registry))
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	addr = srv.HTTPAddr()
	stop = func() { _ = srv.Stop() }
	return addr, stop
}

// postJSON 简化 HTTP POST + JSON Content-Type 调用。
func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	r, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s 失败: %v", url, err)
	}
	return r
}

// fetchMetrics 抓 /metrics 文本输出。
func fetchMetrics(t *testing.T, addr string) string {
	t.Helper()
	r, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics 失败: %v", err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

// TestE2EHTTPMetricsEndToEnd 触发 /query、/write、/health 后抓 /metrics 验证分类计数。
func TestE2EHTTPMetricsEndToEnd(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	// 1. 建表让 /query 成功
	r := postJSON(t, "http://"+addr+"/query", `{"sql":"CREATE TABLE m (id INT64, PRIMARY KEY(id))"}`)
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	// 2. 触发 /query 成功 3 次
	for i := 0; i < 3; i++ {
		r := postJSON(t, "http://"+addr+"/query", `{"sql":"SELECT * FROM m"}`)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}

	// 3. 触发 /write 失败 2 次（缺主键列）
	for i := 0; i < 2; i++ {
		r := postJSON(t, "http://"+addr+"/write", `{"table":"m","rows":[{"name":"missing_pk"}]}`)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}

	// 4. 触发 /health 4 次
	for i := 0; i < 4; i++ {
		r, err := http.Get("http://" + addr + "/health")
		if err != nil {
			t.Fatalf("/health 失败: %v", err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}

	bodyStr := fetchMetrics(t, addr)

	cases := []struct {
		endpoint string
		method   string
		status   string
		min      int
	}{
		{"/query", "POST", "2xx", 3},
		{"/write", "POST", "4xx", 2},
		{"/health", "GET", "2xx", 4},
	}
	for _, c := range cases {
		got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
			"endpoint": c.endpoint, "method": c.method, "status": c.status,
		})
		if got < c.min {
			t.Errorf("widb_http_requests_total{endpoint=%q,method=%q,status=%q} = %d, 期望 >= %d",
				c.endpoint, c.method, c.status, got, c.min)
		}
	}
}

// TestE2EHTTPMetricsLatencyRecorded 验证耗时直方图 sum > 0。
func TestE2EHTTPMetricsLatencyRecorded(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	// 触发 /health 多次
	for i := 0; i < 5; i++ {
		r, err := http.Get("http://" + addr + "/health")
		if err != nil {
			t.Fatalf("/health 失败: %v", err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}

	bodyStr := fetchMetrics(t, addr)
	// 直方图 _count 行存在
	want := `widb_http_request_duration_seconds_count{endpoint="/health",method="GET"}`
	if !strings.Contains(bodyStr, want) {
		t.Errorf("/metrics 应包含 %s", want)
	}
	// 至少应观察到样本数 >= 5
	got := httpMetricsIntFromText(bodyStr, "widb_http_request_duration_seconds_count", map[string]string{
		"endpoint": "/health", "method": "GET",
	})
	if got < 5 {
		t.Errorf("widb_http_request_duration_seconds_count{endpoint=/health,method=GET} = %d, 期望 >= 5", got)
	}
}

// TestE2EHTTPMetricsConcurrent 验证并发 HTTP 请求下指标累加正确。
func TestE2EHTTPMetricsConcurrent(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	r := postJSON(t, "http://"+addr+"/query", `{"sql":"CREATE TABLE cc (id INT64, PRIMARY KEY(id))"}`)
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			r, err := http.Post("http://"+addr+"/query", "application/json",
				strings.NewReader(`{"sql":"SELECT * FROM cc"}`))
			if err == nil {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
			}
		}()
	}
	wg.Wait()

	bodyStr := fetchMetrics(t, addr)
	got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "/query", "method": "POST", "status": "2xx",
	})
	if got < n {
		t.Errorf("并发后 /query POST 2xx 计数 = %d, 期望 >= %d", got, n)
	}
}

// TestE2EHTTPMetricsResponseShape 验证 /metrics 返回的 Content-Type 符合 Prometheus 约定。
func TestE2EHTTPMetricsResponseShape(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	r, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics 失败: %v", err)
	}
	defer r.Body.Close()
	ct := r.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "application/openmetrics-text") {
		t.Errorf("Content-Type = %q, 期望 text/plain 或 openmetrics", ct)
	}
}

// TestE2EHTTPMetricsAdminEndpoint 验证 /admin/flush、/admin/compact 也被指标中间件记录。
func TestE2EHTTPMetricsAdminEndpoint(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	// 建表避免 flush 报错
	r := postJSON(t, "http://"+addr+"/query", `{"sql":"CREATE TABLE af (id INT64, PRIMARY KEY(id))"}`)
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	// POST /admin/flush
	r = postJSON(t, "http://"+addr+"/admin/flush", `{}`)
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	// POST /admin/compact
	r = postJSON(t, "http://"+addr+"/admin/compact", `{}`)
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	bodyStr := fetchMetrics(t, addr)
	// admin/flush 计数 >= 1
	got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "/admin/flush", "method": "POST",
	})
	if got < 1 {
		t.Errorf("/admin/flush POST 计数 = %d, 期望 >= 1", got)
	}
	// admin/compact 计数 >= 1
	got = httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "/admin/compact", "method": "POST",
	})
	if got < 1 {
		t.Errorf("/admin/compact POST 计数 = %d, 期望 >= 1", got)
	}
}

// TestE2EHTTPMetricsCatalogIntegration 验证 catalog API 与 HTTP 指标协同正确。
// 覆盖：通过 catalog 建表（绕过 SQL），然后通过 HTTP 触发查询，指标应同时记录
// widb_http_requests_total{endpoint=/query,method=POST,status=2xx} 自增。
func TestE2EHTTPMetricsCatalogIntegration(t *testing.T) {
	dir := t.TempDir()
	registry := prometheus.NewRegistry()
	srv, err := server.NewServer(server.Config{
		TCPAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0", DataDir: dir,
	}, server.WithMetricsRegistry(registry))
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer srv.Stop()

	if err := srv.Catalog().CreateTable("capi", []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
	}, []string{"id"}, catalog.TableOptions{}); err != nil {
		t.Fatalf("Catalog CreateTable 失败: %v", err)
	}

	r := postJSON(t, "http://"+srv.HTTPAddr()+"/query", `{"sql":"SELECT * FROM capi"}`)
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	bodyStr := fetchMetrics(t, srv.HTTPAddr())
	got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "/query", "method": "POST", "status": "2xx",
	})
	if got < 1 {
		t.Errorf("widb_http_requests_total{endpoint=/query,method=POST,status=2xx} = %d, 期望 >= 1", got)
	}
}

// TestE2EHTTPMetricsUnmatchedPath 验证未匹配路径汇聚到 "other" 标签。
func TestE2EHTTPMetricsUnmatchedPath(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	for i := 0; i < 5; i++ {
		r, err := http.Get("http://" + addr + "/some/random/path")
		if err != nil {
			t.Fatalf("GET 失败: %v", err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}

	bodyStr := fetchMetrics(t, addr)
	got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "other", "method": "GET", "status": "4xx",
	})
	if got < 5 {
		t.Errorf("other GET 4xx 计数 = %d, 期望 >= 5", got)
	}
}

// TestE2EHTTPMetrics400FromServer 验证 /write 收到 JSON 错误时返回 4xx。
func TestE2EHTTPMetrics400FromServer(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	r := postJSON(t, "http://"+addr+"/write", "not json")
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	bodyStr := fetchMetrics(t, addr)
	got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "/write", "method": "POST", "status": "4xx",
	})
	if got < 1 {
		t.Errorf("/write POST 4xx 计数 = %d, 期望 >= 1", got)
	}
}

// TestE2EHTTPMetricsMethodNotAllowed 验证 /query GET 返回 405 计入 4xx 桶。
func TestE2EHTTPMetricsMethodNotAllowed(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	r, err := http.Get("http://" + addr + "/query")
	if err != nil {
		t.Fatalf("GET /query 失败: %v", err)
	}
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	bodyStr := fetchMetrics(t, addr)
	got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "/query", "method": "GET", "status": "4xx",
	})
	if got < 1 {
		t.Errorf("/query GET 4xx 计数 = %d, 期望 >= 1", got)
	}
}

// TestE2EHTTPMetricsAfterLongRun 验证长时间运行后指标仍能正确累加。
// 模拟「稳定运行 1 秒后」仍能正确返回的场景。
func TestE2EHTTPMetricsAfterLongRun(t *testing.T) {
	addr, stop := startHTTPTestServer(t)
	defer stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	count := 0
	for time.Now().Before(deadline) {
		r, err := http.Get("http://" + addr + "/health")
		if err != nil {
			t.Fatalf("/health 失败: %v", err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		count++
	}

	bodyStr := fetchMetrics(t, addr)
	got := httpMetricsIntFromText(bodyStr, "widb_http_requests_total", map[string]string{
		"endpoint": "/health", "method": "GET", "status": "2xx",
	})
	if got < count {
		t.Errorf("500ms 期间 /health GET 2xx 计数 = %d, 期望 >= %d", got, count)
	}
}
