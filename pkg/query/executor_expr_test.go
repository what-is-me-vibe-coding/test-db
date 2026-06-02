package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// --- 表达式求值测试 ---

func TestEvalExprComparisonOps(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk")
	}

	colIndexMap := buildColIndexMap(schema)

	tests := []struct {
		name     string
		expr     Expression
		rowIdx   uint32
		wantBool bool
		wantNull bool
	}{
		{"id = 1", &BinaryExpr{OpEq, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}}, 0, true, false},
		{"id != 1", &BinaryExpr{OpNe, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}}, 0, false, false},
		{"id != 2", &BinaryExpr{OpNe, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(2)}}, 0, true, false},
		{"age < 30", &BinaryExpr{OpLt, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(30)}}, 0, false, false},
		{"age <= 30", &BinaryExpr{OpLe, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(30)}}, 0, true, false},
		{"age > 25", &BinaryExpr{OpGt, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(25)}}, 0, true, false},
		{"age >= 30", &BinaryExpr{OpGe, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(30)}}, 0, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalExpr(tt.expr, chunk, tt.rowIdx, colIndexMap)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if tt.wantNull {
				if !val.IsNull() {
					t.Errorf("expected NULL, got %v", val)
				}
				return
			}
			if toBool(val) != tt.wantBool {
				t.Errorf("expected %v, got %v (value=%v)", tt.wantBool, toBool(val), val)
			}
		})
	}
}

func TestEvalExprArithmetic(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &BinaryExpr{OpAdd, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(10)}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != 40 {
		t.Errorf("age + 10: expected 40, got %d", val.Int64)
	}

	expr = &BinaryExpr{OpSub, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(5)}}
	val, err = evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != 25 {
		t.Errorf("age - 5: expected 25, got %d", val.Int64)
	}

	expr = &BinaryExpr{OpMul, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(2)}}
	val, err = evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != 60 {
		t.Errorf("age * 2: expected 60, got %d", val.Int64)
	}

	expr = &BinaryExpr{OpDiv, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(3)}}
	val, err = evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != 10 {
		t.Errorf("age / 3: expected 10, got %d", val.Int64)
	}
}

func TestEvalExprDivisionByZero(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &BinaryExpr{OpDiv, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(0)}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("division by zero: expected NULL, got %v", val)
	}
}

func TestEvalExprUnaryNeg(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &UnaryExpr{Op: OpNeg, Expr: &ColumnExpr{Name: testColAge}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != -30 {
		t.Errorf("-age: expected -30, got %d", val.Int64)
	}
}

func TestEvalExprLiteral(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &LiteralExpr{Value: common.NewInt64(42)}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Int64 != 42 {
		t.Errorf("literal: expected 42, got %d", val.Int64)
	}
}

func TestEvalExprNullPropagation(t *testing.T) {
	schema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: true},
	}
	entries := []storage.ScanEntry{
		{Key: "k1", Value: storage.Row{Version: 1, Columns: map[string]common.Value{
			"id": common.NewNull(),
		}}},
	}
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &BinaryExpr{OpEq, &ColumnExpr{Name: "id"}, &LiteralExpr{Value: common.NewInt64(1)}}
	val, err := evalExpr(expr, chunk, 0, colIndexMap)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("NULL comparison: expected NULL, got %v", val)
	}
}

func TestEvalExprColumnNotFound(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &ColumnExpr{Name: "nonexistent"}
	_, err = evalExpr(expr, chunk, 0, colIndexMap)
	if err == nil {
		t.Fatal("expected error for nonexistent column, got nil")
	}
}

func TestEvalExprStarExpr(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	colIndexMap := buildColIndexMap(schema)

	expr := &StarExpr{}
	_, err = evalExpr(expr, chunk, 0, colIndexMap)
	if err == nil {
		t.Fatal("expected error for star expression, got nil")
	}
}

// --- ResolvedColumnExpr 测试 ---

func TestEvalResolvedColumnExpr(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	expr := &ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString}
	val, err := evalExpr(expr, chunk, 0, nil)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if val.Str != testNameAlice {
		t.Errorf("resolved column: expected %s, got %s", testNameAlice, val.Str)
	}
}

func TestEvalResolvedColumnExprOutOfRange(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	chunk, err := scanExec.NextChunk()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	expr := &ResolvedColumnExpr{Name: "id", Idx: 0, typ: common.TypeInt64}
	_, err = evalExpr(expr, chunk, 999, nil)
	if err == nil {
		t.Fatal("expected error for out of range row index, got nil")
	}
}

// --- 错误路径测试 ---

func TestEvalFuncUnknown(t *testing.T) {
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

	expr := &FuncExpr{Name: "UNKNOWN_FUNC", Args: []Expression{&ColumnExpr{Name: testColVal}}}
	_, err = evalExpr(expr, chunk, 0, colIndexMap)
	if err == nil {
		t.Fatal("expected error for unknown function, got nil")
	}
}

func TestEvalFuncCoalesceAllNull(t *testing.T) {
	args := []common.Value{common.NewNull(), common.NewNull()}
	val, err := applyCoalesce(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("expected NULL, got %v", val)
	}
}

func TestEvalArithmeticTypeMismatch(t *testing.T) {
	_, err := applyArithmetic(OpAdd, common.NewString("a"), common.NewInt64(1))
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
}

func TestEvalLikeTypeMismatch(t *testing.T) {
	_, err := applyLike(common.NewInt64(1), common.NewString("%"))
	if err == nil {
		t.Fatal("expected error for LIKE type mismatch, got nil")
	}
}

func TestApplyNegUnsupportedType(t *testing.T) {
	_, err := applyNeg(common.NewString("hello"))
	if err == nil {
		t.Fatal("expected error for neg on string, got nil")
	}
}

func TestApplyAbsNull(t *testing.T) {
	val, err := applyAbs(common.NewNull())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.IsNull() {
		t.Errorf("expected NULL, got %v", val)
	}
}

func TestApplyAbsUnsupportedType(t *testing.T) {
	_, err := applyAbs(common.NewString("hello"))
	if err == nil {
		t.Fatal("expected error for ABS on string, got nil")
	}
}

func TestApplyFuncAbsWrongArgCount(t *testing.T) {
	_, err := applyFunc("ABS", nil)
	if err == nil {
		t.Fatal("expected error for ABS with no args, got nil")
	}
}

func TestApplyBinaryOpUnsupported(t *testing.T) {
	_, err := applyBinaryOp(BinaryOp(100), common.NewInt64(1), common.NewInt64(2))
	if err == nil {
		t.Fatal("expected error for unsupported binary op, got nil")
	}
}

func TestEvalUnaryUnsupported(t *testing.T) {
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

	expr := &UnaryExpr{Op: UnaryOp(100), Expr: &ColumnExpr{Name: testColVal}}
	_, err = evalExpr(expr, chunk, 0, colIndexMap)
	if err == nil {
		t.Fatal("expected error for unsupported unary op, got nil")
	}
}

type unsupportedExpr struct{}

func (e *unsupportedExpr) exprNode()      {}
func (e *unsupportedExpr) String() string { return "unsupported" }

func TestEvalExprUnsupported(t *testing.T) {
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

	_, err = evalExpr(&unsupportedExpr{}, chunk, 0, colIndexMap)
	if err == nil {
		t.Fatal("expected error for unsupported expr type, got nil")
	}
}
