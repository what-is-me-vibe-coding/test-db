package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestExecutorLimitBasic(t *testing.T) {
	ms := newMockStorage()
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		ms.addEntry(key, map[string]common.Value{
			testColID:    common.NewInt64(int64(i)),
			testColName:  common.NewString(key),
			testColAge:   common.NewInt64(int64(20 + i)),
			testColScore: common.NewFloat64(float64(60 + i)),
		})
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	limit := &LimitNode{
		Child:  scan,
		Offset: 0,
		Count:  3,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(limit)
	if err != nil {
		t.Fatalf("execute limit: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 3 {
		t.Errorf("expected 3 rows (LIMIT 3), got %d", totalRows)
	}
}

func TestExecutorLimitWithOffset(t *testing.T) {
	ms := newMockStorage()
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		ms.addEntry(key, map[string]common.Value{
			testColID:    common.NewInt64(int64(i)),
			testColName:  common.NewString(key),
			testColAge:   common.NewInt64(int64(20 + i)),
			testColScore: common.NewFloat64(float64(60 + i)),
		})
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	limit := &LimitNode{
		Child:  scan,
		Offset: 5,
		Count:  3,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(limit)
	if err != nil {
		t.Fatalf("execute limit offset: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 3 {
		t.Errorf("expected 3 rows (LIMIT 5,3), got %d", totalRows)
	}
}

func TestExecutorLimitMoreThanAvailable(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString("alice"),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	limit := &LimitNode{
		Child:  scan,
		Offset: 0,
		Count:  100,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(limit)
	if err != nil {
		t.Fatalf("execute limit overflow: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (LIMIT 100 but only 1), got %d", totalRows)
	}
}

func TestExecutorLimitOffsetBeyondData(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString("alice"),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	limit := &LimitNode{
		Child:  scan,
		Offset: 10,
		Count:  5,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(limit)
	if err != nil {
		t.Fatalf("execute limit beyond data: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("expected 0 rows (offset beyond data), got %d", totalRows)
	}
}

func TestExecutorFilterLimit(t *testing.T) {
	ms := newMockStorage()
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		ms.addEntry(key, map[string]common.Value{
			testColID:    common.NewInt64(int64(i)),
			testColName:  common.NewString(key),
			testColAge:   common.NewInt64(int64(20 + i)),
			testColScore: common.NewFloat64(float64(60 + i)),
		})
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(25)}},
	}

	limit := &LimitNode{
		Child:  filter,
		Offset: 0,
		Count:  2,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(limit)
	if err != nil {
		t.Fatalf("execute filter+limit: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 rows (filtered + LIMIT 2), got %d", totalRows)
	}
}

func TestExecutorScanWithPrimaryKeyRange(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColKey: common.NewString("a"), testColName: common.NewString("alice"),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColKey: common.NewString("b"), testColName: common.NewString("bob"),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColKey: common.NewString("c"), testColName: common.NewString("charlie"),
		testColAge: common.NewInt64(35), testColScore: common.NewFloat64(72.0),
	})

	schema := []ColumnDef{
		{Name: testColKey, Type: common.TypeString, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
		{Name: testColScore, Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColKey, testColName, testColAge, testColScore},
		Predicate: &BinaryExpr{
			Op:    OpEq,
			Left:  &ResolvedColumnExpr{Name: testColKey, Idx: 0, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("b")},
		},
		schema: schema,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan pk eq: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (key='b'), got %d", totalRows)
	}
}

func TestExecutorScanWithPrimaryKeyGtRange(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColKey: common.NewString("a"), testColName: common.NewString("alice"),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColKey: common.NewString("b"), testColName: common.NewString("bob"),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColKey: common.NewString("c"), testColName: common.NewString("charlie"),
		testColAge: common.NewInt64(35), testColScore: common.NewFloat64(72.0),
	})

	schema := []ColumnDef{
		{Name: testColKey, Type: common.TypeString, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
		{Name: testColScore, Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColKey, testColName, testColAge, testColScore},
		Predicate: &BinaryExpr{
			Op:    OpGt,
			Left:  &ResolvedColumnExpr{Name: testColKey, Idx: 0, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("a")},
		},
		schema: schema,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan pk gt: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 rows (key>'a'), got %d", totalRows)
	}
}

func TestExecutorScanWithPrimaryKeyLtRange(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColKey: common.NewString("a"), testColName: common.NewString("alice"),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColKey: common.NewString("b"), testColName: common.NewString("bob"),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColKey: common.NewString("c"), testColName: common.NewString("charlie"),
		testColAge: common.NewInt64(35), testColScore: common.NewFloat64(72.0),
	})

	schema := []ColumnDef{
		{Name: testColKey, Type: common.TypeString, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
		{Name: testColScore, Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColKey, testColName, testColAge, testColScore},
		Predicate: &BinaryExpr{
			Op:    OpLt,
			Left:  &ResolvedColumnExpr{Name: testColKey, Idx: 0, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("c")},
		},
		schema: schema,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan pk lt: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 rows (key<'c'), got %d", totalRows)
	}
}

func TestExecutorScanWithPrimaryKeyLeRange(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColKey: common.NewString("a"), testColName: common.NewString("alice"),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColKey: common.NewString("b"), testColName: common.NewString("bob"),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})

	schema := []ColumnDef{
		{Name: testColKey, Type: common.TypeString, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
		{Name: testColScore, Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColKey, testColName, testColAge, testColScore},
		Predicate: &BinaryExpr{
			Op:    OpLe,
			Left:  &ResolvedColumnExpr{Name: testColKey, Idx: 0, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("a")},
		},
		schema: schema,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan pk le: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (key<='a'), got %d", totalRows)
	}
}

func TestExecutorScanWithPrimaryKeyGeRange(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColKey: common.NewString("a"), testColName: common.NewString("alice"),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColKey: common.NewString("b"), testColName: common.NewString("bob"),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})

	schema := []ColumnDef{
		{Name: testColKey, Type: common.TypeString, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
		{Name: testColScore, Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColKey, testColName, testColAge, testColScore},
		Predicate: &BinaryExpr{
			Op:    OpGe,
			Left:  &ResolvedColumnExpr{Name: testColKey, Idx: 0, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("b")},
		},
		schema: schema,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan pk ge: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (key>='b'), got %d", totalRows)
	}
}

func TestExecutorAggregateEmptyInput(t *testing.T) {
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
			{Func: AggCount, Arg: &StarExpr{}},
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggCountStar, Type: common.TypeInt64, Nullable: false},
			{Name: testAggSumAge, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate empty: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row for aggregate on empty input, got %d", totalRows)
	}
}

func TestExecutorAggregateEmptySum(t *testing.T) {
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
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: testColAge, Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: testAggSumAge, Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate empty sum: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Valid {
			t.Errorf("expected NULL for SUM on empty input, got %v", val)
		}
	}
}
