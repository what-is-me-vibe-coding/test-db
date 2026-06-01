package storage

import (
	"math/rand"
	"sync"
	"unsafe"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

const skipListMaxLevel = 32
const skipListP = 0.25

type skipNode struct {
	key     string
	value   Row
	forward []*skipNode
}

// SkipList 是并发安全的跳表实现，支持插入、查找、删除与有序迭代。
type SkipList struct {
	head   *skipNode
	level  int
	length int
	mu     sync.RWMutex
}

func newSkipNode(key string, value Row, level int) *skipNode {
	return &skipNode{
		key:     key,
		value:   value,
		forward: make([]*skipNode, level),
	}
}

// NewSkipList 创建一个空的跳表。
func NewSkipList() *SkipList {
	head := newSkipNode("", Row{}, skipListMaxLevel)
	return &SkipList{
		head:  head,
		level: 1,
	}
}

func (sl *SkipList) randomLevel() int {
	level := 1
	for level < skipListMaxLevel && rand.Float64() < skipListP {
		level++
	}
	return level
}

// Put 插入或更新指定键的值。
func (sl *SkipList) Put(key string, value Row) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.putLocked(key, value)
}

func (sl *SkipList) putLocked(key string, value Row) {
	update := make([]*skipNode, skipListMaxLevel)
	current := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
		update[i] = current
	}

	current = current.forward[0]

	if current != nil && current.key == key {
		current.value = value
		return
	}

	level := sl.randomLevel()
	if level > sl.level {
		for i := sl.level; i < level; i++ {
			update[i] = sl.head
		}
		sl.level = level
	}

	node := newSkipNode(key, value, level)
	for i := 0; i < level; i++ {
		node.forward[i] = update[i].forward[i]
		update[i].forward[i] = node
	}
	sl.length++
}

// Get 查找指定键的值。
func (sl *SkipList) Get(key string) (Row, error) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	current := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
	}

	current = current.forward[0]
	if current != nil && current.key == key {
		return current.value, nil
	}
	return Row{}, common.ErrKeyNotFound
}

// Delete 从跳表中删除指定键。
func (sl *SkipList) Delete(key string) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	update := make([]*skipNode, skipListMaxLevel)
	current := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
		update[i] = current
	}

	current = current.forward[0]
	if current == nil || current.key != key {
		return common.ErrKeyNotFound
	}

	for i := 0; i < sl.level; i++ {
		if update[i].forward[i] != current {
			break
		}
		update[i].forward[i] = current.forward[i]
	}

	for sl.level > 1 && sl.head.forward[sl.level-1] == nil {
		sl.level--
	}
	sl.length--
	return nil
}

// Len 返回跳表中的元素数量。
func (sl *SkipList) Len() int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.length
}

// Iter 返回一个有序迭代器。
func (sl *SkipList) Iter() *SkipListIter {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	nodes := make([]*skipNode, 0, sl.length)
	current := sl.head.forward[0]
	for current != nil {
		nodes = append(nodes, current)
		current = current.forward[0]
	}
	return &SkipListIter{nodes: nodes, pos: -1}
}

// SkipListIter 是跳表的有序迭代器。
type SkipListIter struct {
	nodes []*skipNode
	pos   int
}

// Next 移动到下一个元素，如果存在则返回 true。
func (it *SkipListIter) Next() bool {
	it.pos++
	return it.pos < len(it.nodes)
}

// Key 返回当前元素的键。
func (it *SkipListIter) Key() string {
	return it.nodes[it.pos].key
}

// Value 返回当前元素的值。
func (it *SkipListIter) Value() Row {
	return it.nodes[it.pos].value
}

// RowSize 估算 Row 的内存占用。
func (r Row) RowSize() int64 {
	size := int64(unsafe.Sizeof(r))
	size += int64(len(r.Columns)) * 64
	for k, v := range r.Columns {
		size += int64(len(k))
		size += int64(len(v.Str))
	}
	return size
}
