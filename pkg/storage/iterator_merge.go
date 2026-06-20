package storage

import (
	"container/heap"
)

// mergeHeapEntry wraps an iterator for use in the merge heap.
type mergeHeapEntry struct {
	it    ScanIterator
	key   string
	index int
}

// mergeHeap implements heap.Interface for merging sorted iterators.
type mergeHeap []*mergeHeapEntry

func (h mergeHeap) Len() int { return len(h) }

func (h mergeHeap) Less(i, j int) bool {
	if h[i].key != h[j].key {
		return h[i].key < h[j].key
	}
	return h[i].index > h[j].index
}

func (h mergeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *mergeHeap) Push(x any) {
	*h = append(*h, x.(*mergeHeapEntry))
}

func (h *mergeHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// MergeIterator merges multiple sorted iterators into one, deduplicating by key
// with priority given to higher-index iterators (newer data wins).
type MergeIterator struct {
	heap     mergeHeap
	current  ScanEntry
	err      error
	started  bool
	finished bool
	iters    []ScanIterator
}

// NewMergeIterator creates a merge iterator from multiple sorted iterators.
// Iterators are ordered by priority: higher index = higher priority.
// When the same key appears in multiple iterators, the one with the highest
// index wins (i.e., the last iterator's value takes precedence).
func NewMergeIterator(iters ...ScanIterator) *MergeIterator {
	mi := &MergeIterator{
		iters: iters,
		heap:  make(mergeHeap, 0, len(iters)),
	}

	for i, it := range iters {
		if it.Next() {
			mi.heap = append(mi.heap, &mergeHeapEntry{
				it:    it,
				key:   it.Key(),
				index: i,
			})
		}
		if it.Err() != nil {
			mi.err = it.Err()
			return mi
		}
	}

	heap.Init(&mi.heap)
	return mi
}

// Next advances the iterator to the next unique key.
func (mi *MergeIterator) Next() bool {
	if mi.finished || mi.err != nil {
		return false
	}

	if !mi.started {
		return mi.advanceFirst()
	}

	return mi.advanceNext()
}

func (mi *MergeIterator) advanceFirst() bool {
	if len(mi.heap) == 0 {
		mi.finished = true
		return false
	}

	mi.started = true
	entry := mi.heap[0]
	mi.current = ScanEntry{Key: entry.key}

	it := entry.it
	mi.current.Value = it.Entry().Value

	mi.advanceHeapTop()
	return true
}

func (mi *MergeIterator) advanceNext() bool {
	if len(mi.heap) == 0 {
		mi.finished = true
		return false
	}

	prevKey := mi.current.Key

	for len(mi.heap) > 0 && mi.heap[0].key == prevKey {
		mi.advanceHeapTop()
	}

	if len(mi.heap) == 0 {
		mi.finished = true
		return false
	}

	entry := mi.heap[0]
	mi.current = ScanEntry{Key: entry.key}

	it := entry.it
	mi.current.Value = it.Entry().Value

	mi.advanceHeapTop()
	return true
}

func (mi *MergeIterator) advanceHeapTop() {
	top := mi.heap[0]
	it := top.it

	if it.Next() {
		top.key = it.Key()
		heap.Fix(&mi.heap, 0)
	} else {
		heap.Pop(&mi.heap)
		if it.Err() != nil && mi.err == nil {
			mi.err = it.Err()
		}
	}
}

// Entry returns the current scan entry.
func (mi *MergeIterator) Entry() ScanEntry {
	if !mi.started {
		return ScanEntry{}
	}
	return ScanEntry{Key: mi.current.Key, Value: mi.current.Value}
}

// Err returns any error encountered during iteration.
func (mi *MergeIterator) Err() error { return mi.err }

// Close closes all underlying iterators.
func (mi *MergeIterator) Close() {
	for _, it := range mi.iters {
		it.Close()
	}
}
