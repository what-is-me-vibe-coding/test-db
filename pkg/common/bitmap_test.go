package common

import (
	"testing"
)

func TestBitmapBasic(t *testing.T) {
	bm := NewBitmap(100)

	if bm.Len() != 100 {
		t.Errorf("Len() = %d, want 100", bm.Len())
	}

	bm.Set(10)
	bm.Set(50)
	bm.Set(99)

	if !bm.Get(10) {
		t.Error("Get(10) = false, want true")
	}
	if !bm.Get(50) {
		t.Error("Get(50) = false, want true")
	}
	if !bm.Get(99) {
		t.Error("Get(99) = false, want true")
	}
	if bm.Get(11) {
		t.Error("Get(11) = true, want false")
	}

	if bm.Count() != 3 {
		t.Errorf("Count() = %d, want 3", bm.Count())
	}

	bm.Clear(50)
	if bm.Get(50) {
		t.Error("Get(50) after Clear = true, want false")
	}
	if bm.Count() != 2 {
		t.Errorf("Count() after Clear = %d, want 2", bm.Count())
	}
}

func TestBitmapEdgeCases(t *testing.T) {
	bm := NewBitmap(0)
	if !bm.IsEmpty() {
		t.Error("Empty bitmap should be empty")
	}

	bm2 := NewBitmap(1)
	bm2.Set(0)
	if bm2.Count() != 1 {
		t.Errorf("Count() = %d, want 1", bm2.Count())
	}

	bm3 := NewBitmap(64)
	bm3.Set(63)
	if !bm3.Get(63) {
		t.Error("Get(63) = false, want true")
	}
}

func TestBitmapOperations(t *testing.T) {
	bm1 := NewBitmap(10)
	bm1.Set(1)
	bm1.Set(3)

	bm2 := NewBitmap(10)
	bm2.Set(3)
	bm2.Set(5)

	bmAnd := bm1.Clone()
	bmAnd.And(bm2)
	if bmAnd.Count() != 1 || !bmAnd.Get(3) {
		t.Error("And operation failed")
	}

	bmOr := bm1.Clone()
	bmOr.Or(bm2)
	if bmOr.Count() != 3 {
		t.Errorf("Or Count() = %d, want 3", bmOr.Count())
	}

	bmXor := bm1.Clone()
	bmXor.Xor(bm2)
	if bmXor.Count() != 2 {
		t.Errorf("Xor Count() = %d, want 2", bmXor.Count())
	}
}

func TestBitmapBytes(t *testing.T) {
	bm := NewBitmap(10)
	bm.Set(0)
	bm.Set(1)
	bm.Set(7)

	bytes := bm.ToBytes()
	if len(bytes) != 2 {
		t.Errorf("ToBytes() len = %d, want 2", len(bytes))
	}

	bm2 := NewBitmapFromBytes(bytes)
	if !bm2.Get(0) || !bm2.Get(1) || !bm2.Get(7) {
		t.Error("NewBitmapFromBytes failed")
	}
}

func TestBitmapForEach(t *testing.T) {
	bm := NewBitmap(10)
	bm.Set(2)
	bm.Set(5)
	bm.Set(7)

	count := 0
	bm.ForEach(func(_ uint32) {
		count++
	})
	if count != 3 {
		t.Errorf("ForEach count = %d, want 3", count)
	}
}

func TestBitmapFlip(t *testing.T) {
	bm := NewBitmap(10)
	bm.Set(3)
	if !bm.Get(3) {
		t.Error("Get(3) should be true after Set")
	}
	bm.Flip(3)
	if bm.Get(3) {
		t.Error("Get(3) should be false after Flip")
	}
	bm.Flip(3)
	if !bm.Get(3) {
		t.Error("Get(3) should be true after second Flip")
	}
}

func TestBitmapNot(t *testing.T) {
	bm := NewBitmap(10)
	bm.Set(1)
	bm.Set(3)
	bm.Set(5)

	bm.Not()
	if bm.Get(1) || bm.Get(3) || bm.Get(5) {
		t.Error("Not() should clear set bits")
	}
	if !bm.Get(0) || !bm.Get(2) || !bm.Get(4) {
		t.Error("Not() should set unset bits")
	}
}

func TestBitmapEquals(t *testing.T) {
	bm1 := NewBitmap(10)
	bm1.Set(1)
	bm1.Set(3)

	bm2 := NewBitmap(10)
	bm2.Set(1)
	bm2.Set(3)

	if !bm1.Equals(bm2) {
		t.Error("Equals should return true for identical bitmaps")
	}

	bm2.Set(5)
	if bm1.Equals(bm2) {
		t.Error("Equals should return false for different bitmaps")
	}
}

func TestBitmapOutOfBounds(_ *testing.T) {
	bm := NewBitmap(10)
	bm.Set(100) // out of bounds, should not panic
	bm.Clear(100)
	bm.Get(100)
	bm.Flip(100)
}

func TestBitmapAndWithDifferentSizes(t *testing.T) {
	bm1 := NewBitmap(10)
	bm1.Set(1)
	bm1.Set(5)

	bm2 := NewBitmap(5)
	bm2.Set(1)
	bm2.Set(3)

	bm1.And(bm2)
	if !bm1.Get(1) {
		t.Error("And should keep common bit 1")
	}
	if bm1.Get(5) {
		t.Error("And should clear bit 5 (out of range of bm2)")
	}
}

func TestBitmapOrWithDifferentSizes(t *testing.T) {
	bm1 := NewBitmap(5)
	bm1.Set(1)

	bm2 := NewBitmap(10)
	bm2.Set(3)
	bm2.Set(7)

	bm1.Or(bm2)
	if !bm1.Get(1) {
		t.Error("Or should keep bit 1 from bm1")
	}
	if !bm1.Get(3) {
		t.Error("Or should set bit 3 from bm2")
	}
	if !bm1.Get(7) {
		t.Error("Or should set bit 7 from bm2")
	}
}

func TestBitmapXorWithDifferentSizes(t *testing.T) {
	bm1 := NewBitmap(5)
	bm1.Set(1)

	bm2 := NewBitmap(10)
	bm2.Set(1)
	bm2.Set(3)

	bm1.Xor(bm2)
	if bm1.Get(1) {
		t.Error("Xor should clear bit 1 (common)")
	}
	if !bm1.Get(3) {
		t.Error("Xor should set bit 3 (only in bm2)")
	}
}

func TestBitmapClone(t *testing.T) {
	bm1 := NewBitmap(10)
	bm1.Set(1)
	bm1.Set(5)

	bm2 := bm1.Clone()
	if !bm2.Get(1) || !bm2.Get(5) {
		t.Error("Clone should copy all set bits")
	}

	bm2.Set(3)
	if bm1.Get(3) {
		t.Error("Modifying clone should not affect original")
	}
}

func TestBitmapEmptyBytes(t *testing.T) {
	bm := NewBitmap(0)
	bytes := bm.ToBytes()
	if bytes != nil {
		t.Errorf("ToBytes() on empty bitmap should return nil, got %v", bytes)
	}

	bm2 := NewBitmapFromBytes(nil)
	if bm2.Len() != 0 {
		t.Error("NewBitmapFromBytes(nil) should create empty bitmap")
	}
}

func TestBitmapIsEmpty(t *testing.T) {
	bm := NewBitmap(10)
	if !bm.IsEmpty() {
		t.Error("New bitmap should be empty")
	}
	bm.Set(5)
	if bm.IsEmpty() {
		t.Error("Bitmap with set bit should not be empty")
	}
	bm.Clear(5)
	if !bm.IsEmpty() {
		t.Error("Bitmap after clearing all bits should be empty")
	}
}

func TestBitmapEqualsDifferentLen(t *testing.T) {
	bm1 := NewBitmap(5)
	bm1.Set(1)

	bm2 := NewBitmap(10)
	bm2.Set(1)

	if bm1.Equals(bm2) {
		t.Error("Equals should return false for different lengths")
	}
}

func TestBitmapForEachEmpty(t *testing.T) {
	bm := NewBitmap(10)
	count := 0
	bm.ForEach(func(_ uint32) {
		count++
	})
	if count != 0 {
		t.Errorf("ForEach on empty bitmap should not call fn, got %d calls", count)
	}
}
