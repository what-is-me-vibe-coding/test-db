# 服务层模块详解

## 1. 概述

服务层模块（`pkg/server/`）是 WiDB 的网络接入层，负责接收客户端请求并路由到对应的处理逻辑。模块同时提供 TCP 自定义协议和 HTTP REST API 两种接入方式，并内置 Prometheus 监控指标，支持连接管理与优雅关闭。

```
┌──────────────────────────────────────────────────┐
│                    Server                         │
│  ┌──────────────┐  ┌──────────────┐  ┌────────┐ │
│  │  TCP Handler │  │ HTTP Handler │  │Metrics │  │
│  └──────┬───────┘  └──────┬───────┘  └────────┘ │
│         └────────┬────────┘                      │
│                  ▼                                │
│  ┌───────────────────────────────────────────┐   │
│  │         handleQuery / handleWrite          │   │
│  │  Parser → Analyzer → Optimizer → Executor │   │
│  └───────────────────────────────────────────┘   │
│                  │                                │
│                  ▼                                │
│  ┌───────────────────────────────────────────┐   │
│  │         storage.Engine / catalog.Catalog   │   │
│  └───────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
```

## 2. 核心组件

### 2.1 Server

Server 是服务层的顶层结构，协调所有子组件：

```go
type Server struct {
    cfg       Config
    storage   *storage.Engine
    catalog   *catalog.Catalog
    parser    *query.Parser
    analyzer  *query.Analyzer
    optimizer *query.Optimizer
    executor  *query.Executor
    metrics   *Metrics
    registry  prometheus.Registerer

    tcpListener  net.Listener
    httpServer   *http.Server
    httpListener net.Listener

    connCount int64            // 当前活跃 TCP 连接数
    conns     map[net.Conn]struct{}  // 连接跟踪集合
    connMu    sync.Mutex
    wg        sync.WaitGroup
    done      chan struct{}
    stopOnce  sync.Once
}
```

**设计要点**：

- **双协议监听**：同时启动 TCP 和 HTTP 监听，共享同一套业务逻辑
- **连接跟踪**：通过 `conns` 集合跟踪所有活跃 TCP 连接，支持优雅关闭时主动断开
- **一次性关闭**：`stopOnce` 确保关闭逻辑仅执行一次，多次调用 `Stop()` 安全
- **WaitGroup 协调**：所有 goroutine 通过 `wg` 注册，关闭时等待全部退出

### 2.2 Config

Config 定义服务器的配置参数：

```go
type Config struct {
    TCPAddr         string
    HTTPAddr        string
    DataDir         string
    MaxMemTableSize int64
    MaxConnections  int                     // 最大并发 TCP 连接数，0 表示不限制
    EnableScheduler bool                    // 是否启用后台调度器
    SchedulerConfig storage.SchedulerConfig // 调度器配置
}
```

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `TCPAddr` | `0.0.0.0:9000` | TCP 监听地址 |
| `HTTPAddr` | `0.0.0.0:8080` | HTTP 监听地址 |
| `DataDir` | `./data` | 数据存储目录 |
| `MaxMemTableSize` | `67108864`（64MB） | MemTable 最大字节数，<=0 时自动设为 64MB |
| `MaxConnections` | `0`（不限制） | TCP 最大并发连接数 |
| `EnableScheduler` | `true` | 是否启用后台调度器 |
| `SchedulerConfig` | 默认值 | 调度器配置（Flush/Compact/WAL Clean 间隔与阈值） |

### 2.3 storageAdapter

storageAdapter 适配 `storage.Engine` 以实现 `query.StorageProvider` 接口，解耦查询引擎与存储引擎的直接依赖：

```go
type storageAdapter struct {
    engine *storage.Engine
}
```

| 方法 | 说明 |
|------|------|
| `ScanRange(start, end string) []storage.ScanEntry` | 范围扫描 |
| `ColumnMeta() []storage.ColumnMeta` | 获取列元数据 |
| `PrimaryIndex() *index.PrimaryIndex` | 获取主键索引 |
| `SparseIndex() *index.SparseIndex` | 获取稀疏索引 |

## 3. TCP 协议处理

### 3.1 连接接受

`acceptTCP` 在独立 goroutine 中循环接受新连接：

```
acceptTCP 循环
    │
    ├─ Accept() 新连接
    │   ├─ 检查连接数限制（MaxConnections）
    │   │   └─ 超限时关闭连接并继续
    │   ├─ 原子递增 connCount
    │   └─ 启动 handleTCPConn goroutine
    │
    └─ 错误处理
        ├─ done 通道关闭 → 退出
        ├─ 瞬态错误（资源耗尽）→ 继续重试
        └─ 其他错误 → 退出
```

**瞬态错误判断**：`isTransientAcceptErr` 识别 `resource temporarily unavailable` 和 `too many open files` 等可恢复错误，避免偶发资源耗尽导致服务退出。

### 3.2 连接处理

`handleTCPConn` 处理单个 TCP 连接的生命周期：

```
handleTCPConn(conn)
    │
    ├─ trackConn(conn)        注册连接跟踪
    ├─ defer untrackConn      退出时取消跟踪
    ├─ defer connCount--      退出时递减连接计数
    ├─ defer conn.Close()     退出时关闭连接
    │
    └─ 请求循环
        ├─ 检查 done 通道（优雅关闭）
        ├─ SetReadDeadline(30s)
        ├─ DecodePacket(reader)  解码请求包
        ├─ handlePacket(pkt)     路由到处理器
        ├─ SetWriteDeadline(10s)
        └─ conn.Write(resp)     写入响应
```

**超时设计**：

| 操作 | 超时 | 说明 |
|------|------|------|
| 读取 | 30s | 防止空闲连接占用资源 |
| 写入 | 10s | 防止慢客户端阻塞服务端 |

### 3.3 包路由

`handlePacket` 根据包类型路由到对应处理器：

| 包类型 | 值 | 处理方法 | 说明 |
|--------|-----|----------|------|
| `PacketQuery` | 1 | `handleQueryPacket` | SQL 查询请求 |
| `PacketWrite` | 2 | `handleWritePacket` | 批量写入请求 |
| `PacketPing` | 3 | `handlePing` | 心跳检测，返回 "pong" |
| 其他 | - | 返回错误 | 未知包类型 |

### 3.4 协议编解码

TCP 协议采用固定包头 + 变长负载的格式：

```go
type Packet struct {
    Magic   uint32   // 魔数 0x57494442 ("WIDB")
    Version uint16   // 协议版本，当前为 1
    Type    uint8    // 包类型
    Length  uint32   // Payload 长度
    Payload []byte   // JSON 格式的请求/响应体
}
```

**编码流程**（`Encode`）：

```
┌──────────────────────────────────────────┐
│  Magic(4B) | Version(2B) | Type(1B)     │  ← 固定 11 字节头部
│  Length(4B)                               │
├──────────────────────────────────────────┤
│  Payload(变长, 最大 16MB)                │
└──────────────────────────────────────────┘
```

**解码流程**（`DecodePacket`）：

1. 读取固定 11 字节头部
2. 校验 Magic（必须为 `0x57494442`）
3. 校验 Version（必须为 `1`）
4. 校验 Length（不超过 `MaxPacketSize` = 16MB）
5. 读取 Length 字节的 Payload

## 4. HTTP 处理

### 4.1 路由注册

`registerHTTPHandlers` 注册以下路由：

| 路径 | 方法 | 处理函数 | 说明 |
|------|------|----------|------|
| `/query` | POST | `httpQuery` | 执行 SQL 查询 |
| `/write` | POST | `httpWrite` | 批量写入数据 |
| `/health` | GET | `httpHealth` | 健康检查 |
| `/metrics` | GET | `promhttp.Handler` | Prometheus 指标 |

### 4.2 通用 POST 处理

`handlePostJSON` 封装了 POST JSON 请求的通用处理逻辑：

```
handlePostJSON(w, r, req, handler)
    │
    ├─ 方法检查：非 POST → 405 Method Not Allowed
    ├─ 请求体大小限制：MaxBytesReader(w, r.Body, 10MB)
    ├─ JSON 解码：Decode(req)
    │   └─ 失败 → 400 Bad Request
    ├─ 业务处理：handler()
    │   └─ 内部错误 → 500 Internal Server Error
    └─ 响应写入
        ├─ Code == 0 → 200 OK
        └─ Code != 0 → 400 Bad Request
```

**请求体限制**：`maxRequestBodySize = 10MB`，防止大请求体导致 OOM。

### 4.3 健康检查

`httpHealth` 返回服务健康状态和调度器统计信息：

```json
{
  "status": "ok",
  "timestamp": "2026-06-15T12:00:00.000Z",
  "scheduler": {
    "flush_count": 42,
    "compact_count": 10,
    "wal_clean_count": 5,
    "last_error": ""
  }
}
```

## 5. 业务处理

### 5.1 查询处理

`handleQuery` 执行完整的 SQL 查询流程：

```
handleQuery(req *QueryRequest)
    │
    ├─ parser.Parse(req.SQL)         SQL → AST
    │   └─ 失败 → QueriesTotal{type="parse_error"}
    ├─ analyzer.Analyze(stmt)        AST → 逻辑计划
    │   └─ 失败 → QueriesTotal{type="analyze_error"}
    ├─ optimizer.Optimize(plan)      逻辑计划 → 物理计划
    ├─ executor.Execute(optimized)   执行 → Chunk 结果流
    │   └─ 失败 → QueriesTotal{type="execute_error"}
    ├─ chunksToRows(chunks, colNames)  Chunk → JSON 行数据
    └─ QueriesTotal{type="success"}
```

**延迟记录**：`QueryDuration` 直方图记录整个查询流程的耗时（从 Parse 到返回结果）。

### 5.2 写入处理

`handleWrite` 执行批量写入流程：

```
handleWrite(req *WriteRequest)
    │
    ├─ catalog.GetTable(req.Table)   获取表定义
    │   └─ 失败 → WritesTotal{result="table_not_found"}
    ├─ convertWriteRow(tbl, row)     逐行类型转换
    │   ├─ interfaceToValue(raw, typ)  JSON 值 → common.Value
    │   └─ buildPrimaryKey(tbl, row)   提取主键，\x00 分隔
    │   └─ 失败 → WritesTotal{result="convert_error"}
    ├─ storage.WriteBatch(writeRows)  写入存储引擎
    │   └─ 失败 → WritesTotal{result="write_error"}
    └─ WritesTotal{result="success"}
```

**主键构建**：`buildPrimaryKey` 使用 `\x00` 作为分隔符拼接主键列值，避免主键值包含分隔符时产生碰撞。

### 5.3 数据转换

`converter.go` 实现 JSON 数据与内部类型的双向转换：

**写入路径**（JSON → Value）：

| JSON 类型 | 目标类型 | 转换函数 |
|-----------|----------|----------|
| `bool` | `TypeBool` | `interfaceToValue` |
| `float64` / `int64` / `int` | `TypeInt64` | `toInt64Value` |
| `float64` / `int64` / `int` | `TypeFloat64` | `toFloat64Value` |
| `string` | `TypeString` | `interfaceToValue` |
| RFC3339 字符串 | `TypeTimestamp` | `toTimestampValue` |
| `null` | 任意类型 | 返回 `NewNull()` |

**读取路径**（Value → JSON）：

| 内部类型 | JSON 类型 | 转换函数 |
|----------|-----------|----------|
| `TypeBool` | `bool` | `valueToInterface` |
| `TypeInt64` | `int64` | `valueToInterface` |
| `TypeFloat64` | `float64` | `valueToInterface` |
| `TypeString` | `string` | `valueToInterface` |
| `TypeTimestamp` | RFC3339Nano 字符串 | `valueToInterface` |
| 无效值 | `null` | `valueToInterface` |

**Chunk → JSON 行**：`chunksToRows` 将 Chunk 切片转换为 `[]map[string]any`，列名优先使用查询计划 Schema 中的名称，回退到 `col_N` 格式。

## 6. 监控指标

### 6.1 Metrics 结构

Metrics 包含所有 Prometheus 监控指标，命名空间为 `widb_`：

```go
type Metrics struct {
    QueriesTotal   *prometheus.CounterVec    // 查询总数
    QueryDuration  *prometheus.HistogramVec  // 查询耗时分布
    WritesTotal    *prometheus.CounterVec    // 写入总数
    WriteDuration  *prometheus.HistogramVec  // 写入耗时分布
    MemTableSize   prometheus.Gauge          // MemTable 大小
    SegmentCount   *prometheus.GaugeVec      // Segment 数量
    L0SegmentCount prometheus.Gauge          // L0 Segment 数量
    WALSizeBytes   prometheus.Gauge          // WAL 文件大小
    ActiveConns    prometheus.Gauge          // 活跃连接数
    FlushTotal     prometheus.Counter        // 刷盘总次数
    CompactTotal   prometheus.Counter        // Compaction 总次数
    WALCleanTotal  prometheus.Counter        // WAL 清理总次数
    CacheHits      *prometheus.CounterVec    // 缓存命中次数
    CacheMisses    *prometheus.CounterVec    // 缓存未命中次数
    CacheSizeBytes *prometheus.GaugeVec      // 缓存占用字节数
    CacheEntries   *prometheus.GaugeVec      // 缓存条目数
}
```

### 6.2 指标标签

**QueriesTotal**（标签 `type`）：

| 标签值 | 说明 |
|--------|------|
| `success` | 查询成功 |
| `parse_error` | SQL 解析错误 |
| `analyze_error` | SQL 分析错误 |
| `execute_error` | SQL 执行错误 |

**WritesTotal**（标签 `result`）：

| 标签值 | 说明 |
|--------|------|
| `success` | 写入成功 |
| `table_not_found` | 表不存在 |
| `convert_error` | 数据类型转换错误 |
| `write_error` | 存储引擎写入错误 |

**SegmentCount**（标签 `level`）：`l0`、`l1`

**CacheHits / CacheMisses / CacheSizeBytes / CacheEntries**（标签 `cache`）：`block`、`index`

### 6.3 标签初始化

`initLabels` 在创建 Metrics 时调用，预先初始化所有标签组合（`Add(0)` / `Set(0)` / `Observe(0)`），确保指标在未使用时也可见，避免 Prometheus 抓取时遗漏标签组合。

### 6.4 自定义注册器

通过 `WithMetricsRegistry` 选项可设置自定义 Prometheus 注册器，用于测试隔离：

```go
reg := prometheus.NewRegistry()
srv, err := NewServer(cfg, WithMetricsRegistry(reg))
```

## 7. 连接管理

### 7.1 连接跟踪

Server 维护一个连接跟踪集合 `conns`，用于优雅关闭时主动断开所有 TCP 连接：

| 方法 | 说明 |
|------|------|
| `trackConn(conn)` | 将连接加入跟踪集合 |
| `untrackConn(conn)` | 将连接从跟踪集合移除 |
| `closeAllConns()` | 关闭所有已跟踪的连接 |

### 7.2 连接数限制

`MaxConnections` 配置 TCP 最大并发连接数：

- `0`（默认）：不限制
- `> 0`：当 `connCount >= MaxConnections` 时，新连接被拒绝并关闭

连接计数使用 `atomic.AddInt64` 原子操作，避免锁竞争。

## 8. 优雅关闭

`Stop` 方法执行优雅关闭流程：

```
Stop()
    │
    ├─ stopOnce.Do(func() { ... })  确保仅执行一次
    │   ├─ close(done)              通知所有 goroutine 退出
    │   ├─ tcpListener.Close()      停止接受新 TCP 连接
    │   ├─ httpListener.Close()     停止接受新 HTTP 连接
    │   ├─ httpServer.Close()       关闭 HTTP 服务器
    │   ├─ closeAllConns()          主动关闭所有已建立的 TCP 连接
    │   ├─ wg.Wait()                等待所有 goroutine 退出
    │   └─ storage.Close()          关闭存储引擎
    │
    └─ 返回 stopErr（如有）
```

**关键设计**：

- **done 通道**：关闭后所有 goroutine 中的 `select { case <-s.done: }` 立即返回
- **主动关闭连接**：`closeAllConns` 使 `handleTCPConn` 中的阻塞读取立即返回，避免空闲连接阻塞关闭
- **HTTP 监听失败回滚**：`Start()` 中若 HTTP 监听失败，会关闭已启动的 TCP goroutine 并重置 done 通道

## 9. API 参考

### 9.1 Server

| 方法 | 签名 | 说明 |
|------|------|------|
| `NewServer` | `(cfg Config, opts ...Option) (*Server, error)` | 创建服务器实例，初始化所有组件 |
| `Start` | `() error` | 启动 TCP 和 HTTP 监听 |
| `Stop` | `() error` | 优雅关闭服务器 |
| `TCPAddr` | `() string` | 返回 TCP 监听地址 |
| `HTTPAddr` | `() string` | 返回 HTTP 监听地址 |
| `Catalog` | `() *catalog.Catalog` | 返回 Catalog 实例 |

### 9.2 Option

| 函数 | 签名 | 说明 |
|------|------|------|
| `WithMetricsRegistry` | `(reg prometheus.Registerer) Option` | 设置自定义 Prometheus 注册器 |

### 9.3 Packet

| 方法 | 签名 | 说明 |
|------|------|------|
| `Encode` | `() []byte` | 编码为二进制字节流（大端序） |
| `DecodePacket` | `(r io.Reader) (*Packet, error)` | 从 Reader 解码一个 Packet |
| `NewPacket` | `(typ uint8, payload []byte) *Packet` | 创建新协议包 |

### 9.4 请求/响应结构

```go
type QueryRequest struct {
    SQL string `json:"sql"`
}

type WriteRequest struct {
    Table string           `json:"table"`
    Rows  []map[string]any `json:"rows"`
}

type Response struct {
    Code    int    `json:"code"`
    Message string `json:"message,omitempty"`
    Data    any    `json:"data,omitempty"`
    Rows    int    `json:"rows,omitempty"`
}
```

### 9.5 协议常量

| 常量 | 值 | 说明 |
|------|-----|------|
| `Magic` | `0x57494442` | 协议魔数 "WIDB" |
| `ProtocolVersion` | `1` | 协议版本 |
| `HeaderSize` | `11` | 固定包头长度（字节） |
| `MaxPacketSize` | `16MB` | 单个数据包最大负载大小 |
| `PacketQuery` | `1` | 查询请求包类型 |
| `PacketWrite` | `2` | 写入请求包类型 |
| `PacketPing` | `3` | 心跳请求包类型 |
| `PacketResponse` | `10` | 响应包类型 |

## 10. 代码示例

### 10.1 创建并启动服务器

```go
cfg := server.Config{
    TCPAddr:         "0.0.0.0:9000",
    HTTPAddr:        "0.0.0.0:8080",
    DataDir:         "./data",
    MaxMemTableSize: 64 * 1024 * 1024,
    MaxConnections:  1000,
    EnableScheduler: true,
    SchedulerConfig: storage.SchedulerConfig{
        FlushInterval:     5 * time.Second,
        CompactInterval:   10 * time.Second,
        WALCleanInterval:  30 * time.Second,
        WALCleanThreshold: 64 << 20,
    },
}

srv, err := server.NewServer(cfg)
if err != nil {
    log.Fatal(err)
}

if err := srv.Start(); err != nil {
    log.Fatal(err)
}

// 优雅关闭
defer srv.Stop()
```

### 10.2 使用自定义 Prometheus 注册器

```go
reg := prometheus.NewRegistry()
srv, err := server.NewServer(cfg, server.WithMetricsRegistry(reg))
```

### 10.3 编码 TCP 协议包

```go
// 构造查询请求
req := server.QueryRequest{SQL: "SELECT * FROM sensor WHERE id = 1"}
payload, _ := json.Marshal(req)
pkt := server.NewPacket(server.PacketQuery, payload)

// 编码为字节流
data := pkt.Encode()

// 写入 TCP 连接
conn.Write(data)
```

### 10.4 解码 TCP 响应包

```go
// 从连接读取
reader := bufio.NewReader(conn)
pkt, err := server.DecodePacket(reader)
if err != nil {
    log.Fatal(err)
}

// 解析响应
var resp server.Response
if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
    log.Fatal(err)
}

if resp.Code == 0 {
    fmt.Printf("查询成功，返回 %d 行\n", resp.Rows)
} else {
    fmt.Printf("查询失败: %s\n", resp.Message)
}
```

## 11. 文档索引

| 文档 | 说明 |
|------|------|
| [getting-started.md](getting-started.md) | 快速入门指南 |
| [architecture.md](architecture.md) | 系统架构 |
| [storage.md](storage.md) | 存储引擎详解 |
| [query.md](query.md) | 查询引擎详解 |
| [index.md](index.md) | 索引模块详解 |
| [catalog.md](catalog.md) | 元数据管理详解 |
| [common.md](common.md) | 公共模块详解 |
| [api.md](api.md) | API 参考 |
| [performance.md](performance.md) | 性能调优指南 |
