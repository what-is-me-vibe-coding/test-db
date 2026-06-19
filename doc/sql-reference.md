# WiDB SQL 参考手册

> 本文档是 WiDB 支持的 SQL 语法的权威参考。涵盖 DDL、DML、查询、表达式、聚合、类型、字面量与限制。
> 与模块详解 [query.md](query.md)、[dml.md](dml.md)、[types.md](types.md) 互补：
> 那些文档讲「怎么实现」，本参考讲「怎么用」。

## 目录

- [1. 语句总览](#1-语句总览)
- [2. DDL：数据定义](#2-ddl数据定义)
  - [2.1 CREATE TABLE](#21-create-table)
  - [2.2 DROP TABLE](#22-drop-table)
- [3. DML：数据操作](#3-dml数据操作)
  - [3.1 INSERT](#31-insert)
  - [3.2 UPDATE](#32-update)
  - [3.3 DELETE](#33-delete)
- [4. 查询：SELECT](#4-查询select)
  - [4.1 语法骨架](#41-语法骨架)
  - [4.2 列投影与别名](#42-列投影与别名)
  - [4.3 WHERE 过滤](#43-where-过滤)
  - [4.4 GROUP BY 聚合](#44-group-by-聚合)
  - [4.5 LIMIT 与 OFFSET](#45-limit-与-offset)
- [5. 元命令](#5-元命令)
  - [5.1 SHOW TABLES](#51-show-tables)
  - [5.2 DESCRIBE / DESC](#52-describe--desc)
  - [5.3 EXPLAIN](#53-explain)
- [6. 表达式与运算符](#6-表达式与运算符)
  - [6.1 算术运算](#61-算术运算)
  - [6.2 比较与逻辑](#62-比较与逻辑)
  - [6.3 LIKE 模式匹配](#63-like-模式匹配)
- [7. 数据类型](#7-数据类型)
- [8. 字面量与 NULL](#8-字面量与-null)
- [9. 存储引擎选择](#9-存储引擎选择)
- [10. 已知限制与注意事项](#10-已知限制与注意事项)

## 1. 语句总览

| 类别 | 语句 | 简介 |
|------|------|------|
| DDL | `CREATE TABLE` | 创建表（必须指定 `PRIMARY KEY`，可选 `ENGINE=`） |
| DDL | `DROP TABLE` | 删除表 |
| DML | `INSERT INTO ... VALUES` | 通过 SQL 通道插入行（同时支持 `/write` 批量 API） |
| DML | `UPDATE ... SET ... WHERE` | 按条件更新列 |
| DML | `DELETE FROM ... WHERE` | 按条件删除行 |
| Query | `SELECT` | 查询（投影、过滤、聚合、LIMIT） |
| Meta | `SHOW TABLES` | 列出所有表 |
| Meta | `DESCRIBE` / `DESC` | 查看表结构 |
| Meta | `EXPLAIN <stmt>` | 输出查询计划树 |

> 语句关键字大小写不敏感；表名与列名区分大小写。

## 2. DDL：数据定义

### 2.1 CREATE TABLE

#### 语法

```sql
CREATE TABLE [IF NOT EXISTS] table_name (
    col1 type1 [NOT NULL],
    col2 type2,
    ...,
    PRIMARY KEY (pk_col | (pk_col1, pk_col2, ...))
) [ENGINE = lsm | memory];
```

#### 关键约束

- **必须** 显式声明 `PRIMARY KEY`；支持单列主键与**多列复合主键**。
- **可选** `ENGINE=memory`：使用内存引擎（重启后数据丢失）；不指定或 `ENGINE=lsm` 则使用 LSM 引擎（持久化 + 崩溃恢复）。
- **可选** `IF NOT EXISTS`：表已存在时不报错；不带则同名表会触发错误。
- 整数族类型（`INT8/INT16/INT32/INT64/UINT64`）与 `STRING/TIMESTAMP` 默认允许 `NULL`；如需非空可显式写 `NOT NULL`。
- 浮点与布尔类型允许 `NULL`，不可写 `NOT NULL`（语义上 NaN 已是「无效」表示）。

#### 示例

```sql
-- 通用 LSM 引擎（默认）
CREATE TABLE sensor (
    id        INT64      NOT NULL,
    name      STRING,
    temp      FLOAT64,
    humidity  FLOAT64,
    active    BOOL,
    PRIMARY KEY (id)
);

-- 内存引擎（临时表/维度表）
CREATE TABLE dim_region (
    region_id   INT32     NOT NULL,
    region_name STRING,
    PRIMARY KEY (region_id)
) ENGINE=memory;

-- 复合主键
CREATE TABLE order_item (
    order_id    INT64 NOT NULL,
    sku         STRING NOT NULL,
    qty         INT64,
    PRIMARY KEY (order_id, sku)
);
```

### 2.2 DROP TABLE

```sql
DROP TABLE table_name;
```

- 不存在的表返回错误；不区分 `IF EXISTS`（这是与 MySQL 的差异）。
- 删除表会移除表的所有数据、索引与 Segment 文件。

## 3. DML：数据操作

### 3.1 INSERT

#### 语法

```sql
INSERT INTO table_name [(col1, col2, ...)] VALUES
  (val1, val2, ...),
  (val3, val4, ...),
  ...;
```

- **列名列表可选**：缺省时按表定义顺序提供所有列的值。
- 支持批量 VALUES（多行），可减少网络往返。
- 类型必须可隐式转换（如 `INT64 ← INT32`）；不兼容的类型（如 `STRING ← INT64`）触发解析/执行错误。
- 重复主键会触发错误（LSM 引擎）；内存引擎行为一致。
- **生产建议**：高吞吐写入使用 HTTP `POST /write` 批量 API 以获得 GroupCommit 优化；SQL INSERT 适合少量手动插入与脚本。

#### 示例

```sql
-- 单行插入
INSERT INTO sensor (id, name, temp, humidity, active)
VALUES (1, 'sensor-1', 23.5, 60.0, true);

-- 批量插入
INSERT INTO sensor (id, name, temp) VALUES
  (2, 'sensor-2', 18.2),
  (3, 'sensor-3', 25.0),
  (4, 'sensor-4', 19.8);
```

### 3.2 UPDATE

#### 语法

```sql
UPDATE table_name
SET col1 = expr1, col2 = expr2, ...
[WHERE condition];
```

- `SET` 右侧是任意合法表达式（字面量、列引用、算术运算）。
- `WHERE` 与 `SELECT` 共享同一套谓词求值规则（见 [§6 表达式与运算符](#6-表达式与运算符)）。
- 匹配行数会写入响应的 `rows` 字段。
- **主键冲突**：若 `SET` 改变主键列且新值已存在，触发冲突错误。
- 无 `WHERE` 时更新全表（谨慎使用）。

#### 示例

```sql
-- 单列更新
UPDATE sensor SET temp = 24.0 WHERE id = 1;

-- 多列 + 算术
UPDATE sensor SET temp = temp + 0.5, active = NOT active WHERE id = 1;

-- 范围更新
UPDATE sensor SET active = false WHERE id >= 100 AND id < 200;
```

### 3.3 DELETE

#### 语法

```sql
DELETE FROM table_name [WHERE condition];
```

- 无 `WHERE` 时清空整张表。
- 命中行数写入响应的 `rows` 字段。
- 删除 LSM 引擎表的数据会写入墓碑（tombstone），由 Compaction 物理回收。

#### 示例

```sql
-- 条件删除
DELETE FROM sensor WHERE active = false;

-- 范围删除（用 >= + <= 组合替代不支持的 BETWEEN）
DELETE FROM sensor WHERE id >= 1000 AND id <= 1999;
```

> 当前 parser **不支持** `BETWEEN` / `IN` / `IS NULL`，会返回非零错误码（见 [§10 已知限制](#10-已知限制与注意事项)）。

## 4. 查询：SELECT

### 4.1 语法骨架

```sql
SELECT [DISTINCT] select_expr [, ...]
FROM table_name [AS alias]
[WHERE condition]
[GROUP BY col [, ...]]
[HAVING condition]
[ORDER BY col [ASC | DESC] [, ...]]
[LIMIT n [OFFSET m]];
```

### 4.2 列投影与别名

```sql
-- 全部列
SELECT * FROM sensor;

-- 指定列
SELECT id, name, temp FROM sensor;

-- 别名（投影计算结果）
SELECT id, temp * 2 AS doubled, name AS label FROM sensor;

-- 算术表达式
SELECT id, qty + 10 AS new_qty, price * (1 - discount) AS net_price FROM order_item;
```

> `SELECT` 中可使用 `+ - * /` 算术；类型不同时按以下规则提升：
> - 整数族（`INT8/16/32/64/UINT64`）之间自由转换；
> - `FLOAT64` 与整数族混合时，结果为 `FLOAT64`；
> - `BOOL` 不参与自动类型提升；如需转换请用显式比较。

### 4.3 WHERE 过滤

| 类别 | 支持 |
|------|------|
| 比较 | `=` / `!=` / `<>` / `>` / `>=` / `<` / `<=` |
| 逻辑 | `AND` / `OR` / `NOT` |
| 模式 | `LIKE` / `NOT LIKE`（`%` 多字符，`_` 单字符，区分大小写） |
| 范围 | `BETWEEN ... AND ...`（**当前不支持**，返回错误码） |
| 集合 | `IN`（**当前不支持**，返回错误码） |
| 空值 | `IS NULL` / `IS NOT NULL`（**当前不支持**） |

**示例**：

```sql
-- 组合过滤
SELECT id, name FROM sensor
WHERE active = true AND temp >= 20.0 AND temp <= 30.0;

-- LIKE 模糊匹配
SELECT id, name FROM sensor WHERE name LIKE 'sensor-%';

-- 复合布尔
SELECT id, temp FROM sensor
WHERE (active = true AND temp > 25.0) OR id = 1;
```

### 4.4 GROUP BY 聚合

#### 聚合函数

| 函数 | 行为 |
|------|------|
| `COUNT(*)` | 匹配行数（含 NULL） |
| `COUNT(col)` | col 非 NULL 的行数 |
| `SUM(col)` | 数值列求和；忽略 NULL；空集合返回 NULL |
| `AVG(col)` | 数值列平均值；忽略 NULL；空集合返回 NULL |
| `MIN(col)` / `MAX(col)` | 最小/最大值；支持数值、字符串、时间戳 |

#### 示例

```sql
-- 按 product 分组统计
SELECT product,
       COUNT(*)         AS cnt,
       SUM(qty)         AS total_qty,
       AVG(amount)      AS avg_amount,
       MIN(qty)         AS min_qty,
       MAX(qty)         AS max_qty
FROM orders
GROUP BY product;

-- 多列分组
SELECT region, product, SUM(qty) AS total
FROM orders
GROUP BY region, product;
```

> 当前 parser **静默丢弃** `HAVING` 子句；如需分组后过滤请改用 `WHERE` 或在外层嵌套查询。`ORDER BY` 当前**不保证排序**（实现细节，详见 [§10 已知限制](#10-已知限制与注意事项)）。

### 4.5 LIMIT 与 OFFSET

```sql
-- 前 10 行
SELECT * FROM sensor LIMIT 10;

-- 第 11-20 行
SELECT * FROM sensor LIMIT 10 OFFSET 10;
```

## 5. 元命令

### 5.1 SHOW TABLES

```sql
SHOW TABLES;
```

返回当前所有表（按表名字典序）。响应每行 `table` 字段为表名。

### 5.2 DESCRIBE / DESC

```sql
DESCRIBE table_name;  -- 或 DESC table_name;
```

返回表的所有列定义：列名、类型、是否主键、是否允许 NULL。

### 5.3 EXPLAIN

```sql
EXPLAIN SELECT ... ;
```

输出查询计划树，固定 4 列：`id` / `depth` / `operation` / `detail`：

| 列 | 含义 |
|----|------|
| `id` | 节点在树中的唯一编号 |
| `depth` | 节点深度（0 为叶子） |
| `operation` | 算子类型（`Scan` / `Filter` / `Project` / `Aggregate` / `Limit`） |
| `detail` | 算子细节（列名、谓词表达式、聚合函数等） |

#### 示例

```sql
widb> EXPLAIN SELECT product, SUM(qty) FROM orders WHERE active = true GROUP BY product;
+----+-------+-----------+--------------------------------------+
| id | depth | operation | detail                               |
+----+-------+-----------+--------------------------------------+
|  1 |     0 | Aggregate | GROUP BY product; SUM(qty)           |
|  2 |     1 | Filter    | active = true                        |
|  3 |     2 | Project   | product, qty                         |
|  4 |     3 | Scan      | orders                               |
+----+-------+-----------+--------------------------------------+
```

> 当前 EXPLAIN 仅对 SELECT 输出计划树；DDL/DML 语句的 EXPLAIN 会返回非零码。

## 6. 表达式与运算符

### 6.1 算术运算

| 运算符 | 含义 |
|--------|------|
| `+` | 加 |
| `-` | 减 |
| `*` | 乘 |
| `/` | 除（整数除法结果向下取整） |

- 整数族 + 整数族 = 整数族（结果可能溢出，超出范围时返回错误）。
- 含 `FLOAT64` 的算术 = `FLOAT64`。
- 除以 0：整数除法返回错误（不 panic）；浮点除法得到 `+Inf` / `-Inf` / `NaN`。

### 6.2 比较与逻辑

| 类别 | 运算符 |
|------|--------|
| 比较 | `=`, `!=`, `<>`, `>`, `>=`, `<`, `<=` |
| 逻辑 | `AND`, `OR`, `NOT` |
| 模式 | `LIKE`, `NOT LIKE` |

> `NULL` 的语义遵循三值逻辑：与 `NULL` 的任何算术/比较结果为 `NULL`，`NULL` 在 `WHERE` 中被视为「不通过」（即行被过滤）。

### 6.3 LIKE 模式匹配

| 通配符 | 含义 |
|--------|------|
| `%` | 匹配任意长度（含 0）的字符序列 |
| `_` | 匹配恰好 1 个字符 |
| 其他 | 字面匹配（区分大小写） |

```sql
SELECT id, name FROM sensor WHERE name LIKE 'sensor-%';   -- 前缀匹配
SELECT id, name FROM sensor WHERE name LIKE '%.log';      -- 后缀匹配
SELECT id, name FROM sensor WHERE name LIKE '%temp%';     -- 包含匹配
SELECT id, name FROM sensor WHERE name LIKE 'sensor_';    -- 7 字符前缀 + 任意 1 字符
```

## 7. 数据类型

| 类型 | 范围 | 字节 | 字面量示例 |
|------|------|------|----------|
| `INT8` | -128..127 | 1 | `INT8`, `TINYINT` |
| `INT16` | -32768..32767 | 2 | `INT16`, `SMALLINT` |
| `INT32` | -2^31..2^31-1 | 4 | `INT32`, `INT` |
| `INT64` | -2^63..2^63-1 | 8 | `INT64`, `BIGINT` |
| `UINT64` | 0..2^64-1 | 8 | `UINT64` |
| `FLOAT64` | IEEE 754 双精度 | 8 | `FLOAT64`, `DOUBLE` |
| `STRING` | UTF-8 变长 | 变长 | `STRING`, `TEXT`, `VARCHAR` |
| `BOOL` | true / false | 1 | `BOOL`, `BOOLEAN` |
| `TIMESTAMP` | RFC 3339 字符串 | 8 | `TIMESTAMP` |
| `DATE` | YYYY-MM-DD | 8 | `DATE` |

> 详细类型系统、编码与比较规则见 [types.md](types.md)。

## 8. 字面量与 NULL

| 字面量 | 语法 | 说明 |
|--------|------|------|
| 整数 | `123`, `-456` | 自动推断为最小容纳类型；显式类型用 `CAST` 或构造时声明 |
| 浮点 | `3.14`, `-0.5`, `1e10` | 一律解释为 `FLOAT64` |
| 字符串 | `'foo'`, `'it''s ok'` | 单引号；字符串内单引号用两个单引号转义 |
| 布尔 | `TRUE`, `FALSE` | 也接受 `true` / `false`（不区分大小写） |
| 时间戳 | `TIMESTAMP '2026-01-01T12:00:00Z'` | RFC 3339 字符串 |
| NULL | `NULL` | 唯一合法空值字面量 |

## 9. 存储引擎选择

```sql
-- 持久化 + 崩溃恢复（默认）
CREATE TABLE orders (
    id INT64, ...,
    PRIMARY KEY (id)
);                                      -- 等价于 ENGINE=lsm

-- 零 I/O 延迟、重启后清空（临时表/维度表/缓存表）
CREATE TABLE dim_region (
    region_id INT32, ...,
    PRIMARY KEY (region_id)
) ENGINE=memory;
```

引擎路由对 SQL 完全透明：`SELECT/INSERT/UPDATE/DELETE` 走相同的 parser/analyzer/optimizer/executor 管线，仅底层存储介质不同。

## 10. 已知限制与注意事项

> 这些是当前实现的边界；后续 PR 会逐项修复。

| 限制 | 影响 | 临时方案 |
|------|------|----------|
| `IN` / `BETWEEN` 子句不被支持 | `WHERE col IN (...)` / `col BETWEEN a AND b` 返回错误码 | 用 `OR` 链或 `>=` + `<=` 组合替代 |
| `IS NULL` / `IS NOT NULL` 不被支持 | 返回错误码 | 用 `col = NULL`（语义上仍按 NULL 三值逻辑过滤行） |
| `ORDER BY` 不保证排序 | 客户端需自行排序或接受任意顺序 | 加 LIMIT 后客户端排序 |
| `DISTINCT` / `HAVING` 静默丢弃 | 不会报错但无效 | 用 `GROUP BY` 替代 `DISTINCT`；用子查询替代 `HAVING` |
| `EXPLAIN` 仅支持 SELECT | DDL/DML 返回错误 | 无 |
| 多表 `FROM` 不支持 JOIN | parser 不解析 JOIN | 单表查询 |
| 复合主键（多列）创建支持但查询时仍按字符串拼接排序 | 范围/排序可能不符合预期 | 单列主键 + 应用层组合键 |
| `IF EXISTS` 在 `DROP TABLE` 不支持 | 删不存在的表会报错 | 应用层先查询 `SHOW TABLES` |

> 完整模块设计与实现细节见 [architecture.md](architecture.md) / [query.md](query.md) / [dml.md](dml.md) / [storage.md](storage.md)。
