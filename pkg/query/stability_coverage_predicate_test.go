package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// ---------------------------------------------------------------------------
// queryOpToIndexOp / queryOpToIndexOpFlip: 全操作符覆盖
// ---------------------------------------------------------------------------

func TestQueryOpToIndexOp_AllOperators(t *testing.T) {
	tests := []struct {
		name   string
		op     BinaryOp
		want   index.PredicateOp
		wantOK bool
	}{
		{"OpEq", OpEq, index.OpEqual, true},
		{"OpNe", OpNe, index.OpNotEqual, true},
		{"OpLt", OpLt, index.OpLess, true},
		{"OpLe", OpLe, index.OpLessEqual, true},
		{"OpGt", OpGt, index.OpGreater, true},
		{"OpGe", OpGe, index.OpGreaterEqual, true},
		{"OpAnd_unsupported", OpAnd, 0, false},
		{"OpOr_unsupported", OpOr, 0, false},
		{"OpAdd_unsupported", OpAdd, 0, false},
		{"OpLike_unsupported", OpLike, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := queryOpToIndexOp(tt.op)
			if ok != tt.wantOK {
				t.Errorf("queryOpToIndexOp(%v) ok = %v, want %v", tt.op, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("queryOpToIndexOp(%v) = %v, want %v", tt.op, got, tt.want)
			}
		})
	}
}

func TestQueryOpToIndexOpFlip_AllOperators(t *testing.T) {
	tests := []struct {
		name   string
		op     BinaryOp
		want   index.PredicateOp
		wantOK bool
	}{
		{"OpLt_flips_to_OpGreater", OpLt, index.OpGreater, true},
		{"OpLe_flips_to_OpGreaterEqual", OpLe, index.OpGreaterEqual, true},
		{"OpGt_flips_to_OpLess", OpGt, index.OpLess, true},
		{"OpGe_flips_to_OpLessEqual", OpGe, index.OpLessEqual, true},
		{"OpEq_stays_OpEqual", OpEq, index.OpEqual, true},
		{"OpNe_stays_OpNotEqual", OpNe, index.OpNotEqual, true},
		{"OpAnd_unsupported", OpAnd, 0, false},
		{"OpOr_unsupported", OpOr, 0, false},
		{"OpAdd_unsupported", OpAdd, 0, false},
		{"OpLike_unsupported", OpLike, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := queryOpToIndexOpFlip(tt.op)
			if ok != tt.wantOK {
				t.Errorf("queryOpToIndexOpFlip(%v) ok = %v, want %v", tt.op, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("queryOpToIndexOpFlip(%v) = %v, want %v", tt.op, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// binaryExprToColumnPredicate: "column op literal" 和 "literal op column" 覆盖
// ---------------------------------------------------------------------------

func TestBinaryExprToColumnPredicate_ColumnOpLiteral(t *testing.T) {
	e := NewExecutor(newMockStorage())

	tests := []struct {
		name    string
		bin     *BinaryExpr
		wantOK  bool
		wantCol string
		wantOp  index.PredicateOp
		wantVal int64
	}{
		{
			name:   "column_eq_literal",
			bin:    &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpEqual, wantVal: 30,
		},
		{
			name:   "column_ne_literal",
			bin:    &BinaryExpr{Op: OpNe, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpNotEqual, wantVal: 30,
		},
		{
			name:   "column_lt_literal",
			bin:    &BinaryExpr{Op: OpLt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpLess, wantVal: 30,
		},
		{
			name:   "column_le_literal",
			bin:    &BinaryExpr{Op: OpLe, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpLessEqual, wantVal: 30,
		},
		{
			name:   "column_gt_literal",
			bin:    &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpGreater, wantVal: 30,
		},
		{
			name:   "column_ge_literal",
			bin:    &BinaryExpr{Op: OpGe, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpGreaterEqual, wantVal: 30,
		},
		{
			name:   "unsupported_op_And",
			bin:    &BinaryExpr{Op: OpAnd, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pred, ok := e.binaryExprToColumnPredicate(tt.bin)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if pred.ColumnName != tt.wantCol {
				t.Errorf("ColumnName = %q, want %q", pred.ColumnName, tt.wantCol)
			}
			if pred.Op != tt.wantOp {
				t.Errorf("Op = %v, want %v", pred.Op, tt.wantOp)
			}
			if pred.Value.Int64 != tt.wantVal {
				t.Errorf("Value = %v, want %v", pred.Value.Int64, tt.wantVal)
			}
		})
	}
}

func TestBinaryExprToColumnPredicate_LiteralOpColumn(t *testing.T) {
	e := NewExecutor(newMockStorage())

	tests := []struct {
		name    string
		bin     *BinaryExpr
		wantOK  bool
		wantCol string
		wantOp  index.PredicateOp
		wantVal int64
	}{
		{
			name:   "literal_lt_column_flips_to_column_gt",
			bin:    &BinaryExpr{Op: OpLt, Left: &LiteralExpr{Value: common.NewInt64(30)}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpGreater, wantVal: 30,
		},
		{
			name:   "literal_le_column_flips_to_column_ge",
			bin:    &BinaryExpr{Op: OpLe, Left: &LiteralExpr{Value: common.NewInt64(30)}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpGreaterEqual, wantVal: 30,
		},
		{
			name:   "literal_gt_column_flips_to_column_lt",
			bin:    &BinaryExpr{Op: OpGt, Left: &LiteralExpr{Value: common.NewInt64(30)}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpLess, wantVal: 30,
		},
		{
			name:   "literal_ge_column_flips_to_column_le",
			bin:    &BinaryExpr{Op: OpGe, Left: &LiteralExpr{Value: common.NewInt64(30)}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpLessEqual, wantVal: 30,
		},
		{
			name:   "literal_eq_column",
			bin:    &BinaryExpr{Op: OpEq, Left: &LiteralExpr{Value: common.NewInt64(30)}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpEqual, wantVal: 30,
		},
		{
			name:   "literal_ne_column",
			bin:    &BinaryExpr{Op: OpNe, Left: &LiteralExpr{Value: common.NewInt64(30)}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			wantOK: true, wantCol: testColAge, wantOp: index.OpNotEqual, wantVal: 30,
		},
		{
			name:   "unsupported_op_And_literal_column",
			bin:    &BinaryExpr{Op: OpAnd, Left: &LiteralExpr{Value: common.NewInt64(30)}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pred, ok := e.binaryExprToColumnPredicate(tt.bin)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if pred.ColumnName != tt.wantCol {
				t.Errorf("ColumnName = %q, want %q", pred.ColumnName, tt.wantCol)
			}
			if pred.Op != tt.wantOp {
				t.Errorf("Op = %v, want %v", pred.Op, tt.wantOp)
			}
			if pred.Value.Int64 != tt.wantVal {
				t.Errorf("Value = %v, want %v", pred.Value.Int64, tt.wantVal)
			}
		})
	}
}

func TestBinaryExprToColumnPredicate_EdgeCases(t *testing.T) {
	e := NewExecutor(newMockStorage())

	// 两侧都不是列表达式
	t.Run("neither_side_is_column", func(t *testing.T) {
		bin := &BinaryExpr{Op: OpEq, Left: &LiteralExpr{Value: common.NewInt64(1)}, Right: &LiteralExpr{Value: common.NewInt64(2)}}
		_, ok := e.binaryExprToColumnPredicate(bin)
		if ok {
			t.Error("expected false for literal op literal")
		}
	})

	// 列在左侧但右侧是 NULL（无效值）
	t.Run("column_op_null_literal", func(t *testing.T) {
		bin := &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewNull()}}
		_, ok := e.binaryExprToColumnPredicate(bin)
		if ok {
			t.Error("expected false for column op NULL")
		}
	})

	// 列在右侧但左侧是 NULL
	t.Run("null_literal_op_column", func(t *testing.T) {
		bin := &BinaryExpr{Op: OpEq, Left: &LiteralExpr{Value: common.NewNull()}, Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}}
		_, ok := e.binaryExprToColumnPredicate(bin)
		if ok {
			t.Error("expected false for NULL op column")
		}
	})
}

// ---------------------------------------------------------------------------
// extractColumnPredicates: 非 BinaryExpr 的 conjunct 被跳过
// ---------------------------------------------------------------------------

func TestExtractColumnPredicates_SkipsNonBinaryExpr(t *testing.T) {
	e := NewExecutor(newMockStorage())

	// 混合 AND：一个 BinaryExpr + 一个 UnaryExpr（非 BinaryExpr）
	pred := &BinaryExpr{
		Op:    OpAnd,
		Left:  &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
		Right: &UnaryExpr{Op: OpNot, Expr: &ColumnExpr{Name: "active"}},
	}

	preds := e.extractColumnPredicates(pred)
	if len(preds) != 1 {
		t.Fatalf("expected 1 predicate, got %d", len(preds))
	}
	if preds[0].ColumnName != testColAge {
		t.Errorf("ColumnName = %q, want %q", preds[0].ColumnName, testColAge)
	}
}

func TestExtractColumnPredicates_MultipleValid(t *testing.T) {
	e := NewExecutor(newMockStorage())

	pred := &BinaryExpr{
		Op:    OpAnd,
		Left:  &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
		Right: &BinaryExpr{Op: OpLt, Left: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(90.0)}},
	}

	preds := e.extractColumnPredicates(pred)
	if len(preds) != 2 {
		t.Fatalf("expected 2 predicates, got %d", len(preds))
	}
}

func TestExtractColumnPredicates_EmptyWhenNoValid(t *testing.T) {
	e := NewExecutor(newMockStorage())

	// 仅包含不支持的操作符
	pred := &BinaryExpr{Op: OpAnd, Left: &LiteralExpr{Value: common.NewInt64(1)}, Right: &LiteralExpr{Value: common.NewInt64(2)}}
	preds := e.extractColumnPredicates(pred)
	if len(preds) != 0 {
		t.Errorf("expected 0 predicates, got %d", len(preds))
	}
}

// ---------------------------------------------------------------------------
// scanWithPredicate: 有/无谓词路径覆盖
// ---------------------------------------------------------------------------

func TestScanWithPredicate_NilPredicate(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:     testTableUsers,
		Columns:   []string{testColID, testColName, testColAge, testColScore},
		Predicate: nil,
		schema:    buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if countRows(chunks) != 1 {
		t.Errorf("expected 1 row, got %d", countRows(chunks))
	}
}

func TestScanWithPredicate_WithColumnPredicate(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(20), testColScore: common.NewFloat64(60.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		Predicate: &BinaryExpr{
			Op:    OpGt,
			Left:  &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64},
			Right: &LiteralExpr{Value: common.NewInt64(25)},
		},
		schema: buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (age > 25), got %d", totalRows)
	}
}

func TestScanWithPredicate_LiteralOpColumn(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(20), testColScore: common.NewFloat64(60.0),
	})

	// "25 < age" 等价于 "age > 25"
	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		Predicate: &BinaryExpr{
			Op:    OpLt,
			Left:  &LiteralExpr{Value: common.NewInt64(25)},
			Right: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64},
		},
		schema: buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (25 < age), got %d", totalRows)
	}
}

// ---------------------------------------------------------------------------
// appendValueSafe: 类型不匹配时回退到 NULL
// ---------------------------------------------------------------------------

func TestAppendValueSafe_TypeMismatchFillsNull(t *testing.T) {
	// 创建一个 INT64 列，尝试追加 STRING 值
	col := storage.NewColumnVector(1, common.TypeInt64, 16)

	// 正常追加
	appendValueSafe(col, common.NewInt64(42), common.TypeInt64)
	if col.Len() != 1 {
		t.Fatalf("expected len 1, got %d", col.Len())
	}

	// 类型不匹配：STRING 追加到 INT64 列，应回退为 NULL
	appendValueSafe(col, common.NewString("not-an-int"), common.TypeInt64)
	if col.Len() != 2 {
		t.Fatalf("expected len 2, got %d", col.Len())
	}
	// 第二个值应该是 NULL
	v := col.GetValue(1)
	if !v.IsNull() {
		t.Errorf("expected NULL at index 1, got %v", v)
	}
}

func TestAppendValueSafe_CoerceFloatToInt(t *testing.T) {
	col := storage.NewColumnVector(1, common.TypeInt64, 16)

	// FLOAT64 值追加到 INT64 列，coerceValue 应转换成功
	appendValueSafe(col, common.NewFloat64(42.0), common.TypeInt64)
	if col.Len() != 1 {
		t.Fatalf("expected len 1, got %d", col.Len())
	}
	v := col.GetValue(0)
	if v.Typ != common.TypeInt64 || v.Int64 != 42 {
		t.Errorf("expected Int64(42), got %v", v)
	}
}
