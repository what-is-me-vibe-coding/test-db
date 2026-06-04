package query

import "testing"

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
