package common

import (
	"testing"
)

// TestBitmapNotPreservesCount 验证 Not() 取反后 Count() 仅统计有效位，
// 不将最后一个 word 的填充位误计为 1。
// 修复前：len=10、3 个置位的位图 Not() 后 Count() 返回 61（填充位 10..63 被翻转）。
func TestBitmapNotPreservesCount(t *testing.T) {
	tests := []struct {
		name    string
		length  uint32
		setBits []uint32
	}{
		{"len_10_three_set", 10, []uint32{1, 3, 5}},
		{"len_1_single_set", 1, []uint32{0}},
		{"len_64_full_word", 64, []uint32{0, 31, 63}},
		{"len_65_cross_word", 65, []uint32{0, 64}},
		{"len_100_mixed", 100, []uint32{0, 50, 99}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := NewBitmap(tt.length)
			for _, i := range tt.setBits {
				bm.Set(i)
			}
			original := bm.Count()
			bm.Not()
			got := bm.Count()
			want := tt.length - original
			if got != want {
				t.Errorf("Count() after Not() = %d, want %d (len=%d, set=%d)",
					got, want, tt.length, original)
			}
		})
	}
}

// TestBitmapNotForEachValidBits 验证 Not() 后 ForEach 仅遍历有效范围内的位。
func TestBitmapNotForEachValidBits(t *testing.T) {
	bm := NewBitmap(10)
	bm.Set(1)
	bm.Set(3)
	bm.Set(5)
	bm.Not()

	var visited []uint32
	bm.ForEach(func(idx uint32) {
		visited = append(visited, idx)
		if idx >= 10 {
			t.Errorf("ForEach visited padding bit %d >= len 10", idx)
		}
	})

	wantCount := uint32(7) // 10 - 3
	if uint32(len(visited)) != wantCount {
		t.Errorf("ForEach visited %d bits, want %d", len(visited), wantCount)
	}
	// 原本未置位的 0,2,4,6,7,8,9 取反后应为 1
	expectSet := map[uint32]bool{0: true, 2: true, 4: true, 6: true, 7: true, 8: true, 9: true}
	for _, idx := range visited {
		if !expectSet[idx] {
			t.Errorf("ForEach visited unexpected bit %d", idx)
		}
	}
}

// TestBitmapNotRoundTrip 验证两次 Not() 应回到原始状态（含 Count 与 Equals）。
func TestBitmapNotRoundTrip(t *testing.T) {
	bm := NewBitmap(30)
	bm.Set(2)
	bm.Set(17)
	bm.Set(29)
	original := bm.Clone()

	bm.Not()
	bm.Not()

	if !bm.Equals(original) {
		t.Errorf("double Not() should restore original bitmap")
	}
	if bm.Count() != original.Count() {
		t.Errorf("Count() after double Not() = %d, want %d", bm.Count(), original.Count())
	}
}

// TestBitmapNotEmpty 验证空位图 Not() 不 panic 且保持空。
func TestBitmapNotEmpty(t *testing.T) {
	bm := NewBitmap(0)
	bm.Not()
	if bm.Count() != 0 {
		t.Errorf("empty bitmap Not() Count() = %d, want 0", bm.Count())
	}
}

// TestBitmapNotMultipleOf64 验证 len 为 64 整数倍时 Not() 不误清有效位。
func TestBitmapNotMultipleOf64(t *testing.T) {
	bm := NewBitmap(128)
	bm.Set(0)
	bm.Set(63)
	bm.Set(64)
	bm.Set(127)
	bm.Not()
	// 4 个置位 → 取反后 124 个
	if got := bm.Count(); got != 124 {
		t.Errorf("Count() after Not() = %d, want 124", got)
	}
	if bm.Get(0) || bm.Get(63) || bm.Get(64) || bm.Get(127) {
		t.Errorf("originally set bits should be cleared after Not()")
	}
	if !bm.Get(1) || !bm.Get(126) {
		t.Errorf("originally clear bits should be set after Not()")
	}
}
