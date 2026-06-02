package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestExecutorAggregateCount(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
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
		},
		schema: []ColumnDef{
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate count: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (COUNT), got %d", totalRows)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		val := func() common.Value { c, _ := chunks[0].GetColumn(0); return c.GetValue(0) }()
		if val.Int64 != 2 {
			t.Errorf("expected COUNT(*) = 2, got %d", val.Int64)
		}
	}
}

func TestExecutorAggregateSum(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
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
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: testAggSumScore, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate sum: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		val := func() common.Value { c, _ := chunks[0].GetColumn(0); return c.GetValue(0) }()
		expected := 95.5 + 88.0
		if val.Float64 != expected {
			t.Errorf("expected SUM(score) = %g, got %g", expected, val.Float64)
		}
	}
}

func TestExecutorAggregateGroupBy(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameCharlie),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(72.0),
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
		},
		schema: []ColumnDef{
			{Name: testColAge, Type: common.TypeInt64, Nullable: true},
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate group by: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 groups, got %d", totalRows)
	}
}

func TestExecutorAggregateMin(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
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
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggMinAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate min: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		val := func() common.Value { c, _ := chunks[0].GetColumn(0); return c.GetValue(0) }()
		if val.Int64 != 25 {
			t.Errorf("expected MIN(age) = 25, got %d", val.Int64)
		}
	}
}

func TestExecutorAggregateMax(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
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
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggMaxAge, Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate max: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		val := func() common.Value { c, _ := chunks[0].GetColumn(0); return c.GetValue(0) }()
		if val.Int64 != 30 {
			t.Errorf("expected MAX(age) = 30, got %d", val.Int64)
		}
	}
}

func TestExecutorAggregateAvg(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(90.0),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(20), testColScore: common.NewFloat64(70.0),
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
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggAvgAge, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate avg: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		val := func() common.Value { c, _ := chunks[0].GetColumn(0); return c.GetValue(0) }()
		expected := 25.0
		if val.Float64 != expected {
			t.Errorf("expected AVG(age) = %g, got %g", expected, val.Float64)
		}
	}
}

func TestExecutorAggregateWithNulls(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewNull(), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewNull(),
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
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: testAggSumAge, Type: common.TypeFloat64, Nullable: true},
			{Name: testAggAvgScore, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate with nulls: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		sumCol, _ := chunks[0].GetColumn(0)
		sumVal := sumCol.GetValue(0)
		if sumVal.Float64 != 25.0 {
			t.Errorf("expected SUM(age) = 25.0 (skip NULL), got %g", sumVal.Float64)
		}

		avgCol, _ := chunks[0].GetColumn(1)
		avgVal := avgCol.GetValue(0)
		if avgVal.Float64 != 95.5 {
			t.Errorf("expected AVG(score) = 95.5 (skip NULL), got %g", avgVal.Float64)
		}
	}
}

func TestExecutorMultipleAggregates(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(90.0),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(20), testColScore: common.NewFloat64(80.0),
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
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
			{Name: testAggSumAge, Type: common.TypeFloat64, Nullable: true},
			{Name: testAggMinAge, Type: common.TypeInt64, Nullable: true},
			{Name: testAggMaxAge, Type: common.TypeInt64, Nullable: true},
			{Name: testAggAvgAge, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute multiple aggregates: %v", err)
	}

	if len(chunks) == 0 || chunks[0].RowCount() == 0 {
		t.Fatal("expected at least 1 row")
	}

	countCol, _ := chunks[0].GetColumn(0)
	if countCol.GetValue(0).Int64 != 2 {
		t.Errorf("expected COUNT=2, got %d", countCol.GetValue(0).Int64)
	}

	sumCol, _ := chunks[0].GetColumn(1)
	if sumCol.GetValue(0).Float64 != 50.0 {
		t.Errorf("expected SUM=50.0, got %g", sumCol.GetValue(0).Float64)
	}

	minCol, _ := chunks[0].GetColumn(2)
	if minCol.GetValue(0).Int64 != 20 {
		t.Errorf("expected MIN=20, got %d", minCol.GetValue(0).Int64)
	}

	maxCol, _ := chunks[0].GetColumn(3)
	if maxCol.GetValue(0).Int64 != 30 {
		t.Errorf("expected MAX=30, got %d", maxCol.GetValue(0).Int64)
	}

	avgCol, _ := chunks[0].GetColumn(4)
	if avgCol.GetValue(0).Float64 != 25.0 {
		t.Errorf("expected AVG=25.0, got %g", avgCol.GetValue(0).Float64)
	}
}

func TestExecutorAggregateMinFloat(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(72.0),
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
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: testAggMinScore, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate min float: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != 72.0 {
			t.Errorf("expected MIN(score) = 72.0, got %g", val.Float64)
		}
	}
}

func TestExecutorAggregateMaxFloat(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(72.0),
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
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: testColScore, Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: testAggMaxScore, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate max float: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != 95.5 {
			t.Errorf("expected MAX(score) = 95.5, got %g", val.Float64)
		}
	}
}
