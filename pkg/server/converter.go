package server

import (
	"fmt"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// interfaceToValue 将 any 转换为 common.Value。
func interfaceToValue(raw any, typ common.DataType) (common.Value, error) {
	if raw == nil {
		return common.NewNull(), nil
	}

	switch typ {
	case common.TypeBool:
		v, ok := raw.(bool)
		if !ok {
			return common.NewNull(), fmt.Errorf("%w: expected bool, got %T", common.ErrTypeMismatch, raw)
		}
		return common.NewBool(v), nil
	case common.TypeInt64, common.TypeInt8, common.TypeInt16, common.TypeInt32, common.TypeUint64:
		iv, err := toInt64Value(raw)
		if err != nil {
			return iv, err
		}
		return common.NewIntFamilyValue(typ, iv.Int64), nil
	case common.TypeDate:
		return toDateValue(raw)
	case common.TypeFloat64:
		return toFloat64Value(raw)
	case common.TypeString:
		v, ok := raw.(string)
		if !ok {
			return common.NewNull(), fmt.Errorf("%w: expected string, got %T", common.ErrTypeMismatch, raw)
		}
		return common.NewString(v), nil
	case common.TypeTimestamp:
		return toTimestampValue(raw)
	default:
		return common.NewNull(), fmt.Errorf("不支持的数据类型: %s", typ)
	}
}

// toInt64Value 将 any 转换为 INT64 Value。
func toInt64Value(raw any) (common.Value, error) {
	switch v := raw.(type) {
	case float64:
		return common.NewInt64(int64(v)), nil
	case int64:
		return common.NewInt64(v), nil
	case int:
		return common.NewInt64(int64(v)), nil
	default:
		return common.NewNull(), fmt.Errorf("%w: expected int64, got %T", common.ErrTypeMismatch, raw)
	}
}

// toFloat64Value 将 any 转换为 FLOAT64 Value。
func toFloat64Value(raw any) (common.Value, error) {
	switch v := raw.(type) {
	case float64:
		return common.NewFloat64(v), nil
	case int64:
		return common.NewFloat64(float64(v)), nil
	case int:
		return common.NewFloat64(float64(v)), nil
	default:
		return common.NewNull(), fmt.Errorf("%w: expected float64, got %T", common.ErrTypeMismatch, raw)
	}
}

// toTimestampValue 将 any 转换为 TIMESTAMP Value。
func toTimestampValue(raw any) (common.Value, error) {
	v, ok := raw.(string)
	if !ok {
		return common.NewNull(), fmt.Errorf("%w: expected timestamp string, got %T",
			common.ErrTypeMismatch, raw)
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return common.NewNull(), fmt.Errorf("解析时间戳: %w", err)
	}
	return common.NewTimestamp(t), nil
}

// toDateValue 将 any 转换为 DATE Value。
// 接受 "YYYY-MM-DD" 格式字符串或 int64（自 1970-01-01 起的天数）。
func toDateValue(raw any) (common.Value, error) {
	switch v := raw.(type) {
	case string:
		t, err := time.Parse(common.DateFormat(), v)
		if err != nil {
			return common.NewNull(), fmt.Errorf("解析日期: %w", err)
		}
		return common.NewDateFromTime(t), nil
	case float64:
		return common.NewDate(int64(v)), nil
	case int64:
		return common.NewDate(v), nil
	case int:
		return common.NewDate(int64(v)), nil
	default:
		return common.NewNull(), fmt.Errorf("%w: expected date string or int64, got %T",
			common.ErrTypeMismatch, raw)
	}
}

// chunksToRows 将 Chunk 切片转换为可 JSON 序列化的行数据。
// colNames 为每列的名称，按列索引顺序排列。若 colNames 为空则回退到 col_N 格式。
// 预分配 result 切片容量，避免追加时的反复扩容。
//
// 性能优化：每个 Chunk 的列向量与列名只解析一次，避免在逐行遍历中重复调用
// GetColumn（含边界检查与错误返回）与 columnName。行 map 按列数预分配容量，
// 消除插入时的 rehash。列遍历改为直接 range 切片，跳过逐次索引边界检查。
func chunksToRows(chunks []*storage.Chunk, colNames []string) []map[string]any {
	totalRows := countRows(chunks)
	if totalRows == 0 {
		return nil
	}
	result := make([]map[string]any, 0, totalRows)
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		cols := chunk.Columns()
		colCount := len(cols)
		if colCount == 0 {
			continue
		}
		// 预计算每列名称，避免逐行重复解析
		names := make([]string, colCount)
		for i := 0; i < colCount; i++ {
			names[i] = columnName(colNames, i)
		}
		rowCount := chunk.RowCount()
		for i := uint32(0); i < rowCount; i++ {
			// 按列数预分配 map 容量，避免渐进式扩容
			rowMap := make(map[string]any, colCount)
			for colIdx, col := range cols {
				if i < col.Len() {
					rowMap[names[colIdx]] = valueToInterface(col.GetValue(i))
				}
			}
			result = append(result, rowMap)
		}
	}
	return result
}

// columnName 解析列名，优先使用 colNames 中指定名称，否则回退到 col_N 格式。
func columnName(colNames []string, colIdx int) string {
	if colNames != nil && colIdx < len(colNames) && colNames[colIdx] != "" {
		return colNames[colIdx]
	}
	return fmt.Sprintf("col_%d", colIdx)
}

// valueToInterface 将 common.Value 转换为 any。
func valueToInterface(v common.Value) any {
	if !v.Valid {
		return nil
	}
	switch v.Typ {
	case common.TypeBool:
		return v.Int64 != 0
	case common.TypeInt64, common.TypeInt8, common.TypeInt16, common.TypeInt32, common.TypeUint64:
		return v.Int64
	case common.TypeDate:
		return v.String()
	case common.TypeFloat64:
		return v.Float64
	case common.TypeString:
		return v.Str
	case common.TypeTimestamp:
		return v.Time.Format(time.RFC3339Nano)
	default:
		return nil
	}
}

// countRows 统计 Chunk 切片中的总行数。
func countRows(chunks []*storage.Chunk) int {
	total := 0
	for _, chunk := range chunks {
		if chunk != nil {
			total += int(chunk.RowCount())
		}
	}
	return total
}
