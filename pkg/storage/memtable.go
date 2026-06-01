package storage

import (
	"fmt"
	"sync"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

const defaultMemTableMaxSize = 32 << 20 // 32MB

// Row 表示 MemTable 中的一行数据。
type Row struct {
	Version uint64
	Columns map[string]common.Value
}

// MemTable 是并发安全的内存表实现。
// 基于跳表按主键排序，支持插入、点查、快照读取。
type MemTable struct {
	tree    *SkipList
	maxSize int64
	curSize int64
	mu      sync.RWMutex
	imm     bool // 标记为不可变（冻结状态，等待刷盘）
}

// NewMemTable 创建一个新的 MemTable。
func NewMemTable(maxSize int64) *MemTable {
	if maxSize <= 0 {
		maxSize = defaultMemTableMaxSize
	}
	return &MemTable{
		tree:    NewSkipList(),
		maxSize: maxSize,
	}
}

// Put 插入或更新一行数据。
func (m *MemTable) Put(key string, row Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.imm {
		return common.ErrReadOnly
	}

	old, err := m.tree.Get(key)
	oldSize := int64(0)
	if err == nil {
		oldSize = old.RowSize()
	}

	newSize := row.RowSize()
	m.tree.Put(key, row)
	m.curSize += newSize - oldSize
	return nil
}

// Get 按主键查找一行数据。
func (m *MemTable) Get(key string) (Row, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.tree.Get(key)
}

// Delete 删除一行数据。
func (m *MemTable) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.imm {
		return common.ErrReadOnly
	}

	old, err := m.tree.Get(key)
	if err != nil {
		return err
	}
	if err := m.tree.Delete(key); err != nil {
		return err
	}
	m.curSize -= old.RowSize()
	return nil
}

// Size 返回当前 MemTable 的估算内存占用。
func (m *MemTable) Size() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.curSize
}

// Len 返回当前 MemTable 中的行数。
func (m *MemTable) Len() int {
	return m.tree.Len()
}

// NeedFlush 判断 MemTable 是否已达到刷盘阈值。
func (m *MemTable) NeedFlush() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.curSize >= m.maxSize
}

// Freeze 冻结当前 MemTable 为只读状态，准备刷盘。
func (m *MemTable) Freeze() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.imm = true
}

// IsFrozen 判断 MemTable 是否已冻结。
func (m *MemTable) IsFrozen() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.imm
}

// Scan 对 [start, end] 范围内的所有行执行 fn 回调。
// 遍历在持有读锁期间完成，回调函数不应执行耗时操作。
func (m *MemTable) Scan(start, end string, fn func(key string, row Row) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	iter := m.tree.Iter()
	for iter.Next() {
		key := iter.Key()
		if key < start {
			continue
		}
		if key > end {
			break
		}
		if err := fn(key, iter.Value()); err != nil {
			return fmt.Errorf("memtable scan: %w", err)
		}
	}
	return nil
}

// Snapshot 返回当前 MemTable 中所有行的快照。
func (m *MemTable) Snapshot() []RowEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]RowEntry, 0, m.tree.Len())
	iter := m.tree.Iter()
	for iter.Next() {
		entries = append(entries, RowEntry{
			Key: iter.Key(),
			Row: iter.Value(),
		})
	}
	return entries
}

// RowEntry 表示一次快照中的条目。
type RowEntry struct {
	Key string
	Row Row
}
