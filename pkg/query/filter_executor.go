package query

import (
	"fmt"

	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// FilterExecutor 对子执行器的输出按条件过滤，仅保留满足条件的行。
type FilterExecutor struct {
	child       Executor
	condition   Expression
	colIndexMap map[string]int
}

// NewFilterExecutor 创建一个 FilterExecutor。
// child 为子执行器，condition 为过滤条件表达式。
func NewFilterExecutor(child Executor, condition Expression) *FilterExecutor {
	return &FilterExecutor{
		child:     child,
		condition: condition,
	}
}

// Schema 返回 FilterExecutor 的输出列定义（与子执行器相同）。
func (f *FilterExecutor) Schema() []ColumnDef {
	return f.child.Schema()
}

// NextChunk 返回下一个满足过滤条件的结果批次。
// 内部从子执行器获取 Chunk，逐行评估条件，仅保留匹配行。
// 如果过滤后的 Chunk 为空，继续获取下一个 Chunk 直到有数据或 EOF。
func (f *FilterExecutor) NextChunk() (*storage.Chunk, error) {
	if f.colIndexMap == nil {
		f.colIndexMap = buildColIndexMap(f.child.Schema())
	}

	for {
		chunk, err := f.child.NextChunk()
		if err != nil {
			return nil, fmt.Errorf("filter executor: child next chunk: %w", err)
		}
		if chunk == nil {
			return nil, nil
		}

		filtered, err := f.filterChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("filter executor: filter chunk: %w", err)
		}
		if filtered != nil {
			return filtered, nil
		}
		// 过滤后为空，继续获取下一个 Chunk
	}
}

// filterChunk 对 Chunk 中的行逐行评估条件，构建仅包含匹配行的新 Chunk。
func (f *FilterExecutor) filterChunk(chunk *storage.Chunk) (*storage.Chunk, error) {
	schema := f.child.Schema()
	rowCount := chunk.RowCount()
	if rowCount == 0 {
		return nil, nil
	}

	// 收集满足条件的行索引
	var selected []uint32
	for i := uint32(0); i < rowCount; i++ {
		val, err := evalExpr(f.condition, chunk, i, f.colIndexMap)
		if err != nil {
			return nil, fmt.Errorf("evaluate condition at row %d: %w", i, err)
		}
		if !val.IsNull() && toBool(val) {
			selected = append(selected, i)
		}
	}

	if len(selected) == 0 {
		return nil, nil
	}

	// 构建新 Chunk，仅包含选中行
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
				return nil, fmt.Errorf("append filtered value: %w", err)
			}
		}
		if err := result.AddColumn(dstCol); err != nil {
			return nil, fmt.Errorf("add filtered column %d: %w", colIdx, err)
		}
	}

	return result, nil
}

// Close 关闭 FilterExecutor 及其子执行器。
func (f *FilterExecutor) Close() {
	f.child.Close()
}
