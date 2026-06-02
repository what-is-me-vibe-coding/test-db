package query

import (
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

const (
	testColName      = "name"
	testColAge       = "age"
	testNameAlice    = "alice"
	testNameBob      = "bob"
	testNameCharlie  = "charlie"
	testNameDiana    = "diana"
	testNameEve      = "eve"
	testTableUsers   = "users"
	testColAgePlus10 = "age_plus_10"
	testColVal       = "val"
	testColPrice     = "price"
)

// mockIterator 是用于测试的模拟 ScanIterator。
type mockIterator struct {
	entries []storage.ScanEntry
	pos     int
	err     error
}

func newMockIterator(entries []storage.ScanEntry) *mockIterator {
	return &mockIterator{entries: entries, pos: -1}
}

func (m *mockIterator) Next() bool {
	m.pos++
	return m.pos < len(m.entries)
}

func (m *mockIterator) Entry() storage.ScanEntry {
	if m.pos < 0 || m.pos >= len(m.entries) {
		return storage.ScanEntry{}
	}
	return m.entries[m.pos]
}

func (m *mockIterator) Err() error { return m.err }
func (m *mockIterator) Close()     {}

// makeTestSchema 创建测试用的列定义。
func makeTestSchema() []ColumnDef {
	return []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAge, Type: common.TypeInt64, Nullable: true},
	}
}

// makeTestEntries 创建测试用的 ScanEntry 列表。
func makeTestEntries() []storage.ScanEntry {
	return []storage.ScanEntry{
		{
			Key: "key1",
			Value: storage.Row{Version: 1, Columns: map[string]common.Value{
				"id":        common.NewInt64(1),
				testColName: common.NewString(testNameAlice),
				testColAge:  common.NewInt64(30),
			}},
		},
		{
			Key: "key2",
			Value: storage.Row{Version: 1, Columns: map[string]common.Value{
				"id":        common.NewInt64(2),
				testColName: common.NewString(testNameBob),
				testColAge:  common.NewInt64(25),
			}},
		},
		{
			Key: "key3",
			Value: storage.Row{Version: 1, Columns: map[string]common.Value{
				"id":        common.NewInt64(3),
				testColName: common.NewString(testNameCharlie),
				testColAge:  common.NewInt64(35),
			}},
		},
		{
			Key: "key4",
			Value: storage.Row{Version: 1, Columns: map[string]common.Value{
				"id":        common.NewInt64(4),
				testColName: common.NewString(testNameDiana),
				testColAge:  common.NewInt64(28),
			}},
		},
		{
			Key: "key5",
			Value: storage.Row{Version: 1, Columns: map[string]common.Value{
				"id":        common.NewInt64(5),
				testColName: common.NewString(testNameEve),
				testColAge:  common.NewInt64(22),
			}},
		},
	}
}

// collectChunks 收集执行器所有输出 Chunk 的行数。
func collectChunks(t *testing.T, exec Executor) (int, [][]common.Value) {
	t.Helper()
	var allRows [][]common.Value
	for {
		chunk, err := exec.NextChunk()
		if err != nil {
			t.Fatalf("NextChunk error: %v", err)
		}
		if chunk == nil {
			break
		}
		for i := uint32(0); i < chunk.RowCount(); i++ {
			row, err := chunk.GetRow(i)
			if err != nil {
				t.Fatalf("GetRow error: %v", err)
			}
			allRows = append(allRows, row)
		}
	}
	return len(allRows), allRows
}

func TestScanExecutorBasic(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	exec := NewScanExecutor(iter, schema)
	defer exec.Close()

	count, rows := collectChunks(t, exec)
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}

	if rows[0][0].Int64 != 1 {
		t.Errorf("row 0 id: expected 1, got %d", rows[0][0].Int64)
	}
	if rows[0][1].Str != testNameAlice {
		t.Errorf("row 0 name: expected %s, got %s", testNameAlice, rows[0][1].Str)
	}
	if rows[0][2].Int64 != 30 {
		t.Errorf("row 0 age: expected 30, got %d", rows[0][2].Int64)
	}
}

func TestScanExecutorEmpty(t *testing.T) {
	schema := makeTestSchema()
	iter := newMockIterator(nil)
	exec := NewScanExecutor(iter, schema)
	defer exec.Close()

	chunk, err := exec.NextChunk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Fatalf("expected nil chunk for empty iterator, got non-nil")
	}
}

func TestScanExecutorSchema(t *testing.T) {
	schema := makeTestSchema()
	iter := newMockIterator(nil)
	exec := NewScanExecutor(iter, schema)
	defer exec.Close()

	got := exec.Schema()
	if len(got) != len(schema) {
		t.Fatalf("schema length: expected %d, got %d", len(schema), len(got))
	}
	for i, col := range got {
		if col.Name != schema[i].Name {
			t.Errorf("schema[%d].Name: expected %s, got %s", i, schema[i].Name, col.Name)
		}
	}
}

func TestScanExecutorMissingColumn(t *testing.T) {
	schema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: true},
		{Name: "missing_col", Type: common.TypeString, Nullable: true},
	}
	entries := []storage.ScanEntry{
		{
			Key: "k1",
			Value: storage.Row{Version: 1, Columns: map[string]common.Value{
				"id": common.NewInt64(1),
			}},
		},
	}
	iter := newMockIterator(entries)
	exec := NewScanExecutor(iter, schema)
	defer exec.Close()

	chunk, err := exec.NextChunk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk")
	}

	col, err := chunk.GetColumn(1)
	if err != nil {
		t.Fatalf("get column 1: %v", err)
	}
	if !col.IsNull(0) {
		t.Error("expected NULL for missing column")
	}
}

func TestScanExecutorIteratorError(t *testing.T) {
	schema := makeTestSchema()
	iter := &mockIterator{pos: -1, err: fmt.Errorf("iter error")}
	exec := NewScanExecutor(iter, schema)
	defer exec.Close()

	_, err := exec.NextChunk()
	if err == nil {
		t.Fatal("expected error from iterator, got nil")
	}
}

func TestScanExecutorClose(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	exec := NewScanExecutor(iter, schema)

	exec.Close()
	chunk, err := exec.NextChunk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Fatal("expected nil chunk after close")
	}

	exec.Close()
}

func TestScanExecutorLargeDataset(t *testing.T) {
	schema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
	}
	var entries []storage.ScanEntry
	for i := 0; i < 2500; i++ {
		entries = append(entries, storage.ScanEntry{
			Key: fmt.Sprintf("key%d", i),
			Value: storage.Row{Version: 1, Columns: map[string]common.Value{
				"id": common.NewInt64(int64(i)),
			}},
		})
	}
	iter := newMockIterator(entries)
	exec := NewScanExecutor(iter, schema)
	defer exec.Close()

	totalRows := 0
	for {
		chunk, err := exec.NextChunk()
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if chunk == nil {
			break
		}
		totalRows += int(chunk.RowCount())
	}
	if totalRows != 2500 {
		t.Fatalf("expected 2500 rows, got %d", totalRows)
	}
}

// --- BuildExecutor 测试 ---

func TestBuildExecutorScanNode(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()

	plan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName, testColAge},
		schema:  schema,
	}

	iterFn := func(_ string) storage.ScanIterator {
		return newMockIterator(entries)
	}

	exec, err := BuildExecutor(plan, iterFn)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	defer exec.Close()

	count, _ := collectChunks(t, exec)
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}
}

func TestBuildExecutorFilterNode(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()

	scanPlan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName, testColAge},
		schema:  schema,
	}
	plan := &FilterNode{
		Child: scanPlan,
		Condition: &BinaryExpr{
			Op:    OpEq,
			Left:  &ColumnExpr{Name: testColName},
			Right: &LiteralExpr{Value: common.NewString(testNameBob)},
		},
	}

	iterFn := func(_ string) storage.ScanIterator {
		return newMockIterator(entries)
	}

	exec, err := BuildExecutor(plan, iterFn)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	defer exec.Close()

	count, rows := collectChunks(t, exec)
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
	if rows[0][1].Str != testNameBob {
		t.Errorf("expected %s, got %s", testNameBob, rows[0][1].Str)
	}
}

func TestBuildExecutorAggregateNode(t *testing.T) {
	schema := makeTestSchema()
	scanPlan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id"},
		schema:  schema[:1],
	}
	plan := &AggregateNode{
		Child:   scanPlan,
		GroupBy: []Expression{&ColumnExpr{Name: "id"}},
	}

	iterFn := func(_ string) storage.ScanIterator {
		return newMockIterator(nil)
	}

	_, err := BuildExecutor(plan, iterFn)
	if err == nil {
		t.Fatal("expected error for AggregateNode, got nil")
	}
}

func TestBuildExecutorLimitNode(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()

	scanPlan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName, testColAge},
		schema:  schema,
	}
	plan := &LimitNode{
		Child:  scanPlan,
		Offset: 1,
		Count:  2,
	}

	iterFn := func(_ string) storage.ScanIterator {
		return newMockIterator(entries)
	}

	exec, err := BuildExecutor(plan, iterFn)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	defer exec.Close()

	count, rows := collectChunks(t, exec)
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
	if rows[0][1].Str != testNameBob {
		t.Errorf("row 0 name: expected %s, got %s", testNameBob, rows[0][1].Str)
	}
	if rows[1][1].Str != testNameCharlie {
		t.Errorf("row 1 name: expected %s, got %s", testNameCharlie, rows[1][1].Str)
	}
}

func TestBuildExecutorProjectNode(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()

	scanPlan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName, testColAge},
		schema:  schema,
	}
	projSchema := []ColumnDef{
		{Name: testColName, Type: common.TypeString, Nullable: true},
	}
	plan := &ProjectNode{
		Child:       scanPlan,
		Expressions: []Expression{&ColumnExpr{Name: testColName}},
		Aliases:     []string{testColName},
		schema:      projSchema,
	}

	iterFn := func(_ string) storage.ScanIterator {
		return newMockIterator(entries)
	}

	exec, err := BuildExecutor(plan, iterFn)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	defer exec.Close()

	count, rows := collectChunks(t, exec)
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}
	if len(rows[0]) != 1 {
		t.Fatalf("expected 1 column, got %d", len(rows[0]))
	}
	if rows[0][0].Str != testNameAlice {
		t.Errorf("row 0 name: expected %s, got %s", testNameAlice, rows[0][0].Str)
	}
}

// --- BuildExecutor 不支持节点类型测试 ---

type unsupportedPlanNode struct{}

func (n *unsupportedPlanNode) planNode()            {}
func (n *unsupportedPlanNode) Schema() []ColumnDef  { return nil }
func (n *unsupportedPlanNode) Children() []PlanNode { return nil }
func (n *unsupportedPlanNode) String() string       { return "unsupported" }

func TestBuildExecutorUnsupportedNode(t *testing.T) {
	iterFn := func(_ string) storage.ScanIterator {
		return newMockIterator(nil)
	}
	_, err := BuildExecutor(&unsupportedPlanNode{}, iterFn)
	if err == nil {
		t.Fatal("expected error for unsupported plan node, got nil")
	}
}
