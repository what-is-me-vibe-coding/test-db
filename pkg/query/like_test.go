package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// likeMatch: SQL LIKE 模式匹配纯函数
// ---------------------------------------------------------------------------

func TestLikeMatch(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		pattern string
		want    bool
	}{
		// % 通配符
		{"percent_prefix", "alice", "%alice", true},
		{"percent_suffix", "alice", "alice%", true},
		{"percent_both", "alice", "%ali%", true},
		{"percent_middle", "alice", "al%ce", true},
		{"percent_match_empty", "", "%", true},
		{"percent_only", "anything", "%", true},
		{"percent_no_match", "bob", "%ali%", false},

		// _ 通配符（恰好一个字符）
		{"underscore_one", "bob", "b_b", true},
		{"underscore_exact_len", "bob", "___", true},
		{"underscore_too_short", "bo", "___", false},
		{"underscore_too_long", "bobb", "___", false},
		{"underscore_middle", "alice", "al_ce", true},
		{"underscore_no_match", "alice", "al__ce", false},

		// 字面匹配
		{"literal_exact", "alice", "alice", true},
		{"literal_no_match", "alice", "bob", false},
		{"literal_partial_no_match", "alice", "ali", false},

		// 大小写敏感
		{"case_sensitive_mismatch", "Alice", "alice", false},
		{"case_sensitive_mismatch_prefix", "Alice", "A%", true},

		// 组合
		{"combo_prefix_underscore", "alice", "a_ice", true},
		{"combo_multi_percent", "abcdef", "a%%f", true},
		{"combo_underscore_percent", "abcdef", "a_c%", true},

		// 边界
		{"empty_string_empty_pattern", "", "", true},
		{"empty_string_underscore", "", "_", false},
		{"empty_string_literal", "", "a", false},
		{"non_empty_empty_pattern", "a", "", false},
		{"trailing_percent_after_mismatch", "abc", "abd%", false},

		// 特殊字符按字面匹配（非通配符）
		{"dot_literal", "a.b", "a.b", true},
		{"dot_underscore", "a.b", "a_b", true},
		{"regex_metachar_literal", "a(c)d", "a(c)d", true},

		// UTF-8 多字节字符
		{"utf8_literal", "你好", "你好", true},
		{"utf8_percent", "你好世界", "你好%", true},
		{"utf8_underscore", "你好", "你_", true},
		{"utf8_underscore_no_match", "你", "你_", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := likeMatch(tt.s, tt.pattern); got != tt.want {
				t.Errorf("likeMatch(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// matchLike: Value 层 LIKE 匹配（含类型转换）
// ---------------------------------------------------------------------------

func TestMatchLike(t *testing.T) {
	tests := []struct {
		name    string
		left    common.Value
		pattern common.Value
		want    bool
	}{
		{"string_match", common.NewString("alice"), common.NewString("%ali%"), true},
		{"string_no_match", common.NewString("bob"), common.NewString("%ali%"), false},
		{"string_exact", common.NewString("alice"), common.NewString("alice"), true},
		{"int_via_string_repr", common.NewInt64(30), common.NewString("3%"), true},
		{"int_no_match", common.NewInt64(25), common.NewString("3%"), false},
		{"bool_true_string", common.NewBool(true), common.NewString("tru%"), true},
		{"float_string_repr", common.NewFloat64(95.5), common.NewString("95%"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchLike(tt.left, tt.pattern); got != tt.want {
				t.Errorf("matchLike(%v, %v) = %v, want %v", tt.left, tt.pattern, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 执行器层：OpLike 谓词过滤（直接构造计划节点）
// ---------------------------------------------------------------------------

func TestExecutorFilterLike(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameCharlie),
		testColAge: common.NewInt64(40), testColScore: common.NewFloat64(72.0),
	})

	scan := &ScanNode{
		Table:   testTableUsers,
		Columns: []string{testColID, testColName, testColAge, testColScore},
		schema:  buildTestSchema(),
	}

	filter := &FilterNode{
		Child: scan,
		Condition: &BinaryExpr{
			Op:    OpLike,
			Left:  &ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("%a%")},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter like: %v", err)
	}
	// alice 与 charlie 均含 'a'，bob 不含
	if got := countRows(chunks); got != 2 {
		t.Errorf("expected 2 rows (name LIKE '%%a%%'), got %d", got)
	}
}

// TestExecutorFilterLikeNullOperand 验证 NULL 参与 LIKE 时返回 NULL（WHERE 中视为不匹配）。
func TestExecutorFilterLikeNullOperand(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewNull(),
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

	filter := &FilterNode{
		Child: scan,
		Condition: &BinaryExpr{
			Op:    OpLike,
			Left:  &ResolvedColumnExpr{Name: testColName, Idx: 1, typ: common.TypeString},
			Right: &LiteralExpr{Value: common.NewString("%")},
		},
	}

	exec := NewExecutor(ms)
	chunks, err := exec.Execute(filter)
	if err != nil {
		t.Fatalf("execute filter like null: %v", err)
	}
	// NULL LIKE '%' -> NULL -> 不匹配，仅 bob 命中
	if got := countRows(chunks); got != 1 {
		t.Errorf("expected 1 row (NULL excluded), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// 端到端：SQL 解析 -> 分析 -> 优化 -> 执行
// ---------------------------------------------------------------------------

func TestLikeEndToEnd(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})
	ms.addEntry("b", map[string]common.Value{
		testColID: common.NewInt64(2), testColName: common.NewString(testNameBob),
		testColAge: common.NewInt64(25), testColScore: common.NewFloat64(88.0),
	})
	ms.addEntry("c", map[string]common.Value{
		testColID: common.NewInt64(3), testColName: common.NewString(testNameDiana),
		testColAge: common.NewInt64(40), testColScore: common.NewFloat64(72.0),
	})

	cases := []struct {
		name string
		sql  string
		want int
	}{
		{"like_contains", "SELECT name FROM users WHERE name LIKE '%a%'", 2},   // alice, diana
		{"like_prefix", "SELECT name FROM users WHERE name LIKE 'al%'", 1},     // alice
		{"like_suffix", "SELECT name FROM users WHERE name LIKE '%b'", 1},      // bob
		{"like_underscore", "SELECT name FROM users WHERE name LIKE 'b_b'", 1}, // bob
		{"like_all", "SELECT name FROM users WHERE name LIKE '%'", 3},
		{"like_none", "SELECT name FROM users WHERE name LIKE 'zzz'", 0},
		{"like_case_sensitive", "SELECT name FROM users WHERE name LIKE 'A%'", 0},         // 大小写敏感
		{"not_like", "SELECT name FROM users WHERE name NOT LIKE 'b%'", 2},                // alice, diana
		{"not_expr_like", "SELECT name FROM users WHERE NOT (name LIKE '%a%')", 1},        // bob
		{"like_with_and", "SELECT name FROM users WHERE name LIKE '%a%' AND age > 35", 1}, // diana
		{"like_int_column", "SELECT name FROM users WHERE age LIKE '3%'", 1},              // age=30
	}

	analyzer := NewAnalyzer(testCatalog())
	exec := NewExecutor(ms)
	parser := NewParser()
	optimizer := NewOptimizer()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.sql, err)
			}
			plan, err := analyzer.Analyze(stmt)
			if err != nil {
				t.Fatalf("analyze %q: %v", tt.sql, err)
			}
			chunks, err := exec.Execute(optimizer.Optimize(plan))
			if err != nil {
				t.Fatalf("execute %q: %v", tt.sql, err)
			}
			if got := countRows(chunks); got != tt.want {
				t.Errorf("SQL %q -> %d rows, want %d", tt.sql, got, tt.want)
			}
		})
	}
}

// TestLikeInProjection 验证 LIKE 出现在 SELECT 投影列表中可正确求值为布尔值。
func TestLikeInProjection(t *testing.T) {
	ms := newMockStorage()
	ms.addEntry("a", map[string]common.Value{
		testColID: common.NewInt64(1), testColName: common.NewString(testNameAlice),
		testColAge: common.NewInt64(30), testColScore: common.NewFloat64(95.5),
	})

	analyzer := NewAnalyzer(testCatalog())
	exec := NewExecutor(ms)
	parser := NewParser()
	optimizer := NewOptimizer()

	stmt, err := parser.Parse("SELECT name LIKE 'al%' AS m FROM users")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	plan, err := analyzer.Analyze(stmt)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	chunks, err := exec.Execute(optimizer.Optimize(plan))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := countRows(chunks); got != 1 {
		t.Fatalf("expected 1 row, got %d", got)
	}
	col := chunks[0].Columns()[0]
	v := col.GetValue(0)
	if v.Typ != common.TypeBool || v.Int64 != 1 {
		t.Errorf("expected BOOL true for 'alice' LIKE 'al%%', got %v", v)
	}
}

// TestParseNotLike 验证 NOT LIKE 解析为 NOT(LIKE) 结构。
func TestParseNotLike(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT name FROM users WHERE name NOT LIKE 'b%'")
	if err != nil {
		t.Fatalf("parse NOT LIKE: %v", err)
	}
	sel := stmt.(*SelectStatement)
	unary, ok := sel.Where.(*UnaryExpr)
	if !ok || unary.Op != OpNot {
		t.Fatalf("expected UnaryExpr(OpNot), got %T", sel.Where)
	}
	bin, ok := unary.Expr.(*BinaryExpr)
	if !ok || bin.Op != OpLike {
		t.Fatalf("expected inner BinaryExpr(OpLike), got %T", unary.Expr)
	}
}
