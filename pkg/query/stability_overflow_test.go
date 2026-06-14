package query

import (
	"math"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestIntOverflowAdd 验证整数加法溢出时返回 NULL 而非静默溢出
func TestIntOverflowAdd(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		wantNull bool
	}{
		{"normal_add", 1, 2, false},
		{"max_plus_one", math.MaxInt64, 1, true},
		{"max_plus_max", math.MaxInt64, math.MaxInt64, true},
		{"min_plus_minus_one", math.MinInt64, -1, true},
		{"min_plus_min", math.MinInt64, math.MinInt64, true},
		{"zero_add", 0, 0, false},
		{"negative_add", -10, -20, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntArithmetic(tt.a, tt.b, opAdd)
			if tt.wantNull {
				if val.Valid {
					t.Errorf("expected NULL for %d + %d, got %d", tt.a, tt.b, val.Int64)
				}
				if err == nil {
					t.Errorf("expected error for %d + %d overflow", tt.a, tt.b)
				}
			} else {
				if !val.Valid {
					t.Errorf("unexpected NULL for %d + %d", tt.a, tt.b)
				}
				if err != nil {
					t.Errorf("unexpected error for %d + %d: %v", tt.a, tt.b, err)
				}
			}
		})
	}
}

// TestIntOverflowSub 验证整数减法溢出时返回 NULL
func TestIntOverflowSub(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		wantNull bool
	}{
		{"normal_sub", 10, 3, false},
		{"min_sub_one", math.MinInt64, 1, true},
		{"max_sub_minus_one", math.MaxInt64, -1, true},
		{"zero_sub", 0, 0, false},
		{"negative_sub", -10, -20, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntArithmetic(tt.a, tt.b, opSub)
			if tt.wantNull {
				if val.Valid {
					t.Errorf("expected NULL for %d - %d, got %d", tt.a, tt.b, val.Int64)
				}
				if err == nil {
					t.Errorf("expected error for %d - %d overflow", tt.a, tt.b)
				}
			} else {
				if !val.Valid {
					t.Errorf("unexpected NULL for %d - %d", tt.a, tt.b)
				}
				if err != nil {
					t.Errorf("unexpected error for %d - %d: %v", tt.a, tt.b, err)
				}
			}
		})
	}
}

// TestIntOverflowMul 验证整数乘法溢出时返回 NULL
func TestIntOverflowMul(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		wantNull bool
	}{
		{"normal_mul", 6, 7, false},
		{"max_times_two", math.MaxInt64, 2, true},
		{"min_times_two", math.MinInt64, 2, true},
		{"max_times_max", math.MaxInt64, math.MaxInt64, true},
		{"zero_mul", 0, math.MaxInt64, false},
		{"one_mul", 1, math.MaxInt64, false},
		{"negative_mul", -3, 4, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntArithmetic(tt.a, tt.b, opMul)
			if tt.wantNull {
				if val.Valid {
					t.Errorf("expected NULL for %d * %d, got %d", tt.a, tt.b, val.Int64)
				}
				if err == nil {
					t.Errorf("expected error for %d * %d overflow", tt.a, tt.b)
				}
			} else {
				if !val.Valid {
					t.Errorf("unexpected NULL for %d * %d", tt.a, tt.b)
				}
				if err != nil {
					t.Errorf("unexpected error for %d * %d: %v", tt.a, tt.b, err)
				}
			}
		})
	}
}

// TestIntDivByZero 验证整数除以零返回 NULL
func TestIntDivByZero(t *testing.T) {
	val, err := evalIntArithmetic(10, 0, opDiv)
	if val.Valid {
		t.Errorf("expected NULL for division by zero, got %d", val.Int64)
	}
	if err != nil {
		t.Errorf("unexpected error for division by zero: %v", err)
	}
}

// TestIntNormalArithmetic 验证正常整数运算结果正确
func TestIntNormalArithmetic(t *testing.T) {
	tests := []struct {
		name string
		a, b int64
		op   arithOp
		want int64
	}{
		{"add", 10, 20, opAdd, 30},
		{"sub", 50, 20, opSub, 30},
		{"mul", 6, 5, opMul, 30},
		{"div", 30, 5, opDiv, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntArithmetic(tt.a, tt.b, tt.op)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !val.Valid {
				t.Fatalf("unexpected NULL")
			}
			if val.Int64 != tt.want {
				t.Errorf("expected %d, got %d", tt.want, val.Int64)
			}
		})
	}
}

// TestBuildGroupKeyWithError 验证 buildGroupKey 在表达式求值失败时不会 panic
func TestBuildGroupKeyWithError(t *testing.T) {
	row := map[string]common.Value{testStrCol1: common.NewInt64(42)}
	colIdxMap := map[string]int{testStrCol1: 0}

	// 正常情况
	key := buildGroupKey([]Expression{&ResolvedColumnExpr{Name: testStrCol1, Idx: 0, typ: common.TypeInt64}}, row, colIdxMap)
	if key != "42" {
		t.Errorf("expected group key '42', got %q", key)
	}

	// 不存在的列应返回 NULL 的字符串表示
	key = buildGroupKey([]Expression{&ResolvedColumnExpr{Name: testColNonexistent, Idx: 99, typ: common.TypeInt64}}, row, colIdxMap)
	if key == "" {
		t.Errorf("expected non-empty group key for missing column, got empty")
	}
}

// TestAggregateErrorHandling 验证聚合操作在表达式求值失败时不会崩溃
func TestAggregateErrorHandling(t *testing.T) {
	// 验证 COUNT 累加器：COUNT(*) 统计所有行（包括 NULL）
	countAcc := accumulator{funcType: AggCount}
	countAcc.update(common.NewNull()) // COUNT(*) 统计所有行
	countAcc.update(common.NewInt64(1))
	if countAcc.count != 2 {
		t.Errorf("expected count=2 for COUNT(*), got %d", countAcc.count)
	}

	// SUM 在 NULL 时跳过
	sumAcc := accumulator{funcType: AggSum}
	sumAcc.update(common.NewNull())
	sumAcc.update(common.NewInt64(10))
	if sumAcc.count != 1 {
		t.Errorf("expected sum count=1, got %d", sumAcc.count)
	}

	// MIN/MAX 在 NULL 时跳过
	minAcc := accumulator{funcType: AggMin}
	minAcc.update(common.NewNull())
	if minAcc.hasValue {
		t.Errorf("expected hasValue=false after NULL update for MIN")
	}
	minAcc.update(common.NewInt64(5))
	if !minAcc.hasValue {
		t.Errorf("expected hasValue=true after non-NULL update for MIN")
	}
}

// ---------------------------------------------------------------------------
// mulOverflows direct tests
// ---------------------------------------------------------------------------

// TestMulOverflowsRiPositive tests mulOverflows when ri > 0.
func TestMulOverflowsRiPositive(t *testing.T) {
	tests := []struct {
		name      string
		li, ri    int64
		overflows bool
	}{
		{
			name:      "positive_overflow_li_exceeds_max_div_ri",
			li:        math.MaxInt64/2 + 1,
			ri:        2,
			overflows: true,
		},
		{
			name:      "negative_overflow_li_below_min_div_ri",
			li:        math.MinInt64/2 - 1,
			ri:        2,
			overflows: true,
		},
		{
			name:      "no_overflow_within_range",
			li:        math.MaxInt64 / 2,
			ri:        2,
			overflows: false,
		},
		{
			name:      "no_overflow_small_values",
			li:        100,
			ri:        200,
			overflows: false,
		},
		{
			name:      "positive_overflow_max_times_2",
			li:        math.MaxInt64,
			ri:        2,
			overflows: true,
		},
		{
			name:      "negative_overflow_min_times_2",
			li:        math.MinInt64,
			ri:        2,
			overflows: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mulOverflows(tt.li, tt.ri)
			if got != tt.overflows {
				t.Errorf("mulOverflows(%d, %d) = %v, want %v", tt.li, tt.ri, got, tt.overflows)
			}
		})
	}
}

// TestMulOverflowsRiNegative tests mulOverflows when ri < 0.
func TestMulOverflowsRiNegative(t *testing.T) {
	tests := []struct {
		name      string
		li, ri    int64
		overflows bool
	}{
		{
			name:      "positive_overflow_li_exceeds_min_div_ri",
			li:        math.MinInt64/(-2) + 2,
			ri:        -2,
			overflows: true,
		},
		{
			name:      "negative_overflow_li_below_max_div_ri",
			li:        math.MaxInt64/(-2) - 2,
			ri:        -2,
			overflows: true,
		},
		{
			name:      "no_overflow_within_range",
			li:        math.MinInt64 / (-2),
			ri:        -2,
			overflows: false,
		},
		{
			name:      "no_overflow_small_values",
			li:        100,
			ri:        -3,
			overflows: false,
		},
		{
			name:      "negative_ri_positive_li_no_overflow",
			li:        10,
			ri:        -5,
			overflows: false,
		},
		{
			name:      "negative_ri_negative_li_no_overflow",
			li:        -10,
			ri:        -5,
			overflows: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mulOverflows(tt.li, tt.ri)
			if got != tt.overflows {
				t.Errorf("mulOverflows(%d, %d) = %v, want %v", tt.li, tt.ri, got, tt.overflows)
			}
		})
	}
}

// TestMulOverflowsRiZero tests that mulOverflows returns false when ri == 0.
func TestMulOverflowsRiZero(t *testing.T) {
	if mulOverflows(0, 0) {
		t.Error("mulOverflows(0, 0) = true, want false")
	}
	if mulOverflows(math.MaxInt64, 0) {
		t.Error("mulOverflows(MaxInt64, 0) = true, want false")
	}
	if mulOverflows(math.MinInt64, 0) {
		t.Error("mulOverflows(MinInt64, 0) = true, want false")
	}
	if mulOverflows(12345, 0) {
		t.Error("mulOverflows(12345, 0) = true, want false")
	}
}

// TestMulOverflowsBoundary tests boundary cases ri == 1, ri == -1, li == 0.
func TestMulOverflowsBoundary(t *testing.T) {
	// ri == 1: any li multiplied by 1 cannot overflow
	if mulOverflows(math.MaxInt64, 1) {
		t.Error("mulOverflows(MaxInt64, 1) = true, want false")
	}
	if mulOverflows(math.MinInt64, 1) {
		t.Error("mulOverflows(MinInt64, 1) = true, want false")
	}
	if mulOverflows(0, 1) {
		t.Error("mulOverflows(0, 1) = true, want false")
	}

	// ri == -1: MinInt64 * -1 would overflow (result = MaxInt64+1),
	// but other values * -1 are valid. After the fix, mulOverflows
	// correctly handles ri == -1 without causing integer division overflow.
	t.Run("ri_neg1_fixed", func(t *testing.T) {
		// MinInt64 * -1 overflows (result would be MaxInt64+1)
		if !mulOverflows(math.MinInt64, -1) {
			t.Error("mulOverflows(MinInt64, -1) = false, want true (overflows)")
		}
		// MaxInt64 * -1 = -MaxInt64, valid int64
		if mulOverflows(math.MaxInt64, -1) {
			t.Error("mulOverflows(MaxInt64, -1) = true, want false (no overflow)")
		}
		// 0 * -1 = 0, valid
		if mulOverflows(0, -1) {
			t.Error("mulOverflows(0, -1) = true, want false (no overflow)")
		}
		// 42 * -1 = -42, valid
		if mulOverflows(42, -1) {
			t.Error("mulOverflows(42, -1) = true, want false (no overflow)")
		}
	})

	// li == 0: 0 * anything should not overflow
	if mulOverflows(0, math.MaxInt64) {
		t.Error("mulOverflows(0, MaxInt64) = true, want false")
	}
	if mulOverflows(0, math.MinInt64) {
		t.Error("mulOverflows(0, MinInt64) = true, want false")
	}
}

// TestEvalIntMulMinInt64NegOne verifies that MinInt64 * -1 returns an overflow
// error rather than panicking. The current mulOverflows implementation has a bug:
// computing MinInt64/ri when ri == -1 causes a runtime panic (integer division
// overflow). This test documents that bug.
func TestEvalIntMulMinInt64NegOne(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("evalIntMul(MinInt64, -1) panicked (bug in mulOverflows): %v", r)
		}
	}()

	val, err := evalIntMul(math.MinInt64, -1)
	if err == nil {
		t.Errorf("expected overflow error for MinInt64 * -1, got value %d", val.Int64)
	}
	if val.Valid {
		t.Errorf("expected NULL value for MinInt64 * -1 overflow, got %d", val.Int64)
	}
}

// ---------------------------------------------------------------------------
// evalIntAdd / evalIntSub / evalIntMul direct tests
// ---------------------------------------------------------------------------

// TestEvalIntAddOverflow tests evalIntAdd with overflow and normal cases.
func TestEvalIntAddOverflow(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		wantNull bool
	}{
		{"max_plus_one", math.MaxInt64, 1, true},
		{"min_plus_minus_one", math.MinInt64, -1, true},
		{"max_plus_max", math.MaxInt64, math.MaxInt64, true},
		{"min_plus_min", math.MinInt64, math.MinInt64, true},
		{"add_normal", 100, 200, false},
		{"zero_plus_zero", 0, 0, false},
		{"max_plus_zero", math.MaxInt64, 0, false},
		{"min_plus_zero", math.MinInt64, 0, false},
		{"negative_normal", -50, -30, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntAdd(tt.a, tt.b)
			if tt.wantNull {
				if val.Valid {
					t.Errorf("expected NULL for %d + %d, got %d", tt.a, tt.b, val.Int64)
				}
				if err == nil {
					t.Errorf("expected error for %d + %d overflow", tt.a, tt.b)
				}
			} else {
				if !val.Valid {
					t.Errorf("unexpected NULL for %d + %d", tt.a, tt.b)
				}
				if err != nil {
					t.Errorf("unexpected error for %d + %d: %v", tt.a, tt.b, err)
				}
			}
		})
	}
}

// TestEvalIntSubOverflow tests evalIntSub with overflow and normal cases.
func TestEvalIntSubOverflow(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		wantNull bool
	}{
		{"max_sub_minus_one", math.MaxInt64, -1, true},
		{"min_sub_one", math.MinInt64, 1, true},
		{"sub_normal", 100, 30, false},
		{"zero_sub_zero", 0, 0, false},
		{"max_sub_zero", math.MaxInt64, 0, false},
		{"min_sub_zero", math.MinInt64, 0, false},
		{"negative_sub_negative", -10, -20, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntSub(tt.a, tt.b)
			if tt.wantNull {
				if val.Valid {
					t.Errorf("expected NULL for %d - %d, got %d", tt.a, tt.b, val.Int64)
				}
				if err == nil {
					t.Errorf("expected error for %d - %d overflow", tt.a, tt.b)
				}
			} else {
				if !val.Valid {
					t.Errorf("unexpected NULL for %d - %d", tt.a, tt.b)
				}
				if err != nil {
					t.Errorf("unexpected error for %d - %d: %v", tt.a, tt.b, err)
				}
			}
		})
	}
}

// TestEvalIntMulOverflow tests evalIntMul with overflow and normal cases.
func TestEvalIntMulOverflow(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		wantNull bool
	}{
		{"max_times_two", math.MaxInt64, 2, true},
		{"min_times_two", math.MinInt64, 2, true},
		{"max_times_max", math.MaxInt64, math.MaxInt64, true},
		{"mul_normal", 6, 7, false},
		{"zero_times_max", 0, math.MaxInt64, false},
		{"one_times_max", 1, math.MaxInt64, false},
		{"negative_normal", -3, 4, false},
		{"both_negative", -5, -6, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalIntMul(tt.a, tt.b)
			if tt.wantNull {
				if val.Valid {
					t.Errorf("expected NULL for %d * %d, got %d", tt.a, tt.b, val.Int64)
				}
				if err == nil {
					t.Errorf("expected error for %d * %d overflow", tt.a, tt.b)
				}
			} else {
				if !val.Valid {
					t.Errorf("unexpected NULL for %d * %d", tt.a, tt.b)
				}
				if err != nil {
					t.Errorf("unexpected error for %d * %d: %v", tt.a, tt.b, err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// evalIntArithmetic division by zero
// ---------------------------------------------------------------------------

// TestEvalIntArithmeticDivByZero tests integer division by zero via evalIntArithmetic.
func TestEvalIntArithmeticDivByZero(t *testing.T) {
	val, err := evalIntArithmetic(100, 0, opDiv)
	if val.Valid {
		t.Errorf("expected NULL for integer division by zero, got %d", val.Int64)
	}
	if err != nil {
		t.Errorf("unexpected error for integer division by zero: %v", err)
	}
}

// ---------------------------------------------------------------------------
// evalFloatArithmetic tests
// ---------------------------------------------------------------------------

// TestEvalFloatArithmetic tests all float arithmetic operations.
func TestEvalFloatArithmetic(t *testing.T) {
	tests := []struct {
		name string
		lf   float64
		rf   float64
		op   arithOp
		want float64
	}{
		{"add", 1.5, 2.5, opAdd, 4.0},
		{"sub", 5.0, 2.0, opSub, 3.0},
		{"mul", 3.0, 4.0, opMul, 12.0},
		{"div", 10.0, 4.0, opDiv, 2.5},
		{"add_negative", -1.5, 2.5, opAdd, 1.0},
		{"sub_negative", 1.0, -2.0, opSub, 3.0},
		{"mul_negative", -3.0, 4.0, opMul, -12.0},
		{"div_negative", -10.0, 2.0, opDiv, -5.0},
		{"add_zero", 0.0, 0.0, opAdd, 0.0},
		{"mul_zero", 5.0, 0.0, opMul, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := evalFloatArithmetic(tt.lf, tt.rf, tt.op)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !val.Valid {
				t.Fatalf("unexpected NULL")
			}
			if val.Float64 != tt.want {
				t.Errorf("expected %g, got %g", tt.want, val.Float64)
			}
		})
	}
}

// TestEvalFloatArithmeticDivByZero tests float division by zero returns NULL.
func TestEvalFloatArithmeticDivByZero(t *testing.T) {
	val, err := evalFloatArithmetic(10.0, 0.0, opDiv)
	if val.Valid {
		t.Errorf("expected NULL for float division by zero, got %g", val.Float64)
	}
	if err != nil {
		t.Errorf("unexpected error for float division by zero: %v", err)
	}
}

// ---------------------------------------------------------------------------
// evalArithmetic mixed int/float type tests
// ---------------------------------------------------------------------------

// TestEvalArithmeticMixedTypes tests that when either operand is float,
// the result is computed as float.
func TestEvalArithmeticMixedTypes(t *testing.T) {
	tests := []struct {
		name    string
		left    common.Value
		right   common.Value
		op      arithOp
		wantTyp common.DataType
		wantF   float64
	}{
		{
			name:    "int_plus_float",
			left:    common.NewInt64(3),
			right:   common.NewFloat64(2.5),
			op:      opAdd,
			wantTyp: common.TypeFloat64,
			wantF:   5.5,
		},
		{
			name:    "float_plus_int",
			left:    common.NewFloat64(3.5),
			right:   common.NewInt64(2),
			op:      opAdd,
			wantTyp: common.TypeFloat64,
			wantF:   5.5,
		},
		{
			name:    "int_mul_float",
			left:    common.NewInt64(4),
			right:   common.NewFloat64(2.5),
			op:      opMul,
			wantTyp: common.TypeFloat64,
			wantF:   10.0,
		},
		{
			name:    "float_div_int",
			left:    common.NewFloat64(10.0),
			right:   common.NewInt64(4),
			op:      opDiv,
			wantTyp: common.TypeFloat64,
			wantF:   2.5,
		},
		{
			name:    "int_div_float",
			left:    common.NewInt64(9),
			right:   common.NewFloat64(2.0),
			op:      opDiv,
			wantTyp: common.TypeFloat64,
			wantF:   4.5,
		},
		{
			name:    "int_sub_float",
			left:    common.NewInt64(10),
			right:   common.NewFloat64(3.5),
			op:      opSub,
			wantTyp: common.TypeFloat64,
			wantF:   6.5,
		},
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
			if val.Typ != tt.wantTyp {
				t.Errorf("expected type %v, got %v", tt.wantTyp, val.Typ)
			}
			if val.Float64 != tt.wantF {
				t.Errorf("expected %g, got %g", tt.wantF, val.Float64)
			}
		})
	}
}

// TestEvalArithmeticPureInt tests that int+int stays int.
func TestEvalArithmeticPureInt(t *testing.T) {
	val, err := evalArithmetic(common.NewInt64(10), common.NewInt64(20), opAdd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.Valid {
		t.Fatalf("unexpected NULL")
	}
	if val.Typ != common.TypeInt64 {
		t.Errorf("expected TypeInt64, got %v", val.Typ)
	}
	if val.Int64 != 30 {
		t.Errorf("expected 30, got %d", val.Int64)
	}
}

// TestEvalArithmeticPureFloat tests that float+float stays float.
func TestEvalArithmeticPureFloat(t *testing.T) {
	val, err := evalArithmetic(common.NewFloat64(1.5), common.NewFloat64(2.5), opAdd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.Valid {
		t.Fatalf("unexpected NULL")
	}
	if val.Typ != common.TypeFloat64 {
		t.Errorf("expected TypeFloat64, got %v", val.Typ)
	}
	if val.Float64 != 4.0 {
		t.Errorf("expected 4.0, got %g", val.Float64)
	}
}
