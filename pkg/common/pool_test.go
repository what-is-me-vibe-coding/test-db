package common

import (
	"testing"
)

func TestBufferPool(t *testing.T) {
	pool := NewBufferPool()

	b1 := pool.Get()
	if cap(b1) == 0 {
		t.Error("Get() returned slice with zero capacity")
	}

	b1 = append(b1, []byte("hello")...)
	pool.Put(b1)

	b2 := pool.Get()
	if cap(b2) == 0 {
		t.Error("Second Get() returned slice with zero capacity")
	}

	b3 := pool.GetSize(1024)
	if cap(b3) < 1024 {
		t.Errorf("GetSize(1024) returned slice with capacity %d, want >= 1024", cap(b3))
	}
}

func TestBufferPoolGetSizeLargerThanDefault(t *testing.T) {
	pool := NewBufferPool()

	// 测试请求大于默认容量(4096)的缓冲区
	b := pool.GetSize(8192)
	if cap(b) < 8192 {
		t.Errorf("GetSize(8192) returned slice with capacity %d, want >= 8192", cap(b))
	}
	if len(b) != 0 {
		t.Errorf("GetSize should return slice with len 0, got %d", len(b))
	}
}

func TestDefaultBufferPool(t *testing.T) {
	pool := GetDefaultBufferPool()
	if pool == nil {
		t.Error("GetDefaultBufferPool() returned nil")
	}

	b := pool.Get()
	if cap(b) == 0 {
		t.Error("Default pool Get() returned slice with zero capacity")
	}
}
