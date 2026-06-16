# WiDB 演进路线图

本文档登记六项演进计划的可行性分析、实现思路与外部依赖。所有计划均基于现有代码库评估，模块边界清晰、接口设计合理，改造空间充足。

## 0. 代码基础概览

| 维度 | 现状 |
|---|---|
| 模块结构 | `cmd/{server,cli}` + `pkg/{common,catalog,storage,index,query,server}` |
| 类型系统 | `common.DataType` 仅 5 种：`BOOL/INT64/FLOAT64/STRING/TIMESTAMP` + `NULL` |
| 存储引擎 | LSM-Tree + WAL + MemTable(跳表) + Segment(列式+ZSTD) + Compaction |
| SQL 解析 | `xwb1989/sqlparser`（MySQL 方言），支持 SELECT/INSERT/CREATE TABLE |
| 协议层 | 自定义 TCP 二进制协议（11字节头+JSON payload）+ HTTP REST |
| CLI | `bufio.Scanner` 行读取，无美化、无历史、无补全 |
| 配置 | 全部走 `flag` 命令行参数，无配置文件 |
| 依赖约束 | AGENTS.md 规定：优先标准库；第三方需 Apache/MIT/BSD、无 CGO、活跃维护 |

## 1. 计划 1：一个命令同时启动 server 和 client

**可行性**：✅ 完全可行，低风险

**实现思路**：同进程 goroutine 模式
- 新增 `cmd/widb/main.go`（或复用 `cmd/server` 加 `-interactive` 子命令）
- 主 goroutine 启动 CLI REPL，另一 goroutine 启动 server
- 共享 in-memory engine，零网络开销
- 退出时协调信号，CLI 退出触发 `server.Stop()` 优雅关闭

**优势**：单二进制、零 IPC 开销、调试方便。

**所需外部库**：无

## 2. 计划 2：YAML 配置 + 自动生成带注释模板

**可行性**：✅ 完全可行，低风险

**现状**：`cmd/server/main.go` 用 7 个 `flag` 参数配置，无配置文件支持。

**实现思路**：
- 新增 `pkg/config/config.go`：定义 `Config` 结构体，带 yaml tag
- 命令行仅保留 `-config <path>` 一个参数
- 启动逻辑：
  1. 若指定 `-config`，加载该文件
  2. 若未指定，检查当前目录 `widb.yaml`/`config.yaml`
  3. 若都不存在，生成带注释的默认 `widb.yaml` 并提示用户编辑后重启（或直接用默认值启动）
- 模板生成：用 `embed` 嵌入带注释的 yaml 模板字符串，或用 `yaml.Marshal` + 手工拼接注释

**配置项设计**（覆盖现有 flag + 计划 4/5 新增）：

```yaml
server:
  tcp_addr: "0.0.0.0:9000"
  http_addr: "0.0.0.0:8080"
  pg_addr: "0.0.0.0:5432"      # 计划4新增
storage:
  data_dir: "./data"
  max_memtable_size: 67108864
scheduler:
  enabled: true
  flush_interval: 5s
  compact_interval: 10s
  wal_clean_interval: 30s
  wal_clean_threshold: 67108864
```

**所需外部库**：`gopkg.in/yaml.v3`（MIT，支持注释保留与写出；轻量、标准）

## 3. 计划 3：CLI 美化（贴近 ClickHouse 风格）

**可行性**：✅ 完全可行，中等工作量

**现状**：`cmd/cli/main.go` 用 `bufio.Scanner`，输出为 `json.MarshalIndent`，无颜色、无表格、无历史。

**ClickHouse 风格关键要素**：
1. Unicode box-drawing 紧凑表格（`┌─┬─┐`，PrettyCompact 风格）
2. 数字右对齐、字符串左对齐、NULL 显式标记
3. 多行 SQL 输入 + 历史记录 + `\` 命令
4. 多种输出格式（Pretty/Vertical/JSON/CSV）
5. 克制的颜色（表头加粗、错误红色）

**推荐库组合**（方案 A，最贴近且不过度工程化）：

| 用途 | 库 | 地址 | 许可证 | 理由 |
|---|---|---|---|---|
| 表格渲染 | `jedib0t/go-pretty/v6/table` | github.com/jedib0t/go-pretty | MIT | 内置 `StyleRounded`/`StyleLight` Unicode 样式，数字自动右对齐，最易复刻 PrettyCompact |
| 颜色 | `fatih/color` | github.com/fatih/color | MIT | 生态事实标准（被 28k 模块导入），`NO_COLOR` 兼容 |
| REPL 输入 | `peterh/liner` | github.com/peterh/liner | X11(BSD-like) | 纯 Go 无 cgo，Ctrl-R 历史、Tab 补全，golangci-lint/k9s 验证 |
| 进度反馈 | `schollz/progressbar/v3` | github.com/schollz/progressbar | MIT | 行数/耗时显示 |

**备选**：若想用 `chzyer/readline` API 习惯，选其活跃 fork `ergochat/readline`（MIT）。`chzyer/readline` 本身已基本停止主动维护。

**避免**：`bubbletea` 全屏 TUI——破坏管道友好性，偏离 clickhouse-client/psql 哲学，属过度工程化。

**实现要点**：
- 重写 `runInteractive`：用 `liner` 替换 `bufio.Scanner`
- 新增 `formatTable`：`chunk` → `go-pretty/table` 渲染
- 支持 `\format` 命令切换 Pretty/Vertical/JSON/CSV
- 历史持久化到 `~/.widb_history`

## 4. 计划 4：兼容 MySQL/PostgreSQL JDBC 协议

**可行性**：✅ 可行，推荐 PostgreSQL 协议，中高工作量

**现状**：仅有自定义 TCP 协议，JDBC 无法直连。

**推荐：PostgreSQL wire 协议 + `jackc/pgproto3`**

**为什么不选 MySQL 协议**：
- MySQL 无原生 BOOL（用 TINYINT(1) 凑）、TIMESTAMP 范围受限（2038年问题）
- OLAP 圈事实标准是 PG wire（ClickHouse/QuestDB/CockroachDB/DuckDB/Redshift 全用 PG wire）
- 现有 5 种类型与 PG OID 几乎一一对应：
  - INT64 → int8 (OID 20)
  - FLOAT64 → float8 (OID 701)
  - STRING → text (OID 25)
  - BOOL → bool (OID 16) —— PG 有原生布尔，MySQL 没有
  - TIMESTAMP → timestamp (OID 1114) / timestamptz (OID 1184) —— PG 范围 4713 BC–294276 AD
- BI 工具（Superset/Metabase/Grafana）对 PG wire 兼容性测试最充分

**所需外部库**：

| 库 | 地址 | 许可证 | 用途 |
|---|---|---|---|
| `pgproto3`（首选） | github.com/jackc/pgproto3 | MIT | PG v3 协议消息编解码，需自写 ~300 行 server loop |
| `psql-wire`（备选） | github.com/jeroenrinzema/psql-wire | MPL-2.0 | 开箱即用 `ListenAndServe`，但社区小、MPL 许可 |

**推荐**：`pgproto3`——MIT、工业级、依赖小、Go PG 生态事实标准（pgx 驱动底层）。

**实现路径**：
- 新增 `pkg/server/pgwire/` 子包
- 实现：TCP accept → StartupMessage → 认证(trust/md5) → Simple Query 循环 → RowDescription/DataRow/CommandComplete/ReadyForQuery
- 复用现有 `parser/analyzer/optimizer/executor` 管线
- 类型映射：`common.DataType` → PG OID + 格式码
- 在 `server.Config` 增加 `PGAddr`，`Start()` 中并行监听

**工作量**：~500-800 行代码，主要是协议握手与消息封装。

**可选增强**：若同时要 MySQL 协议，加 `go-mysql-org/go-mysql`（MIT，轻量 server 包），但建议先做 PG。

## 5. 计划 5：内存表特性

**可行性**：✅ 可行，中等工作量，需架构决策

**现状**：
- `storage.Engine` 硬编码 LSM-Tree 路径，所有表都走 WAL+MemTable+Segment
- `catalog.Table` 无 engine 类型字段
- 无 `ENGINE=` 语法支持（parser 用 sqlparser，需扩展）

**实现思路**（方案 A：表级 Engine 标记 + 内存引擎实现）：

1. `catalog.Table` 增加 `Engine string` 字段（`"lsm"` 默认 / `"memory"`）
2. `catalog.CreateTable` 接受 engine 参数
3. Parser 扩展：解析 `CREATE TABLE ... ENGINE=memory`（sqlparser 支持 table options，需在 `convertTableSpec` 提取）
4. 新增 `pkg/storage/memory/` 子包：实现 `MemoryEngine`，用 `sync.Map` 或 `btree` 存全表数据，实现与 LSM Engine 相同的接口
5. `server.Server` 持有 `map[string]Engine`（按表名路由），或 `Engine` 接口化后由 catalog 决定用哪个实现

**接口抽象**（关键）：

```go
type Engine interface {
    Write(key string, values map[string]common.Value) error
    WriteBatch(rows []WriteRow) error
    ScanRange(start, end string) []ScanEntry
    ScanRangeWithPruning(start, end string, predicates []ColumnPredicate) []ScanEntry
    ColumnMeta() []ColumnMeta
    PrimaryIndex() *index.PrimaryIndex
    SparseIndex() *index.SparseIndex
    Close() error
}
```

现有 `storage.Engine` 重命名为 `LSMEngine`，新增 `MemoryEngine` 实现同接口。

**所需外部库**：无（用标准库 `sync` + 可选 `github.com/google/btree`）

**适用场景**：临时表、维度表、元数据表、高频小表查询。

## 6. 计划 6：更多类型支持

**可行性**：✅ 可行，但工作量最大，触及全栈

**现状**：仅 5 种类型，`common.DataType` 是 `int` 枚举，`Value` 结构体用联合字段，`ColumnVector` 按类型分配数组，编码/压缩/索引/查询执行器全栈硬编码类型分支。

**建议新增类型**（按优先级）：

| 类型 | 用途 | 实现复杂度 |
|---|---|---|
| `INT8/INT16/INT32` | 节省内存，常见 | 中（需改 ColumnVector + 编码 + 执行器类型分支） |
| `UINT64` | 自增ID、哈希值 | 低 |
| `DATE` | 日期（无时间） | 低（底层复用 int64） |
| `DECIMAL` | 精确小数 | 高（需 arbitrary precision 或 fixed-point） |
| `BINARY/BLOB` | 二进制数据 | 中 |
| `JSON` | 半结构化 | 高 |
| `ARRAY` | 数组 | 高（需改列式布局） |

**实现路径**（以 INT8/16/32 为例）：
1. `common/types.go`：新增枚举 + `Value` 字段（可复用 `Int64` 字段，按类型截断）
2. `storage/column.go`：`ColumnVector.allocateData` 增加 int8/int16/int32 数组分支
3. `storage/encoding.go`：各编码器增加类型分支
4. `query/parser_convert.go`：`convertColumnType` 增加 SMALLINT/MEDIUMINT/INT 映射
5. `query/executor_expr.go`：比较/算术运算增加类型分支
6. `server/converter.go` + `pgwire`：JSON/PG OID 映射

**所需外部库**：
- `DECIMAL`：`github.com/shopspring/decimal`（MIT，事实标准）
- `JSON`：标准库 `encoding/json` 即可
- 其他类型：无需外部库

**建议**：先做 INT8/16/32 + DATE + UINT64（低风险高收益），DECIMAL/JSON/ARRAY 视需求再定。

## 7. 总体可行性结论

| 计划 | 可行性 | 风险 | 工作量 | 外部依赖 |
|---|---|---|---|---|
| 1. 一键启动 server+client | ✅ | 低 | 小 | 无 |
| 2. YAML 配置 + 自动生成 | ✅ | 低 | 小 | `gopkg.in/yaml.v3` |
| 3. CLI 美化（ClickHouse 风格） | ✅ | 低 | 中 | `go-pretty` + `fatih/color` + `peterh/liner` + `schollz/progressbar` |
| 4. JDBC 协议兼容 | ✅ | 中 | 中高 | `jackc/pgproto3`（推荐 PG 协议） |
| 5. 内存表特性 | ✅ | 中 | 中 | 无（可选 `google/btree`） |
| 6. 更多类型支持 | ✅ | 高 | 大 | 视类型而定（`shopspring/decimal` 等） |

**所有六项计划均可行**，代码库模块边界清晰、接口设计合理，改造空间充足。

## 8. 推荐实施顺序

1. **计划 2（YAML 配置）** → 基础设施，后续所有配置依赖它
2. **计划 1（一键启动）** → 快速见效，改善开发体验
3. **计划 3（CLI 美化）** → 用户可感知，独立于核心引擎
4. **计划 5（内存表）** → 引擎接口抽象，为后续扩展铺路
5. **计划 6（更多类型）** → 全栈改造，建议分批增量
6. **计划 4（JDBC 协议）** → 最后做，可复用前面所有成果

## 9. 关键库地址速查

**配置**
- `gopkg.in/yaml.v3` — https://github.com/go-yaml/yaml （MIT）

**CLI 美化**
- `jedib0t/go-pretty` — https://github.com/jedib0t/go-pretty （MIT，~6k★，v6.7.10）
- `olekukonko/tablewriter` — https://github.com/olekukonko/tablewriter （MIT，~4.8k★，v1.x 重写，sql.Null 原生支持，备选）
- `fatih/color` — https://github.com/fatih/color （MIT，被导入 28k 模块）
- `peterh/liner` — https://github.com/peterh/liner （X11，golangci-lint/k9s 采用）
- `ergochat/readline` — https://github.com/ergochat/readline （MIT，chzyer/readline 活跃 fork，备选）
- `schollz/progressbar` — https://github.com/schollz/progressbar （MIT，v3.19.0）

**JDBC 协议**
- `jackc/pgproto3` — https://github.com/jackc/pgproto3 （MIT，pgx 子包，PG 协议事实标准，首选）
- `jeroenrinzema/psql-wire` — https://github.com/jeroenrinzema/psql-wire （MPL-2.0，开箱即用 server，备选）
- `go-mysql-org/go-mysql` — https://github.com/go-mysql-org/go-mysql （MIT，~4.9k★，若需 MySQL 协议）

**类型扩展**
- `shopspring/decimal` — https://github.com/shopspring/decimal （MIT，DECIMAL 事实标准）
- `google/btree` — https://github.com/google/btree （MIT，内存表索引备选）
