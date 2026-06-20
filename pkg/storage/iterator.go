package storage

import "fmt"

// ScanEntry represents a key-value pair from a scan operation.
type ScanEntry struct {
	Key   string
	Value Row
}

// ScanIterator is the interface for iterating over scan results in key order.
// 延迟物化优化：Key() 方法仅返回当前行的 key，不触发列数据物化；
// Entry() 方法返回完整的行数据（含列值 map），会触发物化。
// 调用方在仅需 key 时应优先使用 Key()，避免不必要的 map 分配。
type ScanIterator interface {
	Next() bool
	Key() string
	Entry() ScanEntry
	Err() error
	Close()
}

// sizedIterator 是可选接口，由能廉价提供精确结果行数的迭代器实现。
// 用于扫描结果切片的精准预分配：MemTable 迭代器在构造时已物化范围内全部数据，
// 可给出精确计数；Segment 迭代器不实现此接口（精确计数需扫描，得不偿失）。
type sizedIterator interface {
	Count() int
}

// sumIterCounts 汇总实现了 sizedIterator 的迭代器的精确行数。
// 未实现该接口的迭代器（如 Segment 迭代器）贡献 0，由调用方回退到估算值补充。
func sumIterCounts(iters []ScanIterator) int {
	total := 0
	for _, it := range iters {
		if si, ok := it.(sizedIterator); ok {
			total += si.Count()
		}
	}
	return total
}

// buildScanIterators creates iterators for all data sources in priority order.
// Order: segments (lowest priority) → immutable memtables → active memtable (highest).
func (e *Engine) buildScanIterators(start, end string) []ScanIterator {
	// 预分配迭代器切片容量
	capacity := len(e.segments) + len(e.immutable) + 1
	iters := make([]ScanIterator, 0, capacity)

	for _, seg := range e.segments {
		if seg.MinKey > end || seg.MaxKey < start {
			continue
		}
		iters = append(iters, newSegmentIterator(seg, e.columnMeta, start, end, e.blockCache))
	}

	for i := 0; i < len(e.immutable); i++ {
		iters = append(iters, newMemTableIterator(e.immutable[i], start, end))
	}

	iters = append(iters, newMemTableIterator(e.activeMem, start, end))

	return iters
}

// ScanRange performs a range scan using the MergeIterator for sorted,
// deduplicated results across all data sources.
// Caller must hold e.mu.RLock.
func (e *Engine) ScanRange(start, end string) []ScanEntry {
	entries, _ := e.scanRangeUnlocked(start, end)
	return entries
}

// scanRangeUnlocked performs the actual scan without acquiring the lock.
// Caller must hold e.mu.RLock.
// Returns scan results and any error encountered during iteration.
func (e *Engine) scanRangeUnlocked(start, end string) ([]ScanEntry, error) {
	iters := e.buildScanIterators(start, end)
	if len(iters) == 0 {
		return nil, nil
	}

	// 优先使用 MemTable 迭代器的精确行数预分配（选择性范围扫描收益最大：
	// 旧实现用全量 Len 估算，100 行命中会按 10000 行预分配，浪费 ~400KB）。
	// 无 MemTable 数据时回退到估算值（含 Segment 行数，已封顶防溢出）。
	estimatedSize := sumIterCounts(iters)
	if estimatedSize == 0 {
		estimatedSize = capScanPrealloc(e.estimateScanSize(start, end))
	}

	mi := NewMergeIterator(iters...)
	defer mi.Close()

	results := make([]ScanEntry, 0, estimatedSize)
	for mi.Next() {
		entry := mi.Entry()
		if isTombstone(entry.Value) {
			continue // 跳过墓碑（已删除的行）
		}
		results = append(results, entry)
	}

	if err := mi.Err(); err != nil {
		return nil, fmt.Errorf("scan range [%q,%q]: %w", start, end, err)
	}

	return results, nil
}
