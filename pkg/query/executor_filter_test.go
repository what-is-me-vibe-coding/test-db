package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestFilterExecutorBasic(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{
		Op:    OpGt,
		Left:  &ColumnExpr{Name: testColAge},
		Right: &LiteralExpr{Value: common.NewInt64(25)},
	}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	count, rows := collectChunks(t, filterExec)
	if count != 3 {
		t.Fatalf("expected 3 rows (age > 25), got %d", count)
	}

	expectedNames := []string{testNameAlice, testNameCharlie, testNameDiana}
	for i, name := range expectedNames {
		if rows[i][1].Str != name {
			t.Errorf("row %d name: expected %s, got %s", i, name, rows[i][1].Str)
		}
	}
}

func TestFilterExecutorEquals(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{
		Op:    OpEq,
		Left:  &ColumnExpr{Name: testColName},
		Right: &LiteralExpr{Value: common.NewString(testNameBob)},
	}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	count, rows := collectChunks(t, filterExec)
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
	if rows[0][1].Str != testNameBob {
		t.Errorf("expected %s, got %s", testNameBob, rows[0][1].Str)
	}
}

func TestFilterExecutorAndCondition(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{
		Op: OpAnd,
		Left: &BinaryExpr{
			Op:    OpGt,
			Left:  &ColumnExpr{Name: testColAge},
			Right: &LiteralExpr{Value: common.NewInt64(25)},
		},
		Right: &BinaryExpr{
			Op:    OpLt,
			Left:  &ColumnExpr{Name: testColAge},
			Right: &LiteralExpr{Value: common.NewInt64(35)},
		},
	}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	count, rows := collectChunks(t, filterExec)
	if count != 2 {
		t.Fatalf("expected 2 rows (25 < age < 35), got %d", count)
	}
	expectedNames := []string{testNameAlice, testNameDiana}
	for i, name := range expectedNames {
		if rows[i][1].Str != name {
			t.Errorf("row %d name: expected %s, got %s", i, name, rows[i][1].Str)
		}
	}
}

func TestFilterExecutorOrCondition(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{
		Op: OpOr,
		Left: &BinaryExpr{
			Op:    OpLt,
			Left:  &ColumnExpr{Name: testColAge},
			Right: &LiteralExpr{Value: common.NewInt64(25)},
		},
		Right: &BinaryExpr{
			Op:    OpGt,
			Left:  &ColumnExpr{Name: testColAge},
			Right: &LiteralExpr{Value: common.NewInt64(33)},
		},
	}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	count, rows := collectChunks(t, filterExec)
	if count != 2 {
		t.Fatalf("expected 2 rows (age < 25 OR age > 33), got %d", count)
	}
	expectedNames := []string{testNameCharlie, testNameEve}
	for i, name := range expectedNames {
		if rows[i][1].Str != name {
			t.Errorf("row %d name: expected %s, got %s", i, name, rows[i][1].Str)
		}
	}
}

func TestFilterExecutorNoMatch(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{
		Op:    OpGt,
		Left:  &ColumnExpr{Name: testColAge},
		Right: &LiteralExpr{Value: common.NewInt64(1000)},
	}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	count, _ := collectChunks(t, filterExec)
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestFilterExecutorNotCondition(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &UnaryExpr{
		Op: OpNot,
		Expr: &BinaryExpr{
			Op:    OpGt,
			Left:  &ColumnExpr{Name: testColAge},
			Right: &LiteralExpr{Value: common.NewInt64(30)},
		},
	}
	filterExec := NewFilterExecutor(scanExec, cond)
	defer filterExec.Close()

	count, rows := collectChunks(t, filterExec)
	if count != 4 {
		t.Fatalf("expected 4 rows (NOT age > 30), got %d", count)
	}
	_ = rows
}

// --- 管道测试 ---

func TestPipelineScanFilterLimit(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{
		Op:    OpGt,
		Left:  &ColumnExpr{Name: testColAge},
		Right: &LiteralExpr{Value: common.NewInt64(25)},
	}
	filterExec := NewFilterExecutor(scanExec, cond)

	limitExec := NewLimitExecutor(filterExec, 0, 2)
	defer limitExec.Close()

	count, rows := collectChunks(t, limitExec)
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
	if rows[0][1].Str != testNameAlice {
		t.Errorf("row 0 name: expected %s, got %s", testNameAlice, rows[0][1].Str)
	}
	if rows[1][1].Str != testNameCharlie {
		t.Errorf("row 1 name: expected %s, got %s", testNameCharlie, rows[1][1].Str)
	}
}

func TestPipelineScanFilterProject(t *testing.T) {
	schema := makeTestSchema()
	entries := makeTestEntries()
	iter := newMockIterator(entries)
	scanExec := NewScanExecutor(iter, schema)

	cond := &BinaryExpr{
		Op:    OpGe,
		Left:  &ColumnExpr{Name: testColAge},
		Right: &LiteralExpr{Value: common.NewInt64(30)},
	}
	filterExec := NewFilterExecutor(scanExec, cond)

	projSchema := []ColumnDef{
		{Name: testColName, Type: common.TypeString, Nullable: true},
		{Name: testColAgePlus10, Type: common.TypeInt64, Nullable: false},
	}
	exprs := []Expression{
		&ColumnExpr{Name: testColName},
		&BinaryExpr{OpAdd, &ColumnExpr{Name: testColAge}, &LiteralExpr{Value: common.NewInt64(10)}},
	}
	projExec := NewProjectExecutor(filterExec, exprs, []string{testColName, testColAgePlus10}, projSchema)
	defer projExec.Close()

	count, rows := collectChunks(t, projExec)
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}

	if rows[0][0].Str != testNameAlice {
		t.Errorf("row 0 name: expected %s, got %s", testNameAlice, rows[0][0].Str)
	}
	if rows[0][1].Int64 != 40 {
		t.Errorf("row 0 age_plus_10: expected 40, got %d", rows[0][1].Int64)
	}
	if rows[1][0].Str != testNameCharlie {
		t.Errorf("row 1 name: expected %s, got %s", testNameCharlie, rows[1][0].Str)
	}
	if rows[1][1].Int64 != 45 {
		t.Errorf("row 1 age_plus_10: expected 45, got %d", rows[1][1].Int64)
	}
}
