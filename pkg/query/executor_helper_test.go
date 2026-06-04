package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestToFloat64 测试 toFloat64 辅助函数。
func TestToFloat64(t *testing.T) {
	tests := []struct {
		name string
		val  common.Value
		want float64
	}{
		{"float64值直接返回", common.NewFloat64(3.14), 3.14},
		{"int64值转换为float64", common.NewInt64(42), 42.0},
		{"其他类型返回0", common.NewString("hello"), 0},
		{"null类型返回0", common.NewNull(), 0},
		{"bool类型返回0", common.NewBool(true), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toFloat64(tt.val); got != tt.want {
				t.Errorf("toFloat64(%v) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

// TestMaxStr 测试 maxStr 辅助函数。
func TestMaxStr(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"b大于a返回b", "b", "a", "b"},
		{"a小于b返回b", "a", "b", "b"},
		{"相等返回自身", "a", "a", "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maxStr(tt.a, tt.b); got != tt.want {
				t.Errorf("maxStr(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestMinStr 测试 minStr 辅助函数。
func TestMinStr(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"a小于b返回a", "a", "b", "a"},
		{"b大于a返回a", "b", "a", "a"},
		{"相等返回自身", "a", "a", "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minStr(tt.a, tt.b); got != tt.want {
				t.Errorf("minStr(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
