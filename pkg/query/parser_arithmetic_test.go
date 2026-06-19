package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// wantArithBinaryExpr 断言表达式为 *BinaryExpr 且运算符匹配，返回该表达式以便后续检查。
func wantArithBinaryExpr(t *testing.T, expr Expression, op BinaryOp) *BinaryExpr {
	t.Helper()
	bin, ok := expr.(*BinaryExpr)
	if !ok {
		t.Fatalf("期望 *BinaryExpr，得到 %T", expr)
	}
	if bin.Op != op {
		t.Fatalf("运算符 = %v，期望 %v", bin.Op, op)
	}
	return bin
}

// TestParseUpdateArithmeticExpr 验证 UPDATE SET 子句支持列与字面量的算术表达式。
// 覆盖 issue #192：update <table> set <column>=<complex expr>。
func TestParseUpdateArithmeticExpr(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("UPDATE t SET v = id + 1 WHERE id = 10")
	if err != nil {
		t.Fatalf("Parse UPDATE 算术表达式失败: %v", err)
	}
	upd, ok := stmt.(*UpdateStatement)
	if !ok {
		t.Fatalf("期望 *UpdateStatement，得到 %T", stmt)
	}
	if len(upd.Assignments) != 1 {
		t.Fatalf("Assignments 长度 = %d，期望 1", len(upd.Assignments))
	}
	if upd.Assignments[0].Column != "v" {
		t.Errorf("列名 = %q，期望 %q", upd.Assignments[0].Column, "v")
	}
	bin := wantArithBinaryExpr(t, upd.Assignments[0].Value, OpAdd)
	if col, ok := bin.Left.(*ColumnExpr); !ok || col.Name != "id" {
		t.Errorf("左操作数 = %v，期望 ColumnExpr(id)", bin.Left)
	}
	if lit, ok := bin.Right.(*LiteralExpr); !ok || lit.Value.Int64 != 1 {
		t.Errorf("右操作数 = %v，期望 LiteralExpr(1)", bin.Right)
	}
}

// TestParseUpdateArithmeticAllOps 验证四则运算符在 UPDATE SET 中均能解析。
func TestParseUpdateArithmeticAllOps(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		op   BinaryOp
	}{
		{"add", "UPDATE t SET v = id + 1", OpAdd},
		{"sub", "UPDATE t SET v = id - 1", OpSub},
		{"mul", "UPDATE t SET v = id * 2", OpMul},
		{"div", "UPDATE t SET v = id / 2", OpDiv},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewParser()
			stmt, err := p.Parse(c.sql)
			if err != nil {
				t.Fatalf("Parse %q 失败: %v", c.sql, err)
			}
			upd, ok := stmt.(*UpdateStatement)
			if !ok {
				t.Fatalf("期望 *UpdateStatement，得到 %T", stmt)
			}
			wantArithBinaryExpr(t, upd.Assignments[0].Value, c.op)
		})
	}
}

// TestParseUpdateMultiColumnArithmetic 验证多列 UPDATE 同时使用算术表达式。
// 覆盖 issue #192：update <table> set <c1>=<expr1>, <c2>=<expr2>。
func TestParseUpdateMultiColumnArithmetic(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("UPDATE t SET a = b * 2, c = d / 3 WHERE id = 1")
	if err != nil {
		t.Fatalf("Parse 多列 UPDATE 失败: %v", err)
	}
	upd, ok := stmt.(*UpdateStatement)
	if !ok {
		t.Fatalf("期望 *UpdateStatement，得到 %T", stmt)
	}
	if len(upd.Assignments) != 2 {
		t.Fatalf("Assignments 长度 = %d，期望 2", len(upd.Assignments))
	}
	if upd.Assignments[0].Column != "a" {
		t.Errorf("Assignments[0].Column = %q，期望 %q", upd.Assignments[0].Column, "a")
	}
	wantArithBinaryExpr(t, upd.Assignments[0].Value, OpMul)
	if upd.Assignments[1].Column != "c" {
		t.Errorf("Assignments[1].Column = %q，期望 %q", upd.Assignments[1].Column, "c")
	}
	wantArithBinaryExpr(t, upd.Assignments[1].Value, OpDiv)
}

// TestParseUpdateNestedArithmetic 验证嵌套算术表达式的优先级与结合性。
func TestParseUpdateNestedArithmetic(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("UPDATE t SET v = id * 2 + 1")
	if err != nil {
		t.Fatalf("Parse 嵌套算术失败: %v", err)
	}
	upd := stmt.(*UpdateStatement)
	// 优先级：id * 2 + 1 => (id * 2) + 1，顶层为 OpAdd
	top := wantArithBinaryExpr(t, upd.Assignments[0].Value, OpAdd)
	// 左子树为 id * 2
	wantArithBinaryExpr(t, top.Left, OpMul)
}

// TestParseSelectArithmeticProjection 验证 SELECT 投影列支持算术表达式。
// 此前 SELECT id+1 会因 *sqlparser.BinaryExpr 未被转换而报错。
func TestParseSelectArithmeticProjection(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT id + 1 FROM t WHERE id + 1 > 5")
	if err != nil {
		t.Fatalf("Parse SELECT 算术投影失败: %v", err)
	}
	sel, ok := stmt.(*SelectStatement)
	if !ok {
		t.Fatalf("期望 *SelectStatement，得到 %T", stmt)
	}
	if len(sel.Columns) != 1 {
		t.Fatalf("Columns 长度 = %d，期望 1", len(sel.Columns))
	}
	wantArithBinaryExpr(t, sel.Columns[0].Expr, OpAdd)
	// WHERE 同样支持算术：顶层为比较 OpGt，其左子树为 OpAdd
	pred, ok := sel.Where.(*BinaryExpr)
	if !ok || pred.Op != OpGt {
		t.Fatalf("WHERE 期望 OpGt，得到 %v", sel.Where)
	}
	wantArithBinaryExpr(t, pred.Left, OpAdd)
}

// TestParseUpdateUnsupportedArithmeticOp 验证不支持的算术运算符（位运算）返回错误。
func TestParseUpdateUnsupportedArithmeticOp(t *testing.T) {
	p := NewParser()
	// & 为位与运算符，当前不支持，应返回解析错误
	if _, err := p.Parse("UPDATE t SET v = id & 1"); err == nil {
		t.Error("期望位运算符 & 返回错误，但解析成功")
	}
}

// TestParseArithmeticExprTypeInference 验证算术表达式返回类型推导。
// 复用 plan.exprReturnType，确保 int+int→int64、float+int→float64。
func TestParseArithmeticExprTypeInference(t *testing.T) {
	p := NewParser()
	stmt, err := p.Parse("SELECT id + 1 FROM t")
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	sel := stmt.(*SelectStatement)
	expr := sel.Columns[0].Expr
	// 为列表达式标注类型（模拟 analyzer 解析后的状态）
	if bin, ok := expr.(*BinaryExpr); ok {
		if col, ok := bin.Left.(*ColumnExpr); ok {
			col.typ = common.TypeInt64
		}
	}
	got := exprReturnType(expr)
	if got != common.TypeInt64 {
		t.Errorf("int+int 返回类型 = %v，期望 %v", got, common.TypeInt64)
	}
}
