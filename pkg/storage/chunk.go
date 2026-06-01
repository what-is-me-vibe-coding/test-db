package storage

import (
	"fmt"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// Chunk 是一批行数据的向量化集合。
// 按列组织，每列一个 ColumnVector，所有列行数一致。
type Chunk struct {
	columns  []*ColumnVector
	rowCount uint32
	capacity uint32
}

// NewChunk 创建一个指定容量的 Chunk。
func NewChunk(capacity uint32) *Chunk {
	if capacity == 0 {
		capacity = defaultColumnCapacity
	}
	return &Chunk{
		capacity: capacity,
	}
}

// AddColumn 向 Chunk 中添加一列。
func (c *Chunk) AddColumn(col *ColumnVector) error {
	if c.rowCount > 0 && col.Len() != c.rowCount {
		return fmt.Errorf("chunk add column: column length %d does not match row count %d",
			col.Len(), c.rowCount)
	}
	if c.rowCount == 0 {
		c.rowCount = col.Len()
	}
	c.columns = append(c.columns, col)
	return nil
}

// AppendRow 向 Chunk 末尾追加一行数据。
func (c *Chunk) AppendRow(values []common.Value) error {
	if len(values) != len(c.columns) {
		return fmt.Errorf("chunk append row: values count %d != columns count %d",
			len(values), len(c.columns))
	}
	for i, v := range values {
		if err := c.columns[i].Append(v); err != nil {
			return fmt.Errorf("chunk append row column %d: %w", i, err)
		}
	}
	c.rowCount++
	return nil
}

// GetColumn 按索引获取指定列。
func (c *Chunk) GetColumn(idx int) (*ColumnVector, error) {
	if idx < 0 || idx >= len(c.columns) {
		return nil, fmt.Errorf("chunk get column: index %d out of range [0, %d)",
			idx, len(c.columns))
	}
	return c.columns[idx], nil
}

// ColumnCount 返回 Chunk 中的列数。
func (c *Chunk) ColumnCount() int {
	return len(c.columns)
}

// RowCount 返回 Chunk 中的行数。
func (c *Chunk) RowCount() uint32 {
	return c.rowCount
}

// Capacity 返回 Chunk 的容量。
func (c *Chunk) Capacity() uint32 {
	return c.capacity
}

// Reset 重置 Chunk，清空所有列的数据但保留列结构。
func (c *Chunk) Reset() {
	for _, col := range c.columns {
		col.Reset()
	}
	c.rowCount = 0
}

// Clear 重置 Chunk 并清空所有列。
func (c *Chunk) Clear() {
	c.columns = nil
	c.rowCount = 0
}

// GetRow 返回指定行中所有列的值。
func (c *Chunk) GetRow(rowIdx uint32) ([]common.Value, error) {
	if rowIdx >= c.rowCount {
		return nil, fmt.Errorf("chunk get row: row index %d out of range [0, %d)",
			rowIdx, c.rowCount)
	}
	row := make([]common.Value, len(c.columns))
	for i, col := range c.columns {
		row[i] = col.GetValue(rowIdx)
	}
	return row, nil
}

// Columns 返回所有列向量的切片。
func (c *Chunk) Columns() []*ColumnVector {
	return c.columns
}
