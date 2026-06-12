package common

import (
	"testing"
)

// TestBitmapCopyFrom_Aligned 测试源起始位对齐到 word 边界的 CopyFrom。
func TestBitmapCopyFrom_Aligned(t *testing.T) {
	src := NewBitmap(128)
	src.Set(0)
	src.Set(63)
	src.Set(64)
	src.Set(100)

	dst := NewBitmap(64)
	dst.CopyFrom(src, 0, 64)

	if !dst.Get(0) {
		t.Error("期望 dst[0] = true")
	}
	if !dst.Get(63) {
		t.Error("期望 dst[63] = true")
	}
	if dst.Get(64) {
		t.Error("期望 dst[64] = false（超出复制范围）")
	}
}

// TestBitmapCopyFrom_Unaligned 测试源起始位未对齐的 CopyFrom。
func TestBitmapCopyFrom_Unaligned(t *testing.T) {
	src := NewBitmap(200)
	src.Set(10)
	src.Set(73)
	src.Set(74)

	dst := NewBitmap(64)
	dst.CopyFrom(src, 10, 64)

	if !dst.Get(0) {
		t.Error("期望 dst[0] = true（来自 src[10]）")
	}
	if !dst.Get(63) {
		t.Error("期望 dst[63] = true（来自 src[73]）")
	}
}

// TestBitmapCopyFrom_Empty 测试空范围复制。
func TestBitmapCopyFrom_Empty(t *testing.T) {
	src := NewBitmap(64)
	src.Set(0)

	dst := NewBitmap(64)
	dst.CopyFrom(src, 0, 0)

	if dst.Get(0) {
		t.Error("期望 dst[0] = false（空复制）")
	}
}

// TestBitmapCopyFrom_PartialWord 测试不足一个 word 的部分复制。
func TestBitmapCopyFrom_PartialWord(t *testing.T) {
	src := NewBitmap(64)
	src.Set(0)
	src.Set(1)
	src.Set(5)

	dst := NewBitmap(64)
	dst.CopyFrom(src, 0, 6)

	if !dst.Get(0) || !dst.Get(1) || !dst.Get(5) {
		t.Error("期望 dst[0,1,5] = true")
	}
	if dst.Get(6) {
		t.Error("期望 dst[6] = false（超出复制范围）")
	}
}

// TestBitmapGrow_Expand 测试 Grow 扩展位图。
func TestBitmapGrow_Expand(t *testing.T) {
	bm := NewBitmap(64)
	bm.Set(0)
	bm.Set(63)

	bm.Grow(128)

	if !bm.Get(0) {
		t.Error("Grow 后期望 bm[0] = true")
	}
	if !bm.Get(63) {
		t.Error("Grow 后期望 bm[63] = true")
	}
	if bm.Len() != 128 {
		t.Errorf("Grow 后期望 Len = 128，得到 %d", bm.Len())
	}
	// 新区域应为 false
	if bm.Get(64) {
		t.Error("Grow 后期望 bm[64] = false")
	}
}

// TestBitmapGrow_NoOp 测试 Grow 不扩展（新容量 <= 当前容量）。
func TestBitmapGrow_NoOp(t *testing.T) {
	bm := NewBitmap(128)
	bm.Set(50)

	bm.Grow(64) // 不应改变任何东西

	if !bm.Get(50) {
		t.Error("Grow(64) 后期望 bm[50] = true")
	}
	if bm.Len() != 128 {
		t.Errorf("Grow(64) 后期望 Len = 128，得到 %d", bm.Len())
	}
}

// TestBitmapGrow_SameSize 测试 Grow 到相同大小。
func TestBitmapGrow_SameSize(t *testing.T) {
	bm := NewBitmap(64)
	bm.Set(10)

	bm.Grow(64)

	if !bm.Get(10) {
		t.Error("Grow(64) 后期望 bm[10] = true")
	}
}

// TestBitmapCopyFrom_LargeRange 测试大范围 CopyFrom。
func TestBitmapCopyFrom_LargeRange(t *testing.T) {
	src := NewBitmap(256)
	for i := uint32(0); i < 256; i += 2 {
		src.Set(i)
	}

	dst := NewBitmap(128)
	dst.CopyFrom(src, 0, 128)

	for i := uint32(0); i < 128; i++ {
		want := i%2 == 0
		if dst.Get(i) != want {
			t.Errorf("dst[%d] = %v，期望 %v", i, dst.Get(i), want)
		}
	}
}
