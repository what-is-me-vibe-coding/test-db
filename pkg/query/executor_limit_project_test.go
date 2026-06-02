package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

func TestLimitExecutorBasic(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 0, 3)
	defer limitExec.Close()

	count, rows := collectChunks(t, limitExec)
	if count != 3 {
		t.Fatalf("expected 3 rows, got %d", count)
	}
	if rows[0][1].Str != testNameAlice {
		t.Errorf("row 0 name: expected %s, got %s", testNameAlice, rows[0][1].Str)
	}
}

func TestLimitExecutorWithOffset(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 2, 2)
	defer limitExec.Close()

	count, rows := collectChunks(t, limitExec)
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
	if rows[0][1].Str != testNameCharlie {
		t.Errorf("row 0 name: expected %s, got %s", testNameCharlie, rows[0][1].Str)
	}
	if rows[1][1].Str != testNameDiana {
		t.Errorf("row 1 name: expected %s, got %s", testNameDiana, rows[1][1].Str)
	}
}

func TestLimitExecutorZeroCount(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 0, 0)
	defer limitExec.Close()

	count, _ := collectChunks(t, limitExec)
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestLimitExecutorOffsetExceedsTotal(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 100, 10)
	defer limitExec.Close()

	count, _ := collectChunks(t, limitExec)
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestLimitExecutorCountExceedsTotal(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 0, 100)
	defer limitExec.Close()

	count, _ := collectChunks(t, limitExec)
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}
}

func TestProjectExecutorBasic(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	projSchema := []ColumnDef{
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
	}
	exprs := []Expression{
		&ColumnExpr{Name: testColName},
		&ColumnExpr{Name: testColAge},
	}
	aliases := []string{testColName, testColAge}

	projExec := NewProjectExecutor(scanExec, exprs, aliases, projSchema)
	defer projExec.Close()

	count, rows := collectChunks(t, projExec)
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}

	if len(rows[0]) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(rows[0]))
	}
	if rows[0][0].Str != testNameAlice {
		t.Errorf("row 0 name: expected %s, got %s", testNameAlice, rows[0][0].Str)
	}
	if rows[0][1].Int64 != 30 {
		t.Errorf("row 0 age: expected 30, got %d", rows[0][1].Int64)
	}
}

func TestProjectExecutorWithExpression(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	projSchema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: testColAgePlus10, Type: common.TypeInt64, Nullable: false},
	}
	exprs := []Expression{
		&ColumnExpr{Name: "id"},
		&BinaryExpr{
			Op:    OpAdd,
			Left:  &ColumnExpr{Name: testColAge},
			Right: &LiteralExpr{Value: common.NewInt64(10)},
		},
	}
	aliases := []string{"id", testColAgePlus10}

	projExec := NewProjectExecutor(scanExec, exprs, aliases, projSchema)
	defer projExec.Close()

	count, rows := collectChunks(t, projExec)
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}

	if rows[0][1].Int64 != 40 {
		t.Errorf("row 0 age_plus_10: expected 40, got %d", rows[0][1].Int64)
	}
}

func TestLimitExecutorSchema(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 0, 1)
	got := limitExec.Schema()
	if len(got) != len(schema) {
		t.Fatalf("schema length: expected %d, got %d", len(schema), len(got))
	}
	limitExec.Close()
}

func TestProjectExecutorSchema(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	projSchema := []ColumnDef{
		{Name: testColName, Type: common.TypeString, Nullable: true},
	}
	exprs := []Expression{&ColumnExpr{Name: testColName}}
	projExec := NewProjectExecutor(scanExec, exprs, []string{testColName}, projSchema)
	got := projExec.Schema()
	if len(got) != 1 {
		t.Fatalf("schema length: expected 1, got %d", len(got))
	}
	if got[0].Name != testColName {
		t.Errorf("schema[0].Name: expected %s, got %s", testColName, got[0].Name)
	}
	projExec.Close()
}

// --- PlanNode String 测试 ---

func TestPlanNodeString(t *testing.T) {
	scanNode := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName},
		schema:  makeTestSchema()[:2],
	}
	s := scanNode.String()
	if s != "Scan(users, [id, name])" {
		t.Errorf("ScanNode.String(): expected 'Scan(users, [id, name])', got '%s'", s)
	}

	filterNode := &FilterNode{
		Child: scanNode,
		Condition: &BinaryExpr{
			Op:    OpEq,
			Left:  &ColumnExpr{Name: "id"},
			Right: &LiteralExpr{Value: common.NewInt64(1)},
		},
	}
	s = filterNode.String()
	if s != "Filter((id = 1))" {
		t.Errorf("FilterNode.String(): got '%s'", s)
	}

	limitNode := &LimitNode{Child: scanNode, Offset: 5, Count: 10}
	s = limitNode.String()
	if s != "Limit(offset=5, count=10)" {
		t.Errorf("LimitNode.String(): got '%s'", s)
	}
}

func TestScanNodeStringWithPredicate(t *testing.T) {
	node := &ScanNode{
		Table:     testTableUsers,
		Columns:   []string{"id"},
		Predicate: &BinaryExpr{OpEq, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}},
		schema:    makeTestSchema()[:1],
	}
	s := node.String()
	expected := "Scan(users, [id]) WHERE (id = 1)"
	if s != expected {
		t.Errorf("expected %q, got %q", expected, s)
	}
}

func TestProjectNodeString(t *testing.T) {
	node := &ProjectNode{
		Child:       &ScanNode{Table: "t", Columns: []string{"a"}, schema: makeTestSchema()[:1]},
		Expressions: []Expression{&ColumnExpr{Name: "a"}},
		Aliases:     []string{"x"},
		schema:      makeTestSchema()[:1],
	}
	s := node.String()
	if s != "Project([a AS x])" {
		t.Errorf("got %q", s)
	}
}

func TestAggregateNodeString(t *testing.T) {
	node := &AggregateNode{
		Child:      &ScanNode{Table: "t", Columns: []string{"a"}, schema: makeTestSchema()[:1]},
		GroupBy:    []Expression{&ColumnExpr{Name: "a"}},
		Aggregates: []AggregateExpr{{Func: testAggCount, Arg: &StarExpr{}}},
		schema:     makeTestSchema()[:1],
	}
	s := node.String()
	if s != "Aggregate(groupBy=[a], aggs=[COUNT(*)])" {
		t.Errorf("got %q", s)
	}
}

func TestLimitNodeStringNoOffset(t *testing.T) {
	node := &LimitNode{
		Child:  &ScanNode{Table: "t", Columns: []string{"a"}, schema: makeTestSchema()[:1]},
		Offset: 0,
		Count:  10,
	}
	s := node.String()
	if s != "Limit(count=10)" {
		t.Errorf("got %q", s)
	}
}

// --- 函数求值测试 ---

func TestEvalFuncAbs(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColVal, Type: common.TypeInt64, Nullable: false},
	}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			testColVal: common.NewInt64(-42),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &FuncExpr{Name: "ABS", Args: []Expression{&ColumnExpr{Name: testColVal}}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != 42 {
		t.Errorf("ABS(-42): expected 42, got %d", val.Int64)
	}
}

func TestEvalFuncCoalesce(t *testing.T) {
	schema := []ColumnDef{
		{Name: "a", Type: common.TypeInt64, Nullable: true},
		{Name: "b", Type: common.TypeInt64, Nullable: true},
	}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			"a": common.NewNull(),
			"b": common.NewInt64(99),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &FuncExpr{
		Name: "COALESCE",
		Args: []Expression{&ColumnExpr{Name: "a"}, &ColumnExpr{Name: "b"}},
	}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != 99 {
		t.Errorf("COALESCE(NULL, 99): expected 99, got %d", val.Int64)
	}
}

// --- 浮点数运算测试 ---

func TestEvalExprFloatArithmetic(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColPrice, Type: common.TypeFloat64, Nullable: false},
	}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			testColPrice: common.NewFloat64(19.99),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &BinaryExpr{OpMul, &ColumnExpr{Name: testColPrice}, &LiteralExpr{Value: common.NewFloat64(2.0)}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Typ != common.TypeFloat64 {
		t.Errorf("expected Float64 type, got %s", val.Typ.String())
	}
	if val.Float64 < 39.98-0.001 || val.Float64 > 39.98+0.001 {
		t.Errorf("price * 2: expected ~39.98, got %f", val.Float64)
	}
}

// --- 浮点数取负测试 ---

func TestEvalExprNegFloat(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColPrice, Type: common.TypeFloat64, Nullable: false},
	}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			testColPrice: common.NewFloat64(3.14),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &UnaryExpr{Op: OpNeg, Expr: &ColumnExpr{Name: testColPrice}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Float64 > -3.13 || val.Float64 < -3.15 {
		t.Errorf("-price: expected ~-3.14, got %f", val.Float64)
	}
}

// --- LIKE 测试 ---

func TestEvalExprLike(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColName, Type: common.TypeString, Nullable: false},
	}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			testColName: common.NewString(testNameAlice),
		}}},
		{Key: "k2", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			testColName: common.NewString("alexander"),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &BinaryExpr{
		Op:    OpLike,
		Left:  &ColumnExpr{Name: testColName},
		Right: &LiteralExpr{Value: common.NewString("ali%")},
	}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !toBool(val) {
		t.Error("alice LIKE ali%: expected true")
	}

	val, err = evalExpr(expr, chunk, 1, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if toBool(val) {
		t.Error("alexander LIKE ali%: expected false")
	}
}

// --- 表驱动测试：比较运算符 ---

func TestComparisonOpsTableDriven(t *testing.T) {
	tests := []struct {
		name  string
		op    BinaryOp
		left  common.Value
		right common.Value
		want  bool
	}{
		{"int eq", OpEq, common.NewInt64(5), common.NewInt64(5), true},
		{"int ne", OpNe, common.NewInt64(5), common.NewInt64(3), true},
		{"int lt", OpLt, common.NewInt64(3), common.NewInt64(5), true},
		{"int le", OpLe, common.NewInt64(5), common.NewInt64(5), true},
		{"int gt", OpGt, common.NewInt64(5), common.NewInt64(3), true},
		{"int ge", OpGe, common.NewInt64(5), common.NewInt64(5), true},
		{"str eq", OpEq, common.NewString("abc"), common.NewString("abc"), true},
		{"str lt", OpLt, common.NewString("abc"), common.NewString("def"), true},
		{"float eq", OpEq, common.NewFloat64(1.5), common.NewFloat64(1.5), true},
		{"float lt", OpLt, common.NewFloat64(1.5), common.NewFloat64(2.5), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := applyBinaryOp(tt.op, tt.left, tt.right)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if toBool(result) != tt.want {
				t.Errorf("expected %v, got %v", tt.want, toBool(result))
			}
		})
	}
}
