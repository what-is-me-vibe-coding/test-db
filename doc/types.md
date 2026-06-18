# 数据类型参考

## 1. 概述

WiDB 支持 10 种数据类型，定义于 `pkg/common/types.go`。类型系统围绕 `DataType` 枚举与 `Value` 结构体组织：

```go
type DataType int

type Value struct {
    Typ     DataType
    Valid   bool // false 表示 NULL
    Int64   int64
    Float64 float64
    Str     string
    Time    time.Time
}
```

## 2. 类型清单

| 类型 | 常量 | 内存大小 | SQL 别名 | 取值范围 / 说明 |
|------|------|----------|----------|-----------------|
| `BOOL` | `TypeBool` | 1 字节 | BOOLEAN, TINYINT | true / false |
| `INT8` | `TypeInt8` | 8 字节* | TINYINT UNSIGNED | 整数族，按 int64 存储 |
| `INT16` | `TypeInt16` | 8 字节* | SMALLINT | 整数族，按 int64 存储 |
| `INT32` | `TypeInt32` | 8 字节* | MEDIUMINT | 整数族，按 int64 存储 |
| `INT64` | `TypeInt64` | 8 字节 | BIGINT, INT | -2^63 ~ 2^63-1 |
| `UINT64` | `TypeUint64` | 8 字节 | BIGINT UNSIGNED | 整数族，按 int64 存储 |
| `FLOAT64` | `TypeFloat64` | 8 字节 | DOUBLE, FLOAT | IEEE 754 双精度 |
| `STRING` | `TypeString` | 变长 | TEXT, VARCHAR, CHAR | UTF-8 字符串 |
| `DATE` | `TypeDate` | 8 字节* | DATE | 自 1970-01-01 起的天数，显示为 "YYYY-MM-DD" |
| `TIMESTAMP` | `TypeTimestamp` | 8 字节 | TIMESTAMP, DATETIME | time.Time，显示为 RFC3339Nano |

> \* 整数族（INT8/16/32/UINT64/DATE）统一以 `Value.Int64` 字段存储，故内存固定 8 字节。

## 3. 整数族（Int Family）

`DataType.IsIntFamily()` 报告一个类型是否属于整数族：INT64、INT8、INT16、INT32、UINT64、DATE 均返回 true。

整数族共享：

- **存储**：`ColumnVector` 的 `int64s` 数组，统一 8 字节/值
- **编码**：Plain / RLE 编码路径
- **统计**：Min/Max 按 int64 计算
- **比较**：跨整数族类型按 int64 字段比较，使 `WHERE int8_col = 5`（字面量为 INT64）能正确命中

仅在类型标签、显示格式与语义取值范围上存在差异。例如 INT8 在应用层表示 8 位整数，但底层存储仍为 int64。

## 4. SQL 类型映射

建表时的列类型经 `query/parser_convert.go` 的 `convertColumnType` 映射为 WiDB 类型：

| SQL 类型 | WiDB 类型 | 备注 |
|----------|-----------|------|
| BIGINT | INT64 | 无符号变体 → UINT64 |
| INT | INT64 | |
| SMALLINT | INT16 | |
| MEDIUMINT | INT32 | |
| TINYINT | BOOL | MySQL 约定；无符号变体 → INT8 |
| TINYINT UNSIGNED | INT8 | |
| BIGINT UNSIGNED | UINT64 | |
| BOOLEAN | BOOL | |
| DOUBLE, FLOAT | FLOAT64 | |
| TEXT, VARCHAR, CHAR | STRING | |
| DATE | DATE | |
| TIMESTAMP, DATETIME | TIMESTAMP | |

## 5. DATE 类型

DATE 以自 1970-01-01 起的天数存储于 int64 字段（UTC）：

- `dateToDays(t time.Time) int64`：time.Time → 天数
- `daysToDate(days int64) time.Time`：天数 → UTC 午夜 time.Time
- 显示格式：`"2006-01-02"`（`common.DateFormat()`）
- 创建值：`common.NewDate(days)` 或 `common.NewDateFromTime(t)`

## 6. NULL 语义

每个 `Value` 通过 `Valid` 字段表示是否为 NULL：

- `Valid == false` 即 NULL，`IsNull()` 返回 true
- `NewNull()` 创建 NULL 值
- 建表时列默认可空（`Nullable = true`），`NOT NULL` 约束使列不可空
- 比较：NULL 与任何值（含 NULL）的 `<`、`=` 比较均返回 false，符合 SQL 三值逻辑

## 7. 值构造与比较

`pkg/common/types.go` 为每种类型提供 `NewXxx` 构造函数：

```go
common.NewBool(true)
common.NewInt64(42)
common.NewInt8(8)        // 整数族统一接收 int64
common.NewDateFromTime(time.Now())
common.NewString("hello")
common.NewNull()
```

`Value.Equal` / `Value.Less` 实现跨类型比较：

- 整数族之间按 int64 比较
- 同类型按各自字段比较（FLOAT64 按 float64，STRING 按字典序，TIMESTAMP 按 time.Time）
- 类型不同（且非整数族）时比较返回 false
