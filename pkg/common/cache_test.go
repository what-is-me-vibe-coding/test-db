package common

import (
	"testing"
)

func TestLRUCacheBasicOperations(t *testing.T) {
	cache := NewLRUCache(1000)

	// Put and Get
	cache.Put("key1", "value1", 10)
	val, ok := cache.Get("key1")
	if !ok || val.(string) != "value1" {
		t.Fatalf("expected value1, got %v, ok=%v", val, ok)
	}

	// Miss
	_, ok = cache.Get("nonexistent")
	if ok {
		t.Fatal("expected miss for nonexistent key")
	}

	// Len
	if cache.Len() != 1 {
		t.Fatalf("expected len 1, got %d", cache.Len())
	}

	// Delete
	cache.Delete("key1")
	_, ok = cache.Get("key1")
	if ok {
		t.Fatal("expected miss after delete")
	}
	if cache.Len() != 0 {
		t.Fatalf("expected len 0 after delete, got %d", cache.Len())
	}
}

func TestLRUCacheEviction(t *testing.T) {
	cache := NewLRUCache(30) // 容量 30 字节

	cache.Put("a", "val-a", 10)
	cache.Put("b", "val-b", 10)
	cache.Put("c", "val-c", 10)

	// 此时总大小 30，刚好等于容量
	if cache.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", cache.Len())
	}

	// 再插入一个，应淘汰最旧的
	cache.Put("d", "val-d", 10)

	_, ok := cache.Get("a")
	if ok {
		t.Fatal("expected 'a' to be evicted")
	}

	_, ok = cache.Get("d")
	if !ok {
		t.Fatal("expected 'd' to exist")
	}
}

func TestLRUCacheUpdateExistingKey(t *testing.T) {
	cache := NewLRUCache(1000)

	cache.Put("key", "old", 10)
	cache.Put("key", "new", 20)

	val, ok := cache.Get("key")
	if !ok || val.(string) != "new" {
		t.Fatalf("expected new, got %v", val)
	}

	if cache.Len() != 1 {
		t.Fatalf("expected len 1, got %d", cache.Len())
	}
}

func TestLRUCacheStats(t *testing.T) {
	cache := NewLRUCache(1000)

	cache.Put("key1", "value1", 10)
	cache.Get("key1") // hit
	cache.Get("key1") // hit
	cache.Get("miss") // miss

	hit, miss := cache.Stats()
	if hit != 2 {
		t.Fatalf("expected 2 hits, got %d", hit)
	}
	if miss != 1 {
		t.Fatalf("expected 1 miss, got %d", miss)
	}

	rate := cache.HitRate()
	if rate < 0.65 || rate > 0.68 {
		t.Fatalf("expected hit rate ~0.667, got %f", rate)
	}
}

func TestLRUCacheClear(t *testing.T) {
	cache := NewLRUCache(1000)

	cache.Put("key1", "value1", 10)
	cache.Put("key2", "value2", 10)
	cache.Get("key1")

	cache.Clear()

	if cache.Len() != 0 {
		t.Fatalf("expected len 0 after clear, got %d", cache.Len())
	}

	hit, miss := cache.Stats()
	if hit != 0 || miss != 0 {
		t.Fatalf("expected stats reset after clear, got hit=%d miss=%d", hit, miss)
	}
}

func TestLRUCacheUnlimitedCapacity(t *testing.T) {
	cache := NewLRUCache(0) // 无容量限制

	for i := 0; i < 1000; i++ {
		cache.Put(string(rune(i)), i, 8)
	}

	if cache.Len() != 1000 {
		t.Fatalf("expected 1000 entries with unlimited capacity, got %d", cache.Len())
	}
}

func TestLRUCacheTotalSize(t *testing.T) {
	cache := NewLRUCache(1000)

	cache.Put("a", "v", 100)
	cache.Put("b", "v", 200)
	cache.Put("c", "v", 300)

	size := cache.TotalSize()
	if size != 600 {
		t.Fatalf("expected total size 600, got %d", size)
	}
}

func TestLRUCacheConcurrentAccess(t *testing.T) {
	cache := NewLRUCache(10000)
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := string(rune(id*100 + j))
				cache.Put(key, id, 8)
				cache.Get(key)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// 不验证具体数量，只验证不 panic 且最终状态一致
	if cache.Len() == 0 {
		t.Fatal("expected some entries after concurrent access")
	}
}
