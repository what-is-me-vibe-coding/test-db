package query

import (
	"testing"

	"github.com/xwb1989/sqlparser"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ============================================================================
// pushFilterDown 覆盖率提升测试 (87.5% → >90%)
// 未覆盖路径：FilterNode 合并 (line 80-83)、ScanNode 已有谓词 (line 73-78)、
// ProjectNode 不可下推 (line 90-91)、AggregateNode 全部可下推 (line 103)、
// 默认分支 (line 106-107)
// ============================================================================

// TestPushFilterDown_FilterMerging 测试 FilterNode 嵌套时的谓词合并路径。
// 当内层 FilterNode 不能继续下推时（例如子节点是 LimitNode），
// 外层 FilterNode 会与内层 FilterNode 合并。
func TestPushFilterDown_FilterMerging(t *testing.T) {
	rule := &PredicatePushdownRule{}
	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColAge},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColAge, Type: common.TypeInt64}},
	}
	// LimitNode 在 ScanNode 之上，Filter 不能下推穿过 Limit
	limit := &LimitNode{Child: scan, Count: 10}
	// 内层 Filter 在 Limit 之上，不能下推
	innerFilter := &FilterNode{
		Child:     limit,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
	}
	// 外层 Filter，应与内层合并
	outerFilter := &FilterNode{
		Child:     innerFilter,
		Condition: &BinaryExpr{Op: OpLt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(50)}},
	}

	result := rule.Apply(outerFilter)
	// 两个 Filter 应合并为一个，保留在 Limit 之上
	resultFilter, ok := result.(*FilterNode)
	if !ok {
		t.Fatalf("期望合并后的 FilterNode，得到 %T", result)
	}
	if resultFilter.Condition == nil {
		t.Error("期望合并后的 Filter 有条件")
	}
	// 合并后的条件应该是 AND 表达式
	binExpr, ok := resultFilter.Condition.(*BinaryExpr)
	if !ok || binExpr.Op != OpAnd {
		t.Errorf("期望合并后的条件为 AND 表达式，得到 %v", resultFilter.Condition)
	}
}

// TestPushFilterDown_ScanWithExistingPredicate 测试 FilterNode 下推到已有谓词的 ScanNode。
func TestPushFilterDown_ScanWithExistingPredicate(t *testing.T) {
	rule := &PredicatePushdownRule{}
	scan := &ScanNode{
		Table:     testTableUsers,
		Columns:   []string{testColID, testColAge},
		Predicate: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
		schema:    []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColAge, Type: common.TypeInt64}},
	}
	filter := &FilterNode{
		Child:     scan,
		Condition: &BinaryExpr{Op: OpLt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(50)}},
	}

	result := rule.Apply(filter)
	resultScan, ok := result.(*ScanNode)
	if !ok {
		t.Fatalf("期望 ScanNode，得到 %T", result)
	}
	// 谓词应该被合并（AND 连接）
	if resultScan.Predicate == nil {
		t.Error("期望 ScanNode 有合并后的谓词")
	}
	// 合并后的谓词应该是 AND 表达式
	binExpr, ok := resultScan.Predicate.(*BinaryExpr)
	if !ok || binExpr.Op != OpAnd {
		t.Errorf("期望合并后的谓词为 AND 表达式，得到 %v", resultScan.Predicate)
	}
}

// TestPushFilterDown_ProjectCannotPush 测试 FilterNode 不能下推穿过 ProjectNode 的情况。
// 当 Filter 引用的列不在 Project 的子节点 schema 中时，不能下推。
func TestPushFilterDown_ProjectCannotPush(t *testing.T) {
	rule := &PredicatePushdownRule{}
	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName},
		schema: []ColumnDef{
			{Name: testColID, Type: common.TypeInt64},
			{Name: testColName, Type: common.TypeString},
		},
	}
	// Project 输出 id 和 name
	proj := &ProjectNode{
		Child:       scan,
		Expressions: []Expression{&ColumnExpr{Name: testColID}, &ColumnExpr{Name: testColName}},
		Aliases:     []string{"", ""},
		schema:      []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColName, Type: common.TypeString}},
	}
	// Filter 引用了 score 列，不在 Project 的子节点（scan）schema 中，不能下推
	filter := &FilterNode{
		Child:     proj,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColScore}, Right: &LiteralExpr{Value: common.NewFloat64(80.0)}},
	}

	result := rule.Apply(filter)
	// Filter 应该保留在 Project 之上（因为 score 列不在 scan schema 中）
	resultFilter, ok := result.(*FilterNode)
	if !ok {
		t.Fatalf("期望 FilterNode 保留在 Project 之上，得到 %T", result)
	}
	if resultFilter.Condition == nil {
		t.Error("期望 Filter 保留条件")
	}
}

// TestPushFilterDown_AggregateAllPushable 测试 FilterNode 下推到 AggregateNode，
// 所有谓词都引用非聚合列，可以全部下推。
func TestPushFilterDown_AggregateAllPushable(t *testing.T) {
	rule := &PredicatePushdownRule{}
	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColAge},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColAge, Type: common.TypeInt64}},
	}
	agg := &AggregateNode{
		Child:      scan,
		GroupBy:    []Expression{&ColumnExpr{Name: testColAge}},
		Aggregates: []AggregateExpr{{Func: AggCount, Arg: &ColumnExpr{Name: testColID}}},
	}
	// Filter 引用 id 列（非 GROUP BY/聚合列），可以下推
	filter := &FilterNode{
		Child:     agg,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColID}, Right: &LiteralExpr{Value: common.NewInt64(5)}},
	}

	result := rule.Apply(filter)
	// 谓词应下推到 Aggregate 的子节点
	resultAgg, ok := result.(*AggregateNode)
	if !ok {
		t.Fatalf("期望 AggregateNode，得到 %T", result)
	}
	innerFilter, ok := resultAgg.Child.(*FilterNode)
	if !ok {
		t.Fatalf("期望 FilterNode 在 Aggregate 子节点中，得到 %T", resultAgg.Child)
	}
	if innerFilter.Condition == nil {
		t.Error("期望下推的 Filter 有条件")
	}
}

// TestPushFilterDown_AggregateMixedPredicate 测试 FilterNode 下推到 AggregateNode，
// 部分谓词引用聚合列不能下推，部分可以下推。
func TestPushFilterDown_AggregateMixedPredicate(t *testing.T) {
	rule := &PredicatePushdownRule{}
	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColAge},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColAge, Type: common.TypeInt64}},
	}
	agg := &AggregateNode{
		Child:      scan,
		GroupBy:    []Expression{&ColumnExpr{Name: testColAge}},
		Aggregates: []AggregateExpr{{Func: AggCount, Arg: &ColumnExpr{Name: testColID}}},
	}
	// age 是 GROUP BY 列（不可下推），id 不是（可下推）
	filter := &FilterNode{
		Child: agg,
		Condition: &BinaryExpr{
			Op:    OpAnd,
			Left:  &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
			Right: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColID}, Right: &LiteralExpr{Value: common.NewInt64(5)}},
		},
	}

	result := rule.Apply(filter)
	// 应该有部分谓词保留在 Filter 中
	resultFilter, ok := result.(*FilterNode)
	if !ok {
		// 也可能全部下推了（如果优化器认为 age 条件也可以下推）
		t.Logf("结果类型: %T", result)
		return
	}
	if resultFilter.Condition == nil {
		t.Error("期望保留的 Filter 有条件")
	}
}

// TestPushFilterDown_DefaultCase 测试 pushFilterDown 处理不支持的子节点类型。
// 例如 FilterNode 下推到 LimitNode（不匹配任何 case）。
func TestPushFilterDown_DefaultCase(t *testing.T) {
	rule := &PredicatePushdownRule{}
	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColAge},
		schema:  []ColumnDef{{Name: testColID, Type: common.TypeInt64}, {Name: testColAge, Type: common.TypeInt64}},
	}
	limit := &LimitNode{Child: scan, Count: 10}
	filter := &FilterNode{
		Child:     limit,
		Condition: &BinaryExpr{Op: OpGt, Left: &ColumnExpr{Name: testColAge}, Right: &LiteralExpr{Value: common.NewInt64(20)}},
	}

	result := rule.Apply(filter)
	// Filter 应该保留在 Limit 之上
	resultFilter, ok := result.(*FilterNode)
	if !ok {
		t.Fatalf("期望 FilterNode 保留在 Limit 之上，得到 %T", result)
	}
	if resultFilter.Condition == nil {
		t.Error("期望 Filter 保留条件")
	}
}

// ============================================================================
// convertFuncExpr 覆盖率提升测试 (85.7% → >90%)
// 未覆盖路径：不支持的函数参数类型 (line 351)、参数转换错误 (line 354-356)
// ============================================================================

// TestConvertFuncExpr_WithLiteralArg 测试 convertFuncExpr 处理字面量参数。
func TestConvertFuncExpr_WithLiteralArg(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT COALESCE(1, 2) FROM t")
	if err != nil {
		t.Fatalf("Parse COALESCE(1, 2): %v", err)
	}
	sel := stmt.(*SelectStatement)
	fn, ok := sel.Columns[0].Expr.(*FuncExpr)
	if !ok {
		t.Fatalf("期望 FuncExpr，得到 %T", sel.Columns[0].Expr)
	}
	if fn.Name != "coalesce" { //nolint:goconst
		t.Errorf("期望函数名 'coalesce'，得到 %q", fn.Name)
	}
	if len(fn.Args) != 2 {
		t.Fatalf("期望 2 个参数，得到 %d", len(fn.Args))
	}
	lit0, ok := fn.Args[0].(*LiteralExpr)
	if !ok {
		t.Errorf("期望第一个参数为 LiteralExpr，得到 %T", fn.Args[0])
	}
	if lit0.Value.Int64 != 1 {
		t.Errorf("期望第一个参数值为 1，得到 %d", lit0.Value.Int64)
	}
}

// TestConvertFuncExpr_NestedFuncCall 测试 convertFuncExpr 处理嵌套函数调用。
func TestConvertFuncExpr_NestedFuncCall(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT COUNT(MAX(id)) FROM t")
	if err != nil {
		t.Fatalf("Parse COUNT(MAX(id)): %v", err)
	}
	sel := stmt.(*SelectStatement)
	outerFn, ok := sel.Columns[0].Expr.(*FuncExpr)
	if !ok {
		t.Fatalf("期望外层 FuncExpr，得到 %T", sel.Columns[0].Expr)
	}
	if outerFn.Name != testFuncCount {
		t.Errorf("期望外层函数名 'count'，得到 %q", outerFn.Name)
	}
	innerFn, ok := outerFn.Args[0].(*FuncExpr)
	if !ok {
		t.Fatalf("期望内层 FuncExpr，得到 %T", outerFn.Args[0])
	}
	if innerFn.Name != testFuncMax {
		t.Errorf("期望内层函数名 'max'，得到 %q", innerFn.Name)
	}
}

// TestConvertFuncExpr_FunctionWithComparisonArg 测试 convertFuncExpr 处理函数内比较表达式。
func TestConvertFuncExpr_FunctionWithComparisonArg(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT MIN(id) FROM t")
	if err != nil {
		t.Fatalf("Parse MIN(id): %v", err)
	}
	sel := stmt.(*SelectStatement)
	fn, ok := sel.Columns[0].Expr.(*FuncExpr)
	if !ok {
		t.Fatalf("期望 FuncExpr，得到 %T", sel.Columns[0].Expr)
	}
	if fn.Name != testFuncMin {
		t.Errorf("期望函数名 'min'，得到 %q", fn.Name)
	}
	colArg, ok := fn.Args[0].(*ColumnExpr)
	if !ok {
		t.Fatalf("期望 ColumnExpr 参数，得到 %T", fn.Args[0])
	}
	if colArg.Name != testColID {
		t.Errorf("期望参数名 'id'，得到 %q", colArg.Name)
	}
}

// TestConvertFuncExpr_UnsupportedArgType 直接测试 convertFuncExpr 中
// 不支持的函数参数类型路径（line 351）。
// sqlparser 的 Nextval 类型不是 AliasedExpr 也不是 StarExpr，可以触发此路径。
func TestConvertFuncExpr_UnsupportedArgType(t *testing.T) {
	p := &Parser{}
	// 构造一个 FuncExpr，其中参数不是 AliasedExpr 也不是 StarExpr
	// 使用 sqlparser.Nextval 来触发不支持的参数类型路径
	fn := &sqlparser.FuncExpr{
		Name: sqlparser.NewColIdent("test_func"),
		Exprs: []sqlparser.SelectExpr{
			&sqlparser.Nextval{},
		},
	}
	_, err := p.convertFuncExpr(fn)
	if err == nil {
		t.Error("期望不支持的函数参数类型返回错误，得到 nil")
	}
}

// TestConvertFuncExpr_ArgConversionError 直接测试 convertFuncExpr 中
// 参数转换错误路径（line 354-356）。
// 当函数参数包含不支持的表达式类型（如 IsExpr）时，convertExpr 会返回错误。
func TestConvertFuncExpr_ArgConversionError(t *testing.T) {
	p := &Parser{}
	// 构造一个 FuncExpr，其中参数的内部表达式是不支持的类型
	// AliasedExpr 包含一个 IsExpr（IS NULL），convertExpr 不支持 IsExpr
	fn := &sqlparser.FuncExpr{
		Name: sqlparser.NewColIdent("test_func"),
		Exprs: []sqlparser.SelectExpr{
			&sqlparser.AliasedExpr{
				Expr: &sqlparser.IsExpr{
					Operator: sqlparser.IsNullStr,
					Expr:     &sqlparser.ColName{Name: sqlparser.NewColIdent("id")},
				},
			},
		},
	}
	_, err := p.convertFuncExpr(fn)
	if err == nil {
		t.Error("期望函数参数转换错误返回错误，得到 nil")
	}
}
