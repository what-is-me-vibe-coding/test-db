package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// ==================== executeAggregate 覆盖率测试 ====================

// TestAggregateEmptyChunks 测试聚合在空输入（无数据行）时的行为。
// 验证无 GROUP BY 时，空输入仍产生一行结果（COUNT=0, SUM/AVG/MIN/MAX=NULL）。
func TestAggregateEmptyChunks(t *testing.T) {
	ms := newMockStorage()
	// 不添加任何数据，ScanNode 返回空 chunks

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	// 同时测试 COUNT、SUM、AVG、MIN、MAX 在空输入上的行为
	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
			{Name: testAggSumAge, Type: common.TypeFloat64, Nullable: true},
			{Name: testAggAvgAge, Type: common.TypeFloat64, Nullable: true},
			{Name: testAggMinAge, Type: common.TypeInt64, Nullable: true},
			{Name: testAggMaxAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("aggregate on empty input: %v", err)
	}

	// 空输入聚合应返回 1 行
	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Fatalf("expected 1 row for aggregate on empty input, got %d", totalRows)
	}

	// COUNT(*) = 0
	countCol, _ := chunks[0].GetColumn(0)
	countVal := countCol.GetValue(0)
	if countVal.Int64 != 0 {
		t.Errorf("expected COUNT(*)=0 on empty input, got %d", countVal.Int64)
	}

	// SUM = NULL
	sumCol, _ := chunks[0].GetColumn(1)
	sumVal := sumCol.GetValue(0)
	if sumVal.Valid {
		t.Errorf("expected NULL for SUM on empty input, got %v", sumVal)
	}

	// AVG = NULL
	avgCol, _ := chunks[0].GetColumn(2)
	avgVal := avgCol.GetValue(0)
	if avgVal.Valid {
		t.Errorf("expected NULL for AVG on empty input, got %v", avgVal)
	}

	// MIN = NULL
	minCol, _ := chunks[0].GetColumn(3)
	minVal := minCol.GetValue(0)
	if minVal.Valid {
		t.Errorf("expected NULL for MIN on empty input, got %v", minVal)
	}

	// MAX = NULL
	maxCol, _ := chunks[0].GetColumn(4)
	maxVal := maxCol.GetValue(0)
	if maxVal.Valid {
		t.Errorf("expected NULL for MAX on empty input, got %v", maxVal)
	}
}

// TestAggregateGroupByAllSameKey 测试 GROUP BY 所有行具有相同分组键的场景。
// 验证只产生一个分组，且聚合值正确。
func TestAggregateGroupByAllSameKey(t *testing.T) {
	ms := newMockStorage()
	// 所有行的 age=30（相同分组键）
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(90.0),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(80.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameCharlie),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(70.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child: scan,
		GroupBy: []Expression{
			&ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64},
		},
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: testColAge, Type: common.TypeInt64, Nullable: true},
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
			{Name: testAggSumScore, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("aggregate group by same key: %v", err)
	}

	// 所有行属于同一分组，应只有 1 行输出
	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 group (all same key), got %d", totalRows)
	}

	// COUNT = 3
	countCol, _ := chunks[0].GetColumn(1)
	countVal := countCol.GetValue(0)
	if countVal.Int64 != 3 {
		t.Errorf("expected COUNT=3, got %d", countVal.Int64)
	}

	// SUM(score) = 240.0
	sumCol, _ := chunks[0].GetColumn(2)
	sumVal := sumCol.GetValue(0)
	if sumVal.Float64 != 240.0 {
		t.Errorf("expected SUM(score)=240.0, got %g", sumVal.Float64)
	}
}

// TestAggregateWithNullArgs 测试聚合参数中包含 NULL 值的场景。
// SUM/AVG/MIN/MAX 应跳过 NULL，COUNT 应统计所有行。
func TestAggregateWithNullArgs(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewNull(), testColScore: common.NewFloat64(90.0),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(20), testColScore: common.NewNull(),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameCharlie),
		testColAge: common.NewInt64(40), testColScore: common.NewFloat64(70.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
			{Name: testAggSumAge, Type: common.TypeFloat64, Nullable: true},
			{Name: testAggAvgScore, Type: common.TypeFloat64, Nullable: true},
			{Name: testAggMinAge, Type: common.TypeInt64, Nullable: true},
			{Name: testAggMaxScore, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("aggregate with null args: %v", err)
	}

	if len(chunks) == 0 || chunks[0].RowCount() == 0 {
		t.Fatal("expected at least 1 row")
	}

	// COUNT(*) = 3（包含 NULL 行）
	countCol, _ := chunks[0].GetColumn(0)
	if countCol.GetValue(0).Int64 != 3 {
		t.Errorf("expected COUNT(*)=3, got %d", countCol.GetValue(0).Int64)
	}

	// SUM(age) = 20+40=60（跳过 NULL）
	sumCol, _ := chunks[0].GetColumn(1)
	sumVal := sumCol.GetValue(0)
	if sumVal.Float64 != 60.0 {
		t.Errorf("expected SUM(age)=60.0 (skip NULL), got %g", sumVal.Float64)
	}

	// AVG(score) = (90+70)/2=80.0（跳过 NULL）
	avgCol, _ := chunks[0].GetColumn(2)
	avgVal := avgCol.GetValue(0)
	if avgVal.Float64 != 80.0 {
		t.Errorf("expected AVG(score)=80.0 (skip NULL), got %g", avgVal.Float64)
	}

	// MIN(age) = 20（跳过 NULL）
	minCol, _ := chunks[0].GetColumn(3)
	minVal := minCol.GetValue(0)
	if minVal.Int64 != 20 {
		t.Errorf("expected MIN(age)=20 (skip NULL), got %d", minVal.Int64)
	}

	// MAX(score) = 90.0（跳过 NULL）
	maxCol, _ := chunks[0].GetColumn(4)
	maxVal := maxCol.GetValue(0)
	if maxVal.Float64 != 90.0 {
		t.Errorf("expected MAX(score)=90.0 (skip NULL), got %g", maxVal.Float64)
	}
}

// TestAggregateAllNullArgs 测试聚合参数全部为 NULL 的场景。
// SUM/AVG/MIN/MAX 应返回 NULL，COUNT 仍统计行数。
func TestAggregateAllNullArgs(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewNull(), testColScore: common.NewNull(),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewNull(), testColScore: common.NewNull(),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
			{Name: testAggSumAge, Type: common.TypeFloat64, Nullable: true},
			{Name: testAggMinAge, Type: common.TypeInt64, Nullable: true},
			{Name: testAggMaxAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("aggregate all null args: %v", err)
	}

	if len(chunks) == 0 || chunks[0].RowCount() == 0 {
		t.Fatal("expected at least 1 row")
	}

	// COUNT(*) = 2
	countCol, _ := chunks[0].GetColumn(0)
	if countCol.GetValue(0).Int64 != 2 {
		t.Errorf("expected COUNT(*)=2, got %d", countCol.GetValue(0).Int64)
	}

	// SUM(age) = NULL（全部为 NULL）
	sumCol, _ := chunks[0].GetColumn(1)
	if sumCol.GetValue(0).Valid {
		t.Errorf("expected NULL for SUM when all values are NULL, got %v", sumCol.GetValue(0))
	}

	// MIN(age) = NULL
	minCol, _ := chunks[0].GetColumn(2)
	if minCol.GetValue(0).Valid {
		t.Errorf("expected NULL for MIN when all values are NULL, got %v", minCol.GetValue(0))
	}

	// MAX(age) = NULL
	maxCol, _ := chunks[0].GetColumn(3)
	if maxCol.GetValue(0).Valid {
		t.Errorf("expected NULL for MAX when all values are NULL, got %v", maxCol.GetValue(0))
	}
}

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

// TestCoerceValueSameTypeV13 测试 coerceValue 在类型相同时直接返回原值。
func TestCoerceValueSameTypeV13(t *testing.T) {
	// int -> int：类型相同，直接返回
	intVal := common.NewInt64(42)
	result := coerceValue(intVal, common.TypeInt64)
	if result.Int64 != 42 {
		t.Errorf("expected same type int64 to pass through, got %d", result.Int64)
	}

	// float -> float：类型相同
	floatVal := common.NewFloat64(3.14)
	result = coerceValue(floatVal, common.TypeFloat64)
	if result.Float64 != 3.14 {
		t.Errorf("expected same type float64 to pass through, got %g", result.Float64)
	}

	// string -> string：类型相同
	strVal := common.NewString("hello")
	result = coerceValue(strVal, common.TypeString)
	if result.Str != "hello" {
		t.Errorf("expected same type string to pass through, got %q", result.Str)
	}
}

// TestCoerceValueUnsupportedConversionV13 测试 coerceValue 不支持的类型转换。
// 不支持的转换应返回原值。
func TestCoerceValueUnsupportedConversionV13(t *testing.T) {
	// string -> int：不支持的转换，返回原值
	const testStrHello = "hello"
	strVal := common.NewString(testStrHello)
	result := coerceValue(strVal, common.TypeInt64)
	if result.Str != testStrHello {
		t.Errorf("expected unsupported conversion to return original value, got %v", result)
	}

	// int -> string：不支持的转换
	intVal := common.NewInt64(42)
	result = coerceValue(intVal, common.TypeString)
	if result.Int64 != 42 {
		t.Errorf("expected unsupported conversion to return original value, got %v", result)
	}
}
