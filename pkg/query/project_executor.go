package query

import (
	"fmt"

	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// ProjectExecutor 对子执行器的输出进行投影，按表达式列表选择/计算输出列。
type ProjectExecutor struct {
	child       Executor
	expressions []Expression
	aliases     []string
	schema      []ColumnDef
	colIndexMap map[string]int
}

// NewProjectExecutor 创建一个 ProjectExecutor。
// child 为子执行器，expressions 为投影表达式列表，
// aliases 为对应的列别名，schema 为输出列定义。
func NewProjectExecutor(child Executor, expressions []Expression, aliases []string, schema []ColumnDef) *ProjectExecutor {
	return &ProjectExecutor{
		child:       child,
		expressions: expressions,
		aliases:     aliases,
		schema:      schema,
	}
}

// Schema 返回 ProjectExecutor 的输出列定义。
func (p *ProjectExecutor) Schema() []ColumnDef {
	return p.schema
}

// NextChunk 返回下一个投影后的结果批次。
func (p *ProjectExecutor) NextChunk() (*storage.Chunk, error) {
	if p.colIndexMap == nil {
		p.colIndexMap = buildColIndexMap(p.child.Schema())
	}

	chunk, err := p.child.NextChunk()
	if err != nil {
		return nil, fmt.Errorf("project executor: child next chunk: %w", err)
	}
	if chunk == nil {
		return nil, nil
	}

	return p.projectChunk(chunk)
}

// projectChunk 对 Chunk 中的每行求值投影表达式，构建新 Chunk。
func (p *ProjectExecutor) projectChunk(chunk *storage.Chunk) (*storage.Chunk, error) {
	rowCount := chunk.RowCount()
	result := storage.NewChunk(rowCount)

	for exprIdx, expr := range p.expressions {
		colDef := p.schema[exprIdx]
		dstCol := storage.NewColumnVector(uint32(exprIdx), colDef.Type, rowCount)

		for i := uint32(0); i < rowCount; i++ {
			val, err := evalExpr(expr, chunk, i, p.colIndexMap)
			if err != nil {
				return nil, fmt.Errorf("project executor: eval expr %d at row %d: %w", exprIdx, i, err)
			}
			if err := dstCol.Append(val); err != nil {
				return nil, fmt.Errorf("project executor: append value: %w", err)
			}
		}

		if err := result.AddColumn(dstCol); err != nil {
			return nil, fmt.Errorf("project executor: add column %d: %w", exprIdx, err)
		}
	}

	return result, nil
}

// Close 关闭 ProjectExecutor 及其子执行器。
func (p *ProjectExecutor) Close() {
	p.child.Close()
}
