package server

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus 指标名称常量。
const (
	metricQueriesTotal  = "widb_queries_total"
	metricQueryDuration = "widb_query_duration_seconds"
	metricWritesTotal   = "widb_writes_total"
	metricMemtableSize  = "widb_memtable_size_bytes"
	metricSegmentCount  = "widb_segment_count"

	// Segment level 标签值。
	levelL0    = "l0"
	levelTotal = "total"
)

// metrics 包含服务器的 Prometheus 监控指标。
type metrics struct {
	registry          *prometheus.Registry
	queriesTotal      *prometheus.CounterVec
	queryDuration     *prometheus.HistogramVec
	writesTotal       prometheus.Counter
	memtableSizeBytes prometheus.Gauge
	segmentCount      *prometheus.GaugeVec
}

// newMetrics 创建并注册所有 Prometheus 指标。
func newMetrics() *metrics {
	reg := prometheus.NewRegistry()

	m := &metrics{
		registry: reg,
		queriesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricQueriesTotal,
				Help: "查询总数",
			},
			[]string{"type"},
		),
		queryDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    metricQueryDuration,
				Help:    "查询耗时分布",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"type"},
		),
		writesTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: metricWritesTotal,
				Help: "写入总数",
			},
		),
		memtableSizeBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: metricMemtableSize,
				Help: "当前 MemTable 大小",
			},
		),
		segmentCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: metricSegmentCount,
				Help: "Segment 数量",
			},
			[]string{"level"},
		),
	}

	reg.MustRegister(m.queriesTotal)
	reg.MustRegister(m.queryDuration)
	reg.MustRegister(m.writesTotal)
	reg.MustRegister(m.memtableSizeBytes)
	reg.MustRegister(m.segmentCount)

	return m
}

// updateStorageMetrics 从存储引擎更新存储相关的 Gauge 指标。
func (s *Server) updateStorageMetrics() {
	if s.storage == nil {
		return
	}
	s.metrics.memtableSizeBytes.Set(float64(s.storage.MemTableSize()))
	s.metrics.segmentCount.WithLabelValues(levelL0).Set(float64(s.storage.L0SegmentCount()))
	s.metrics.segmentCount.WithLabelValues(levelTotal).Set(float64(s.storage.SegmentCount()))
}

// recordQuery 记录一次查询的指标。
func (m *metrics) recordQuery(typ string, duration time.Duration) {
	m.queriesTotal.WithLabelValues(typ).Inc()
	m.queryDuration.WithLabelValues(typ).Observe(duration.Seconds())
}

// recordWrite 记录一次写入的指标。
func (m *metrics) recordWrite() {
	m.writesTotal.Inc()
}

// promhttpHandlerFor 为给定的 Registry 创建 promhttp 处理器。
func promhttpHandlerFor(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
