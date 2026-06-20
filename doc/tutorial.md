# WiDB 端到端上手教程

> 本教程以「智能家居时序数据」为场景，带你走完 WiDB 的核心流程：建表 → 写入 → 查询 → 聚合 → 维护。
> 每一节都给出可直接复制执行的命令，配套 [sql-reference.md](sql-reference.md) 查阅语法细节。

## 1. 场景

假设你在搭建一个智能家居平台，需要记录每 5 秒一次的传感器读数：

| 字段 | 含义 | 类型 |
|------|------|------|
| `device_id` | 设备唯一编号 | `INT64` |
| `ts` | 采集时间 | `TIMESTAMP` |
| `temperature` | 温度（°C） | `FLOAT64` |
| `humidity` | 湿度（%） | `FLOAT64` |
| `online` | 是否在线 | `BOOL` |

我们关心：
1. 单台设备的最新一次读数（点查）
2. 某时间窗口内的所有读数（范围扫描）
3. 每台设备的平均温湿度（聚合）
4. 离线设备的台数（条件聚合）
5. 一天结束后清理过期数据（删除）

## 2. 启动服务

```bash
# 1. 编译
go build -o widb-server ./cmd/server
go build -o widb-cli   ./cmd/cli

# 2. 启动服务（监听 9000 TCP / 8080 HTTP / 5432 PG wire）
./widb-server -data ./data &

# 3. 进入客户端（默认连本地 TCP 9000）
./widb-cli
```

进入 REPL 后，提示符为 `widb>`，所有 SQL 以分号 `;` 结尾。

> 也可以用一键启动 `./widb`（同进程 server + CLI，零网络延迟）；外部客户端照常能连。详见 [getting-started.md](getting-started.md)。

## 3. 建表

```sql
widb> CREATE TABLE sensor (
  ...>   device_id   INT64      NOT NULL,
  ...>   ts          TIMESTAMP  NOT NULL,
  ...>   temperature FLOAT64,
  ...>   humidity    FLOAT64,
  ...>   online      BOOL,
  ...>   PRIMARY KEY (device_id, ts)
  ...> );
成功
```

说明：
- 复合主键 `(device_id, ts)` 让同一设备的读数按时间自然排序。
- 整数族 + `TIMESTAMP` 都可声明 `NOT NULL`；`FLOAT64` 与 `BOOL` 允许 `NULL`（不可写 `NOT NULL`）。
- 写 `PRIMARY KEY (col1, col2)` 等价于「先按 col1 排序，再按 col2 排序」的字符串拼接键。

如果你经常需要在线设备列表（重启后无需持久化），可加一张内存引擎表：

```sql
widb> CREATE TABLE online_devices (
  ...>   device_id INT64 NOT NULL,
  ...>   label     STRING,
  ...>   PRIMARY KEY (device_id)
  ...> ) ENGINE=memory;
成功
```

## 4. 写入数据

### 4.1 单条插入（REPL/脚本）

```sql
widb> INSERT INTO sensor (device_id, ts, temperature, humidity, online) VALUES
  ...>   (1, TIMESTAMP '2026-06-01T08:00:00Z', 22.5, 60.0, true),
  ...>   (1, TIMESTAMP '2026-06-01T08:00:05Z', 22.7, 59.8, true),
  ...>   (2, TIMESTAMP '2026-06-01T08:00:00Z', 24.1, 55.3, true);
成功 (3 行)
```

### 4.2 批量写入（生产推荐）

HTTP `POST /write` 走 GroupCommit 优化，写入吞吐远高于 SQL 路径：

```bash
curl -X POST http://localhost:8080/write \
  -H "Content-Type: application/json" \
  -d '{
    "table": "sensor",
    "rows": [
      {"device_id":1,"ts":"2026-06-01T08:00:10Z","temperature":22.9,"humidity":59.5,"online":true},
      {"device_id":1,"ts":"2026-06-01T08:00:15Z","temperature":23.0,"humidity":59.2,"online":true},
      {"device_id":2,"ts":"2026-06-01T08:00:10Z","temperature":24.3,"humidity":55.0,"online":false}
    ]
  }'
# {"code":0,"rows":3}
```

### 4.3 通过 PostgreSQL 客户端写入

`psql` 或任何 JDBC 客户端可直接连 `5432`：

```bash
psql -h 127.0.0.1 -p 5432 -U postgres
psql> INSERT INTO sensor VALUES
       (3, '2026-06-01T08:00:00Z', 21.0, 65.0, true);
```

## 5. 查询

### 5.1 单台设备的最新读数

由于主键是 `(device_id, ts)`，可用主键前缀 + ts 后缀做点查（`Scan` 走主键索引）：

```sql
widb> SELECT * FROM sensor WHERE device_id = 1 LIMIT 1;
┌───────────┬──────────────────────────┬─────────────┬──────────┬────────┐
│ device_id │           ts             │ temperature │ humidity │ online │
├───────────┼──────────────────────────┼─────────────┼──────────┼────────┤
│         1 │ 2026-06-01T08:00:15Z     │        23.0 │     59.2 │   true │
└───────────┴──────────────────────────┴─────────────┴──────────┴────────┘
1 行
```

### 5.2 范围扫描（某时间窗口）

```sql
widb> SELECT device_id, ts, temperature FROM sensor
  ...> WHERE device_id = 1
  ...>   AND ts >= TIMESTAMP '2026-06-01T08:00:00Z'
  ...>   AND ts <=  TIMESTAMP '2026-06-01T08:00:12Z'
  ...> ORDER BY ts ASC;
```

> `ORDER BY` 当前**不保证排序**（parser 静默丢弃）；如需顺序，请在客户端按 `ts` 排序，或等待后续 PR 修复。

### 5.3 模糊匹配（LIKE）

```sql
widb> SELECT * FROM sensor
  ...> WHERE ts = TIMESTAMP '2026-06-01T08:00:00Z'
  ...>   AND humidity LIKE '5%';  -- 整数家族 LIKE 不适用，这里只示意语法
```

> 实际上 `LIKE` 主要用于 `STRING` 列，例如 `WHERE label LIKE 'sensor-%'`。

## 6. 聚合

### 6.1 每台设备的平均温湿度

```sql
widb> SELECT device_id,
  ...>        COUNT(*)        AS readings,
  ...>        AVG(temperature) AS avg_t,
  ...>        AVG(humidity)    AS avg_h,
  ...>        MIN(temperature) AS min_t,
  ...>        MAX(temperature) AS max_t
  ...> FROM sensor
  ...> GROUP BY device_id;
```

### 6.2 离线设备的台数

> 当前 parser **不支持** `DISTINCT`，如下写法会被静默丢弃 `DISTINCT`，实际执行 `COUNT(device_id)`（统计非 NULL 值）。如需真正去重，请在客户端聚合或改用 `GROUP BY`：

```sql
-- 实际执行的是 COUNT(device_id) 而不是 COUNT(DISTINCT device_id)
widb> SELECT COUNT(DISTINCT device_id) AS offline_devices
  ...> FROM sensor
  ...> WHERE online = false;

-- 推荐：等价且显式
widb> SELECT COUNT(*) AS offline_devices
  ...> FROM (SELECT DISTINCT device_id FROM sensor WHERE online = false) t;
```

### 6.3 时段内的最高温

```sql
widb> SELECT MAX(temperature) AS peak_temp
  ...> FROM sensor
  ...> WHERE ts >= TIMESTAMP '2026-06-01T08:00:00Z'
  ...>   AND ts <  TIMESTAMP '2026-06-01T09:00:00Z';
```

## 7. DML：更新与删除

### 7.1 修正一条读数

```sql
widb> UPDATE sensor SET temperature = 22.8 WHERE device_id = 1 AND ts = TIMESTAMP '2026-06-01T08:00:00Z';
成功 (1 行受影响)
```

### 7.2 批量更新在线状态

```sql
widb> UPDATE sensor SET online = false WHERE device_id = 2;
成功 (1 行受影响)
```

### 7.3 清理过期数据

```sql
widb> DELETE FROM sensor
  ...> WHERE ts < TIMESTAMP '2026-05-01T00:00:00Z';
成功 (N 行受影响)
```

> 当前 `BETWEEN` / `IN` / `IS NULL` 暂不支持，请用 `>=` / `<=` / `OR` 替代。

## 8. 元命令

### 8.1 列出所有表

```sql
widb> SHOW TABLES;
┌────────────────┐
│     table      │
├────────────────┤
│ online_devices │
│ sensor         │
└────────────────┘
2 行
```

### 8.2 查看表结构

```sql
widb> DESCRIBE sensor;
┌─────────────┬────────────┬───────┬──────┐
│   field     │    type    │ null  │ key  │
├─────────────┼────────────┼───────┼──────┤
│ device_id   │ INT64      │ false │ true │
│ ts          │ TIMESTAMP  │ false │ true │
│ temperature │ FLOAT64    │ true  │ false│
│ humidity    │ FLOAT64    │ true  │ false│
│ online      │ BOOL       │ true  │ false│
└─────────────┴────────────┴───────┴──────┘
5 行
```

### 8.3 查看查询计划

```sql
widb> EXPLAIN SELECT device_id, AVG(temperature) FROM sensor WHERE online = true GROUP BY device_id;
┌────┬───────┬───────────┬──────────────────────────────────────────┐
│ id │ depth │ operation │ detail                                   │
├────┼───────┼───────────┼──────────────────────────────────────────┤
│  1 │   0   │ Project   │ Expressions: [device_id, AVG(...) AS ...]│
│  2 │   1   │ Aggregate │ GroupBy: [device_id], Aggregates: [...]  │
│  3 │   2   │ Scan      │ Table: sensor, Columns: [...], ...       │
└────┴───────┴───────────┴──────────────────────────────────────────┘
3 行
```

## 9. 维护与监控

### 9.1 健康检查

```bash
curl http://localhost:8080/health
# {"status":"ok","timestamp":"...","scheduler":{"flush_count":42,"compact_count":3,"wal_clean_count":1,"last_error":""}}
```

### 9.2 Prometheus 指标

```bash
curl http://localhost:8080/metrics
# widb_query_total{type="select"} 1234
# widb_write_total 5678
# widb_memtable_bytes 123456
# ...
```

完整指标列表见 [api.md](api.md) / [server.md](server.md)。

### 9.3 优雅关闭

```bash
# 方式 1：REPL 内
widb> \q
再见!

# 方式 2：外部
kill -TERM $(pgrep widb-server)
```

## 10. 进一步学习

| 想了解 | 阅读 |
|--------|------|
| SQL 完整语法 | [sql-reference.md](sql-reference.md) |
| 架构与设计 | [architecture.md](architecture.md) |
| HTTP / TCP / PG wire API | [api.md](api.md) |
| 性能调优 | [performance.md](performance.md) |
| 参与开发 | [development.md](development.md) |
| 常见 SQL 套路 | [cookbook.md](cookbook.md) |
| 故障排查 | [troubleshooting.md](troubleshooting.md) |
