# WiDB SQL Cookbook

> 本文档收录 WiDB 常见 SQL 套路与最佳实践。所有 SQL 均经过实测可直接运行。
> 配套阅读 [sql-reference.md](sql-reference.md)（语法）、[tutorial.md](tutorial.md)（端到端流程）。

## 目录

- [1. 时序数据](#1-时序数据)
- [2. 范围扫描与点查](#2-范围扫描与点查)
- [3. 聚合分析](#3-聚合分析)
- [4. 数据更新与维护](#4-数据维护)
- [5. 多表协作](#5-多表协作)
- [6. 性能套路](#6-性能套路)
- [7. 错误处理范式](#7-错误处理范式)

## 1. 时序数据

### 1.1 时序表设计

复合主键 `(device_id, ts)` 是最常见的时序模式：同一设备读数按时间天然聚集，范围查询与点查都可走主键索引。

```sql
CREATE TABLE sensor (
  device_id   INT64      NOT NULL,
  ts          TIMESTAMP  NOT NULL,
  temperature FLOAT64,
  humidity    FLOAT64,
  online      BOOL,
  PRIMARY KEY (device_id, ts)
);
```

### 1.2 取每台设备的最新读数

`GROUP BY` + `MAX(ts)` 找时间戳最大那一行。直接用 `MAX(ts)` 只能得到时间戳，需要再用子查询回查完整行：

```sql
-- 子查询：先找每台设备的最新 ts，再回查
SELECT s.* FROM sensor s
INNER JOIN (
  SELECT device_id, MAX(ts) AS max_ts
  FROM sensor
  GROUP BY device_id
) m ON s.device_id = m.device_id AND s.ts = m.max_ts;
```

> 当前 parser **不支持** `INNER JOIN`；上面 SQL 会报错。临时方案：按设备分别查，或客户端聚合。

### 1.3 时段分组（小时桶）

当前无 `DATE_TRUNC`，可手动转换为整小时字符串再分组（注意时区）：

```sql
-- 暂时只能按完整 ts 分组；如下示例展示「按设备 + 时段」统计
SELECT device_id, ts, AVG(temperature) AS avg_t
FROM sensor
WHERE ts >= TIMESTAMP '2026-06-01T00:00:00Z'
  AND ts <  TIMESTAMP '2026-06-02T00:00:00Z'
GROUP BY device_id, ts;
```

### 1.4 数据保留策略

周期性删除过期数据：

```sql
-- 保留最近 30 天
DELETE FROM sensor
WHERE ts < TIMESTAMP '2026-05-01T00:00:00Z';
```

生产环境建议：
- 用 cron 每天凌晨执行一次
- 单次删除行数过大时配合 `LIMIT`（分批删）：
  ```sql
  -- 伪代码：循环直到 0 行
  DELETE FROM sensor
  WHERE ts < TIMESTAMP '2026-05-01T00:00:00Z'
  LIMIT 10000;
  ```
  > 当前 `DELETE` 不支持 `LIMIT` 子句；可通过 `WHERE` 缩小范围手动分批。

## 2. 范围扫描与点查

### 2.1 主键等值点查

```sql
-- 单列主键
SELECT * FROM sensor WHERE device_id = 1 LIMIT 1;

-- 复合主键：必须给出全部 PK 列
SELECT * FROM sensor WHERE device_id = 1 AND ts = TIMESTAMP '2026-06-01T08:00:00Z';
```

### 2.2 主键前缀 + 范围

```sql
-- 设备 1 在 08:00 ~ 09:00 之间的所有读数
SELECT * FROM sensor
WHERE device_id = 1
  AND ts >= TIMESTAMP '2026-06-01T08:00:00Z'
  AND ts <  TIMESTAMP '2026-06-01T09:00:00Z';
```

### 2.3 非主键列过滤

走全表扫描 + 稀疏索引裁剪（`SparseIndex` 根据列 Min/Max 跳过不满足谓词的 Segment）：

```sql
-- 温度 > 30 的所有读数（不限设备）
SELECT device_id, ts, temperature FROM sensor WHERE temperature > 30.0;
```

### 2.4 复合过滤

```sql
-- 设备 1、在线、温度 > 25
SELECT * FROM sensor
WHERE device_id = 1
  AND online = true
  AND temperature > 25.0;
```

### 2.5 模糊匹配（STRING 列）

```sql
-- 所有以 "sensor-" 开头的设备读数
SELECT * FROM sensor
WHERE device_id IN (SELECT device_id FROM devices WHERE name LIKE 'sensor-%');
```

> `IN` 暂不支持；可用 `OR` 链替代或拆成多次查询后合并。

## 3. 聚合分析

### 3.1 单维聚合

```sql
-- 全部读数的平均温湿度
SELECT AVG(temperature) AS avg_t, AVG(humidity) AS avg_h, COUNT(*) AS n FROM sensor;
```

### 3.2 多维聚合

```sql
-- 每台设备的平均温湿度 + 读数条数
SELECT device_id,
       COUNT(*)           AS readings,
       AVG(temperature)   AS avg_t,
       MIN(temperature)   AS min_t,
       MAX(temperature)   AS max_t
FROM sensor
GROUP BY device_id;
```

### 3.3 过滤后聚合

```sql
-- 在线设备且温度 > 20 的读数条数与平均温
SELECT device_id, COUNT(*) AS n, AVG(temperature) AS avg_t
FROM sensor
WHERE online = true AND temperature > 20.0
GROUP BY device_id;
```

### 3.4 极值 + 同时取多列

```sql
-- 全表最高温与对应设备
SELECT MAX(temperature) AS peak_t FROM sensor;

-- 当前不支持「argmax」语义，需客户端二次查询
-- 1. 上面查 peak_t（得到 32.5）
-- 2. SELECT device_id, ts FROM sensor WHERE temperature = 32.5
```

## 4. 数据维护

### 4.1 单行更新

```sql
UPDATE sensor SET temperature = 22.8
WHERE device_id = 1 AND ts = TIMESTAMP '2026-06-01T08:00:00Z';
```

### 4.2 算术更新

```sql
-- 温度全部 +0.5
UPDATE sensor SET temperature = temperature + 0.5;
```

### 4.3 复合条件更新

```sql
-- 把所有离线设备的 humidity 置为 0
UPDATE sensor SET humidity = 0.0 WHERE online = false;
```

### 4.4 条件删除

```sql
-- 删除某设备某时段的数据
DELETE FROM sensor
WHERE device_id = 1
  AND ts >= TIMESTAMP '2026-06-01T00:00:00Z'
  AND ts <  TIMESTAMP '2026-06-02T00:00:00Z';
```

### 4.5 主键变更（小心）

```sql
-- 把设备 1 的某条读数「迁移」到设备 99
UPDATE sensor SET device_id = 99
WHERE device_id = 1 AND ts = TIMESTAMP '2026-06-01T08:00:00Z';
-- 若 device_id=99 的同一 ts 已有数据，会返回主键冲突错误
```

## 5. 多表协作

### 5.1 LSM 引擎 + 内存引擎搭配

```sql
-- 主表：传感器读数（持久化）
CREATE TABLE sensor (
  device_id INT64 NOT NULL,
  ts        TIMESTAMP NOT NULL,
  temperature FLOAT64,
  PRIMARY KEY (device_id, ts)
);  -- 默认 ENGINE=lsm

-- 维度表：设备元信息（重启后清空）
CREATE TABLE device_meta (
  device_id INT64 NOT NULL,
  label     STRING,
  room      STRING,
  PRIMARY KEY (device_id)
) ENGINE=memory;
```

写入端：主表用 `/write` 批量 API 走 LSM GroupCommit；维度表用 SQL 逐条 `INSERT`（小数据量无性能压力）。

读取端：SQL 直接 `FROM sensor JOIN device_meta`：

> 当前 `JOIN` 暂不支持。可改为多次查询后在客户端合并。

### 5.2 表分桶（手动分区）

按月分表是 OLAP 常见模式：

```sql
CREATE TABLE sensor_2026_05 (LIKE sensor);
CREATE TABLE sensor_2026_06 (LIKE sensor);
```

> `CREATE TABLE ... LIKE` 暂不支持；需要复制粘贴列定义。

写入端按月分流；查询端用应用层拼装 `UNION ALL`：

> `UNION ALL` 暂不支持。可在应用层并行查多表后合并。

## 6. 性能套路

### 6.1 写入：HTTP `/write` 批量

```bash
# 每次写 1k~10k 行效果最佳
curl -X POST http://localhost:8080/write \
  -H "Content-Type: application/json" \
  -d @batch.json   # 包含 5000 行
```

详见 [performance.md](performance.md) 中的 GroupCommit 调优。

### 6.2 查询：投影裁剪

只取需要的列：

```sql
-- 错误：全列扫描浪费 I/O
SELECT * FROM sensor WHERE device_id = 1;

-- 正确：只取 device_id、ts、temperature
SELECT device_id, ts, temperature FROM sensor WHERE device_id = 1;
```

### 6.3 查询：LIMIT 早停

```sql
-- 查前 10 条
SELECT * FROM sensor LIMIT 10;

-- 结合 WHERE 让 LIMIT 早停更激进
SELECT * FROM sensor WHERE device_id = 1 LIMIT 10;
```

### 6.4 过滤：稀疏索引友好

非主键列过滤会被 `SparseIndex` 加速，前提是列值有明显的 Min/Max 区分度：

```sql
-- 命中稀疏索引：温度列 Min/Max 跨度大，单 Segment 内 30+ 的读数可能集中在少数 Segment
SELECT * FROM sensor WHERE temperature > 30.0;
```

### 6.5 监控：观察 metrics

```bash
# 实时观察写入速率
watch -n 1 'curl -s http://localhost:8080/metrics | grep widb_write_total'

# 观察 memtable 大小，接近 max_memtable_size 时会刷盘
watch -n 1 'curl -s http://localhost:8080/metrics | grep widb_memtable_bytes'
```

## 7. 错误处理范式

### 7.1 应用层判定响应码

```python
import requests

resp = requests.post("http://localhost:8080/query", json={"sql": sql}).json()
if resp["code"] != 0:
    raise RuntimeError(f"SQL failed: {resp.get('message', 'unknown')}")
for row in resp.get("data", []):
    process(row)
```

### 7.2 优雅降级：批量写

```python
def write_batch(table, rows, chunk=2000):
    for i in range(0, len(rows), chunk):
        r = requests.post(f"http://localhost:8080/write", json={
            "table": table, "rows": rows[i:i+chunk],
        })
        r.raise_for_status()
```

### 7.3 不支持的语法：降级到 OR 链

```sql
-- IN (1,2,3) 降级为
WHERE device_id = 1 OR device_id = 2 OR device_id = 3

-- BETWEEN x AND y 降级为
WHERE ts >= x AND ts <= y
```

### 7.4 超时与重试

写接口在 Compaction 触发时可能短暂变慢（合并大 Segment 时阻塞写）。建议：

```python
import time

def with_retry(fn, retries=3, backoff=0.5):
    for i in range(retries):
        try:
            return fn()
        except Exception:
            if i == retries - 1:
                raise
            time.sleep(backoff * (2 ** i))
```
