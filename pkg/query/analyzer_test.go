package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func testCatalog() *catalog.Database {
	db := catalog.NewDatabase()
	db.Tables["users"] = &catalog.Table{
		Name: "users",
		Columns: []catalog.ColumnDef{
			{Name: "id", Type: common.TypeInt64, Nullable: false},
			{Name: "name", Type: common.TypeString, Nullable: true},
			{Name: "age", Type: common.TypeInt64, Nullable: true},
			{Name: "score", Type: common.TypeFloat64, Nullable: true},
		},
		PrimaryKey: []string{"id"},
	}
	db.Tables["orders"] = &catalog.Table{
		Name: "orders",
		Columns: []catalog.ColumnDef{
			{Name: "order_id", Type: common.TypeInt64, Nullable: false},
			{Name: "user_id", Type: common.TypeInt64, Nullable: true},
			{Name: "amount", Type: common.TypeFloat64, Nullable: true},
		},
		PrimaryKey: []string{"order_id"},
	}
	return db
}

func TestAnalyzerSelectBasic(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT id, name FROM users WHERE age > 20")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	scan := findScanNode(plan)
	if scan == nil {
		t.Fatal("expected scan node in plan")
	}
	if scan.Table != "users" {
		t.Errorf("expected scan table 'users', got %q", scan.Table)
	}
}

func TestAnalyzerSelectStar(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT * FROM users")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	scan := findScanNode(plan)
	if scan == nil {
		t.Fatal("expected scan node in plan")
	}
	if scan.Table != "users" {
		t.Errorf("expected scan table 'users', got %q", scan.Table)
	}
}

func TestAnalyzerSelectWithLimit(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT id, name FROM users LIMIT 10")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	limit, ok := plan.(*LimitNode)
	if !ok {
		t.Fatalf("expected LimitNode, got %T", plan)
	}
	if limit.Count != 10 {
		t.Errorf("expected limit count 10, got %d", limit.Count)
	}
}

func TestAnalyzerSelectWithGroupBy(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT age, COUNT(*) FROM users GROUP BY age")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	agg := findAggregateNode(plan)
	if agg == nil {
		t.Fatal("expected AggregateNode in plan")
	}
	if len(agg.GroupBy) != 1 {
		t.Errorf("expected 1 group by column, got %d", len(agg.GroupBy))
	}
	if len(agg.Aggregates) != 1 {
		t.Errorf("expected 1 aggregate, got %d", len(agg.Aggregates))
	}
	if agg.Aggregates[0].Func != AggCount {
		t.Errorf("expected COUNT aggregate, got %v", agg.Aggregates[0].Func)
	}
}

func TestAnalyzerSelectWithAggregateNoGroupBy(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT COUNT(*) FROM users")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	agg := findAggregateNode(plan)
	if agg == nil {
		t.Fatal("expected AggregateNode for COUNT without GROUP BY")
	}
	if len(agg.Aggregates) != 1 || agg.Aggregates[0].Func != AggCount {
		t.Error("expected COUNT aggregate")
	}
}

func TestAnalyzerSelectNoFrom(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT 1")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	proj, ok := plan.(*ProjectNode)
	if !ok {
		t.Fatalf("expected ProjectNode, got %T", plan)
	}
	if len(proj.Expressions) != 1 {
		t.Errorf("expected 1 expression, got %d", len(proj.Expressions))
	}
}

func TestAnalyzerSelectNoFromColumnRef(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())

	stmt := &SelectStatement{
		Columns: []SelectColumn{
			{Expr: &ColumnExpr{Name: "id"}},
		},
	}

	_, err := analyzer.Analyze(stmt)
	if err == nil {
		t.Fatal("expected error for column reference without table context")
	}
}

func TestAnalyzerTableNotExist(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT id FROM nonexistent")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	_, err = analyzer.Analyze(stmt)
	if err == nil {
		t.Fatal("expected error for nonexistent table")
	}
}

func TestAnalyzerColumnNotExist(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT nonexistent_col FROM users")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	_, err = analyzer.Analyze(stmt)
	if err == nil {
		t.Fatal("expected error for nonexistent column")
	}
}

func TestAnalyzerInsert(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("INSERT INTO users (id, name, age) VALUES (1, 'Alice', 30)")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	scan, ok := plan.(*ScanNode)
	if !ok {
		t.Fatalf("expected ScanNode, got %T", plan)
	}
	if scan.Table != "users" {
		t.Errorf("expected scan table 'users', got %q", scan.Table)
	}
}

func TestAnalyzerInsertInvalidColumn(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("INSERT INTO users (id, nonexistent) VALUES (1, 'test')")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	_, err = analyzer.Analyze(stmt)
	if err == nil {
		t.Fatal("expected error for nonexistent column in INSERT")
	}
}

func TestAnalyzerCreateTable(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("CREATE TABLE test (id INT64 PRIMARY KEY, name STRING)")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	scan, ok := plan.(*ScanNode)
	if !ok {
		t.Fatalf("expected ScanNode, got %T", plan)
	}
	if scan.Table != "test" {
		t.Errorf("expected scan table 'test', got %q", scan.Table)
	}
}

func TestAnalyzerUnsupportedStatement(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())

	_, err := analyzer.Analyze(&unsupportedStmt{})
	if err == nil {
		t.Fatal("expected error for unsupported statement type")
	}
}

type unsupportedStmt struct{}

func (s *unsupportedStmt) statementNode() {}
func (s *unsupportedStmt) String() string { return "UNSUPPORTED" }

func TestAnalyzerSelectWithAlias(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT id AS user_id, name AS user_name FROM users")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	proj := findProjectNode(plan)
	if proj != nil {
		if len(proj.Aliases) < 2 {
			t.Errorf("expected at least 2 aliases, got %d", len(proj.Aliases))
		}
	}
}

func TestAnalyzerSelectWithFuncExpr(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT COUNT(*) FROM users")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	agg := findAggregateNode(plan)
	if agg == nil {
		t.Fatal("expected AggregateNode for COUNT(*)")
	}
}

func TestAnalyzerSelectWithSumAvg(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT SUM(score), AVG(score) FROM users")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	agg := findAggregateNode(plan)
	if agg == nil {
		t.Fatal("expected AggregateNode")
	}
	if len(agg.Aggregates) != 2 {
		t.Fatalf("expected 2 aggregates, got %d", len(agg.Aggregates))
	}
	if agg.Aggregates[0].Func != AggSum {
		t.Errorf("expected SUM, got %v", agg.Aggregates[0].Func)
	}
	if agg.Aggregates[1].Func != AggAvg {
		t.Errorf("expected AVG, got %v", agg.Aggregates[1].Func)
	}
}

func TestAnalyzerSelectWithAndOr(t *testing.T) {
	analyzer := NewAnalyzer(testCatalog())
	parser := NewParser()

	stmt, err := parser.Parse("SELECT id FROM users WHERE age > 20 AND score < 90.0")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	scan := findScanNode(plan)
	if scan == nil {
		t.Fatal("expected scan node")
	}
}

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

func TestOptimizerConstantFolding(t *testing.T) {
	rule := &ConstantFoldingRule{}

	scan := &ScanNode{
		Table:   "users",
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

	if !colSet["name"] {
		t.Error("expected 'name' column to be preserved")
	}
	if !colSet["age"] {
		t.Error("expected 'age' column to be preserved (used in WHERE)")
	}
}

func TestOptimizerPredicatePushdownEliminatesFilter(t *testing.T) {
	rule := &PredicatePushdownRule{}

	scan := &ScanNode{
		Table:   "users",
		Columns: []string{"id", "name"},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64, Nullable: false},
			{Name: "name", Type: common.TypeString, Nullable: true},
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
		Table:   "users",
		Columns: []string{"id", "age"},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: "age", Type: common.TypeInt64},
		},
	}

	innerFilter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "age"}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
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
		Table:   "users",
		Columns: []string{"id", "name", "age"},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: "name", Type: common.TypeString},
			{Name: "age", Type: common.TypeInt64},
		},
	}

	proj := &ProjectNode{
		Child:       scan,
		Expressions: []Expression{&ColumnExpr{Name: "id"}, &ColumnExpr{Name: "name"}},
		Aliases:     []string{"", ""},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: "name", Type: common.TypeString},
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
		Table:   "users",
		Columns: []string{"id", "age"},
		schema: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: "age", Type: common.TypeInt64},
		},
	}

	agg := &AggregateNode{
		Child:   scan,
		GroupBy: []Expression{&ColumnExpr{Name: "age"}},
		Aggregates: []AggregateExpr{
			{Func: AggCount, Arg: &StarExpr{}},
		},
		schema: []ColumnDef{
			{Name: "age", Type: common.TypeInt64},
			{Name: "COUNT(*)", Type: common.TypeInt64},
		},
	}

	filter := &FilterNode{
		Child:     agg,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "COUNT(*)"}, Right: &LiteralExpr{Value: common.NewInt64(5)}},
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

func TestPlanNodeString(t *testing.T) {
	scan := &ScanNode{
		Table:     "users",
		Columns:   []string{"id", "name"},
		Predicate: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "id"}, Right: &LiteralExpr{Value: common.NewInt64(0)}},
		schema:    []ColumnDef{{Name: "id", Type: common.TypeInt64}, {Name: "name", Type: common.TypeString}},
	}

	s := scan.String()
	if s == "" {
		t.Error("expected non-empty string representation")
	}

	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpEq, Left: &ColumnExpr{Name: "name"}, Right: &LiteralExpr{Value: common.NewString("test")}},
	}
	s = filter.String()
	if s == "" {
		t.Error("expected non-empty string representation for FilterNode")
	}

	limit := &LimitNode{Child: filter, Offset: 0, Count: 10}
	s = limit.String()
	if s == "" {
		t.Error("expected non-empty string representation for LimitNode")
	}

	proj := &ProjectNode{
		Child:       scan,
		Expressions: []Expression{&ColumnExpr{Name: "id"}},
		Aliases:     []string{"user_id"},
		schema:      []ColumnDef{{Name: "user_id", Type: common.TypeInt64}},
	}
	s = proj.String()
	if s == "" {
		t.Error("expected non-empty string representation for ProjectNode")
	}

	agg := &AggregateNode{
		Child:      scan,
		GroupBy:    []Expression{&ColumnExpr{Name: "id"}},
		Aggregates: []AggregateExpr{{Func: AggCount, Arg: &StarExpr{}}},
		schema:     []ColumnDef{{Name: "id", Type: common.TypeInt64}, {Name: "COUNT(*)", Type: common.TypeInt64}},
	}
	s = agg.String()
	if s == "" {
		t.Error("expected non-empty string representation for AggregateNode")
	}
}

func TestAggregateFuncString(t *testing.T) {
	funcs := []AggregateFunc{AggCount, AggSum, AggMin, AggMax, AggAvg}
	expected := []string{"COUNT", "SUM", "MIN", "MAX", "AVG"}
	for i, f := range funcs {
		if f.String() != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], f.String())
		}
	}
}

func TestAggregateExprString(t *testing.T) {
	agg := AggregateExpr{Func: AggCount, Arg: nil}
	if agg.String() != "COUNT(*)" {
		t.Errorf("expected 'COUNT(*)', got %q", agg.String())
	}

	agg = AggregateExpr{Func: AggSum, Arg: &ColumnExpr{Name: "score"}}
	if agg.String() != "SUM(score)" {
		t.Errorf("expected 'SUM(score)', got %q", agg.String())
	}
}

func TestSplitConjuncts(t *testing.T) {
	expr := &BinaryExpr{
		Op:    OpAnd,
		Left:  &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "a"}, Right: &LiteralExpr{Value: common.NewInt64(1)}},
		Right: &BinaryExpr{Op: OpLt, Left: &ColumnExpr{Name: "b"}, Right: &LiteralExpr{Value: common.NewInt64(10)}},
	}

	conjuncts := splitConjuncts(expr)
	if len(conjuncts) != 2 {
		t.Errorf("expected 2 conjuncts, got %d", len(conjuncts))
	}
}

func TestMergeConjuncts(t *testing.T) {
	conjuncts := []Expression{
		&BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "a"}, Right: &LiteralExpr{Value: common.NewInt64(1)}},
		&BinaryExpr{Op: OpLt, Left: &ColumnExpr{Name: "b"}, Right: &LiteralExpr{Value: common.NewInt64(10)}},
	}

	result := mergeConjuncts(conjuncts)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	bin, ok := result.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", result)
	}
	if bin.Op != OpAnd {
		t.Errorf("expected AND operator, got %v", bin.Op)
	}
}

func TestMergeConjunctsEmpty(t *testing.T) {
	result := mergeConjuncts(nil)
	if result != nil {
		t.Errorf("expected nil for empty conjuncts, got %v", result)
	}
}

func TestCollectColumnRefs(t *testing.T) {
	expr := &BinaryExpr{
		Op:    OpAnd,
		Left:  &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: "a"}, Right: &LiteralExpr{Value: common.NewInt64(1)}},
		Right: &BinaryExpr{Op: OpLt, Left: &ColumnExpr{Name: "b"}, Right: &LiteralExpr{Value: common.NewInt64(10)}},
	}

	refs := collectColumnRefs(expr)
	if len(refs) != 2 {
		t.Errorf("expected 2 column refs, got %d", len(refs))
	}

	refSet := make(map[string]bool)
	for _, r := range refs {
		refSet[r] = true
	}
	if !refSet["a"] || !refSet["b"] {
		t.Errorf("expected refs 'a' and 'b', got %v", refs)
	}
}

func findScanNode(node PlanNode) *ScanNode {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case *ScanNode:
		return n
	case *FilterNode:
		return findScanNode(n.Child)
	case *ProjectNode:
		return findScanNode(n.Child)
	case *AggregateNode:
		return findScanNode(n.Child)
	case *LimitNode:
		return findScanNode(n.Child)
	}
	return nil
}

func findAggregateNode(node PlanNode) *AggregateNode {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case *AggregateNode:
		return n
	case *FilterNode:
		return findAggregateNode(n.Child)
	case *ProjectNode:
		return findAggregateNode(n.Child)
	case *LimitNode:
		return findAggregateNode(n.Child)
	}
	return nil
}

func findProjectNode(node PlanNode) *ProjectNode {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case *ProjectNode:
		return n
	case *FilterNode:
		return findProjectNode(n.Child)
	case *AggregateNode:
		return findProjectNode(n.Child)
	case *LimitNode:
		return findProjectNode(n.Child)
	}
	return nil
}
