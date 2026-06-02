package query

import (
	"fmt"
	"math"
	"strings"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// evalExpr 在 Chunk 的指定行上求值表达式。
// colIndexMap 将列名映射到 Chunk 中的列索引。
func evalExpr(expr Expression, chunk *storage.Chunk, rowIdx uint32, colIndexMap map[string]int) (common.Value, error) {
	switch e := expr.(type) {
	case *ColumnExpr:
		return evalColumnExpr(e.Name, chunk, rowIdx, colIndexMap)
	case *ResolvedColumnExpr:
		return evalResolvedColumnExpr(e, chunk, rowIdx)
	case *LiteralExpr:
		return e.Value, nil
	case *BinaryExpr:
		return evalBinaryExpr(e, chunk, rowIdx, colIndexMap)
	case *UnaryExpr:
		return evalUnaryExpr(e, chunk, rowIdx, colIndexMap)
	case *FuncExpr:
		return evalFuncExpr(e, chunk, rowIdx, colIndexMap)
	case *StarExpr:
		return common.NewNull(), fmt.Errorf("eval expr: star expression cannot be evaluated")
	default:
		return common.NewNull(), fmt.Errorf("eval expr: unsupported expression type %T", expr)
	}
}

// evalColumnExpr 按列名在 Chunk 中查找值。
func evalColumnExpr(name string, chunk *storage.Chunk, rowIdx uint32, colIndexMap map[string]int) (common.Value, error) {
	idx, ok := colIndexMap[name]
	if !ok {
		return common.NewNull(), fmt.Errorf("eval expr: column %q not found", name)
	}
	col, err := chunk.GetColumn(idx)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval expr: get column %q: %w", name, err)
	}
	if rowIdx >= col.Len() {
		return common.NewNull(), fmt.Errorf("eval expr: row index %d out of range [0, %d)", rowIdx, col.Len())
	}
	return col.GetValue(rowIdx), nil
}

// evalResolvedColumnExpr 按已解析的列索引在 Chunk 中查找值。
func evalResolvedColumnExpr(e *ResolvedColumnExpr, chunk *storage.Chunk, rowIdx uint32) (common.Value, error) {
	col, err := chunk.GetColumn(e.Idx)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval expr: get column idx %d: %w", e.Idx, err)
	}
	if rowIdx >= col.Len() {
		return common.NewNull(), fmt.Errorf("eval expr: row index %d out of range", rowIdx)
	}
	return col.GetValue(rowIdx), nil
}

// evalBinaryExpr 求值二元表达式。
func evalBinaryExpr(e *BinaryExpr, chunk *storage.Chunk, rowIdx uint32, colIndexMap map[string]int) (common.Value, error) {
	// 短路求值 AND/OR
	if e.Op == OpAnd {
		return evalAnd(e, chunk, rowIdx, colIndexMap)
	}
	if e.Op == OpOr {
		return evalOr(e, chunk, rowIdx, colIndexMap)
	}

	left, err := evalExpr(e.Left, chunk, rowIdx, colIndexMap)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval binary left: %w", err)
	}
	right, err := evalExpr(e.Right, chunk, rowIdx, colIndexMap)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval binary right: %w", err)
	}

	// NULL 传播：任一操作数为 NULL 时，比较运算返回 NULL
	if left.IsNull() || right.IsNull() {
		if e.Op == OpEq || e.Op == OpNe || e.Op == OpLt ||
			e.Op == OpLe || e.Op == OpGt || e.Op == OpGe {
			return common.NewNull(), nil
		}
		return common.NewNull(), nil
	}

	return applyBinaryOp(e.Op, left, right)
}

// evalAnd 短路求值 AND 表达式。
func evalAnd(e *BinaryExpr, chunk *storage.Chunk, rowIdx uint32, colIndexMap map[string]int) (common.Value, error) {
	left, err := evalExpr(e.Left, chunk, rowIdx, colIndexMap)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval and left: %w", err)
	}
	if left.IsNull() {
		return common.NewNull(), nil
	}
	if !toBool(left) {
		return common.NewBool(false), nil
	}
	right, err := evalExpr(e.Right, chunk, rowIdx, colIndexMap)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval and right: %w", err)
	}
	if right.IsNull() {
		return common.NewNull(), nil
	}
	return common.NewBool(toBool(right)), nil
}

// evalOr 短路求值 OR 表达式。
func evalOr(e *BinaryExpr, chunk *storage.Chunk, rowIdx uint32, colIndexMap map[string]int) (common.Value, error) {
	left, err := evalExpr(e.Left, chunk, rowIdx, colIndexMap)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval or left: %w", err)
	}
	if left.IsNull() {
		// NULL OR false => NULL, NULL OR true => true
		right, err := evalExpr(e.Right, chunk, rowIdx, colIndexMap)
		if err != nil {
			return common.NewNull(), fmt.Errorf("eval or right: %w", err)
		}
		if !right.IsNull() && toBool(right) {
			return common.NewBool(true), nil
		}
		return common.NewNull(), nil
	}
	if toBool(left) {
		return common.NewBool(true), nil
	}
	right, err := evalExpr(e.Right, chunk, rowIdx, colIndexMap)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval or right: %w", err)
	}
	if right.IsNull() {
		return common.NewNull(), nil
	}
	return common.NewBool(toBool(right)), nil
}

// applyBinaryOp 对两个非 NULL 值应用二元运算符。
func applyBinaryOp(op BinaryOp, left, right common.Value) (common.Value, error) {
	switch op {
	case OpEq:
		return common.NewBool(left.Equal(right)), nil
	case OpNe:
		return common.NewBool(!left.Equal(right)), nil
	case OpLt:
		return common.NewBool(left.Less(right)), nil
	case OpLe:
		return common.NewBool(left.Less(right) || left.Equal(right)), nil
	case OpGt:
		return common.NewBool(right.Less(left)), nil
	case OpGe:
		return common.NewBool(right.Less(left) || left.Equal(right)), nil
	case OpAdd, OpSub, OpMul, OpDiv:
		return applyArithmetic(op, left, right)
	case OpLike:
		return applyLike(left, right)
	default:
		return common.NewNull(), fmt.Errorf("eval binary: unsupported op %s", op.String())
	}
}

// applyArithmetic 执行算术运算。
func applyArithmetic(op BinaryOp, left, right common.Value) (common.Value, error) {
	// 类型提升：INT64 与 FLOAT64 运算结果为 FLOAT64
	if left.Typ == common.TypeFloat64 || right.Typ == common.TypeFloat64 {
		lv := toFloat64(left)
		rv := toFloat64(right)
		switch op {
		case OpAdd:
			return common.NewFloat64(lv + rv), nil
		case OpSub:
			return common.NewFloat64(lv - rv), nil
		case OpMul:
			return common.NewFloat64(lv * rv), nil
		case OpDiv:
			if rv == 0 {
				return common.NewNull(), nil
			}
			return common.NewFloat64(lv / rv), nil
		}
	}

	if left.Typ == common.TypeInt64 && right.Typ == common.TypeInt64 {
		lv := left.Int64
		rv := right.Int64
		switch op {
		case OpAdd:
			return common.NewInt64(lv + rv), nil
		case OpSub:
			return common.NewInt64(lv - rv), nil
		case OpMul:
			return common.NewInt64(lv * rv), nil
		case OpDiv:
			if rv == 0 {
				return common.NewNull(), nil
			}
			return common.NewInt64(lv / rv), nil
		}
	}

	return common.NewNull(), fmt.Errorf("eval arithmetic: type mismatch %s %s %s",
		left.Typ.String(), op.String(), right.Typ.String())
}

// applyLike 执行 LIKE 模式匹配（简化版，仅支持 % 和 _ 通配符）。
func applyLike(left, right common.Value) (common.Value, error) {
	if left.Typ != common.TypeString || right.Typ != common.TypeString {
		return common.NewNull(), fmt.Errorf("eval like: operands must be STRING")
	}
	pattern := right.Str
	input := left.Str
	regexPattern := likeToRegex(pattern)
	matched := simpleMatch(input, regexPattern)
	return common.NewBool(matched), nil
}

// likeToRegex 将 SQL LIKE 模式转换为简单的匹配规则。
func likeToRegex(pattern string) string {
	var b strings.Builder
	for _, ch := range pattern {
		switch ch {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteString(".")
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// simpleMatch 简单的模式匹配实现，支持 * 和 ? 通配符。
func simpleMatch(input, pattern string) bool {
	return matchRunes([]rune(input), []rune(pattern), 0, 0)
}

// matchRunes 递归匹配输入和模式。
func matchRunes(input, pattern []rune, i, j int) bool {
	if j == len(pattern) {
		return i == len(input)
	}
	if pattern[j] == '*' {
		// .* 匹配任意序列
		for k := i; k <= len(input); k++ {
			if matchRunes(input, pattern, k, j+1) {
				return true
			}
		}
		return false
	}
	if i >= len(input) {
		return false
	}
	if pattern[j] == '.' || pattern[j] == input[i] {
		return matchRunes(input, pattern, i+1, j+1)
	}
	return false
}

// evalUnaryExpr 求值一元表达式。
func evalUnaryExpr(e *UnaryExpr, chunk *storage.Chunk, rowIdx uint32, colIndexMap map[string]int) (common.Value, error) {
	val, err := evalExpr(e.Expr, chunk, rowIdx, colIndexMap)
	if err != nil {
		return common.NewNull(), fmt.Errorf("eval unary: %w", err)
	}
	if val.IsNull() {
		return common.NewNull(), nil
	}
	switch e.Op {
	case OpNot:
		return common.NewBool(!toBool(val)), nil
	case OpNeg:
		return applyNeg(val)
	default:
		return common.NewNull(), fmt.Errorf("eval unary: unsupported op %s", e.Op.String())
	}
}

// applyNeg 对值取负。
func applyNeg(val common.Value) (common.Value, error) {
	switch val.Typ {
	case common.TypeInt64:
		return common.NewInt64(-val.Int64), nil
	case common.TypeFloat64:
		return common.NewFloat64(-val.Float64), nil
	default:
		return common.NewNull(), fmt.Errorf("eval neg: unsupported type %s", val.Typ.String())
	}
}

// evalFuncExpr 求值函数调用表达式（基础支持）。
func evalFuncExpr(e *FuncExpr, chunk *storage.Chunk, rowIdx uint32, colIndexMap map[string]int) (common.Value, error) {
	args := make([]common.Value, len(e.Args))
	for i, arg := range e.Args {
		v, err := evalExpr(arg, chunk, rowIdx, colIndexMap)
		if err != nil {
			return common.NewNull(), fmt.Errorf("eval func %s arg %d: %w", e.Name, i, err)
		}
		args[i] = v
	}
	return applyFunc(e.Name, args)
}

// applyFunc 应用内置函数。
func applyFunc(name string, args []common.Value) (common.Value, error) {
	switch strings.ToUpper(name) {
	case "ABS":
		if len(args) != 1 {
			return common.NewNull(), fmt.Errorf("func ABS: expected 1 arg, got %d", len(args))
		}
		return applyAbs(args[0])
	case "COALESCE":
		return applyCoalesce(args)
	default:
		return common.NewNull(), fmt.Errorf("eval func: unsupported function %s", name)
	}
}

// applyAbs 计算绝对值。
func applyAbs(val common.Value) (common.Value, error) {
	if val.IsNull() {
		return common.NewNull(), nil
	}
	switch val.Typ {
	case common.TypeInt64:
		v := val.Int64
		if v < 0 {
			v = -v
		}
		return common.NewInt64(v), nil
	case common.TypeFloat64:
		return common.NewFloat64(math.Abs(val.Float64)), nil
	default:
		return common.NewNull(), fmt.Errorf("func ABS: unsupported type %s", val.Typ.String())
	}
}

// applyCoalesce 返回第一个非 NULL 参数。
func applyCoalesce(args []common.Value) (common.Value, error) {
	for _, arg := range args {
		if !arg.IsNull() {
			return arg, nil
		}
	}
	return common.NewNull(), nil
}

// toBool 将 Value 转换为布尔值。
func toBool(v common.Value) bool {
	if v.IsNull() {
		return false
	}
	switch v.Typ {
	case common.TypeBool:
		return v.Int64 != 0
	case common.TypeInt64:
		return v.Int64 != 0
	case common.TypeFloat64:
		return v.Float64 != 0
	default:
		return false
	}
}

// toFloat64 将 Value 转换为 float64。
func toFloat64(v common.Value) float64 {
	switch v.Typ {
	case common.TypeFloat64:
		return v.Float64
	case common.TypeInt64:
		return float64(v.Int64)
	default:
		return 0
	}
}
