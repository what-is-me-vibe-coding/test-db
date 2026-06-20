package query

import (
	"fmt"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

func TestExecutorProjectBasic(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString},
			&ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64},
		},
		Aliases: []string{"", ""},
		schema: []ColumnDef{
			{Name: testColName, Type: common.TypeString, Nullable: true},
			{Name: testColAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute project: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row, got %d", totalRows)
	}

	if len(chunks) > 0 {
		colCount := chunks[0].ColumnCount()
		if colCount != 2 {
			t.Errorf("expected 2 columns after projection, got %d", colCount)
		}
	}
}

func TestExecutorFilterAndProject(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameCharlie),
		testColAge: common.NewInt64(35), testColScore: common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(25)}},
	}

	project := &ProjectNode{
		Child: filter,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString},
			&ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64},
		},
		Aliases: []string{"", ""},
		schema: []ColumnDef{
			{Name: testColName, Type: common.TypeString, Nullable: true},
			{Name: testColAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute filter+project: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 rows (age > 25), got %d", totalRows)
	}

	if len(chunks) > 0 {
		colCount := chunks[0].ColumnCount()
		if colCount != 2 {
			t.Errorf("expected 2 columns, got %d", colCount)
		}
	}
}

func TestExecutorProjectWithArithmetic(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString},
			&BinaryExpr{Op: OpMul, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(2)}},
		},
		Aliases: []string{testColName, "double_age"},
		schema: []ColumnDef{
			{Name: testColName, Type: common.TypeString, Nullable: true},
			{Name: "double_age", Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute project with arithmetic: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		nameCol, _ := chunks[0].GetColumn(0)
		nameVal := nameCol.GetValue(0)
		if nameVal.Str != testNameAlice {
			t.Errorf("expected name='alice', got %q", nameVal.Str)
		}

		ageCol, _ := chunks[0].GetColumn(1)
		ageVal := ageCol.GetValue(0)
		if ageVal.Int64 != 60 {
			t.Errorf("expected double_age=60, got %d", ageVal.Int64)
		}
	}
}

func TestExecutorProjectTypeCoercion(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	// Project int column as float (type coercion)
	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64},
		},
		Aliases: []string{"age_as_float"},
		schema: []ColumnDef{
			{Name: "age_as_float", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute project type coercion: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != 30.0 {
			t.Errorf("expected 30.0 (coerced to float), got %g", val.Float64)
		}
	}
}

// TestProjectChunkRowMajor 验证 projectChunk 行优先迭代在「多表达式 + 多列」
// 场景下产出与列优先实现完全一致的结果，回归保护：投影值与列序均不应变化。
//
// 该测试直接调用 projectChunk 构造 6 列输入、6 个表达式（其中含算术与
// 类型 coercion），并按行核对所有列值，验证行优先迭代未导致跨列错位。
func TestProjectChunkRowMajor(t *testing.T) {
	inputSchema, chunk, exprs, outputSchema := buildRowMajorTestFixture()
	colIdxMap := buildColIdxMapFromSchema(inputSchema)

	out, err := projectChunk(chunk, exprs, inputSchema, outputSchema, colIdxMap)
	if err != nil {
		t.Fatalf("projectChunk: %v", err)
	}
	if out.RowCount() != chunk.RowCount() {
		t.Fatalf("rowCount: got %d, want %d", out.RowCount(), chunk.RowCount())
	}
	if out.ColumnCount() != len(outputSchema) {
		t.Fatalf("columnCount: got %d, want %d", out.ColumnCount(), len(outputSchema))
	}
	assertRowMajorProjectValues(t, out.Columns(), chunk.RowCount())
}

// buildRowMajorTestFixture 构造 TestProjectChunkRowMajor 所需的 6 列、64 行
// 测试数据：id/name/age/score/active/ts，投影包含 ResolvedColumnExpr 引用、
// 算术表达式 age*2 与 score+1.0 浮点 coercion，类型覆盖 INT64/STRING/
// FLOAT64/BOOL/TIMESTAMP。
func buildRowMajorTestFixture() ([]ColumnDef, *storage.Chunk, []Expression, []ColumnDef) {
	inputSchema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "age", Type: common.TypeInt64, Nullable: true},
		{Name: "score", Type: common.TypeFloat64, Nullable: true},
		{Name: "active", Type: common.TypeBool, Nullable: true},
		{Name: "ts", Type: common.TypeTimestamp, Nullable: true},
	}
	rowCount := uint32(64)
	chunk := storage.NewChunk(rowCount)
	cols := []*storage.ColumnVector{
		storage.NewColumnVector(0, common.TypeInt64, rowCount),
		storage.NewColumnVector(1, common.TypeString, rowCount),
		storage.NewColumnVector(2, common.TypeInt64, rowCount),
		storage.NewColumnVector(3, common.TypeFloat64, rowCount),
		storage.NewColumnVector(4, common.TypeBool, rowCount),
		storage.NewColumnVector(5, common.TypeTimestamp, rowCount),
	}
	for r := uint32(0); r < rowCount; r++ {
		cols[0].SetInt64(r, int64(r))
		cols[1].SetString(r, fmt.Sprintf("name-%d", r))
		cols[2].SetInt64(r, int64(20+r%40))
		cols[3].SetFloat64(r, float64(r)*0.5)
		cols[4].SetBool(r, r%2 == 0)
		cols[5].SetTimestamp(r, time.Unix(int64(r)*60, 0))
	}
	for _, c := range cols {
		c.SetLen(rowCount)
		_ = chunk.AddColumn(c)
	}
	exprs := []Expression{
		&ResolvedColumnExpr{Name: "id", Idx: 0, typ: common.TypeInt64},
		&ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString},
		&BinaryExpr{Op: OpMul, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(2)}},
		&BinaryExpr{Op: OpAdd, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(1.0)}},
		&ResolvedColumnExpr{Name: "active", Idx: 4, typ: common.TypeBool},
		&ResolvedColumnExpr{Name: "ts", Idx: 5, typ: common.TypeTimestamp},
	}
	outputSchema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "double_age", Type: common.TypeInt64, Nullable: true},
		{Name: "score_plus_one", Type: common.TypeFloat64, Nullable: true},
		{Name: "active", Type: common.TypeBool, Nullable: true},
		{Name: "ts", Type: common.TypeTimestamp, Nullable: true},
	}
	return inputSchema, chunk, exprs, outputSchema
}

// assertRowMajorProjectValues 验证 6 列输出列向量逐行值与 buildRowMajorTestFixture
// 的构造规则完全一致：id=r、name="name-r"、age*2、score+1.0、active=(r%2==0)、ts=r*60s。
func assertRowMajorProjectValues(t *testing.T, outCols []*storage.ColumnVector, rowCount uint32) {
	t.Helper()
	for r := uint32(0); r < rowCount; r++ {
		if got := outCols[0].GetValue(r).Int64; got != int64(r) {
			t.Fatalf("row %d: id got %d, want %d", r, got, r)
		}
		if got := outCols[1].GetValue(r).Str; got != fmt.Sprintf("name-%d", r) {
			t.Fatalf("row %d: name got %q", r, got)
		}
		if got := outCols[2].GetValue(r).Int64; got != int64(20+r%40)*2 {
			t.Fatalf("row %d: double_age got %d, want %d", r, got, int64(20+r%40)*2)
		}
		wantScore := float64(r)*0.5 + 1.0
		if got := outCols[3].GetValue(r).Float64; got != wantScore {
			t.Fatalf("row %d: score_plus_one got %g, want %g", r, got, wantScore)
		}
		if got := outCols[4].GetValue(r).Int64; got != int64(1-(r%2)) {
			t.Fatalf("row %d: active got %d, want %d", r, got, 1-(r%2))
		}
		wantTs := time.Unix(int64(r)*60, 0)
		if got := outCols[5].GetValue(r).Time; !got.Equal(wantTs) {
			t.Fatalf("row %d: ts got %v, want %v", r, got, wantTs)
		}
	}
}

// TestProjectChunkRowMajorEmpty 验证空输入 chunk 下不会执行 rowVals
// 填充与表达式求值，但仍按表达式数量预留空列结构（与旧实现语义一致）。
func TestProjectChunkRowMajorEmpty(t *testing.T) {
	inputSchema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
	}
	outputSchema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
	}
	chunk := storage.NewChunk(0)
	colIdxMap := buildColIdxMapFromSchema(inputSchema)
	exprs := []Expression{
		&ResolvedColumnExpr{Name: "id", Idx: 0, typ: common.TypeInt64},
		&ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString},
	}
	out, err := projectChunk(chunk, exprs, inputSchema, outputSchema, colIdxMap)
	if err != nil {
		t.Fatalf("projectChunk empty: %v", err)
	}
	if out.RowCount() != 0 {
		t.Fatalf("expected 0 rows, got %d", out.RowCount())
	}
	if out.ColumnCount() != len(outputSchema) {
		t.Fatalf("expected %d columns, got %d", len(outputSchema), out.ColumnCount())
	}
}

func TestExecutorColumnExpr(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ColumnExpr{Name: testColName},
		},
		Aliases: []string{testColName},
		schema: []ColumnDef{
			{Name: testColName, Type: common.TypeString, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute column expr: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Str != testNameAlice {
			t.Errorf("expected 'alice', got %q", val.Str)
		}
	}
}

func TestExecutorFullPipeline(t *testing.T) {
	ms := newMockStorage()
	for i := 0; i < 20; i++ {
		key := fmtKey(i)
		ms.addEntry(key, map[string]common.Value{
			testColID:    common.NewInt64(int64(i)),
			testColName:  common.NewString(key),
			testColAge:   common.NewInt64(int64(20 + i%10)),
			testColScore: common.NewFloat64(float64(80 + i%20)),
		})
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(70)}},
	}

	project := &ProjectNode{
		Child: filter,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString},
			&ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64},
		},
		Aliases: []string{testColName, testColScore},
		schema: []ColumnDef{
			{Name: testColName, Type: common.TypeString, Nullable: true},
			{Name: testColScore, Type: common.TypeFloat64, Nullable: true},
		},
	}

	limit := &LimitNode{
		Child:  project,
		Offset: 0,
		Count:  5,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(limit)
	if err != nil {
		t.Fatalf("execute full pipeline: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 5 {
		t.Errorf("expected 5 rows (full pipeline), got %d", totalRows)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		colCount := chunks[0].ColumnCount()
		if colCount != 2 {
			t.Errorf("expected 2 columns after projection, got %d", colCount)
		}
	}
}

func TestExecutorStringComparison(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameCharlie),
		testColAge: common.NewInt64(35), testColScore: common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString}, Right: &LiteralExpr{Value: common.NewString(testNameBob)}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute string comparison: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (name='bob'), got %d", totalRows)
	}
}

func TestExecutorDivByZero(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&BinaryExpr{Op: OpDiv, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(0)}},
		},
		Aliases: []string{testAliasDivZero},
		schema: []ColumnDef{
			{Name: testAliasDivZero, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute div by zero: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Valid {
			t.Errorf("expected NULL for division by zero, got %v", val)
		}
	}
}

func TestExecutorAggregateEmptyMin(t *testing.T) {
	ms := newMockStorage()

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggMinAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate empty min: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Valid {
			t.Errorf("expected NULL for MIN on empty input, got %v", val)
		}
	}
}

func TestExecutorAggregateEmptyMax(t *testing.T) {
	ms := newMockStorage()

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggMaxAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate empty max: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Valid {
			t.Errorf("expected NULL for MAX on empty input, got %v", val)
		}
	}
}

func TestExecutorAggregateEmptyAvg(t *testing.T) {
	ms := newMockStorage()

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggAvgAge, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate empty avg: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Valid {
			t.Errorf("expected NULL for AVG on empty input, got %v", val)
		}
	}
}
