# WiDB 可观测性指南

> 本文档系统化汇总 WiDB 当前暴露的所有 Prometheus 监控指标、PromQL 查询模板、Grafana 仪表板面板、告警规则与排障路径。`operations.md` §4 给出了与运维相关的概要；本文档更偏向「指标参考手册 + 仪表板蓝图 + 告警目录」的角色。
>
> 适用读者：SRE、DBA、性能工程师，以及任何需要长期运行 WiDB 并希望回答「系统现在健康吗？」「为什么慢？」「什么时候该扩盘？」的工程同学。

## 目录

- [1. 端点与抓取](#1-端点与抓取)
- [2. 指标完整参考](#2-指标完整参考)
  - [2.1 查询（QPS / 延迟 / 错误）](#21-查询qps--延迟--错误)
  - [2.2 写入（吞吐 / 错误）](#22-写入吞吐--错误)
  - [2.3 存储（MemTable / Segment / WAL）](#23-存储memtable--segment--wal)
  - [2.4 缓存（Block / Index）](#24-缓存block--index)
  - [2.5 后台任务（Flush / Compaction / WAL Clean）](#25-后台任务flush--compaction--wal-clean)
  - [2.6 HTTP 接入层](#26-http-接入层)
  - [2.7 连接](#27-连接)
- [3. PromQL 速查](#3-promql-速查)
  - [3.1 流量与错误率](#31-流量与错误率)
  - [3.2 延迟分位数](#32-延迟分位数)
  - [3.3 资源与健康度](#33-资源与健康度)
  - [3.4 命中率与裁剪率](#34-命中率与裁剪率)
- [4. Grafana 仪表板蓝图](#4-grafana-仪表板蓝图)
  - [4.1 Overview 面板](#41-overview-面板)
  - [4.2 Query 面板](#42-query-面板)
  - [4.3 Write 面板](#43-write-面板)
  - [4.4 Storage 面板](#44-storage-面板)
  - [4.5 Cache 面板](#45-cache-面板)
  - [4.6 HTTP 面板](#46-http-面板)
  - [4.7 JSON 片段（可直接导入）](#47-json-片段可直接导入)
- [5. 告警规则（Prometheus rules）](#5-告警规则prometheus-rules)
- [6. 故障诊断路径](#6-故障诊断路径)
  - [6.1 查询慢](#61-查询慢)
  - [6.2 写入慢或卡顿](#62-写入慢或卡顿)
  - [6.3 内存泄漏 / RSS 增长](#63-内存泄漏--rss-增长)
  - [6.4 数据丢失风险](#64-数据丢失风险)

## 1. 端点与抓取

| 端点 | 方法 | Content-Type | 用途 |
|------|------|--------------|------|
| `/metrics` | GET | `text/plain; version=0.0.4; charset=utf-8`（或 `application/openmetrics-text`） | Prometheus 抓取端点 |
| `/health` | GET | `application/json` | 进程健康（K8s liveness/readiness 探针） |

抓取频率建议：

- 生产环境：`scrape_interval: 15s`，与 Prometheus 默认 15s 对齐
- 调优期：`scrape_interval: 5s`，便于观察瞬时尖刺
- 长期存储：`scrape_interval: 30s`，降低 Prometheus 自身负载

```yaml
# prometheus.yml 片段
scrape_configs:
  - job_name: widb
    metrics_path: /metrics
    scrape_interval: 15s
    scrape_timeout: 10s
    static_configs:
      - targets: ['widb-1.internal:8080', 'widb-2.internal:8080']
    relabel_configs:
      - source_labels: [__address__]
        regex: 'widb-([0-9]+)\..*'
        target_label: instance
        replacement: 'widb-$1'
```

## 2. 指标完整参考

所有指标的命名空间为 `widb_`。直方图指标同时暴露 `_bucket{le="..."}`、`_sum` 与 `_count` 三个子指标；本表只列出主名称，使用时请通过 `widb_xxx_seconds_bucket` 等具体子名称。

### 2.1 查询（QPS / 延迟 / 错误）

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `widb_queries_total` | Counter | `type=success\|parse_error\|analyze_error\|execute_error` | 查询总数（按结果分类） |
| `widb_query_duration_seconds` | Histogram | `type=sql` | 查询耗时分布（默认 buckets：0.005/0.01/0.025/0.05/0.1/0.25/0.5/1/2.5/5/10） |

**关键说明**：

- `type=success` 表示 SQL 完整走通「解析 → 分析 → 执行」且无业务错误；`type=execute_error` 包含执行期异常（如主键冲突、列不存在、类型不兼容）。
- 直方图 buckets 是 Prometheus 默认 `DefBuckets`，对亚毫秒级与分钟级查询的精度不够；如需更细粒度，应在客户端二次埋点。
- 当前未按 SQL 类型（DDL/DML/DQL）拆分 tag；如需精细分析，建议在 HTTP/TCP 网关层加打点。

### 2.2 写入（吞吐 / 错误）

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `widb_writes_total` | Counter | `result=success\|table_not_found\|convert_error\|write_error` | 写入总数（按结果分类） |
| `widb_write_duration_seconds` | Histogram | `result=success` | 写入耗时分布 |

**关键说明**：

- `result=table_not_found` 通常是客户端 schema 漂移；可作为「上游写错了表名」的可观测信号。
- `result=convert_error` 与 `result=write_error` 比例上升说明写入路径中存在频繁的「重客户端 → server」类型不一致，应推动客户端修代码而不是放大 server 配额。

### 2.3 存储（MemTable / Segment / WAL）

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `widb_memtable_size_bytes` | Gauge | — | 当前 MemTable 字节数 |
| `widb_segment_count` | Gauge | `level=l0\|l1` | 各层 Segment 数量 |
| `widb_l0_segment_count` | Gauge | — | L0 Segment 数量（独立导出，便于告警） |
| `widb_wal_size_bytes` | Gauge | — | 当前 WAL 文件字节数 |
| `widb_active_connections` | Gauge | — | 当前活跃连接数 |

**关键说明**：

- `widb_segment_count` 当前只暴露 L0/L1 两层；当未来加入 L2+ 时，dashboard 与告警规则需相应扩展。
- `widb_active_connections` 同时统计 TCP、HTTP、PG wire 三种协议的活跃连接，**不区分协议**。如需按协议拆分，请在 HTTP 中间件或 PG wire server loop 中加入额外 Gauge。

### 2.4 缓存（Block / Index）

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `widb_cache_hits_total` | Counter | `cache=block\|index` | 缓存命中次数 |
| `widb_cache_misses_total` | Counter | `cache=block\|index` | 缓存未命中次数 |
| `widb_cache_size_bytes` | Gauge | `cache=block\|index` | 当前占用字节数 |
| `widb_cache_entries` | Gauge | `cache=block\|index` | 当前条目数 |

**关键说明**：

- Block 缓存命中率长期低于 70% 通常意味着数据规模超过缓存容量；建议提高 `BlockCacheSize` 或缩短扫描范围。
- Index 缓存命中率长期低于 90% 通常意味着查询范围在「时间上分散」；考虑按时间分表或提高 `IndexCacheSize`。

### 2.5 后台任务（Flush / Compaction / WAL Clean）

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `widb_flush_total` | Counter | — | MemTable → Segment 刷盘累计次数 |
| `widb_compact_total` | Counter | — | Compaction 累计次数 |
| `widb_wal_clean_total` | Counter | — | WAL 清理累计次数 |

**关键说明**：

- 当前仅暴露「次数」未暴露「耗时」；如需分析写停顿与 Compaction 慢，需结合 `widb_query_duration_seconds`/`widb_write_duration_seconds` 与 `widb_l0_segment_count` 的拐点交叉判断。
- 短期可加的扩展：`widb_flush_duration_seconds` / `widb_compact_duration_seconds` / `widb_wal_clean_duration_seconds` 三个 Histogram。

### 2.6 HTTP 接入层

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `widb_http_requests_total` | Counter | `endpoint` / `method` / `status` | HTTP 请求总数 |
| `widb_http_request_duration_seconds` | Histogram | `endpoint` / `method` | HTTP 请求耗时分布 |

**关键说明**：

- `endpoint` 已知值：`/query` / `/write` / `/health` / `/admin/flush` / `/admin/compact` / `other`（未匹配的 URL）。
- `status` 由状态码百位归一：`2xx` / `3xx` / `4xx` / `5xx`，刻意控制标签基数。
- `method` 当前只有 `GET` 与 `POST`。

### 2.7 连接

| 指标 | 类型 | 含义 |
|------|------|------|
| `widb_active_connections` | Gauge | 当前活跃连接数 |

> 当前未按协议拆分；如果要按协议分别观测 TCP / HTTP / PG wire 的并发连接，需在协议层单独打 Gauge。

## 3. PromQL 速查

下面所有 PromQL 片段默认已经在 PromQL 表达式中用 `widb_` 命名空间过滤；直接复制到 Grafana / Prometheus 中可用。

### 3.1 流量与错误率

```promql
# QPS（按 type 拆分）
sum by (type) (rate(widb_queries_total[1m]))

# 写入 QPS（按 result 拆分）
sum by (result) (rate(widb_writes_total[1m]))

# 查询错误率
sum(rate(widb_queries_total{type!="success"}[5m]))
  /
sum(rate(widb_queries_total[5m]))

# 写入错误率
sum(rate(widb_writes_total{result!="success"}[5m]))
  /
sum(rate(widb_writes_total[5m]))
```

### 3.2 延迟分位数

```promql
# P50 / P95 / P99 查询延迟
histogram_quantile(0.50, sum by (le) (rate(widb_query_duration_seconds_bucket[5m])))
histogram_quantile(0.95, sum by (le) (rate(widb_query_duration_seconds_bucket[5m])))
histogram_quantile(0.99, sum by (le) (rate(widb_query_duration_seconds_bucket[5m])))

# 按 HTTP 端点拆分的 P99
histogram_quantile(0.99, sum by (le, endpoint) (rate(widb_http_request_duration_seconds_bucket[5m])))

# 平均写入耗时
rate(widb_write_duration_seconds_sum[5m])
  /
rate(widb_write_duration_seconds_count[5m])
```

### 3.3 资源与健康度

```promql
# MemTable 占用率（假设 max_memtable=64MiB；实际阈值按部署调整）
widb_memtable_size_bytes / (64 * 1024 * 1024)

# L0 段数（> 20 = 告警阈值）
widb_l0_segment_count

# WAL 文件大小（MB）
widb_wal_size_bytes / (1024 * 1024)

# 当前活跃连接数
widb_active_connections

# 刷盘速率（次/分钟）
rate(widb_flush_total[1m]) * 60

# Compaction 速率
rate(widb_compact_total[1m]) * 60
```

### 3.4 命中率与裁剪率

```promql
# Block 缓存命中率
sum(rate(widb_cache_hits_total{cache="block"}[5m]))
  /
clamp_min(
  sum(rate(widb_cache_hits_total{cache="block"}[5m]))
  + sum(rate(widb_cache_misses_total{cache="block"}[5m])),
  1
)

# Index 缓存命中率
sum(rate(widb_cache_hits_total{cache="index"}[5m]))
  /
clamp_min(
  sum(rate(widb_cache_hits_total{cache="index"}[5m]))
  + sum(rate(widb_cache_misses_total{cache="index"}[5m])),
  1
)
```

> `clamp_min(..., 1)` 防止分母为 0 时产生 `NaN`，确保在空跑期间图表仍能正常渲染（值为 0）。

## 4. Grafana 仪表板蓝图

下面给出一份「最少必要面板」清单，每个面板都附了 PromQL 与建议阈值。具体视觉布局可在 Grafana 中按需调整。

> 推荐布局：12 列网格，单个面板占 6 列 × 8 行（顶部两个一组），可一次铺开 8-10 个面板。

### 4.1 Overview 面板

| 图 | PromQL | 阈值/参考 |
|----|--------|----------|
| 活跃连接数（折线） | `widb_active_connections` | < 80% `max_connections` |
| QPS（柱状，按 type） | `sum by (type) (rate(widb_queries_total[1m]))` | 健康：稳定；剧增 = 突发流量 |
| 写入 QPS（柱状，按 result） | `sum by (result) (rate(widb_writes_total[1m]))` | 错误率 < 0.5% |
| 整体错误率（数字） | 见 §3.1 | 告警阈值见 §5 |

### 4.2 Query 面板

| 图 | PromQL | 阈值/参考 |
|----|--------|----------|
| P50/P95/P99 查询延迟 | 见 §3.2 | P99 < 10s（默认 SLO） |
| QPS（按 type） | `sum by (type) (rate(widb_queries_total[1m]))` | 关注 `execute_error` 拐点 |
| 错误率 | 见 §3.1 | < 1% |
| 平均延迟 | `rate(widb_query_duration_seconds_sum[5m]) / rate(widb_query_duration_seconds_count[5m])` | 与 P99 配合判断长尾 |

### 4.3 Write 面板

| 图 | PromQL | 阈值/参考 |
|----|--------|----------|
| 写入 QPS（按 result） | `sum by (result) (rate(widb_writes_total[1m]))` | `result=table_not_found` > 0 = 客户端 bug |
| 错误率 | 见 §3.1 | < 0.5% |
| 平均写入耗时 | `rate(widb_write_duration_seconds_sum[5m]) / rate(widb_write_duration_seconds_count[5m])` | 显著上升 = WAL 抖动 / 刷盘慢 |
| 刷盘速率 | `rate(widb_flush_total[1m]) * 60` | 与 L0 段数交叉看 |

### 4.4 Storage 面板

| 图 | PromQL | 阈值/参考 |
|----|--------|----------|
| MemTable 占用率 | `widb_memtable_size_bytes / (64 * 1024 * 1024)` | > 80% 触发强制刷盘 |
| MemTable 字节数 | `widb_memtable_size_bytes` | 趋势 |
| L0 / L1 段数 | `widb_segment_count` | L0 > 20 = Compaction 跟不上 |
| WAL 大小（MB） | `widb_wal_size_bytes / (1024 * 1024)` | > 256 MB = 调度器可能卡住 |

### 4.5 Cache 面板

| 图 | PromQL | 阈值/参考 |
|----|--------|----------|
| Block 命中率 | 见 §3.4 | > 70% 健康；< 50% 考虑扩容 |
| Index 命中率 | 见 §3.4 | > 90% 健康 |
| 缓存占用 | `widb_cache_size_bytes` | 与 `BlockCacheSize` 配置对比 |
| 缓存条目数 | `widb_cache_entries` | 与配置容量对比 |

### 4.6 HTTP 面板

| 图 | PromQL | 阈值/参考 |
|----|--------|----------|
| 请求 QPS（按 endpoint × method） | `sum by (endpoint, method) (rate(widb_http_requests_total[1m]))` | 关注 `other` 端点占比 |
| 状态分布（按 endpoint × status） | `sum by (endpoint, status) (rate(widb_http_requests_total[1m]))` | 5xx > 1% 需关注 |
| P99 延迟（按 endpoint） | 见 §3.2 | /query /write P99 < 5s |

### 4.7 JSON 片段（可直接导入）

下面提供一份「轻量版」仪表板的 JSON 片段，复制到 Grafana → Dashboard → Settings → JSON Model 即可导入。完整模板包含 6 个 Row、12 个 Panel，每个 Panel 对应一个 PromQL。

> 由于 Grafana JSON 模板较长（> 200 行），建议使用 Grafana 内置的「Export for sharing externally」功能从一份可工作的仪表板导出；本节仅给出每个 Panel 的最小化 JSON 模板与查询，避免文档与最新版本不一致。

最小 Panel 模板（每个 Panel 替换 `targets[0].expr` 与 `title`）：

```json
{
  "type": "timeseries",
  "title": "Query QPS by type",
  "gridPos": { "x": 0, "y": 0, "w": 12, "h": 8 },
  "datasource": { "type": "prometheus", "uid": "prometheus" },
  "targets": [
    {
      "expr": "sum by (type) (rate(widb_queries_total[1m]))",
      "legendFormat": "{{type}}",
      "refId": "A"
    }
  ],
  "fieldConfig": { "defaults": { "unit": "reqps" } }
}
```

> 在生产部署中，建议把仪表板 JSON 放进 `infra/grafana/widb-dashboard.json` 与代码同库；变更时通过 PR 评审，避免「文档/面板漂移」。

## 5. 告警规则（Prometheus rules）

下面给出一份可直接复制到 Prometheus 配置文件 `rule_files` 下的告警规则。所有阈值都是经验值，部署时按硬件配置与 SLO 调整。

```yaml
groups:
  - name: widb.rules
    interval: 30s
    rules:
      # 1) 查询错误率持续 > 1%
      - alert: WidbHighQueryErrorRate
        expr: |
          sum(rate(widb_queries_total{type!="success"}[5m]))
            /
          sum(rate(widb_queries_total[5m])) > 0.01
        for: 5m
        labels:
          severity: warning
          service: widb
        annotations:
          summary: "WiDB 查询错误率超过 1%"
          description: "实例 {{ $labels.instance }} 的查询错误率 = {{ $value | humanizePercentage }}"

      # 2) 写入错误率持续 > 0.5%
      - alert: WidbHighWriteErrorRate
        expr: |
          sum(rate(widb_writes_total{result!="success"}[5m]))
            /
          sum(rate(widb_writes_total[5m])) > 0.005
        for: 5m
        labels:
          severity: warning
          service: widb
        annotations:
          summary: "WiDB 写入错误率超过 0.5%"
          description: "实例 {{ $labels.instance }} 的写入错误率 = {{ $value | humanizePercentage }}"

      # 3) 查询 P99 > 10s
      - alert: WidbQueryP99High
        expr: |
          histogram_quantile(0.99, sum by (le) (rate(widb_query_duration_seconds_bucket[5m]))) > 10
        for: 10m
        labels:
          severity: warning
          service: widb
        annotations:
          summary: "WiDB 查询 P99 超过 10s"
          description: "实例 {{ $labels.instance }} 的查询 P99 = {{ $value }}s"

      # 4) 持续 5 分钟零写入
      - alert: WidbWriteStalled
        expr: sum(rate(widb_writes_total[5m])) == 0 and sum(widb_writes_total) > 0
        for: 10m
        labels:
          severity: warning
          service: widb
        annotations:
          summary: "WiDB 已 5 分钟无写入"
          description: "实例 {{ $labels.instance }} 自上次写入已过 5 分钟，请检查上游"

      # 5) MemTable 占用率 > 80%
      - alert: WidbMemTableNearFull
        expr: |
          widb_memtable_size_bytes / (64 * 1024 * 1024) > 0.8
        for: 5m
        labels:
          severity: warning
          service: widb
        annotations:
          summary: "WiDB MemTable 占用率 > 80%"
          description: "实例 {{ $labels.instance }} 当前占用 = {{ $value | humanizePercentage }}"

      # 6) L0 段数堆积
      - alert: WidbL0SegmentsHigh
        expr: widb_l0_segment_count > 20
        for: 10m
        labels:
          severity: warning
          service: widb
        annotations:
          summary: "WiDB L0 Segment 堆积"
          description: "实例 {{ $labels.instance }} 当前 L0 段数 = {{ $value }}，可能 Compaction 跟不上"

      # 7) WAL 文件异常增长
      - alert: WidbWALTooLarge
        expr: widb_wal_size_bytes > 256 * 1024 * 1024
        for: 5m
        labels:
          severity: critical
          service: widb
        annotations:
          summary: "WiDB WAL 文件超过 256MB"
          description: "实例 {{ $labels.instance }} 当前 WAL = {{ $value | humanize1024 }}B，可能 WAL clean 调度器卡住"

      # 8) HTTP 5xx 错误率
      - alert: WidbHTTP5xxHigh
        expr: |
          sum by (instance) (rate(widb_http_requests_total{status="5xx"}[5m]))
            /
          sum by (instance) (rate(widb_http_requests_total[5m])) > 0.01
        for: 5m
        labels:
          severity: critical
          service: widb
        annotations:
          summary: "WiDB HTTP 5xx 错误率 > 1%"
          description: "实例 {{ $labels.instance }} 5xx 占比 = {{ $value | humanizePercentage }}"

      # 9) Block 缓存命中率低
      - alert: WidbBlockCacheHitRateLow
        expr: |
          sum(rate(widb_cache_hits_total{cache="block"}[10m]))
            /
          clamp_min(
            sum(rate(widb_cache_hits_total{cache="block"}[10m]))
            + sum(rate(widb_cache_misses_total{cache="block"}[10m])),
            1
          ) < 0.5
        for: 30m
        labels:
          severity: info
          service: widb
        annotations:
          summary: "WiDB Block 缓存命中率 < 50%"
          description: "实例 {{ $labels.instance }} 当前命中率 = {{ $value | humanizePercentage }}，考虑扩容或缩短扫描范围"
```

> 调优建议：
> - 「WAL Too Large」使用 `severity: critical`，需要 24x7 有人盯；其他告警用 `warning` 由工单系统兜底。
> - 「Block 缓存命中率」阈值不要定太高（< 50% 已是较严重的可观测信号；60%-70% 属于「有待优化」而非「告警」）。
> - 所有 PromQL 末尾不要加 `or vector(0)`，否则在空跑期告警会一直触发，污染值班。

## 6. 故障诊断路径

下面按「症状 → 关键指标 → 排查方向」三段式总结常见问题。具体修复手段详见 [operations.md](operations.md) §4 与 [performance.md](performance.md) §5。

### 6.1 查询慢

| 症状 | 关键指标 | 排查方向 |
|------|----------|----------|
| P99 > 10s 持续 | `widb_query_duration_seconds` P99 | ① 是不是缺少主键过滤；② 是不是 `widb_cache_hits_total` 命中率低（见 §3.4）；③ 是不是 L0 段数过多导致范围扫读放大 |
| 5xx 错误突增 | `widb_http_requests_total{status="5xx"}` 上升 | 看 server 日志定位 panic 或超时；常见原因：写热路径上的 panic、Group Commit 死锁 |
| 单查询 OOM | （无指标直接可见） | 检查 `data_dir` 所在盘是否还有空间、MemTable 是否被异常放大 |

### 6.2 写入慢或卡顿

| 症状 | 关键指标 | 排查方向 |
|------|----------|----------|
| 写入 P99 上升 | `widb_write_duration_seconds` P99 | ① MemTable 占用率 ≥ 80% → 等待刷盘；② L0 段数 ≥ 20 → Compaction 阻塞前台的 Flush |
| 持续 5 分钟零写入 | `widb_writes_total` 速率归零 | ① 上游是否断开；② HTTP 5xx 比例；③ server 是否在刷盘长尾（看 `widb_memtable_size_bytes` 拐点） |
| WAL 一直涨 | `widb_wal_size_bytes` 单调上升 | 检查 `wal_clean_interval` / `wal_clean_threshold`；查看日志中是否有 WAL clean panic |

### 6.3 内存泄漏 / RSS 增长

WiDB 当前没有直接的 RSS 指标；建议在 host 侧（cAdvisor / node_exporter）补齐：

| 外部指标 | PromQL | 阈值 |
|---------|--------|------|
| Container RSS | `container_memory_rss{name="widb"}` | 24h 增长 < 10% |
| Container Cache | `container_memory_cache{name="widb"}` | 视业务波动 |
| Go heap | `go_memstats_heap_inuse_bytes{job="widb"}` | 24h 增长 < 10% |

> 如果 RSS 持续上升：① 是否有大查询未释放 Chunk？② Block Cache 容量是否合理（看 `widb_cache_size_bytes`）？③ 是否有客户端在长连接上每请求都创建大对象？

### 6.4 数据丢失风险

| 风险 | 关键指标 | 排查方向 |
|------|----------|----------|
| 刷盘失败但未告警 | `widb_flush_total` 长时间不增长 | 检查日志 `flush error` / `flush panic` |
| WAL 损坏 | （无指标） | 启动报 "WAL replay failed"；参考 [troubleshooting.md](troubleshooting.md) §6.1 |
| Catalog 损坏 | （无指标） | 启动报 "catalog load failed"；从备份恢复 |

> 当前指标体系没有直接覆盖「刷盘/Compaction 错误」计数；建议在后续版本加 `widb_flush_errors_total` / `widb_compact_errors_total` 计数器，以便在「次数不变」与「次数跌零」之间做差异化告警。

---

## 附录 A：标签基数控制

为避免 Prometheus / TSDB 因高基数标签爆炸，下面是已遵守的「标签基数控制」原则：

- `widb_http_requests_total.status` 归一为 2xx/3xx/4xx/5xx，**不**保留 200/201/204 等具体值。
- `widb_cache_*` 的 `cache` 标签只有 `block` 与 `index` 两个固定值。
- `widb_segment_count.level` 只有 `l0` 与 `l1`。
- 不暴露「按表名」「按 SQL 文本」「按连接 ID」的标签，避免标签爆炸。

新增指标时请遵循上述原则；如需更高维度打点，优先考虑「服务端采样 + 异步上报」或「客户端埋点 + 日志聚合」。

## 附录 B：版本兼容

- 本文档基于代码中 `pkg/server/metrics.go` 实际注册的全部指标编写。
- 与代码同 PR 变更：当 `metrics.go` 新增/修改指标时，应同步更新本文件与 `operations.md` §4，避免「指标漂移」。
- 旧版已下线但 `troubleshooting.md` §9.1 仍提到的指标（`widb_query_total` / `widb_write_bytes_total` / `widb_memtable_bytes` / `widb_cache_hit_ratio` / `widb_sparse_index_skip_ratio` / `widb_compact_duration_seconds` / `widb_wal_sync_duration_seconds`）均已不在当前 `metrics.go` 中；如使用旧版本请参考 git tag 对应版本的可观测性文档。

---

> 仍有疑问？查阅：
> - [operations.md](operations.md) — 部署、备份、容量、升级
> - [performance.md](performance.md) — 性能调优
> - [troubleshooting.md](troubleshooting.md) — 故障排查
> - [architecture.md](architecture.md) — 系统架构
