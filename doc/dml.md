# DML 详解（INSERT / UPDATE / DELETE）

## 1. 概述

WiDB 支持 SELECT、INSERT、UPDATE、DELETE、CREATE TABLE 等 SQL 语句。本文档聚焦数据操作语言（DML）的 UPDATE 与 DELETE 语义，二者由 `pkg/server/handlers_dml.go` 实现，经 `routingAdapter` 透明路由到 LSM 或内存引擎。

所有 DML 均走完整的 parser → analyzer → optimizer → executor 管线，与 SELECT 共享 WHERE 谓词求值（`query.EvalRowPredicate`）。

## 2. INSERT

INSERT 通过 HTTP `/write` 端点或 TCP `PacketWrite` 批量写入，不走 SQL INSERT 语句路径（解析器支持 INSERT VALUES，但生产写入建议用批量 API 以获得 GroupCommit 吞吐）。

```bash
curl -X POST http://localhost:8080/write \
  -H "Content-Type: application/json" \
  -d '{"table": "sensor", "rows": [{"id": 1, "name": "s1", "temperature": 23.5}]}'
```

写入路径：`Catalog.GetTable` → 类型转换 → `engineForTable(table).WriteBatch` → WAL（LSM 引擎）+ MemTable。

## 3. UPDATE

### 语法

```sql
UPDATE table_name
SET col1 = expr1, col2 = expr2, ...
[WHERE condition]
```

### 执行流程（`handleUpdate`）

1. `Catalog.GetTable` 获取表定义与列类型映射
2. `engineForTable(table).ScanRange("", "\xff\xff\xff\xff")` 扫描全表
3. 对每行用 `query.EvalRowPredicate(upd.Where, columns)` 求 WHERE 谓词
4. 匹配行应用 SET 赋值（`applyUpdateAssignments`），按列类型转换字面量
5. 重新写入更新后的行（覆盖旧版本）

### 主键冲突

若 UPDATE 导致主键变更，且新主键已存在，返回冲突错误。检查逻辑复用统一的 `checkPKConflict`（INSERT 与 UPDATE 共享）。

### 示例

```sql
-- 单列更新
UPDATE sensor SET temperature = 24.0 WHERE id = 1;

-- 多列更新
UPDATE sensor SET temperature = 24.0, active = true WHERE id = 1;

-- 无 WHERE 则更新全表
UPDATE sensor SET active = true;
```

## 4. DELETE

### 语法

```sql
DELETE FROM table_name
[WHERE condition]
```

### 执行流程（`handleDelete`）

1. `Catalog.GetTable` 校验表存在
2. `engineForTable(table).ScanRange("", "\xff\xff\xff\xff")` 扫描全表
3. 对每行求 WHERE 谓词
4. 匹配行调用 `engine.Delete(key)` 删除

### 示例

```sql
-- 条件删除
DELETE FROM sensor WHERE active = false;

-- 清空全表数据（保留表结构）
DELETE FROM sensor;
```

> 无 WHERE 的 DELETE 清空全表数据但保留表定义，相当于 TRUNCATE 的效果。

## 5. 引擎差异

| 维度 | LSM 引擎（默认） | 内存引擎（ENGINE=memory） |
|------|------------------|--------------------------|
| UPDATE/DELETE | 支持，写 WAL + MemTable（DELETE 为 tombstone） | 支持，直接修改内存排序数组 |
| 持久化 | 是，WAL 保证崩溃恢复 | 否，重启丢失 |
| 事务性 | 单行原子，批量写经 GroupCommit | 单行原子，持写锁期间完成 |
| 路由 | `routingAdapter` 默认引擎 | `routingAdapter.memEngines[table]` |

两类引擎实现相同的 `TableEngine` 接口（`Write`/`WriteBatch`/`Delete`/`ScanRange`...），DML 代码无需感知引擎差异。

## 6. 注意事项

- UPDATE/DELETE 当前为「扫描 + 过滤 + 重写/删除」模式，无索引加速，适合中小规模数据。大表批量更新建议通过重建表实现。
- WHERE 谓词支持比较运算符（=、!=、<、<=、>、>=）、LIKE、AND/OR/NOT 组合。
- 主键列可被 UPDATE 修改，但需确保新主键不与现有行冲突。
- 内存引擎表的 DML 不写 WAL，操作不可恢复。
