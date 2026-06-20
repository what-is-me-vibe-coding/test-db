# 开发与贡献指南

本指南面向希望参与 WiDB 开发、调试或二次开发的工程师，覆盖开发环境搭建、项目结构、测试与 CI 流水线、代码规范、Git 工作流以及常见扩展场景。面向最终用户的使用文档请参阅 [getting-started.md](getting-started.md)，模块实现细节请参阅 [architecture.md](architecture.md) 及各模块文档。

## 1. 开发环境

| 要求 | 说明 |
|------|------|
| Go 版本 | 1.25.1 及以上（CI 锁定 `1.25.1`） |
| 操作系统 | Linux / macOS |
| Git | 2.20+ |

验证环境：

```bash
go version
# 期望输出 go version go1.25.1 ... 或更高
```

克隆与首次构建：

```bash
git clone https://github.com/what-is-me-vibe-coding/test-db.git
cd test-db
go mod download
go build ./...
```

### 推荐安装的本地校验工具

CI 会使用下列工具，建议本地也安装以便在提交前自检：

```bash
# golangci-lint（CI 使用 v2.12.2，配置见 .golangci.yml，v2 格式）
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

# 复杂度检查
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
go install github.com/uudashr/gocognit/cmd/gocognit@latest

# 静态分析
go install honnef.co/go/tools/cmd/staticcheck@latest
```

`gofmt` 与 `go vet` 随 Go 工具链自带，无需单独安装。

## 2. 构建与运行

项目提供三个入口二进制：

```bash
go build -o widb-server ./cmd/server   # 独立服务器（TCP + HTTP）
go build -o widb-cli   ./cmd/cli       # 远程命令行客户端
go build -o widb       ./cmd/widb      # 一键启动：同进程 server + CLI
```

### 开发模式快速验证

最便捷的端到端验证方式是使用一键启动二进制，在同进程内启动服务并进入 REPL：

```bash
./widb
# 进入 REPL 后：
widb> CREATE TABLE t (id INT64, v FLOAT64, PRIMARY KEY(id));
widb> INSERT INTO t VALUES (1, 1.5);
widb> SELECT * FROM t;
widb> \q
```

外部客户端仍可通过 TCP/HTTP 接入，便于调试协议层。完整启动参数见 [getting-started.md](getting-started.md)。

## 3. 项目结构与模块边界

```
test-db/
├── cmd/                # 入口二进制
│   ├── cli/            # 远程客户端
│   ├── server/         # 服务器入口
│   └── widb/           # 一键启动入口
├── pkg/                # 核心库
│   ├── common/         # 基础类型与工具（最底层，不依赖其他 pkg）
│   ├── catalog/        # 元数据管理（依赖 common）
│   ├── storage/        # 存储引擎：WAL/MemTable/Segment/Compaction/编码/压缩（依赖 common、catalog）
│   ├── index/          # 索引：主键/布隆/稀疏（依赖 common、catalog、storage）
│   ├── query/          # 查询引擎：解析/分析/优化/执行（依赖 common、catalog、index、storage）
│   ├── server/         # 服务层：TCP/HTTP/pgwire/监控（依赖所有 pkg）
│   ├── render/         # 结果格式化（pretty/vertical/json/csv）
│   ├── config/         # YAML 配置
│   ├── cli/            # REPL 原语：多行 SQL、格式状态（依赖 common、render）
│   └── cmdutil/        # 入口二进制共享的 flag 与配置加载（依赖 config、server、storage）
├── tests/integration/  # 端到端集成测试
├── doc/                # 用户与开发文档
├── .agent_plan/        # 设计文档与路线图
└── .github/workflows/  # CI 流水线定义
```

### 模块依赖方向

依赖严格自顶向下，**禁止循环依赖**。出现循环依赖时，应提取公共接口到 `pkg/common`：

```
common ← catalog ← storage ← index ← query ← server
```

| 规则 | 说明 |
|------|------|
| `pkg/common` | 基础类型与工具，保持最底层，不依赖其他 pkg |
| `pkg/catalog` | 可依赖 `common` |
| `pkg/storage` | 可依赖 `common`、`catalog` |
| `pkg/index` | 可依赖 `common`、`catalog`、`storage` |
| `pkg/query` | 可依赖 `common`、`catalog`、`index`、`storage` |
| `pkg/server` | 可依赖所有 pkg，是接入层聚合点 |
| `pkg/cli` | REPL 原语，被 `cmd/cli`、`cmd/widb` 复用 |
| `pkg/cmdutil` | 入口二进制共享的 flag 与配置加载，被 `cmd/server`、`cmd/widb` 复用 |

## 4. 测试

### 4.1 单元测试

单元测试与实现文件同目录，命名 `*_test.go`。核心逻辑覆盖率要求 ≥ 80%，全仓 CI 阈值为 ≥ 90%。

```bash
# 运行全部单元测试
go test ./...

# 启用竞态检测（CI 标准做法，-p=1 串行避免 TMPDIR 竞争）
go test -race -p=1 ./...

# 运行单个包/测试
go test -race ./pkg/storage/...
go test -race -run TestMemTable ./pkg/storage/
```

### 4.2 覆盖率

```bash
go test -race -coverprofile=coverage.out -p=1 ./...
go tool cover -func=coverage.out        # 查看函数级覆盖率
go tool cover -html=coverage.out        # 浏览器查看 HTML 报告

# CI 阈值校验（total 必须 >= 90.0）
COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
echo "Total coverage: ${COVERAGE}%"
```

### 4.3 集成测试

集成测试位于 `tests/integration/`，覆盖端到端写入 → 查询 → Compaction → 重启恢复、多客户端并发 SQL 等场景。测试使用临时目录并在结束后清理：

```bash
go test -race ./tests/integration/...
```

新增集成测试时，请使用 `t.TempDir()` 作为数据目录，避免污染工作区。

### 4.4 基准测试

基准测试命名 `*_benchmark_test.go` 或 `*_test.go` 内的 `BenchmarkXxx`。性能变更后应对比基准数据，性能退化 > 5% 需在 PR 中说明：

```bash
go test -bench=. -benchmem ./pkg/storage/...
go test -bench=BenchmarkFilter -count=3 ./pkg/query/
```

## 5. CI 流水线

CI 定义于 [.github/workflows/ci.yml](../.github/workflows/ci.yml)，在 `push` 到 `main` 与 `pull_request` 到 `main` 时触发，包含 5 个并行任务：

| 任务 | 工具 | 阈值 / 要求 |
|------|------|-------------|
| Test & Coverage | `go test -race` | 全部通过；总覆盖率 ≥ 90% |
| Lint & Format | `gofmt` / `go vet` / `golangci-lint v2` | 无格式问题、无 vet 警告、lint 通过 |
| Complexity Analysis | `gocyclo` / `gocognit` | 圈复杂度 ≤ 15，认知复杂度 ≤ 20 |
| Code Structure Analysis | 自定义脚本 | 单包 `.go` 文件数 ≤ 20；源文件 ≤ 500 行，测试文件 ≤ 800 行；函数 ≤ 80 行；目录深度 ≤ 6 |
| Code Quality | `staticcheck` | 无告警；并检查 `interface{}`、`panic()` 等反模式 |

> 注意：结构分析的行数/文件数/函数长度限制只针对 `*.go` 文件，文档（`*.md`）不受约束。

### 5.1 本地复现 CI

提交前建议本地完整跑一遍 CI 脚本，尽量保证 PR 一次通过：

```bash
# 1. 格式化与静态检查
gofmt -l .                       # 期望无输出
go vet ./...
golangci-lint run --timeout=5m

# 2. 测试与覆盖率
go test -race -p=1 ./...
go test -race -coverprofile=coverage.out -p=1 ./...
go tool cover -func=coverage.out | grep total

# 3. 复杂度
gocyclo -over 15 .
gocognit -over 20 .

# 4. 静态分析
staticcheck ./...
```

## 6. 代码规范

### 6.1 风格

- 必须通过 `gofmt` 与 `go vet`。
- 命名：接口以 `er` 结尾（如 `StorageEngine`），私有实现不加前缀（如 `storageEngine`），缩写全大写（`WAL`、`HTTP`、`PG`）。
- 注释：导出符号必须写文档注释，说明功能、参数、返回值与错误情况；设计文档与代码注释使用中文。
- 错误处理：使用 `fmt.Errorf("context: %w", err)` 包装；禁止忽略错误，`_ = fn()` 需注释理由。

### 6.2 并发安全

- 共享状态必须显式加锁或使用原子操作，禁止依赖 map/slice 的隐式线程安全。
- 锁粒度优先细粒度（per-segment、per-table），避免全局大锁。
- 多处加锁时定义全局顺序，防止死锁。
- 所有测试必须能通过 `go test -race`。

### 6.3 性能

存储引擎与查询引擎的热点路径需关注内存分配，减少 GC 压力：复用 buffer、避免在批量循环中装箱 `Value`、优先向量化快速路径。

## 7. Git 工作流与 PR 流程

### 7.1 分支策略

**禁止直接 push 到 `main`**。所有变更通过功能分支 → PR → Review → 合并的流程进入 `main`。

| 分支类型 | 命名格式 | 示例 |
|---------|---------|------|
| 功能 | `feat/模块-简述` | `feat/storage-wal` |
| 修复 | `fix/模块-简述` | `fix/index-race` |
| 重构 | `refactor/模块-简述` | `refactor/query-vectorize` |
| 文档 | `docs/简述` | `docs/development-guide` |
| 性能 | `perf/模块-简述` | `perf/filter-output-vectorize` |
| 测试 | `test/简述` | `test/common-sql-integration` |

从最新 `main` 切出分支：

```bash
git checkout main && git pull --ff-only
git checkout -b docs/简述
```

### 7.2 提交信息规范

```
[类型] 模块: 简要描述（50 字以内）

详细说明（可选）：变更背景、实现思路、注意事项
```

类型：`feat` / `fix` / `refactor` / `test` / `docs` / `chore` / `perf`。遵循「小步提交」原则，每个步骤独立实现、独立测试、独立提交。

### 7.3 PR 自检清单

提交 PR 前必须完成：

- [ ] `go test -race ./...` 全部通过
- [ ] `go fmt ./...` 与 `go vet ./...` 无警告
- [ ] `golangci-lint run` 通过
- [ ] 新增代码覆盖率 ≥ 80%（核心模块）
- [ ] 复杂度与结构限制满足 CI 阈值
- [ ] PR 标题格式：`[类型] 模块: 简述`
- [ ] PR 描述包含：变更目的与范围、关联设计文档、测试覆盖情况、性能影响（如有）

### 7.4 合并方式

使用 **Squash and merge**，合并后删除源分支。

## 8. 扩展指南

### 8.1 新增一个 SQL 数据类型

以新增一个整数族类型为例（复用 `int64` 字段存储，改动最小）。**关键：`DataType` 枚举值必须追加在末尾，以保证与历史 WAL/Segment/Catalog 持久化数据兼容。**

按依赖顺序修改以下文件：

1. **`pkg/common/types.go`**
   - 在 `DataType` 常量块末尾追加新类型（如 `TypeInt32`）。
   - 更新 `IsIntFamily()`（整数族）、`String()`、`Size()` 的 `switch`。
   - 新增构造函数 `NewInt32(v int64) Value`。
   - 更新 `Value.Equal` / `Value.Less` / `Value.String` 的类型分支（整数族跨类型比较已由 `IsIntFamily()` 统一处理，通常无需额外分支）。

2. **`pkg/storage/column.go`**
   - `allocateData` 增加类型分支。整数族复用 `int64s` 数组，无需新数组。

3. **`pkg/storage/encoding.go`**
   - 各编码器（Plain/RLE/Dict/Bitmap）增加类型分支。整数族通常复用 int64 编码路径。

4. **`pkg/query/parser_convert.go`**
   - 在 `convertColumnType` 或 `intLikeColumnType` 中增加 SQL 类型名到 `common.DataType` 的映射（如 `SMALLINT` → `TypeInt16`）。

5. **`pkg/query/executor_expr.go`**
   - 比较/算术运算的类型分支（整数族通常已被统一处理）。

6. **`pkg/server/converter.go` 与 `pkg/server/pgwire/`**
   - JSON 序列化与 PostgreSQL OID 映射增加新类型。

7. **测试**
   - 为新类型补充 `common` / `storage` / `query` / `server` 的单元测试与集成测试，确保覆盖率不退化。

### 8.2 新增一个聚合函数

以新增 `STDDEV` 为例：

1. **`pkg/query/plan.go`**
   - 在 `AggregateFunc` 枚举中追加 `AggStddev`，并更新 `String()` 返回 `"STDDEV"`。

2. **`pkg/query/analyzer_aggregate.go`**
   - `parseAggFunc` 增加 `case "stddev": return AggStddev`。
   - `isAggregateFunc` 增加 `"stddev"` 名称识别。

3. **`pkg/query/executor_aggregate.go`**
   - `accumulator.update` 增加 `case AggStddev` 分支，调用新增的 `updateStddev` 方法（需扩展 `accumulator` 结构体保存平方和等中间状态）。
   - `accumulator.result()` 增加 `case AggStddev` 分支，计算并返回标准差。

4. **`pkg/query/analyzer.go`**
   - 在聚合结果列类型推导中，为 `AggStddev` 设置结果类型（通常为 `FLOAT64`）。

5. **测试**
   - 补充 `plan_test.go`、`executor_aggregate_test.go` 与端到端 SQL 测试。

### 8.3 新增一个标量函数

标量函数在 `pkg/query/parser_convert.go` 的 `convertFuncExpr` 中被解析为 `FuncExpr`，参数由 `analyzer_resolve.go` 解析。当前行级求值（`executor_expr.go` 的 `evalFuncExpr`）默认返回「不支持」错误，新增标量函数需在此处增加求值分支，并在分析器中校验参数个数与类型。

## 9. 文档维护

- 代码变更导致接口变动时，同步更新对应 `.agent_plan/*.md` 中的接口定义与 `doc/` 下的模块文档。
- 新增模块需新建 `.agent_plan/模块名.md`，并在 `plan.md` 中追加索引。
- 设计文档使用中文，与代码注释语言保持一致。
- 文档目录与索引见 [doc/getting-started.md §15](getting-started.md) 与 [README.md](../README.md)；其中：
  - 模块详解以「模块名.md」命名（如 `index.md` 为索引模块文档，**不是文档索引**）；
  - 用户/教程类文档以用途命名（`tutorial.md` / `sql-reference.md` / `cookbook.md` / `troubleshooting.md`）。

## 10. 常见开发问题

### Q: 本地 `golangci-lint` 报错与 CI 不一致？

A: 确认本地安装的是 v2 版本（配置文件 `.golangci.yml` 为 v2 格式），并尽量与 CI 的 `v2.12.2` 对齐。v1 与 v2 配置不兼容。

### Q: 覆盖率本地达标但 CI 不达标？

A: CI 使用 `go test -race -coverprofile=coverage.out -p=1 ./...` 计算总覆盖率，阈值 90%。请确认本地使用相同命令与 `-race`、`-p=1` 参数，并检查是否有测试在 CI 环境因临时目录或时区差异而行为不同。

### Q: 函数/文件超过 CI 行数限制怎么办？

A: 结构分析要求源文件 ≤ 500 行、测试文件 ≤ 800 行、函数 ≤ 80 行。请拆分过长的函数或文件，将相关逻辑提取为独立的辅助函数或新文件（注意单包 `.go` 文件数 ≤ 20）。

### Q: 如何调试 pgwire 协议层？

A: 启动服务器后，使用 `psql` 或任意 PostgreSQL JDBC 客户端连接 PG 端口，结合 `pkg/server/pgwire/` 下的连接处理代码与 `conn_test.go` 进行调试。pgwire 复用与 TCP/HTTP 相同的 parser/analyzer/optimizer/executor 管线，仅协议封装不同。

### Q: 引入新第三方依赖有什么约束？

A: 优先使用标准库；第三方库需满足：活跃维护、Apache/MIT/BSD/X11 许可证、无 CGO（除非必要）。引入前在 `AGENTS.md` 的「依赖清单」中登记，说明模块与用途。
