# CI 工作流说明

## 触发条件

- `push` 到 `main` 分支
- `pull_request` 到 `main` 分支

## 检查项

### 1. Test & Coverage（测试与覆盖率）

| 检查项 | 阈值 | 说明 |
|--------|------|------|
| 单元测试 | 100% 通过 | `go test -race -v ./...` |
| 覆盖率 | >= 90% | `go test -race -coverprofile=coverage.out ./...` |

### 2. Lint & Format（代码格式与静态检查）

| 检查项 | 工具 | 说明 |
|--------|------|------|
| 格式化 | `gofmt` | 所有文件必须通过格式化 |
| 静态检查 | `go vet` | 标准 vet 检查 |
| 综合 lint | `golangci-lint` | 包含 20+ 个 linter |

### 3. Complexity Analysis（复杂度分析）

| 检查项 | 阈值 | 工具 |
|--------|------|------|
| 圈复杂度 | <= 15 | `gocyclo` |
| 认知复杂度 | <= 20 | `gocognit` |

### 4. Code Structure（代码结构）

| 检查项 | 阈值 | 说明 |
|--------|------|------|
| 包内文件数 | <= 20 | 避免包过大 |
| 文件行数 | <= 500 | 避免文件过长 |
| 函数行数 | <= 80 | 避免函数过长 |
| 目录深度 | <= 6 | 避免过度嵌套 |

### 5. Code Quality（代码质量）

| 检查项 | 工具 | 说明 |
|--------|------|------|
| 静态分析 | `staticcheck` | 深度静态分析 |
| 反模式检测 | 自定义脚本 | `interface{}`、`panic()` 等 |
| TODO/FIXME | 自定义脚本 | 标记未完成任务 |

## 本地验证

在提交前，建议本地运行以下命令：

```bash
# 格式化
go fmt ./...

# 静态检查
go vet ./...

# 测试（含 race 检测）
go test -race ./...

# 覆盖率
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# 安装并运行 golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run

# 复杂度检查
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
gocyclo -over 15 .

go install github.com/uudashr/gocognit@latest
gocognit -over 20 .
```
