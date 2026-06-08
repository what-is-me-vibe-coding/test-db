package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// ==================== executeFilter 覆盖率测试 ====================

// TestFilterNoMatchAllRows 测试过滤条件不匹配任何行的场景。
// 验证返回空结果（无 chunks）。
func TestFilterNoMatchAllRows(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(20), testColScore: common.NewFloat64(50.0),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(60.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	// age > 1000 不匹配任何行
	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(1000)}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("filter no match: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("expected 0 rows when no rows match filter, got %d", totalRows)
	}
}

// TestFilterEvalExprError 测试过滤条件中表达式求值出错时的行为。
// 使用无效的列引用触发 evalExpr 错误，验证 filterChunk 中 continue 逻辑。
func TestFilterEvalExprError(t *testing.T) {
	// 直接构造 filterChunk 的输入参数来测试 evalExpr 错误路径
	inputSchema := []ColumnDef{
		{Name: testColID, Type: common.TypeInt64, Nullable: false},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
	}

	// 构建一个包含数据的 chunk
	chunk := storage.NewChunk(defaultChunkSize)
	col0 := storage.NewColumnVector(0, common.TypeInt64, 2)
	_ = col0.Append(common.NewInt64(1))
	_ = col0.Append(common.NewInt64(2))
	_ = chunk.AddColumn(col0)

	col1 := storage.NewColumnVector(1, common.TypeInt64, 2)
	_ = col1.Append(common.NewInt64(30))
	_ = col1.Append(common.NewInt64(25))
	_ = chunk.AddColumn(col1)

	colIdxMap := buildColIdxMapFromSchema(inputSchema)

	// 使用一个会导致 evalExpr 错误的条件：FuncExpr 在 evalExpr 中会返回错误
	cond := &FuncExpr{Name: testFuncUnknownFunc, Args: nil}

	output, err := filterChunk(chunk, cond, inputSchema, colIdxMap)
	if err != nil {
		// filterChunk 中 evalExpr 出错会 continue，不应返回错误
		t.Fatalf("filterChunk should not return error for evalExpr errors, got: %v", err)
	}

	// 所有行都因 evalExpr 错误被跳过，结果应为空
	if output.RowCount() != 0 {
		t.Errorf("expected 0 rows when all rows have evalExpr errors, got %d", output.RowCount())
	}
}

// TestFilterOnEmptyChunk 测试对空 chunk 执行过滤。
// 验证返回空 chunk 且不报错。
func TestFilterOnEmptyChunk(t *testing.T) {
	inputSchema := []ColumnDef{
		{Name: testColID, Type: common.TypeInt64, Nullable: false},
	}

	// 构建一个空 chunk（0 行）
	emptyChunk := storage.NewChunk(defaultChunkSize)
	col0 := storage.NewColumnVector(0, common.TypeInt64, 0)
	_ = emptyChunk.AddColumn(col0)

	colIdxMap := buildColIdxMapFromSchema(inputSchema)
	cond := &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColID, Idx: 0, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(0)}}

	output, err := filterChunk(emptyChunk, cond, inputSchema, colIdxMap)
	if err != nil {
		t.Fatalf("filterChunk on empty chunk: %v", err)
	}

	if output.RowCount() != 0 {
		t.Errorf("expected 0 rows from empty chunk filter, got %d", output.RowCount())
	}
}
