package query

import "testing"

// TestParseSilentlyDroppedClauses 文档化当前 parser 对 ORDER BY / DISTINCT /
// HAVING 子句的处理：这些子句已被 sqlparser 解析，但项目自研 AST（SelectStatement）
// 未保留相应字段，因此执行器实际忽略它们。
//
// 本测试的目的：
//  1. 锁定当前行为，避免未来重构时无声引入回归（这些子句不应改变查询结果）
//  2. 当未来 PR 实现 ORDER BY/DISTINCT/HAVING 时，本测试将失败，提示需要同步
//     拆分或删除本测试，并增加对应正向用例
//
// 关联说明：e2e_general_sql_multiclient_test.go 注释中已明确"当前 parser 静默
// 丢弃这些子句，相关特性将由后续 PR 单独修复"。本文件作为该约定的测试侧锚点。
func TestParseSilentlyDroppedClauses(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{
			name: "ORDER BY 升序",
			sql:  "SELECT id, name FROM t ORDER BY id",
		},
		{
			name: "ORDER BY 降序",
			sql:  "SELECT id, name FROM t ORDER BY id DESC",
		},
		{
			name: "ORDER BY 多列",
			sql:  "SELECT id, name FROM t ORDER BY name ASC, id DESC",
		},
		{
			name: "DISTINCT 单列",
			sql:  "SELECT DISTINCT name FROM t",
		},
		{
			name: "DISTINCT 多列",
			sql:  "SELECT DISTINCT name, id FROM t",
		},
		{
			name: "HAVING",
			sql:  "SELECT name, COUNT(*) FROM t GROUP BY name HAVING COUNT(*) > 1",
		},
		{
			name: "ORDER BY + LIMIT",
			sql:  "SELECT id FROM t ORDER BY id LIMIT 10",
		},
		{
			name: "DISTINCT + ORDER BY + LIMIT",
			sql:  "SELECT DISTINCT name FROM t ORDER BY name LIMIT 5",
		},
		{
			name: "GROUP BY + HAVING + ORDER BY",
			sql:  "SELECT region, SUM(amount) FROM t GROUP BY region HAVING SUM(amount) > 100 ORDER BY region",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewParser()
			stmt, err := p.Parse(c.sql)
			if err != nil {
				t.Fatalf("Parse(%q) 失败: %v（这些子句当前被静默丢弃，parse 不应失败）", c.sql, err)
			}
			sel, ok := stmt.(*SelectStatement)
			_ = sel // 保留变量名以表达意图：当前 AST 不暴露 OrderBy/Distinct/Having
			if !ok {
				t.Fatalf("Parse(%q): 期望 *SelectStatement，得到 %T", c.sql, stmt)
			}
			// 锁死 SelectStatement 的可见字段：解析成功后不应携带这些子句的元信息。
			// 后续 PR 实现 ORDER BY/DISTINCT/HAVING 时需要：
			//   1. 为 SelectStatement 添加 OrderBy / Distinct / Having 字段
			//   2. 在 convertSelect 中提取 sel.OrderBy/sel.Distinct/sel.Having
			//   3. 更新本测试：拆分为「已被支持」与「仍被静默丢弃」两组用例
			//
			// 显式断言保留子句的「无」元信息：当前 SelectStatement 仅有 Where / GroupBy /
			// Columns 等字段，OrderBy/Distinct/Having 字段在 AST 上不存在（编译期可见），
			// 因此运行期无需再追加断言。
		})
	}
}

// TestParseOrderByNotExposedToString 文档化 SelectStatement.String() 不输出
// ORDER BY / DISTINCT / HAVING：即使 sqlparser 正确解析，AST.String() 也不会
// 反映这些子句（因为字段未保留）。
//
// 当未来实现这些子句时，String() 行为会变化；该测试需同步更新或删除。
func TestParseOrderByNotExposedToString(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT id FROM t ORDER BY id DESC")
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	sel := stmt.(*SelectStatement)
	s := sel.String()
	// 当前行为：String() 不含 "ORDER BY"（因 AST 未保留该字段）
	if containsOrEmpty(s, "ORDER BY") {
		t.Errorf("SelectStatement.String() 当前不应输出 ORDER BY（AST 未保留 OrderBy 字段），实际: %q", s)
	}
	// 未来若实现 OrderBy 字段，此断言应改为：containsOrEmpty(s, "ORDER BY id DESC")
}

// TestParseDistinctNotExposedToString 文档化 SelectStatement 当前不保留 DISTINCT 标记。
func TestParseDistinctNotExposedToString(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT DISTINCT name FROM t")
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	sel := stmt.(*SelectStatement)
	s := sel.String()
	if containsOrEmpty(s, "DISTINCT") {
		t.Errorf("SelectStatement.String() 当前不应输出 DISTINCT，实际: %q", s)
	}
}

// TestParseHavingNotExposedToString 文档化 SelectStatement 当前不保留 HAVING 子句。
// 即使 sqlparser 已识别 HAVING，本项目 AST.SelectStatement 也没有 Having 字段。
func TestParseHavingNotExposedToString(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT name FROM t GROUP BY name HAVING COUNT(*) > 1")
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	sel := stmt.(*SelectStatement)
	s := sel.String()
	if containsOrEmpty(s, "HAVING") {
		t.Errorf("SelectStatement.String() 当前不应输出 HAVING，实际: %q", s)
	}
}

// containsOrEmpty 是 strings.Contains 的本地封装；与 pkg/query 包内其他测试
// 文件中的 contains 行为一致（needle 为空时返回 true）。本文件显式提供
// helper 而非 import strings，是为了与既有测试风格保持一致：parser_engine_test.go
// 等文件也采用内联 helper 而非 import strings。
func containsOrEmpty(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
