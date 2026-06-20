package storage

// memTableIterator iterates over a MemTable's rows within a key range.
// 直接引用 mem.Scan() 返回的切片，避免双重拷贝。
type memTableIterator struct {
	pairs []struct {
		Key   string
		Value Row
	}
	pos int
	err error
}

// newMemTableIterator creates an iterator over a MemTable for the given range.
// 直接引用 mem.Scan() 返回的切片，消除 ScanEntry 中间转换的拷贝开销。
func newMemTableIterator(mem *MemTable, start, end string) *memTableIterator {
	return &memTableIterator{pairs: mem.Scan(start, end), pos: -1}
}

func (it *memTableIterator) Next() bool {
	it.pos++
	return it.pos < len(it.pairs)
}

func (it *memTableIterator) Key() string {
	if it.pos < 0 || it.pos >= len(it.pairs) {
		return ""
	}
	return it.pairs[it.pos].Key
}

func (it *memTableIterator) Entry() ScanEntry {
	if it.pos < 0 || it.pos >= len(it.pairs) {
		return ScanEntry{}
	}
	p := &it.pairs[it.pos]
	return ScanEntry{Key: p.Key, Value: p.Value}
}

func (it *memTableIterator) Err() error { return it.err }
func (it *memTableIterator) Close()     { it.pos = -1 }

// Count 返回该迭代器待遍历的精确行数。
// memTableIterator 在构造时已通过 mem.Scan 物化了范围内全部键值对，
// 因此可提供精确计数，用于扫描结果切片的精准预分配，避免严重高估。
func (it *memTableIterator) Count() int { return len(it.pairs) }
