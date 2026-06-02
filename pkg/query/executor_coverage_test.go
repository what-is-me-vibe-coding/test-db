package query

import (
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// --- evalOr 覆盖率测试 ---

func TestEvalOrLeftTrue(t *testing.T) {
	schema := []ColumnDef{
		{Name: testColVal, Type: common.TypeInt64, Nullable: false},
	}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			testColVal: common.NewInt64(1),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)
	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &BinaryExpr{OpOr, &LiteralExpr{Value: common.NewBool(true)}, &LiteralExpr{Value: common.NewBool(false)}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !toBool(val) {
		t.Error("true OR false: expected true")
	}
}

func TestEvalOrLeftFalseRightTrue(t *testing.T) {
	expr := &BinaryExpr{OpOr, &LiteralExpr{Value: common.NewBool(false)}, &LiteralExpr{Value: common.NewBool(true)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !toBool(val) {
		t.Error("false OR true: expected true")
	}
}

func TestEvalOrLeftFalseRightFalse(t *testing.T) {
	expr := &BinaryExpr{OpOr, &LiteralExpr{Value: common.NewBool(false)}, &LiteralExpr{Value: common.NewBool(false)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if toBool(val) {
		t.Error("false OR false: expected false")
	}
}

func TestEvalOrLeftNullRightTrue(t *testing.T) {
	expr := &BinaryExpr{OpOr, &LiteralExpr{Value: common.NewNull()}, &LiteralExpr{Value: common.NewBool(true)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !toBool(val) {
		t.Error("NULL OR true: expected true")
	}
}

func TestEvalOrLeftNullRightFalse(t *testing.T) {
	expr := &BinaryExpr{OpOr, &LiteralExpr{Value: common.NewNull()}, &LiteralExpr{Value: common.NewBool(false)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("NULL OR false: expected NULL, got %v", val)
	}
}

func TestEvalOrLeftFalseRightNull(t *testing.T) {
	expr := &BinaryExpr{OpOr, &LiteralExpr{Value: common.NewBool(false)}, &LiteralExpr{Value: common.NewNull()}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("false OR NULL: expected NULL, got %v", val)
	}
}

// --- evalAnd 覆盖率测试 ---

func TestEvalAndLeftFalse(t *testing.T) {
	expr := &BinaryExpr{OpAnd, &LiteralExpr{Value: common.NewBool(false)}, &LiteralExpr{Value: common.NewBool(true)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if toBool(val) {
		t.Error("false AND true: expected false")
	}
}

func TestEvalAndLeftNull(t *testing.T) {
	expr := &BinaryExpr{OpAnd, &LiteralExpr{Value: common.NewNull()}, &LiteralExpr{Value: common.NewBool(true)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("NULL AND true: expected NULL, got %v", val)
	}
}

func TestEvalAndRightNull(t *testing.T) {
	expr := &BinaryExpr{OpAnd, &LiteralExpr{Value: common.NewBool(true)}, &LiteralExpr{Value: common.NewNull()}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("true AND NULL: expected NULL, got %v", val)
	}
}

// --- toBool 覆盖率测试 ---

func TestToBoolWithBool(t *testing.T) {
	if !toBool(common.NewBool(true)) {
		t.Error("toBool(true): expected true")
	}
	if toBool(common.NewBool(false)) {
		t.Error("toBool(false): expected false")
	}
}

func TestToBoolWithFloat(t *testing.T) {
	if !toBool(common.NewFloat64(1.5)) {
		t.Error("toBool(1.5): expected true")
	}
	if toBool(common.NewFloat64(0.0)) {
		t.Error("toBool(0.0): expected false")
	}
}

func TestToBoolWithString(t *testing.T) {
	if toBool(common.NewString("hello")) {
		t.Error("toBool(string): expected false")
	}
}

// --- toFloat64 覆盖率测试 ---

func TestToFloat64FromInt(t *testing.T) {
	v := toFloat64(common.NewInt64(42))
	if v != 42.0 {
		t.Errorf("toFloat64(int64 42): expected 42.0, got %f", v)
	}
}

func TestToFloat64FromString(t *testing.T) {
	v := toFloat64(common.NewString("hello"))
	if v != 0 {
		t.Errorf("toFloat64(string): expected 0, got %f", v)
	}
}

// --- evalUnaryExpr NOT with NULL ---

func TestEvalUnaryNotNull(t *testing.T) {
	expr := &UnaryExpr{Op: OpNot, Expr: &LiteralExpr{Value: common.NewNull()}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("NOT NULL: expected NULL, got %v", val)
	}
}

// --- 浮点数除零测试 ---

func TestEvalExprFloatDivByZero(t *testing.T) {
	expr := &BinaryExpr{OpDiv, &LiteralExpr{Value: common.NewFloat64(10.0)}, &LiteralExpr{Value: common.NewFloat64(0.0)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("10.0 / 0.0: expected NULL, got %v", val)
	}
}

// --- Int64/Float64 混合算术测试 ---

func TestEvalExprMixedIntFloatArithmetic(t *testing.T) {
	expr := &BinaryExpr{OpAdd, &LiteralExpr{Value: common.NewInt64(5)}, &LiteralExpr{Value: common.NewFloat64(2.5)}}
	val, err := evalExpr(expr, nil, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Typ != common.TypeFloat64 {
		t.Errorf("expected Float64 type, got %s", val.Typ.String())
	}
	if val.Float64 != 7.5 {
		t.Errorf("5 + 2.5: expected 7.5, got %f", val.Float64)
	}
}

// --- evalColumnExpr 列不存在测试 ---

func TestEvalColumnExprNotFound(t *testing.T) {
	colIndexMap := map[string]int{"id": 0}
	_, err := evalColumnExpr("nonexistent", nil, 0, colIndexMap)
	if err == nil {
		t.Fatal("expected error for nonexistent column, got nil")
	}
}

// --- evalResolvedColumnExpr 列索引越界测试 ---

func TestEvalResolvedColumnExprInvalidIdx(t *testing.T) {
	schema := []ColumnDef{{Name: "id", Type: common.TypeInt64, Nullable: false}}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			"id": common.NewInt64(1),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)
	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	expr := &ResolvedColumnExpr{Name: "id", Idx: 99, typ: common.TypeInt64}
	_, err = evalExpr(expr, chunk, 0, nil)
	if err == nil {
		t.Fatal("expected error for invalid column index, got nil")
	}
}

// --- BuildExecutor 错误路径测试 ---

const testAggCount = "COUNT"

func TestBuildFilterExecutorChildError(t *testing.T) {
	plan := &FilterNode{
		Child: &AggregateNode{
			Child:   &ScanNode{Table: "t", schema: makeTestSchema()},
			GroupBy: []Expression{&ColumnExpr{Name: "id"}},
		},
		Condition: &BinaryExpr{OpEq, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}},
	}
	iterFn := func(_ string) storage.ScanIterator { return newMockIterator(nil) }
	_, err := BuildExecutor(plan, iterFn)
	if err == nil {
		t.Fatal("expected error when filter child build fails, got nil")
	}
}

func TestBuildProjectExecutorChildError(t *testing.T) {
	plan := &ProjectNode{
		Child: &AggregateNode{
			Child:      &ScanNode{Table: "t", schema: makeTestSchema()},
			GroupBy:    []Expression{&ColumnExpr{Name: "id"}},
			Aggregates: []AggregateExpr{{Func: testAggCount, Arg: &StarExpr{}}},
		},
		Expressions: []Expression{&ColumnExpr{Name: "id"}},
		Aliases:     []string{"id"},
		schema:      makeTestSchema()[:1],
	}
	iterFn := func(_ string) storage.ScanIterator { return newMockIterator(nil) }
	_, err := BuildExecutor(plan, iterFn)
	if err == nil {
		t.Fatal("expected error when project child build fails, got nil")
	}
}

func TestBuildLimitExecutorChildError(t *testing.T) {
	plan := &LimitNode{
		Child:  &AggregateNode{Child: &ScanNode{Table: "t", schema: makeTestSchema()}, GroupBy: []Expression{&ColumnExpr{Name: "id"}}},
		Offset: 0,
		Count:  10,
	}
	iterFn := func(_ string) storage.ScanIterator { return newMockIterator(nil) }
	_, err := BuildExecutor(plan, iterFn)
	if err == nil {
		t.Fatal("expected error when limit child build fails, got nil")
	}
}

// --- PlanNode String/Schema/Children 补充测试 ---

func TestAggregateExprStringWithArg(t *testing.T) {
	expr := AggregateExpr{Func: "SUM", Arg: &ColumnExpr{Name: testColAge}}
	s := expr.String()
	if s != "SUM(age)" {
		t.Errorf("expected 'SUM(age)', got %q", s)
	}
}

func TestProjectNodeStringNoAlias(t *testing.T) {
	node := &ProjectNode{
		Child:       &ScanNode{Table: "t", Columns: []string{"a"}, schema: makeTestSchema()[:1]},
		Expressions: []Expression{&ColumnExpr{Name: "a"}},
		Aliases:     []string{""},
		schema:      makeTestSchema()[:1],
	}
	s := node.String()
	if s != "Project([a])" {
		t.Errorf("got %q", s)
	}
}

func TestFilterNodeSchemaAndChildren(t *testing.T) {
	schema := makeTestSchema()
	scanNode := &ScanNode{Table: "t", Columns: []string{"id"}, schema: schema[:1]}
	filterNode := &FilterNode{
		Child:     scanNode,
		Condition: &BinaryExpr{OpEq, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}},
	}
	if len(filterNode.Schema()) != 1 {
		t.Errorf("expected 1 column in schema, got %d", len(filterNode.Schema()))
	}
	if len(filterNode.Children()) != 1 {
		t.Errorf("expected 1 child, got %d", len(filterNode.Children()))
	}
}

func TestAggregateNodeSchemaAndChildren(t *testing.T) {
	schema := makeTestSchema()[:1]
	scanNode := &ScanNode{Table: "t", Columns: []string{"id"}, schema: schema}
	aggNode := &AggregateNode{
		Child:      scanNode,
		GroupBy:    []Expression{&ColumnExpr{Name: "id"}},
		Aggregates: []AggregateExpr{{Func: testAggCount, Arg: &StarExpr{}}},
		schema:     schema,
	}
	if len(aggNode.Schema()) != 1 {
		t.Errorf("expected 1 column in schema, got %d", len(aggNode.Schema()))
	}
	if len(aggNode.Children()) != 1 {
		t.Errorf("expected 1 child, got %d", len(aggNode.Children()))
	}
}

func TestLimitNodeSchemaAndChildren(t *testing.T) {
	schema := makeTestSchema()[:1]
	scanNode := &ScanNode{Table: "t", Columns: []string{"id"}, schema: schema}
	limitNode := &LimitNode{Child: scanNode, Offset: 0, Count: 10}
	if len(limitNode.Schema()) != 1 {
		t.Errorf("expected 1 column in schema, got %d", len(limitNode.Schema()))
	}
	if len(limitNode.Children()) != 1 {
		t.Errorf("expected 1 child, got %d", len(limitNode.Children()))
	}
}

func TestProjectNodeSchemaAndChildren(t *testing.T) {
	schema := makeTestSchema()[:1]
	scanNode := &ScanNode{Table: "t", Columns: []string{"id"}, schema: schema}
	projNode := &ProjectNode{
		Child:       scanNode,
		Expressions: []Expression{&ColumnExpr{Name: "id"}},
		Aliases:     []string{"id"},
		schema:      schema,
	}
	if len(projNode.Schema()) != 1 {
		t.Errorf("expected 1 column in schema, got %d", len(projNode.Schema()))
	}
	if len(projNode.Children()) != 1 {
		t.Errorf("expected 1 child, got %d", len(projNode.Children()))
	}
}

// --- FilterExecutor 错误路径测试 ---

func TestFilterExecutorChildError(t *testing.T) {
	schema := makeTestSchema()
	iter := &mockIterator{pos: -1, err: fmt.Errorf("scan error")}
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{OpEq, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	_, err := filterExec.NextChunk()
	if err == nil {
		t.Fatal("expected error from child executor, got nil")
	}
}

// --- ProjectExecutor 错误路径测试 ---

func TestProjectExecutorChildError(t *testing.T) {
	schema := makeTestSchema()
	iter := &mockIterator{pos: -1, err: fmt.Errorf("scan error")}
	scanExec := NewScanExecutor(iter, schema)

	projSchema := makeTestSchema()[:1]
	projExec := NewProjectExecutor(scanExec, []Expression{&ColumnExpr{Name: "id"}}, []string{"id"}, projSchema)
	defer projExec.Close()

	_, err := projExec.NextChunk()
	if err == nil {
		t.Fatal("expected error from child executor, got nil")
	}
}

// --- LimitExecutor 错误路径测试 ---

func TestLimitExecutorChildError(t *testing.T) {
	schema := makeTestSchema()
	iter := &mockIterator{pos: -1, err: fmt.Errorf("scan error")}
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 0, 10)
	defer limitExec.Close()

	_, err := limitExec.NextChunk()
	if err == nil {
		t.Fatal("expected error from child executor, got nil")
	}
}

// --- FilterExecutor 空 Chunk 测试 ---

func TestFilterExecutorEmptyChunk(t *testing.T) {
	schema := makeTestSchema()
	iter := newMockIterator(nil)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{OpEq, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	chunk, err := filterExec.NextChunk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Fatal("expected nil chunk for empty input")
	}
}

// --- ProjectExecutor 空 Chunk 测试 ---

func TestProjectExecutorEmptyChunk(t *testing.T) {
	schema := makeTestSchema()
	iter := newMockIterator(nil)
	scanExec := NewScanExecutor(iter, schema)

	projSchema := makeTestSchema()[:1]
	projExec := NewProjectExecutor(scanExec, []Expression{&ColumnExpr{Name: "id"}}, []string{"id"}, projSchema)
	defer projExec.Close()

	chunk, err := projExec.NextChunk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Fatal("expected nil chunk for empty input")
	}
}

// --- LimitExecutor 空 Chunk 测试 ---

func TestLimitExecutorEmptyChunk(t *testing.T) {
	schema := makeTestSchema()
	iter := newMockIterator(nil)
	scanExec := NewScanExecutor(iter, schema)

	limitExec := NewLimitExecutor(scanExec, 0, 10)
	defer limitExec.Close()

	chunk, err := limitExec.NextChunk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Fatal("expected nil chunk for empty input")
	}
}

// --- ResolvedColumnExpr String 测试 ---

func TestResolvedColumnExprString(t *testing.T) {
	expr := &ResolvedColumnExpr{Name: "id", Idx: 2, typ: common.TypeInt64}
	s := expr.String()
	if s != "$2(id)" {
		t.Errorf("expected '$2(id)', got %q", s)
	}
}
