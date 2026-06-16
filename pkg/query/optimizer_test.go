package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestOptimizerConstantFolding(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id"},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64, Nullable: false},
		},
		Predicate: &BinaryExpr{
			Op:    OpEq,
			Left:  &LiteralExpr{Value: common.NewInt64(1)},
			Right: &LiteralExpr{Value: common.NewInt64(1)},
		},
	}

	result := rule.Apply(scan)

	resultScan, ok := result.(*ScanNode)
	if !ok {
		t.Fatalf("expected ScanNode, got %T", result)
	}

	lit, ok := resultScan.Predicate.(*LiteralExpr)
	if !ok {
		t.Fatalf("expected LiteralExpr after folding, got %T", resultScan.Predicate)
	}
	if !lit.Value.Valid || lit.Value.Int64 != 1 {
		t.Errorf("expected folded literal true (1), got %v", lit.Value)
	}
}

func TestOptimizerConstantFoldingComparison(t *testing.T) {
	tests := []struct {
		name     string
		op       BinaryOp
		left     common.Value
		right    common.Value
		expected int64
	}{
		{"5 < 10 = true", OpLt, common.NewInt64(5), common.NewInt64(10), 1},
		{"10 < 5 = false", OpLt, common.NewInt64(10), common.NewInt64(5), 0},
		{"5 <= 5 = true", OpLe, common.NewInt64(5), common.NewInt64(5), 1},
		{"5 > 10 = false", OpGt, common.NewInt64(5), common.NewInt64(10), 0},
		{"10 >= 10 = true", OpGe, common.NewInt64(10), common.NewInt64(10), 1},
		{"5 != 10 = true", OpNe, common.NewInt64(5), common.NewInt64(10), 1},
		{"5 = 5 = true", OpEq, common.NewInt64(5), common.NewInt64(5), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &ConstantFoldingRule{}
			scan := &ScanNode{
				Table:   "t",
				Columns: []string{"id"},
				schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
				Predicate: &BinaryExpr{
					Op:    tt.op,
					Left:  &LiteralExpr{Value: tt.left},
					Right: &LiteralExpr{Value: tt.right},
				},
			}
			result := rule.Apply(scan).(*ScanNode)
			lit, ok := result.Predicate.(*LiteralExpr)
			if !ok {
				t.Fatalf("expected LiteralExpr, got %T", result.Predicate)
			}
			if lit.Value.Int64 != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, lit.Value.Int64)
			}
		})
	}
}

func TestOptimizerConstantFoldingFilterNode(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpEq, Left: &LiteralExpr{Value: common.NewInt64(1)}, Right: &LiteralExpr{Value: common.NewInt64(1)}},
	}

	result := rule.Apply(filter)

	resultFilter, ok := result.(*FilterNode)
	if !ok {
		t.Fatalf("expected FilterNode, got %T", result)
	}

	lit, ok := resultFilter.Condition.(*LiteralExpr)
	if !ok {
		t.Fatalf("expected LiteralExpr after folding, got %T", resultFilter.Condition)
	}
	if !lit.Value.Valid || lit.Value.Int64 != 1 {
		t.Errorf("expected folded literal true, got %v", lit.Value)
	}
}

func TestOptimizerConstantFoldingProjectNode(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
	}

	proj := &ProjectNode{
		Child: scan,
		Expressions: []Expression{
			&BinaryExpr{Op: OpAdd, Left: &LiteralExpr{Value: common.NewInt64(1)}, Right: &LiteralExpr{Value: common.NewInt64(2)}},
		},
		Aliases: []string{""},
		schema:  []ColumnDef{{Name: testStrCol1, Type: common.TypeInt64}},
	}

	result := rule.Apply(proj)

	resultProj, ok := result.(*ProjectNode)
	if !ok {
		t.Fatalf("expected ProjectNode, got %T", result)
	}
	if len(resultProj.Expressions) != 1 {
		t.Fatalf("expected 1 expression, got %d", len(resultProj.Expressions))
	}
}

func TestOptimizerConstantFoldingAggregateNode(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
	}

	agg := &AggregateNode{
		Child:      scan,
		GroupBy:    []Expression{&ColumnExpr{Name: "id"}},
		Aggregates: []AggregateExpr{{Func: AggCount}},
		schema:     []ColumnDef{{Name: "id", Type: common.TypeInt64}},
	}

	result := rule.Apply(agg)

	resultAgg, ok := result.(*AggregateNode)
	if !ok {
		t.Fatalf("expected AggregateNode, got %T", result)
	}
	if len(resultAgg.GroupBy) != 1 {
		t.Errorf("expected 1 group by, got %d", len(resultAgg.GroupBy))
	}
}

func TestOptimizerConstantFoldingLimitNode(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
	}

	limit := &LimitNode{
		Child:  scan,
		Count:  10,
		Offset: 0,
	}

	result := rule.Apply(limit)

	resultLimit, ok := result.(*LimitNode)
	if !ok {
		t.Fatalf("expected LimitNode, got %T", result)
	}
	if resultLimit.Count != 10 {
		t.Errorf("expected count 10, got %d", resultLimit.Count)
	}
}

func TestOptimizerConstantFoldingAnd(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
		Predicate: &BinaryExpr{
			Op:    OpAnd,
			Left:  &LiteralExpr{Value: common.NewBool(true)},
			Right: &LiteralExpr{Value: common.NewBool(false)},
		},
	}

	result := rule.Apply(scan).(*ScanNode)
	lit, ok := result.Predicate.(*LiteralExpr)
	if !ok {
		t.Fatalf("expected LiteralExpr after folding, got %T", result.Predicate)
	}
	if !lit.Value.Valid || lit.Value.Int64 != 0 {
		t.Errorf("expected true AND false = false, got %v", lit.Value)
	}
}

func TestOptimizerConstantFoldingOr(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
		Predicate: &BinaryExpr{
			Op:    OpOr,
			Left:  &LiteralExpr{Value: common.NewBool(false)},
			Right: &LiteralExpr{Value: common.NewBool(true)},
		},
	}

	result := rule.Apply(scan).(*ScanNode)
	lit, ok := result.Predicate.(*LiteralExpr)
	if !ok {
		t.Fatalf("expected LiteralExpr after folding, got %T", result.Predicate)
	}
	if !lit.Value.Valid || lit.Value.Int64 != 1 {
		t.Errorf("expected false OR true = true, got %v", lit.Value)
	}
}

func TestOptimizerConstantFoldingNot(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
		Predicate: &UnaryExpr{
			Op:   OpNot,
			Expr: &LiteralExpr{Value: common.NewBool(true)},
		},
	}

	result := rule.Apply(scan).(*ScanNode)
	lit, ok := result.Predicate.(*LiteralExpr)
	if !ok {
		t.Fatalf("expected LiteralExpr after folding NOT, got %T", result.Predicate)
	}
	if lit.Value.Int64 != 0 {
		t.Errorf("expected NOT true = false, got %v", lit.Value)
	}
}

func TestOptimizerRuleNames(t *testing.T) {
	rules := []OptimizeRule{
		&PredicatePushdownRule{},
		&ConstantFoldingRule{},
		&ColumnPruningRule{},
	}

	names := []string{"PredicatePushdown", "ConstantFolding", "ColumnPruning"}
	for i, rule := range rules {
		if rule.Name() != names[i] {
			t.Errorf("expected rule name %q, got %q", names[i], rule.Name())
		}
	}
}

func TestOptimizerConstantFoldingNonLiteralExpr(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
		Predicate: &BinaryExpr{
			Op:    OpEq,
			Left:  &ColumnExpr{Name: "id"},
			Right: &LiteralExpr{Value: common.NewInt64(1)},
		},
	}

	result := rule.Apply(scan).(*ScanNode)
	bin, ok := result.Predicate.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr (not fully foldable), got %T", result.Predicate)
	}
	if _, ok := bin.Left.(*ColumnExpr); !ok {
		t.Error("expected left side to remain ColumnExpr")
	}
}

func TestOptimizerConstantFoldingNullValues(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
		Predicate: &BinaryExpr{
			Op:    OpEq,
			Left:  &LiteralExpr{Value: common.NewNull()},
			Right: &LiteralExpr{Value: common.NewInt64(1)},
		},
	}

	result := rule.Apply(scan).(*ScanNode)
	bin, ok := result.Predicate.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr (NULL comparison not folded), got %T", result.Predicate)
	}
	if bin.Op != OpEq {
		t.Error("expected OpEq to remain unchanged for NULL comparison")
	}
}

func TestEndToEndAnalyzeAndOptimize(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()
	optimizer := NewOptimizer()

	tests := []struct {
		name string
		sql  string
	}{
		{"simple select", "SELECT id, name FROM users WHERE age > 20"},
		{"select star", "SELECT * FROM users"},
		{"select with limit", "SELECT id FROM users LIMIT 5"},
		{"select with group by", "SELECT age, COUNT(*) FROM users GROUP BY age"},
		{"select no from", "SELECT 1"},
		{"select with alias", "SELECT id AS user_id FROM users"},
		{"select with and", "SELECT id FROM users WHERE age > 20 AND score < 90.0"},
		{"select sum avg", "SELECT SUM(score), AVG(score) FROM users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			plan, err := analyzer.Analyze(stmt)
			if err != nil {
				t.Fatalf("analyze error: %v", err)
			}

			optimized := optimizer.Optimize(plan)
			if optimized == nil {
				t.Error("optimized plan should not be nil")
			}
		})
	}
}

func TestOptimizerColumnPruning(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()
	optimizer := NewOptimizer()

	stmt, err := parser.Parse("SELECT name FROM users WHERE age > 20")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	optimized := optimizer.Optimize(plan)

	scan := findScanNode(optimized)
	if scan == nil {
		t.Fatal("expected scan node in optimized plan")
	}

	colSet := make(map[string]bool)
	for _, col := range scan.Columns {
		colSet[col] = true
	}

	if !colSet[testColName] {
		t.Errorf("expected 'name' column to be preserved")
	}
	if !colSet[testColAge] {
		t.Errorf("expected 'age' column to be preserved (used in WHERE)")
	}
}

func TestOptimizerColumnPruningNoPruningNeeded(t *testing.T) {
	rule := &ColumnPruningRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
	}

	result := rule.Apply(scan)

	resultScan, ok := result.(*ScanNode)
	if !ok {
		t.Fatalf("expected ScanNode, got %T", result)
	}
	if len(resultScan.Columns) != 1 {
		t.Errorf("expected 1 column, got %d", len(resultScan.Columns))
	}
}

func TestOptimizerColumnPruningWithFilter(t *testing.T) {
	rule := &ColumnPruningRule{}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName, testColAge},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: testColName, Type: common.TypeString},
			{Name: testColAge, Type: common.TypeInt64},
		},
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
	}

	proj := &ProjectNode{
		Child:       filter,
		Expressions: []Expression{&ColumnExpr{Name: testColName}},
		Aliases:     []string{""},
		schema:      []ColumnDef{{Name: testColName, Type: common.TypeString}},
	}

	result := rule.Apply(proj)

	resultProj, ok := result.(*ProjectNode)
	if !ok {
		t.Fatalf("expected ProjectNode, got %T", result)
	}

	resultScan := findScanNode(resultProj)
	if resultScan == nil {
		t.Fatal("expected scan node")
	}

	colSet := make(map[string]bool)
	for _, col := range resultScan.Columns {
		colSet[col] = true
	}
	if !colSet[testColName] {
		t.Error("expected 'name' column to be preserved")
	}
	if !colSet[testColAge] {
		t.Error("expected 'age' column to be preserved (used in filter)")
	}
}

func TestOptimizerColumnPruningWithAggregate(t *testing.T) {
	rule := &ColumnPruningRule{}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName, testColAge, testColScore},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: testColName, Type: common.TypeString},
			{Name: testColAge, Type: common.TypeInt64},
			{Name: testColScore, Type: common.TypeFloat64},
		},
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: []Expression{&ColumnExpr{Name: testColAge}},
		Aggregates: []AggregateExpr{
			{Func: AggSum, Arg: &ColumnExpr{Name: testColScore}},
		},
		schema: []ColumnDef{
			{Name: testColAge, Type: common.TypeInt64},
			{Name: testAggSumScore, Type: common.TypeFloat64},
		},
	}

	result := rule.Apply(agg)

	resultAgg, ok := result.(*AggregateNode)
	if !ok {
		t.Fatalf("expected AggregateNode, got %T", result)
	}

	resultScan := findScanNode(resultAgg)
	if resultScan == nil {
		t.Fatal("expected scan node")
	}

	colSet := make(map[string]bool)
	for _, col := range resultScan.Columns {
		colSet[col] = true
	}
	if !colSet[testColAge] {
		t.Error("expected 'age' column to be preserved (group by)")
	}
	if !colSet[testColScore] {
		t.Error("expected 'score' column to be preserved (aggregate arg)")
	}
}

func TestOptimizerColumnPruningWithLimit(t *testing.T) {
	rule := &ColumnPruningRule{}

	scan := &ScanNode{
		Table:   "t",
		Columns: []string{"id"},
		schema:  []ColumnDef{{Name: "id", Type: common.TypeInt64}},
	}

	limit := &LimitNode{Child: scan, Count: 10}

	result := rule.Apply(limit)

	resultLimit, ok := result.(*LimitNode)
	if !ok {
		t.Fatalf("expected LimitNode, got %T", result)
	}
	if resultLimit.Count != 10 {
		t.Errorf("expected count 10, got %d", resultLimit.Count)
	}
}

// --- predicate pushdown tests ---

func TestOptimizerPredicatePushdown(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()
	optimizer := NewOptimizer()

	stmt, err := parser.Parse("SELECT id, name FROM users WHERE age > 20")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	optimized := optimizer.Optimize(plan)

	scan := findScanNode(optimized)
	if scan == nil {
		t.Fatal("expected scan node in optimized plan")
	}
	if scan.Predicate == nil {
		t.Error("expected predicate to be pushed down to scan node")
	}
}

func TestOptimizerPredicatePushdownEliminatesFilter(t *testing.T) {
	rule := &PredicatePushdownRule{}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64, Nullable: false},
			{Name: testColName, Type: common.TypeString, Nullable: true},
		},
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "id"}, Right: &LiteralExpr{Value: common.NewInt64(0)}},
	}

	result := rule.Apply(filter)

	resultScan, ok := result.(*ScanNode)
	if !ok {
		t.Fatalf("expected FilterNode to be eliminated and ScanNode returned, got %T", result)
	}
	if resultScan.Predicate == nil {
		t.Error("expected predicate to be pushed into scan")
	}
}

func TestOptimizerMergeFilters(t *testing.T) {
	rule := &PredicatePushdownRule{}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColAge},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: testColAge, Type: common.TypeInt64},
		},
	}

	innerFilter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
	}

	outerFilter := &FilterNode{
		Child:     innerFilter,
		Condition: &BinaryExpr{Op: OpLt, Left: &ColumnExpr{Name: "id"}, Right: &LiteralExpr{Value: common.NewInt64(100)}},
	}

	result := rule.Apply(outerFilter)

	resultScan, ok := result.(*ScanNode)
	if !ok {
		t.Fatalf("expected merged filters into ScanNode, got %T", result)
	}
	if resultScan.Predicate == nil {
		t.Error("expected merged predicate in scan")
	}
}

func TestOptimizerPushdownThroughProject(t *testing.T) {
	rule := &PredicatePushdownRule{}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColName, testColAge},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: testColName, Type: common.TypeString},
			{Name: testColAge, Type: common.TypeInt64},
		},
	}

	proj := &ProjectNode{
		Child:       scan,
		Expressions: []Expression{&ColumnExpr{Name: "id"}, &ColumnExpr{Name: testColName}},
		Aliases:     []string{"", ""},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: testColName, Type: common.TypeString},
		},
	}

	filter := &FilterNode{
		Child:     proj,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "id"}, Right: &LiteralExpr{Value: common.NewInt64(0)}},
	}

	result := rule.Apply(filter)

	resultProj, ok := result.(*ProjectNode)
	if !ok {
		t.Fatalf("expected ProjectNode, got %T", result)
	}

	innerFilter, ok := resultProj.Child.(*FilterNode)
	if !ok {
		t.Fatalf("expected FilterNode under Project, got %T", resultProj.Child)
	}
	if innerFilter.Condition == nil {
		t.Error("expected pushed-down filter condition")
	}
}

func TestOptimizerPushdownThroughAggregate(t *testing.T) {
	rule := &PredicatePushdownRule{}

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{"id", testColAge},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: testColAge, Type: common.TypeInt64},
		},
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: []Expression{&ColumnExpr{Name: testColAge}},
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
		},
		schema: []ColumnDef{
			{Name: testColAge, Type: common.TypeInt64},
			{Name: testAggCountStar, Type: common.TypeInt64},
		},
	}

	filter := &FilterNode{
		Child:     agg,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testAggCountStar}, Right: &LiteralExpr{Value: common.NewInt64(5)}},
	}

	result := rule.Apply(filter)

	resultFilter, ok := result.(*FilterNode)
	if !ok {
		t.Fatalf("expected FilterNode (remaining after aggregate), got %T", result)
	}
	if resultFilter.Condition == nil {
		t.Error("expected remaining filter condition above aggregate")
	}
}
