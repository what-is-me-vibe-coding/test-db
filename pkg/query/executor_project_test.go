package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestExecutorProjectBasic(t *testing.T) {
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

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString},
			&ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
		},
		Aliases: []string{"", ""},
		schema: []ColumnDef{
			{Name: "name", Type: common.TypeString, Nullable: true},
			{Name: "age", Type: common.TypeInt64, Nullable: true},
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
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(25)}},
	}

	project := &ProjectNode{
		Child: filter,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString},
			&ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
		},
		Aliases: []string{"", ""},
		schema: []ColumnDef{
			{Name: "name", Type: common.TypeString, Nullable: true},
			{Name: "age", Type: common.TypeInt64, Nullable: true},
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
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString},
			&BinaryExpr{Op: OpMul, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(2)}},
		},
		Aliases: []string{"name", "double_age"},
		schema: []ColumnDef{
			{Name: "name", Type: common.TypeString, Nullable: true},
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
		if nameVal.Str != "alice" {
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
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	// Project int column as float (type coercion)
	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
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

func TestExecutorColumnExpr(t *testing.T) {
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

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&ColumnExpr{Name: "name"},
		},
		Aliases: []string{"name"},
		schema: []ColumnDef{
			{Name: "name", Type: common.TypeString, Nullable: true},
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
		if val.Str != "alice" {
			t.Errorf("expected 'alice', got %q", val.Str)
		}
	}
}

func TestExecutorFullPipeline(t *testing.T) {
	ms := newMockStorage()
	for i := 0; i < 20; i++ {
		key := fmtKey(i)
		ms.addEntry(key, map[string]common.Value{
			"id":    common.NewInt64(int64(i)),
			"name":  common.NewString(key),
			"age":   common.NewInt64(int64(20 + i%10)),
			"score": common.NewFloat64(float64(80 + i%20)),
		})
	}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(70)}},
	}

	project := &ProjectNode{
		Child: filter,
		Expressions: []Expression{
			&ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString},
			&ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64},
		},
		Aliases: []string{"name", "score"},
		schema: []ColumnDef{
			{Name: "name", Type: common.TypeString, Nullable: true},
			{Name: "score", Type: common.TypeFloat64, Nullable: true},
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
		Child:     scan,
		Condition: &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "name", Idx: 1, typ: common.TypeString}, Right: &LiteralExpr{Value: common.NewString("bob")}},
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
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&BinaryExpr{Op: OpDiv, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(0)}},
		},
		Aliases: []string{"div_zero"},
		schema: []ColumnDef{
			{Name: "div_zero", Type: common.TypeInt64, Nullable: true},
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
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: "MIN(age)", Type: common.TypeInt64, Nullable: true},
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
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: "MAX(age)", Type: common.TypeInt64, Nullable: true},
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
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: "AVG(age)", Type: common.TypeFloat64, Nullable: true},
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
