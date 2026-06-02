package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestExecutorFilterBasic(t *testing.T) {
	ms := newMockStorage()
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		ms.addEntry(key, map[string]common.Value{
			"id":    common.NewInt64(int64(i)),
			"name":  common.NewString(key),
			"age":   common.NewInt64(int64(20 + i)),
			"score": common.NewFloat64(float64(60 + i)),
		})
	}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(25)}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 4 {
		t.Errorf("expected 4 rows (age > 25), got %d", totalRows)
	}
}

func TestExecutorFilterAndCondition(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		"id": common.NewInt64(3), "name": common.NewString("charlie"),
		"age": common.NewInt64(30), "score": common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child: scan,
		Condition: &BinaryExpr{
			Op:    OpAnd,
			Left:  &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			Right: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(80.0)}},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter and: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (age=30 AND score>80), got %d", totalRows)
	}
}

func TestExecutorFilterOrCondition(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		"id": common.NewInt64(3), "name": common.NewString("charlie"),
		"age": common.NewInt64(35), "score": common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child: scan,
		Condition: &BinaryExpr{
			Op:    OpOr,
			Left:  &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(25)}},
			Right: &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(35)}},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter or: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 rows (age=25 OR age=35), got %d", totalRows)
	}
}

func TestExecutorFilterNotCondition(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child: scan,
		Condition: &UnaryExpr{
			Op:   OpNot,
			Expr: &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter not: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (NOT age=30), got %d", totalRows)
	}
}

func TestExecutorFilterEmptyResult(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(100)}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter empty: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("expected 0 rows, got %d", totalRows)
	}
}

func TestExecutorFilterWithNull(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewNull(), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter null: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (NULL age should not match), got %d", totalRows)
	}
}

func TestExecutorFilterAndShortCircuit(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(0), "name": common.NewString("zero"),
		"age": common.NewInt64(0), "score": common.NewFloat64(0),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("one"),
		"age": common.NewInt64(1), "score": common.NewFloat64(1),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child: scan,
		Condition: &BinaryExpr{
			Op:    OpAnd,
			Left:  &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(0)}},
			Right: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(0.5)}},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute AND short-circuit: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("expected 0 rows (age=0 AND score>0.5), got %d", totalRows)
	}
}

func TestExecutorOrShortCircuit(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child: scan,
		Condition: &BinaryExpr{
			Op:    OpOr,
			Left:  &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			Right: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(100)}},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute OR short-circuit: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (age=30 OR score>100), got %d", totalRows)
	}
}

func TestExecutorFilterNullComparison(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewNull(),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString}, Right: &LiteralExpr{Value: common.NewString("bob")}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter null comparison: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (NULL != 'bob'), got %d", totalRows)
	}
}

func TestExecutorFilterWithFuncExpr(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child: scan,
		Condition: &BinaryExpr{
			Op:    OpAnd,
			Left:  &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(25)}},
			Right: &FuncExpr{Name: "unknown_func", Args: nil},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	// FuncExpr in filter should result in NULL evaluation, which is falsy
	if err != nil {
		t.Fatalf("execute filter with func expr: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("expected 0 rows (func expr returns NULL), got %d", totalRows)
	}
}

func TestExecutorFilterWithColumnExpr(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(0), "name": common.NewString("zero"),
		"age": common.NewInt64(0), "score": common.NewFloat64(0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	// Use ColumnExpr instead of ResolvedColumnExpr
	filter := &FilterNode{
		Child:     scan,
		Condition: &ColumnExpr{Name: "id"},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter column expr: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (id != 0 via ColumnExpr), got %d", totalRows)
	}
}

func TestExecutorBoolFilter(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(0), "name": common.NewString("zero"),
		"age": common.NewInt64(0), "score": common.NewFloat64(0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &ResolvedColumnExpr{Name: "id", Idx: 0, typ: common.TypeInt64},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute bool filter: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (id != 0), got %d", totalRows)
	}
}

func TestExecutorStringNotEqual(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpNe, Left: &ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString}, Right: &LiteralExpr{Value: common.NewString("alice")}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute string ne: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (name != 'alice'), got %d", totalRows)
	}
}
