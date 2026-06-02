package query

import (
	"fmt"

	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// LimitExecutor 对子执行器的输出进行 LIMIT/OFFSET 控制。
type LimitExecutor struct {
	child      Executor
	offset     uint64
	count      uint64
	rowsSeen   uint64
	rowsOutput uint64
	finished   bool
}

// NewLimitExecutor 创建一个 LimitExecutor。
// offset 为跳过的行数，count 为最多返回的行数。
func NewLimitExecutor(child Executor, offset, count uint64) *LimitExecutor {
	return &LimitExecutor{
		child:  child,
		offset: offset,
		count:  count,
	}
}

// Schema 返回 LimitExecutor 的输出列定义（与子执行器相同）。
func (l *LimitExecutor) Schema() []ColumnDef {
	return l.child.Schema()
}

// NextChunk 返回下一个受 LIMIT/OFFSET 限制的结果批次。
func (l *LimitExecutor) NextChunk() (*storage.Chunk, error) {
	if l.finished {
		return nil, nil
	}
	if l.count == 0 {
		l.finished = true
		return nil, nil
	}

	for {
		chunk, err := l.child.NextChunk()
		if err != nil {
			return nil, fmt.Errorf("limit executor: child next chunk: %w", err)
		}
		if chunk == nil {
			l.finished = true
			return nil, nil
		}

		result, err := l.applyLimit(chunk)
		if err != nil {
			return nil, fmt.Errorf("limit executor: apply limit: %w", err)
		}
		if result != nil {
			return result, nil
		}
		// 结果为空（整个 chunk 都被跳过），继续获取
	}
}

// applyLimit 对 Chunk 应用 OFFSET 和 LIMIT 限制。
func (l *LimitExecutor) applyLimit(chunk *storage.Chunk) (*storage.Chunk, error) {
	rowCount := chunk.RowCount()
	if rowCount == 0 {
		return nil, nil
	}

	// 计算本 Chunk 中需要跳过和保留的行
	var selected []uint32
	for i := uint32(0); i < rowCount; i++ {
		l.rowsSeen++
		if l.rowsSeen <= l.offset {
			continue // 跳过 OFFSET 行
		}
		if l.rowsOutput >= l.count {
			l.finished = true
			break
		}
		selected = append(selected, i)
		l.rowsOutput++
	}

	if len(selected) == 0 {
		return nil, nil
	}

	// 构建新 Chunk
	schema := l.child.Schema()
	result := storage.NewChunk(uint32(len(selected)))
	for colIdx, colDef := range schema {
		srcCol, err := chunk.GetColumn(colIdx)
		if err != nil {
			return nil, fmt.Errorf("get source column %d: %w", colIdx, err)
		}
		dstCol := storage.NewColumnVector(uint32(colIdx), colDef.Type, uint32(len(selected)))
		for _, rowIdx := range selected {
			val := srcCol.GetValue(rowIdx)
			if err := dstCol.Append(val); err != nil {
				return nil, fmt.Errorf("append limited value: %w", err)
			}
		}
		if err := result.AddColumn(dstCol); err != nil {
			return nil, fmt.Errorf("add limited column %d: %w", colIdx, err)
		}
	}

	return result, nil
}

// Close 关闭 LimitExecutor 及其子执行器。
func (l *LimitExecutor) Close() {
	l.child.Close()
}
