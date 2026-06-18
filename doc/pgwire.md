# PostgreSQL wire 协议详解

## 1. 概述

WiDB 通过 `pkg/server/pgwire/` 实现 PostgreSQL wire 协议（v3）服务端，使标准 PostgreSQL 客户端（`psql`、pgJDBC 驱动、BI 工具如 Superset/Metabase/Grafana）可直接连接 WiDB，无需专用驱动。

实现基于 `github.com/jackc/pgproto3/v2`（MIT，pgx 生态事实标准），自建连接处理循环。当前支持：

- **认证方式**：trust（AuthenticationOk，无密码）
- **协议模式**：Simple Query（客户端发送 Query 消息，服务端执行整条 SQL）
- **资源保护**：最大并发连接、空闲超时、写入超时

## 2. 启用与配置

PG wire 默认监听 `0.0.0.0:5432`。三种配置方式（优先级：命令行 > 配置文件 > 默认值）：

```bash
# 1. 命令行参数（覆盖配置文件）
./widb-server -pg 0.0.0.0:5432

# 2. 配置文件 widb.yaml
# server:
#   pg_addr: "0.0.0.0:5432"

# 3. 留空禁用
./widb-server -pg ""
```

`widb`（一键启动）与 `widb-server` 共享同一参数。启动后 `\addrs` 命令会显示 PG 监听地址。

## 3. 连接示例

### psql

```bash
# 执行单条查询
psql -h 127.0.0.1 -p 5432 -c "SELECT id, name FROM sensor WHERE id = 1"

# 交互模式
psql -h 127.0.0.1 -p 5432
```

### JDBC（Java）

```java
String url = "jdbc:postgresql://127.0.0.1:5432/";
Connection conn = DriverManager.getConnection(url);
Statement st = conn.createStatement();
ResultSet rs = st.executeQuery("SELECT id, name FROM sensor WHERE id = 1");
```

### Python（psycopg）

```python
import psycopg
conn = psycopg.connect("host=127.0.0.1 port=5432")
cur = conn.execute("SELECT id, name FROM sensor WHERE id = 1")
for row in cur:
    print(row)
```

## 4. 协议流程

```
客户端                          服务端
  │                               │
  │── StartupMessage ────────────▶│   (含数据库名/用户名)
  │◀─ AuthenticationOk ───────────│   (trust 认证直接通过)
  │◀─ ParameterStatus* ───────────│   (server_version 等)
  │◀─ BackendKeyData ─────────────│
  │◀─ ReadyForQuery ──────────────│   (进入就绪状态)
  │                               │
  │── Query("SELECT ...") ───────▶│
  │◀─ RowDescription ─────────────│   (列名 + 类型 OID)
  │◀─ DataRow* ───────────────────│   (每行一个 DataRow)
  │◀─ CommandComplete ────────────│   (如 "SELECT 3")
  │◀─ ReadyForQuery ──────────────│
  │                               │
  │── Terminate ─────────────────▶│   (断开连接)
```

错误时服务端发送 `ErrorResponse`（含 severity/code/message 字段）后回到 `ReadyForQuery`，连接保持可用。

## 5. 类型映射

WiDB 类型到 PostgreSQL 类型的映射（`pkg/server/pgwire/types.go`）：

| WiDB 类型 | PG OID | PG 类型名 | 格式 |
|-----------|--------|-----------|------|
| BOOL | 16 | bool | 文本 "t"/"f" |
| INT8 | 21 | int2 | 文本数字 |
| INT16 | 21 | int2 | 文本数字 |
| INT32 | 23 | int4 | 文本数字 |
| INT64 | 20 | int8 | 文本数字 |
| UINT64 | 20 | int8 | 文本数字 |
| FLOAT64 | 701 | float8 | 文本数字 |
| STRING | 25 | text | 文本 |
| DATE | 1082 | date | "YYYY-MM-DD" |
| TIMESTAMP | 1114 | timestamp | RFC3339Nano |

类型推断优先使用查询计划的 Schema 列类型；Schema 缺失时回退到从结果行值推断（`inferTypeFromValue`），全为 NULL 的列默认 TEXT。

## 6. 资源保护与安全

为防止恶意客户端耗尽服务端 goroutine，`NewServer` 默认启用：

| 保护项 | 默认值 | 选项 | 说明 |
|--------|--------|------|------|
| 最大并发连接 | 256 | `WithMaxConns(n)` | 超限连接立即关闭，<=0 不限制 |
| 单次读取空闲超时 | 5 分钟 | `WithIdleTimeout(d)` | 空闲连接自动断开 |
| 单次写入超时 | 30 秒 | `WithWriteTimeout(d)` | 慢客户端不阻塞 goroutine |

### 安全注意事项

当前认证方式为 **trust**，任何能连上监听端口的客户端均可执行 SQL。在不可信网络中部署时：

1. 将监听地址绑定到回环或内网（如 `127.0.0.1` 或内网 IP），避免暴露到公网；
2. 在网络边界（防火墙/安全组）限制 5432 端口访问来源；
3. 通过反向代理或带认证的网关前置 pgwire 端口。

## 7. 实现结构

| 文件 | 职责 |
|------|------|
| `server.go` | 监听、accept 循环、连接限流、优雅停机 |
| `conn.go` | 单连接处理：Startup 握手、Query 循环、消息封装 |
| `executor.go` | `SQLExecutor` 接口定义（由服务层 `pgwireAdapter` 实现） |
| `encode.go` | 结果行编码为 DataRow |
| `types.go` | WiDB DataType → PG OID 映射、类型推断 |

`pgwireAdapter`（`pkg/server/pgwire_adapter.go`）将 `*Server` 适配为 `pgwire.SQLExecutor`，复用现有的 parser → analyzer → optimizer → executor 管线，与 TCP/HTTP 协议走完全相同的执行路径。
