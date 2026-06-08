package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// ============================================================================
// executeFilter 覆盖率提升测试 (84.6% → >90%)
// 未覆盖路径：子节点错误传播 (line 13-15)、filterChunk 错误传播 (line 23-25)
// ============================================================================

// TestExecuteFilter_ChildError 测试 executeFilter 子节点返回错误时的传播路径。
func TestExecuteFilter_ChildError(t *testing.T) {
	exec := NewExecutor(newMockStorage())
	// 使用 nil 作为子节点，executeNode 会返回 "unsupported plan node type" 错误
	filter := &FilterNode{
		Child:     nil,
		Condition: &LiteralExpr{Value: common.NewBool(true)},
	}
	_, err := exec.executeFilter(filter)
	if err == nil {
		t.Error("期望子节点为 nil 时返回错误，得到 nil")
	}
}

// TestExecuteFilter_MultipleChunksSomeFiltered 测试 executeFilter 处理多个 chunk，
// 其中部分 chunk 过滤后行数为 0（覆盖 output.RowCount() > 0 的 false 分支）。
func TestExecuteFilter_MultipleChunksSomeFiltered(t *testing.T) {
	ms := newMockStorage()
	// 添加多条数据，确保生成至少一个 chunk
	for i := 0; i < 5; i++ {
		ms.addEntry(fmtKey(i), map[string]common.Value{
			testColID:    common.NewInt64(int64(i)),
			testColName:  common.NewString("name"),
			testColAge:   common.NewInt64(int64(i * 10)),
			testColScore: common.NewFloat64(float64(i * 10)),
		})
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	// 条件：age > 100，所有行都不满足，所有 chunk 过滤后行数为 0
	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(100)}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("executeFilter 多 chunk 过滤: %v", err)
	}
	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("期望 0 行（全部被过滤），得到 %d", totalRows)
	}
}

// TestExecuteFilter_FilterChunkTypeNull 测试 filterChunk 处理 TypeNull 列。
// TypeNull 列的 GetValue 总是返回 NULL，isTruthyValue(NULL) = false。
func TestExecuteFilter_FilterChunkTypeNull(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColID, Type: common.TypeInt64, Nullable: false},
	}
	colIdxMap := buildColIdxMapFromSchema(schema)

	chunk := storage.NewChunk(defaultChunkSize)
	nullCol := storage.NewColumnVector(0, common.TypeNull, 1)
	_ = chunk.AddColumn(nullCol)

	cond := &LiteralExpr{Value: common.NewBool(true)}
	output, err := filterChunk(chunk, cond, schema, colIdxMap)
	if err != nil {
		t.Fatalf("filterChunk TypeNull 列不应返回错误: %v", err)
	}
	if output.RowCount() != 0 {
		t.Errorf("期望 0 行（NULL 条件不满足），得到 %d", output.RowCount())
	}
}

// TestExecuteFilter_FilterChunkWithSelection 测试 filterChunk 条件过滤正常路径。
func TestExecuteFilter_FilterChunkWithSelection(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColID, Type: common.TypeInt64, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
	}
	colIdxMap := buildColIdxMapFromSchema(schema)

	chunk := storage.NewChunk(defaultChunkSize)
	idCol := storage.NewColumnVector(0, common.TypeInt64, 2)
	_ = idCol.Append(common.NewInt64(1))
	_ = idCol.Append(common.NewInt64(2))
	_ = chunk.AddColumn(idCol)

	// 条件：id = 1，只有第一行满足
	cond := &BinaryExpr{Op: OpEq, Left: &ColumnExpr{Name: testColID}, Right: &LiteralExpr{Value: common.NewInt64(1)}}
	output, err := filterChunk(chunk, cond, schema, colIdxMap)
	if err != nil {
		t.Fatalf("filterChunk 条件过滤: %v", err)
	}
	if output.RowCount() != 1 {
		t.Errorf("期望 1 行，得到 %d", output.RowCount())
	}
}

// ============================================================================
// executeProject 覆盖率提升测试 (85.7% → >90%)
// 未覆盖路径：子节点错误传播 (line 92-94)、projectChunk 错误传播 (line 103-105)
// ============================================================================

// TestExecuteProject_ChildError 测试 executeProject 子节点返回错误时的传播路径。
func TestExecuteProject_ChildError(t *testing.T) {
	exec := NewExecutor(newMockStorage())
	proj := &ProjectNode{
		Child:       nil, // nil 子节点会导致 executeNode 返回错误
		Expressions: []Expression{&LiteralExpr{Value: common.NewInt64(1)}},
		schema:      []ColumnDef{{Name: "const", Type: common.TypeInt64}},
	}
	_, err := exec.executeProject(proj)
	if err == nil {
		t.Error("期望子节点为 nil 时返回错误，得到 nil")
	}
}

// TestExecuteProject_ProjectChunkEvalError 测试 executeProject 中 projectChunk 的
// evalExpr 错误传播路径。
func TestExecuteProject_ProjectChunkEvalError(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}},
	}

	// 使用不支持的表达式类型，projectChunk 中 evalExpr 会返回错误
	proj := &ProjectNode{
		Child:       scan,
		Expressions: []Expression{&unsupportedExpr{}},
		schema:      []ColumnDef{{Name: "bad", Type: common.TypeInt64}},
	}

	exec := NewExecutor(ms)
	_, err := exec.Execute(proj)
	if err == nil {
		t.Error("期望 projectChunk evalExpr 错误传播，得到 nil")
	}
}

// TestExecuteProject_EmptyChunksAfterProjection 测试 executeProject 处理空投影结果。
func TestExecuteProject_EmptyChunksAfterProjection(t *testing.T) {
	ms := newMockStorage()
	// 不添加任何数据

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}},
	}

	proj := &ProjectNode{
		Child:       scan,
		Expressions: []Expression{&ColumnExpr{Name: testColID}},
		schema:      []ColumnDef{{Name: testColID, Type: common.TypeInt64}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(proj)
	if err != nil {
		t.Fatalf("executeProject 空输入: %v", err)
	}
	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("期望 0 行，得到 %d", totalRows)
	}
}

// ============================================================================
// executeAggregate 覆盖率提升测试 (84.6% → >90%)
// 未覆盖路径：子节点错误传播 (line 97-99)、AddColumn 错误 (line 111-113)、
// accumulator.result() 默认分支 (line 91)
// ============================================================================

// TestExecuteAggregate_ChildError 测试 executeAggregate 子节点返回错误时的传播路径。
func TestExecuteAggregate_ChildError(t *testing.T) {
	exec := NewExecutor(newMockStorage())
	agg := &AggregateNode{
		Child:      nil, // nil 子节点会导致 executeNode 返回错误
		Aggregates: []AggregateExpr{{Func: AggCount, Arg: &StarExpr{}}},
		schema:     []ColumnDef{{Name: testAggCountStar, Type: common.TypeInt64}},
	}
	_, err := exec.executeAggregate(agg)
	if err == nil {
		t.Error("期望子节点为 nil 时返回错误，得到 nil")
	}
}

// TestAccumulatorResult_DefaultCase 测试 accumulator.result() 中未知聚合函数类型的默认分支。
func TestAccumulatorResult_DefaultCase(t *testing.T) {
	acc := accumulator{funcType: AggregateFunc(99)} // 未知聚合函数类型
	result := acc.result()
	if result.Valid {
		t.Error("期望未知聚合函数返回 NULL，得到有效值")
	}
}

// TestExecuteAggregate_MinMaxWithNulls 测试 MIN/MAX 聚合在有 NULL 值时的行为。
func TestExecuteAggregate_MinMaxWithNulls(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColScore: common.NewNull(),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColScore: common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColScore},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColScore, Type: common.TypeFloat64}},
	}

	// 测试 MIN
	aggMin := &AggregateNode{
		Child: scan,
		Aggregates: []AggregateExpr{
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 1, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{{Name: testAggMinScore, Type: common.TypeFloat64}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(aggMin)
	if err != nil {
		t.Fatalf("executeAggregate MIN: %v", err)
	}
	col, _ := chunks[0].GetColumn(0)
	minVal := col.GetValue(0)
	if !minVal.Valid || minVal.Float64 != 72.0 {
		t.Errorf("期望 MIN(score) = 72.0（跳过 NULL），得到 %v", minVal)
	}

	// 测试 MAX
	aggMax := &AggregateNode{
		Child: scan,
		Aggregates: []AggregateExpr{
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 1, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{{Name: testAggMaxScore, Type: common.TypeFloat64}},
	}

	chunks, err = exec.Execute(aggMax)
	if err != nil {
		t.Fatalf("executeAggregate MAX: %v", err)
	}
	col, _ = chunks[0].GetColumn(0)
	maxVal := col.GetValue(0)
	if !maxVal.Valid || maxVal.Float64 != 88.0 {
		t.Errorf("期望 MAX(score) = 88.0（跳过 NULL），得到 %v", maxVal)
	}
}

// TestExecuteAggregate_AllNullValues 测试 SUM/AVG/MIN/MAX 在所有值都为 NULL 时的行为。
func TestExecuteAggregate_AllNullValues(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColScore: common.NewNull(),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColScore: common.NewNull(),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColScore},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColScore, Type: common.TypeFloat64}},
	}

	tests := []struct {
		name    string
		aggFunc AggregateFunc
		aggName string
	}{
		{"SUM全NULL", AggSum, testAggSumScore},
		{"AVG全NULL", AggAvg, testAggAvgScore},
		{"MIN全NULL", AggMin, testAggMinScore},
		{"MAX全NULL", AggMax, testAggMaxScore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &AggregateNode{
				Child: scan,
				Aggregates: []AggregateExpr{
					{Func: tt.aggFunc, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 1, typ: common.TypeFloat64}},
				},
				schema: []ColumnDef{{Name: tt.aggName, Type: common.TypeFloat64}},
			}

			exec := NewExecutor(ms)
			chunks, err := exec.Execute(agg)
			if err != nil {
				t.Fatalf("executeAggregate %s: %v", tt.name, err)
			}
			if len(chunks) == 0 {
				t.Fatal("期望至少 1 个 chunk")
			}
			col, _ := chunks[0].GetColumn(0)
			val := col.GetValue(0)
			if val.Valid {
				t.Errorf("期望 %s 全 NULL 时返回 NULL，得到 %v", tt.name, val)
			}
		})
	}
}

// TestExecuteAggregate_GroupByWithAggregate 测试 GROUP BY 结合多个聚合函数。
func TestExecuteAggregate_GroupByWithAggregate(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.0),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(85.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameCharlie),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(70.0),
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
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: testColAge, Type: common.TypeInt64},
			{Name: testAggCountStar, Type: common.TypeInt64},
			{Name: testAggSumScore, Type: common.TypeFloat64},
			{Name: testAggAvgScore, Type: common.TypeFloat64},
			{Name: testAggMinScore, Type: common.TypeFloat64},
			{Name: testAggMaxScore, Type: common.TypeFloat64},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("executeAggregate GROUP BY 多聚合: %v", err)
	}
	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("期望 2 个分组，得到 %d", totalRows)
	}
}

// ============================================================================
// 其他低覆盖率路径补充测试
// ============================================================================

// TestExecuteLimit_ChildError 测试 executeLimit 子节点返回错误时的传播路径。
func TestExecuteLimit_ChildError(t *testing.T) {
	exec := NewExecutor(newMockStorage())
	limit := &LimitNode{
		Child:  nil, // nil 子节点会导致 executeNode 返回错误
		Count:  10,
		Offset: 0,
	}
	_, err := exec.executeLimit(limit)
	if err == nil {
		t.Error("期望子节点为 nil 时返回错误，得到 nil")
	}
}

// TestProjectChunk_NormalPath 测试 projectChunk 正常投影路径。
func TestProjectChunk_NormalPath(t *testing.T) {
	inputSchema := []ColumnDef{
		{Name: testColID, Type: common.TypeInt64, Nullable: false},
	}
	outputSchema := []ColumnDef{
		{Name: testColID, Type: common.TypeInt64, Nullable: false},
	}
	colIdxMap := buildColIdxMapFromSchema(inputSchema)

	chunk := storage.NewChunk(defaultChunkSize)
	col := storage.NewColumnVector(0, common.TypeInt64, 2)
	_ = col.Append(common.NewInt64(1))
	_ = col.Append(common.NewInt64(2))
	_ = chunk.AddColumn(col)

	exprs := []Expression{&ColumnExpr{Name: testColID}}
	output, err := projectChunk(chunk, exprs, inputSchema, outputSchema, colIdxMap)
	if err != nil {
		t.Fatalf("projectChunk 正常路径不应出错: %v", err)
	}
	if output.RowCount() != 2 {
		t.Errorf("期望 2 行，得到 %d", output.RowCount())
	}
}

// TestFillRowVals_ReuseMap 测试 fillRowVals 复用已有 map 时正确清除旧值。
func TestFillRowVals_ReuseMap(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColID, Type: common.TypeInt64},
		{Name: testColName, Type: common.TypeString},
	}

	chunk := storage.NewChunk(defaultChunkSize)
	idCol := storage.NewColumnVector(0, common.TypeInt64, 2)
	_ = idCol.Append(common.NewInt64(1))
	_ = idCol.Append(common.NewInt64(2))
	_ = chunk.AddColumn(idCol)

	nameCol := storage.NewColumnVector(1, common.TypeString, 2)
	_ = nameCol.Append(common.NewString(testNameAlice))
	_ = nameCol.Append(common.NewString(testNameBob))
	_ = chunk.AddColumn(nameCol)

	rowVals := make(map[string]common.Value)
	// 先填充第一行
	fillRowVals(rowVals, chunk.Columns(), schema, 0)
	if rowVals[testColID].Int64 != 1 {
		t.Errorf("期望 id=1，得到 %d", rowVals[testColID].Int64)
	}

	// 复用 map 填充第二行，旧值应被清除
	fillRowVals(rowVals, chunk.Columns(), schema, 1)
	if rowVals[testColID].Int64 != 2 {
		t.Errorf("期望 id=2（复用后更新），得到 %d", rowVals[testColID].Int64)
	}
	if len(rowVals) != 2 {
		t.Errorf("期望 2 个键值对，得到 %d", len(rowVals))
	}
}

// TestCoerceValue_Float64ToInt64 测试 Float64 转 Int64 的类型强制转换。
func TestCoerceValue_Float64ToInt64(t *testing.T) {
	result := coerceValue(common.NewFloat64(42.7), common.TypeInt64)
	if result.Int64 != 42 {
		t.Errorf("期望 float64->int64 截断为 42，得到 %d", result.Int64)
	}
}

// TestCoerceValue_BoolToTarget 测试 Bool 转换到各种目标类型。
func TestCoerceValue_BoolToTarget(t *testing.T) {
	tests := []struct {
		name   string
		val    common.Value
		target common.DataType
		check  func(common.Value) bool
	}{
		{"bool->int64", common.NewBool(true), common.TypeInt64, func(v common.Value) bool { return v.Int64 == 1 }},
		{"bool->float64", common.NewBool(true), common.TypeFloat64, func(v common.Value) bool { return v.Float64 == 1.0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := coerceValue(tt.val, tt.target)
			if !tt.check(result) {
				t.Errorf("coerceValue %s 失败: 得到 %v", tt.name, result)
			}
		})
	}
}
