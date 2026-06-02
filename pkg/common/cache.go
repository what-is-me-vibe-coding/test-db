package common

import (
	"container/list"
	"sync"
)

// LRUCache 是并发安全的 LRU 缓存，支持容量限制和命中率统计。
type LRUCache struct {
	mu        sync.RWMutex
	capacity  int
	items     map[string]*list.Element
	order     *list.List
	hitCount  uint64
	missCount uint64
}

// lruEntry 是 LRU 缓存中的条目。
type lruEntry struct {
	key   string
	value interface{}
	size  int
}

// NewLRUCache 创建一个指定容量的 LRU 缓存。
// capacity 为 0 表示无容量限制。
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get 从缓存中获取值。如果命中，返回值和 true；否则返回 nil 和 false。
func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		c.hitCount++
		return elem.Value.(*lruEntry).value, true
	}
	c.missCount++
	return nil, false
}

// Put 向缓存中放入键值对。如果缓存已满，淘汰最久未使用的条目。
func (c *LRUCache) Put(key string, value interface{}, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*lruEntry)
		entry.value = value
		entry.size = size
		c.order.MoveToFront(elem)
		return
	}

	entry := &lruEntry{key: key, value: value, size: size}
	elem := c.order.PushFront(entry)
	c.items[key] = elem

	for c.capacity > 0 && c.totalSizeLocked() > c.capacity {
		c.evictOldestLocked()
	}
}

// Delete 从缓存中删除指定键。
func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeElementLocked(elem)
	}
}

// Len 返回缓存中的条目数。
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Stats 返回缓存命中和未命中次数。
func (c *LRUCache) Stats() (hit, miss uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hitCount, c.missCount
}

// HitRate 返回缓存命中率（0.0 ~ 1.0）。
func (c *LRUCache) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := c.hitCount + c.missCount
	if total == 0 {
		return 0
	}
	return float64(c.hitCount) / float64(total)
}

// Clear 清空缓存。
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.order.Init()
	c.hitCount = 0
	c.missCount = 0
}

// TotalSize 返回缓存中所有条目的总大小。
func (c *LRUCache) TotalSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalSizeLocked()
}

func (c *LRUCache) totalSizeLocked() int {
	total := 0
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		total += elem.Value.(*lruEntry).size
	}
	return total
}

func (c *LRUCache) evictOldestLocked() {
	elem := c.order.Back()
	if elem != nil {
		c.removeElementLocked(elem)
	}
}

func (c *LRUCache) removeElementLocked(elem *list.Element) {
	c.order.Remove(elem)
	entry := elem.Value.(*lruEntry)
	delete(c.items, entry.key)
}
