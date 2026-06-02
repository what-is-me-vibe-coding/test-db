package query

import (
	"fmt"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// ScanExecutor 从 ScanIterator 读取行数据，按批次打包为 Chunk 输出。
type ScanExecutor struct {
	iter   storage.ScanIterator
	schema []ColumnDef
	closed bool
}

// NewScanExecutor 创建一个 ScanExecutor。
// iter 为底层数据扫描迭代器，schema 为输出列定义。
func NewScanExecutor(iter storage.ScanIterator, schema []ColumnDef) *ScanExecutor {
	return &ScanExecutor{
		iter:   iter,
		schema: schema,
	}
}

// Schema 返回 ScanExecutor 的输出列定义。
func (s *ScanExecutor) Schema() []ColumnDef {
	return s.schema
}

// NextChunk 从迭代器读取最多 defaultChunkSize 行，打包为一个 Chunk 返回。
// 返回 nil 表示数据已读完（EOF）。
func (s *ScanExecutor) NextChunk() (*storage.Chunk, error) {
	if s.closed {
		return nil, nil
	}

	chunk := storage.NewChunk(defaultChunkSize)
	for i, col := range s.schema {
		cv := storage.NewColumnVector(uint32(i), col.Type, defaultChunkSize)
		if err := chunk.AddColumn(cv); err != nil {
			return nil, fmt.Errorf("scan executor: add column %s: %w", col.Name, err)
		}
	}

	rowCount := uint32(0)
	for rowCount < defaultChunkSize {
		if !s.iter.Next() {
			if err := s.iter.Err(); err != nil {
				return nil, fmt.Errorf("scan executor: iterator error: %w", err)
			}
			s.closed = true
			break
		}
		if err := s.iter.Err(); err != nil {
			return nil, fmt.Errorf("scan executor: iterator error: %w", err)
		}
		entry := s.iter.Entry()
		if err := s.appendRow(chunk, entry.Value.Columns); err != nil {
			return nil, fmt.Errorf("scan executor: append row: %w", err)
		}
		rowCount++
	}

	if rowCount == 0 {
		return nil, nil
	}
	return chunk, nil
}

// appendRow 将一行数据追加到 Chunk 中。
func (s *ScanExecutor) appendRow(chunk *storage.Chunk, rowCols map[string]common.Value) error {
	values := make([]common.Value, len(s.schema))
	for i, col := range s.schema {
		val, ok := rowCols[col.Name]
		if !ok {
			val = common.NewNull()
		}
		values[i] = val
	}
	if err := chunk.AppendRow(values); err != nil {
		return fmt.Errorf("append row: %w", err)
	}
	return nil
}

// Close 关闭 ScanExecutor，释放底层迭代器。
func (s *ScanExecutor) Close() {
	if !s.closed {
		s.iter.Close()
		s.closed = true
	}
}
