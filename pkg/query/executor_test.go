package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// mockStorage 实现 StorageProvider 接口，用于测试。
type mockStorage struct {
	entries    []storage.ScanEntry
	columnMeta []storage.ColumnMeta
	pkIndex    *index.PrimaryIndex
	spIndex    *index.SparseIndex
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		pkIndex: index.NewPrimaryIndex(),
		spIndex: index.NewSparseIndex(),
	}
}

func (m *mockStorage) ScanRange(start, end string) []storage.ScanEntry {
	var result []storage.ScanEntry
	for _, e := range m.entries {
		if e.Key >= start && e.Key <= end {
			result = append(result, e)
		}
	}
	return result
}

func (m *mockStorage) ColumnMeta() []storage.ColumnMeta {
	return m.columnMeta
}

func (m *mockStorage) PrimaryIndex() *index.PrimaryIndex {
	return m.pkIndex
}

func (m *mockStorage) SparseIndex() *index.SparseIndex {
	return m.spIndex
}

func (m *mockStorage) addEntry(key string, cols map[string]common.Value) {
	m.entries = append(m.entries, storage.ScanEntry{
		Key:   key,
		Value: storage.Row{Columns: cols},
	})
}

// buildTestSchema 构建测试用 schema。
func buildTestSchema() []ColumnDef {
	return []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "age", Type: common.TypeInt64, Nullable: true},
		{Name: "score", Type: common.TypeFloat64, Nullable: true},
	}
}

func TestExecutorScanBasic(t *testing.T) {
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

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 rows, got %d", totalRows)
	}
}

func TestExecutorScanWithPredicate(t *testing.T) {
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
		Table:     "users",
		Columns:   []string{"id", "name", "age", "score"},
		Predicate: &BinaryExpr{Op: OpGt, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(28)}},
		schema:    buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan with predicate: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2 {
		t.Errorf("expected 2 rows (age > 28), got %d", totalRows)
	}
}

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

func TestExecutorLimitBasic(t *testing.T) {
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
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
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

func TestExecutorAggregateCount(t *testing.T) {
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

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
		},
		schema: []ColumnDef{
			{Name: "COUNT(*)", Type: common.TypeInt64, Nullable: false},
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

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: "SUM(score)", Type: common.TypeFloat64, Nullable: true},
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
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(30), "score": common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		"id": common.NewInt64(3), "name": common.NewString("charlie"),
		"age": common.NewInt64(25), "score": common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child: scan,
		GroupBy: []Expression{
			&ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
		},
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
		},
		schema: []ColumnDef{
			{Name: "age", Type: common.TypeInt64, Nullable: true},
			{Name: "COUNT(*)", Type: common.TypeInt64, Nullable: false},
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

func TestExecutorScanEmpty(t *testing.T) {
	ms := newMockStorage()

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan empty: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("expected 0 rows, got %d", totalRows)
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

func TestExecutorFilterLimit(t *testing.T) {
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

func TestExecutorComparisonOperators(t *testing.T) {
	tests := []struct {
		name string
		op   BinaryOp
		want int
	}{
		{"eq", OpEq, 1},
		{"ne", OpNe, 2},
		{"lt", OpLt, 1},
		{"le", OpLe, 2},
		{"gt", OpGt, 1},
		{"ge", OpGe, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				Condition: &BinaryExpr{Op: tt.op, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(30)}},
			}

			exec := NewExecutor(ms)
			chunks, err := exec.Execute(filter)
			if err != nil {
				t.Fatalf("execute %s: %v", tt.name, err)
			}

			totalRows := countRows(chunks)
			if totalRows != tt.want {
				t.Errorf("op %s: expected %d rows, got %d", tt.op, tt.want, totalRows)
			}
		})
	}
}

func TestExecutorArithmeticExpressions(t *testing.T) {
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
			&BinaryExpr{Op: OpAdd, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(5)}},
		},
		Aliases: []string{"age_plus_5"},
		schema: []ColumnDef{
			{Name: "age_plus_5", Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute arithmetic: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		val := func() common.Value { c, _ := chunks[0].GetColumn(0); return c.GetValue(0) }()
		if val.Int64 != 35 {
			t.Errorf("expected age+5 = 35, got %d", val.Int64)
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

func TestExecutorLargeDataset(t *testing.T) {
	ms := newMockStorage()
	for i := 0; i < 2000; i++ {
		key := fmtKey(i)
		ms.addEntry(key, map[string]common.Value{
			"id":    common.NewInt64(int64(i)),
			"name":  common.NewString(key),
			"age":   common.NewInt64(int64(20 + i%50)),
			"score": common.NewFloat64(float64(50 + i%50)),
		})
	}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute large dataset: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 2000 {
		t.Errorf("expected 2000 rows, got %d", totalRows)
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

func TestExecutorAggregateMin(t *testing.T) {
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
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(90.0),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(20), "score": common.NewFloat64(70.0),
	})

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

// countRows 统计所有 Chunk 的总行数。
func countRows(chunks []*storage.Chunk) int {
	total := 0
	for _, c := range chunks {
		total += int(c.RowCount())
	}
	return total
}

// fmtKey 生成测试用 key。
func fmtKey(i int) string {
	return fmtIntKey(i)
}

func fmtIntKey(i int) string {
	const digits = "0123456789abcdef"
	if i < 16 {
		return string(digits[i])
	}
	return fmtIntKey(i/16) + string(digits[i%16])
}

func TestExecutorScanWithKeyRange(t *testing.T) {
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
		Predicate: &BinaryExpr{
			Op:    OpGe,
			Left:  &ResolvedColumnExpr{Name: "id", Idx: 0, typ: common.TypeInt64},
			Right: &LiteralExpr{Value: common.NewString("b")},
		},
		schema: buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan with key range: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows < 1 {
		t.Errorf("expected at least 1 row with key range, got %d", totalRows)
	}
}

func TestExecutorUnaryNeg(t *testing.T) {
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
			&UnaryExpr{Op: OpNeg, Expr: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		Aliases: []string{"neg_age"},
		schema: []ColumnDef{
			{Name: "neg_age", Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute unary neg: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Int64 != -30 {
			t.Errorf("expected -30, got %d", val.Int64)
		}
	}
}

func TestExecutorUnaryNegFloat(t *testing.T) {
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
			&UnaryExpr{Op: OpNeg, Expr: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}},
		},
		Aliases: []string{"neg_score"},
		schema: []ColumnDef{
			{Name: "neg_score", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute unary neg float: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != -95.5 {
			t.Errorf("expected -95.5, got %g", val.Float64)
		}
	}
}

func TestExecutorArithmeticSub(t *testing.T) {
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
			&BinaryExpr{Op: OpSub, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(10)}},
		},
		Aliases: []string{"age_minus_10"},
		schema: []ColumnDef{
			{Name: "age_minus_10", Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute sub: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Int64 != 20 {
			t.Errorf("expected 30-10=20, got %d", val.Int64)
		}
	}
}

func TestExecutorArithmeticMul(t *testing.T) {
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
			&BinaryExpr{Op: OpMul, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(2)}},
		},
		Aliases: []string{"age_times_2"},
		schema: []ColumnDef{
			{Name: "age_times_2", Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute mul: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Int64 != 60 {
			t.Errorf("expected 30*2=60, got %d", val.Int64)
		}
	}
}

func TestExecutorArithmeticDiv(t *testing.T) {
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
			&BinaryExpr{Op: OpDiv, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewInt64(3)}},
		},
		Aliases: []string{"age_div_3"},
		schema: []ColumnDef{
			{Name: "age_div_3", Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute div: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Int64 != 10 {
			t.Errorf("expected 30/3=10, got %d", val.Int64)
		}
	}
}

func TestExecutorArithmeticFloatAdd(t *testing.T) {
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
			&BinaryExpr{Op: OpAdd, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(4.5)}},
		},
		Aliases: []string{"score_plus"},
		schema: []ColumnDef{
			{Name: "score_plus", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute float add: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != 100.0 {
			t.Errorf("expected 95.5+4.5=100.0, got %g", val.Float64)
		}
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

func TestExecutorAggregateEmptyInput(t *testing.T) {
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
			{Func: AggCount, Arg: &StarExpr{}},
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: "COUNT(*)", Type: common.TypeInt64, Nullable: false},
			{Name: "SUM(age)", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(agg)
	if err != nil {
		t.Fatalf("execute aggregate empty: %v", err)
	}

	// Empty input should still produce 1 row for COUNT (0) and NULL for SUM
	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row for aggregate on empty input, got %d", totalRows)
	}
}

func TestExecutorAggregateWithNulls(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewNull(), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewNull(),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: "SUM(age)", Type: common.TypeFloat64, Nullable: true},
			{Name: "AVG(score)", Type: common.TypeFloat64, Nullable: true},
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

func TestExecutorUnsupportedNode(t *testing.T) {
	exec := NewExecutor(newMockStorage())
	_, err := exec.Execute(nil)
	if err == nil {
		t.Error("expected error for nil plan node")
	}
}

func TestExecutorLimitOffsetBeyondData(t *testing.T) {
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

func TestExecutorMultipleAggregates(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(90.0),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(20), "score": common.NewFloat64(80.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
			{Func: AggAvg, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: "COUNT(*)", Type: common.TypeInt64, Nullable: false},
			{Name: "SUM(age)", Type: common.TypeFloat64, Nullable: true},
			{Name: "MIN(age)", Type: common.TypeInt64, Nullable: true},
			{Name: "MAX(age)", Type: common.TypeInt64, Nullable: true},
			{Name: "AVG(age)", Type: common.TypeFloat64, Nullable: true},
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

func TestExecutorScanWithEqKeyRange(t *testing.T) {
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

	// Test with predicate on non-key column (age) - uses full scan + filter
	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		Predicate: &BinaryExpr{
			Op:    OpEq,
			Left:  &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
			Right: &LiteralExpr{Value: common.NewInt64(25)},
		},
		schema: buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan eq: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (age=25), got %d", totalRows)
	}
}

func TestExecutorScanWithLtKeyRange(t *testing.T) {
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
		Predicate: &BinaryExpr{
			Op:    OpLt,
			Left:  &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
			Right: &LiteralExpr{Value: common.NewInt64(30)},
		},
		schema: buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan lt: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (age<30), got %d", totalRows)
	}
}

func TestExecutorScanWithLeKeyRange(t *testing.T) {
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
		Predicate: &BinaryExpr{
			Op:    OpLe,
			Left:  &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
			Right: &LiteralExpr{Value: common.NewInt64(30)},
		},
		schema: buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan le key range: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows < 1 {
		t.Errorf("expected at least 1 row, got %d", totalRows)
	}
}

func TestExecutorScanWithGtKeyRange(t *testing.T) {
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
		Predicate: &BinaryExpr{
			Op:    OpGt,
			Left:  &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64},
			Right: &LiteralExpr{Value: common.NewInt64(25)},
		},
		schema: buildTestSchema(),
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan gt key range: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows < 1 {
		t.Errorf("expected at least 1 row, got %d", totalRows)
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

func TestExecutorUnaryNegNull(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewNull(), "score": common.NewFloat64(95.5),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	project := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&UnaryExpr{Op: OpNeg, Expr: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		Aliases: []string{"neg_age"},
		schema: []ColumnDef{
			{Name: "neg_age", Type: common.TypeInt64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute unary neg null: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Valid {
			t.Errorf("expected NULL for negation of NULL, got %v", val)
		}
	}
}

func TestExecutorFilterWithNullLiteral(t *testing.T) {
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

	// Comparison with NULL literal should result in NULL (falsy)
	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpEq, Left: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}, Right: &LiteralExpr{Value: common.NewNull()}},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter null literal: %v", err)
	}

	totalRows := countRows(chunks)
	if totalRows != 0 {
		t.Errorf("expected 0 rows (comparison with NULL), got %d", totalRows)
	}
}

func TestExecutorAggregateMinFloat(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggMin, Arg: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: "MIN(score)", Type: common.TypeFloat64, Nullable: true},
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
		"id": common.NewInt64(1), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"id": common.NewInt64(2), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name", "age", "score"},
		schema:  buildTestSchema(),
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: nil,
		Aggregates: []AggregateExpr{
			{Func: AggMax, Arg: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}},
		},
		schema: []ColumnDef{
			{Name: "MAX(score)", Type: common.TypeFloat64, Nullable: true},
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

func TestExecutorArithmeticFloatDiv(t *testing.T) {
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
			&BinaryExpr{Op: OpDiv, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(2.0)}},
		},
		Aliases: []string{"half_score"},
		schema: []ColumnDef{
			{Name: "half_score", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute float div: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != 47.75 {
			t.Errorf("expected 95.5/2=47.75, got %g", val.Float64)
		}
	}
}

func TestExecutorArithmeticFloatDivByZero(t *testing.T) {
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
			&BinaryExpr{Op: OpDiv, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(0.0)}},
		},
		Aliases: []string{"div_zero"},
		schema: []ColumnDef{
			{Name: "div_zero", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute float div by zero: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Valid {
			t.Errorf("expected NULL for float division by zero, got %v", val)
		}
	}
}

func TestExecutorArithmeticFloatSub(t *testing.T) {
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
			&BinaryExpr{Op: OpSub, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(5.5)}},
		},
		Aliases: []string{"score_minus"},
		schema: []ColumnDef{
			{Name: "score_minus", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute float sub: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != 90.0 {
			t.Errorf("expected 95.5-5.5=90.0, got %g", val.Float64)
		}
	}
}

func TestExecutorArithmeticFloatMul(t *testing.T) {
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
			&BinaryExpr{Op: OpMul, Left: &ResolvedColumnExpr{Name: "score", Idx: 3, typ: common.TypeFloat64}, Right: &LiteralExpr{Value: common.NewFloat64(2.0)}},
		},
		Aliases: []string{"double_score"},
		schema: []ColumnDef{
			{Name: "double_score", Type: common.TypeFloat64, Nullable: true},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(project)
	if err != nil {
		t.Fatalf("execute float mul: %v", err)
	}

	if len(chunks) > 0 && chunks[0].RowCount() > 0 {
		col, _ := chunks[0].GetColumn(0)
		val := col.GetValue(0)
		if val.Float64 != 191.0 {
			t.Errorf("expected 95.5*2=191.0, got %g", val.Float64)
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

func TestExecutorAggregateEmptySum(t *testing.T) {
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
			{Func: AggSum, Arg: &ResolvedColumnExpr{Name: "age", Idx: 2, typ: common.TypeInt64}},
		},
		schema: []ColumnDef{
			{Name: "SUM(age)", Type: common.TypeFloat64, Nullable: true},
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

func TestExecutorScanWithPrimaryKeyRange(t *testing.T) {
	// Test extractKeyRange with primary key column (Idx 0)
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"key": common.NewString("a"), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"key": common.NewString("b"), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		"key": common.NewString("c"), "name": common.NewString("charlie"),
		"age": common.NewInt64(35), "score": common.NewFloat64(72.0),
	})

	// Schema with string primary key as first column
	schema := []ColumnDef{
		{Name: "key", Type: common.TypeString, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "age", Type: common.TypeInt64, Nullable: true},
		{Name: "score", Type: common.TypeFloat64, Nullable: true},
	}

	// Test EQ on primary key
	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"key", "name", "age", "score"},
		Predicate: &BinaryExpr{
			Op:    OpEq,
			Left:  &ResolvedColumnExpr{Name: "key", Idx: 0, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("b")},
		},
		schema: schema,
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(scan)
	if err != nil {
		t.Fatalf("execute scan pk eq: %v", err)
	}

	// Should find entry with key "b"
	totalRows := countRows(chunks)
	if totalRows != 1 {
		t.Errorf("expected 1 row (key='b'), got %d", totalRows)
	}
}

func TestExecutorScanWithPrimaryKeyGtRange(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		"key": common.NewString("a"), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"key": common.NewString("b"), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		"key": common.NewString("c"), "name": common.NewString("charlie"),
		"age": common.NewInt64(35), "score": common.NewFloat64(72.0),
	})

	schema := []ColumnDef{
		{Name: "key", Type: common.TypeString, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "age", Type: common.TypeInt64, Nullable: true},
		{Name: "score", Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"key", "name", "age", "score"},
		Predicate: &BinaryExpr{
			Op:    OpGt,
			Left:  &ResolvedColumnExpr{Name: "key", Idx: 0, typ: common.TypeString},
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
		"key": common.NewString("a"), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"key": common.NewString("b"), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		"key": common.NewString("c"), "name": common.NewString("charlie"),
		"age": common.NewInt64(35), "score": common.NewFloat64(72.0),
	})

	schema := []ColumnDef{
		{Name: "key", Type: common.TypeString, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "age", Type: common.TypeInt64, Nullable: true},
		{Name: "score", Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"key", "name", "age", "score"},
		Predicate: &BinaryExpr{
			Op:    OpLt,
			Left:  &ResolvedColumnExpr{Name: "key", Idx: 0, typ: common.TypeString},
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
		"key": common.NewString("a"), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"key": common.NewString("b"), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})

	schema := []ColumnDef{
		{Name: "key", Type: common.TypeString, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "age", Type: common.TypeInt64, Nullable: true},
		{Name: "score", Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"key", "name", "age", "score"},
		Predicate: &BinaryExpr{
			Op:    OpLe,
			Left:  &ResolvedColumnExpr{Name: "key", Idx: 0, typ: common.TypeString},
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
		"key": common.NewString("a"), "name": common.NewString("alice"),
		"age": common.NewInt64(30), "score": common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		"key": common.NewString("b"), "name": common.NewString("bob"),
		"age": common.NewInt64(25), "score": common.NewFloat64(88.0),
	})

	schema := []ColumnDef{
		{Name: "key", Type: common.TypeString, Nullable: false},
		{Name: "name", Type: common.TypeString, Nullable: true},
		{Name: "age", Type: common.TypeInt64, Nullable: true},
		{Name: "score", Type: common.TypeFloat64, Nullable: true},
	}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"key", "name", "age", "score"},
		Predicate: &BinaryExpr{
			Op:    OpGe,
			Left:  &ResolvedColumnExpr{Name: "key", Idx: 0, typ: common.TypeString},
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
