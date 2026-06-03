package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestBlockCacheBasicOperations(t *testing.T) {
	cache := NewBlockCache(BlockCacheConfig{Capacity: 1024})

	block := &CachedBlock{
		Data:     []int64{1, 2, 3},
		Nulls:    nil,
		Type:     common.TypeInt64,
		RowCount: 3,
		Size:     24,
	}

	// Put and Get
	cache.Put(1, 0, block)
	got, ok := cache.Get(1, 0)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Type != common.TypeInt64 {
		t.Fatalf("expected TypeInt64, got %v", got.Type)
	}

	// Miss
	_, ok = cache.Get(999, 0)
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestBlockCacheEviction(t *testing.T) {
	cache := NewBlockCache(BlockCacheConfig{Capacity: 100})

	// 插入多个块，总大小超过容量
	for i := 0; i < 5; i++ {
		block := &CachedBlock{
			Data:     []int64{int64(i)},
			Type:     common.TypeInt64,
			RowCount: 1,
			Size:     30,
		}
		cache.Put(uint64(i), 0, block)
	}

	stats := cache.Stats()
	if stats.EntryCount > 4 {
		t.Fatalf("expected some eviction, got %d entries", stats.EntryCount)
	}
}

func TestBlockCacheInvalidateSegment(t *testing.T) {
	cache := NewBlockCache(BlockCacheConfig{Capacity: 10000})

	for i := 0; i < 3; i++ {
		block := &CachedBlock{
			Data:     []int64{int64(i)},
			Type:     common.TypeInt64,
			RowCount: 1,
			Size:     8,
		}
		cache.Put(1, uint32(i), block)
	}

	cache.InvalidateSegment(1, 3)

	for i := 0; i < 3; i++ {
		_, ok := cache.Get(1, uint32(i))
		if ok {
			t.Fatalf("expected cache miss after invalidation for col %d", i)
		}
	}
}

func TestBlockCacheInvalidateSingle(t *testing.T) {
	cache := NewBlockCache(BlockCacheConfig{Capacity: 10000})

	block := &CachedBlock{
		Data:     []int64{42},
		Type:     common.TypeInt64,
		RowCount: 1,
		Size:     8,
	}
	cache.Put(1, 0, block)
	cache.Put(1, 1, block)

	cache.Invalidate(1, 0)

	_, ok := cache.Get(1, 0)
	if ok {
		t.Fatal("expected miss after invalidate")
	}
	_, ok = cache.Get(1, 1)
	if !ok {
		t.Fatal("expected hit for non-invalidated entry")
	}
}

func TestBlockCacheStats(t *testing.T) {
	cache := NewBlockCache(BlockCacheConfig{Capacity: 10000})

	block := &CachedBlock{
		Data:     []int64{1},
		Type:     common.TypeInt64,
		RowCount: 1,
		Size:     8,
	}
	cache.Put(1, 0, block)
	cache.Get(1, 0) // hit
	cache.Get(1, 0) // hit
	cache.Get(2, 0) // miss

	stats := cache.Stats()
	if stats.HitCount != 2 {
		t.Fatalf("expected 2 hits, got %d", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.MissCount)
	}
	if stats.HitRate < 0.65 || stats.HitRate > 0.68 {
		t.Fatalf("expected hit rate ~0.667, got %f", stats.HitRate)
	}
}

func TestBlockCacheClear(t *testing.T) {
	cache := NewBlockCache(BlockCacheConfig{Capacity: 10000})

	block := &CachedBlock{
		Data:     []int64{1},
		Type:     common.TypeInt64,
		RowCount: 1,
		Size:     8,
	}
	cache.Put(1, 0, block)
	cache.Clear()

	stats := cache.Stats()
	if stats.EntryCount != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", stats.EntryCount)
	}
}

func TestBlockCachePutNil(t *testing.T) {
	cache := NewBlockCache(BlockCacheConfig{Capacity: 10000})
	cache.Put(1, 0, nil) // 不应 panic
	_, ok := cache.Get(1, 0)
	if ok {
		t.Fatal("expected miss for nil put")
	}
}

func TestEstimateBlockSize(t *testing.T) {
	tests := []struct {
		name     string
		data     interface{}
		typ      common.DataType
		rowCount uint32
	}{
		{"int64", []int64{1, 2, 3}, common.TypeInt64, 3},
		{"float64", []float64{1.0, 2.0}, common.TypeFloat64, 2},
		{"string", []string{"hello", "world"}, common.TypeString, 2},
		{"bool", []uint64{1, 0, 1}, common.TypeBool, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := estimateBlockSize(tt.data, nil, tt.typ, tt.rowCount)
			if size <= 0 {
				t.Fatalf("expected positive size, got %d", size)
			}
		})
	}
}
