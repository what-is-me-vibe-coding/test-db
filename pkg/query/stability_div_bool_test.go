package query

import (
	"math"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestIntDivMinInt64NegOneOverflow 验证 MinInt64 / -1 被识别为溢出，
// 返回 NULL + 错误，而非静默回绕为 MinInt64。
// 修复前：Go 运行时对 MinInt64 / -1 静默返回 MinInt64（无 panic），
// 与加/减/乘的溢出处理不一致，导致 SELECT MinInt64 / -1 返回错误结果。
func TestIntDivMinInt64NegOneOverflow(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("evalIntArithmetic(MinInt64, -1, opDiv) panicked: %v", r)
		}
	}()

	val, err := evalIntArithmetic(math.MinInt64, -1, opDiv)
	if val.Valid {
		t.Errorf("expected NULL for MinInt64 / -1 overflow, got %d", val.Int64)
	}
	if err == nil {
		t.Errorf("expected overflow error for MinInt64 / -1, got nil")
	}
}

// TestIntDivMinInt64NegOneViaArithmetic 验证通过 evalArithmetic（公共入口）
// 触发 MinInt64 / -1 时同样返回 NULL + 错误。
func TestIntDivMinInt64NegOneViaArithmetic(t *testing.T) {
	val, err := evalArithmetic(common.NewInt64(math.MinInt64), common.NewInt64(-1), opDiv)
	if val.Valid {
		t.Errorf("expected NULL for MinInt64 / -1, got %d", val.Int64)
	}
	if err == nil {
		t.Errorf("expected overflow error for MinInt64 / -1, got nil")
	}
}

// TestIntDivBoundaryNonOverflow 验证除法溢出判定不会误伤边界正常值。
func TestIntDivBoundaryNonOverflow(t *testing.T) {
	tests := []struct {
		name string
		a, b int64
		want int64
	}{
		{"max_div_neg_one", math.MaxInt64, -1, -math.MaxInt64},
		{"min_div_one", math.MinInt64, 1, math.MinInt64},
		{"min_div_two", math.MinInt64, 2, math.MinInt64 / 2},
		{"normal_div", 100, 4, 25},
		{"neg_div_neg", -12, -4, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntArithmetic(tt.a, tt.b, opDiv)
			if err != nil {
				t.Fatalf("unexpected error for %d / %d: %v", tt.a, tt.b, err)
			}
			if !val.Valid {
				t.Fatalf("unexpected NULL for %d / %d", tt.a, tt.b)
			}
			if val.Int64 != tt.want {
				t.Errorf("%d / %d = %d, want %d", tt.a, tt.b, val.Int64, tt.want)
			}
		})
	}
}

// TestToFloat64Bool 验证 toFloat64 将 BOOL 按 0/1 转换。
// 修复前：toFloat64 对 BOOL 返回 0，导致 true + 1.5 = 1.5（错误）。
func TestToFloat64Bool(t *testing.T) {
	tests := []struct {
		name string
		v    common.Value
		want float64
	}{
		{"bool_true", common.NewBool(true), 1.0},
		{"bool_false", common.NewBool(false), 0.0},
		{"int64", common.NewInt64(42), 42.0},
		{"int8", common.NewInt8(7), 7.0},
		{"date", common.NewDate(100), 100.0},
		{"float64", common.NewFloat64(3.5), 3.5},
		{"null", common.NewNull(), 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toFloat64(tt.v); got != tt.want {
				t.Errorf("toFloat64(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

// TestEvalArithmeticBoolFloat 验证 BOOL 参与浮点算术时按 0/1 提升。
// 修复前：true + 1.5 = 1.5（toFloat64(true) 错误返回 0）。
func TestEvalArithmeticBoolFloat(t *testing.T) {
	tests := []struct {
		name  string
		left  common.Value
		right common.Value
		op    arithOp
		want  float64
	}{
		{"true_plus_float", common.NewBool(true), common.NewFloat64(1.5), opAdd, 2.5},
		{"false_plus_float", common.NewBool(false), common.NewFloat64(1.5), opAdd, 1.5},
		{"float_plus_true", common.NewFloat64(2.0), common.NewBool(true), opAdd, 3.0},
		{"true_times_float", common.NewBool(true), common.NewFloat64(2.5), opMul, 2.5},
		{"false_times_float", common.NewBool(false), common.NewFloat64(2.5), opMul, 0.0},
		{"float_minus_true", common.NewFloat64(3.0), common.NewBool(true), opSub, 2.0},
		{"true_div_float", common.NewBool(true), common.NewFloat64(2.0), opDiv, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalArithmetic(tt.left, tt.right, tt.op)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !val.Valid {
				t.Fatalf("unexpected NULL")
			}
			if val.Typ != common.TypeFloat64 {
				t.Errorf("expected TypeFloat64, got %v", val.Typ)
			}
			if val.Float64 != tt.want {
				t.Errorf("evalArithmetic(%v, %v, %v) = %g, want %g",
					tt.left, tt.right, tt.op, val.Float64, tt.want)
			}
		})
	}
}

// TestEvalArithmeticBoolInt 验证 BOOL 参与整数算术时按 0/1 提升（int 路径本就正确，
// 此测试作为回归保护，确保 BOOL+INT 不被 toFloat64 修复影响）。
func TestEvalArithmeticBoolInt(t *testing.T) {
	val, err := evalArithmetic(common.NewBool(true), common.NewInt64(41), opAdd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.Valid {
		t.Fatalf("unexpected NULL")
	}
	if val.Int64 != 42 {
		t.Errorf("true + 41 = %d, want 42", val.Int64)
	}
}
