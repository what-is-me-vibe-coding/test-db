package storage

import (
	"fmt"
	"sync"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// segmentIterator iterates over a Segment's rows within a key range.
// 延迟物化优化：Next() 仅记录行索引和 key，不构建 map[string]Value，
// Entry() 时按需构建行数据。同时复用 map 缓冲区，避免每行重新分配。
// Column decoding is deferred until the first row is accessed, avoiding
// unnecessary work for segments that are skipped by index pruning.
// Thread safety: ensureDecoded uses sync.Once for idempotent lazy init;
// all other methods are NOT safe for concurrent use — callers (e.g. MergeIterator)
// must ensure serial access.
type segmentIterator struct {
	seg         *Segment
	colMeta     []ColumnMeta
	start       string
	end         string
	rowIdx      int
	currentKey  string
	err         error
	started     bool
	finished    bool
	decodedCols []decodedColumn
	decodeOnce  sync.Once
	blockCache  *BlockCache
}

// newSegmentIterator creates an iterator over a Segment for the given range.
// Column decoding is deferred until the first row is accessed, avoiding
// unnecessary work for segments that are skipped by index pruning.
func newSegmentIterator(seg *Segment, colMeta []ColumnMeta, start, end string, blockCache *BlockCache) *segmentIterator {
	return &segmentIterator{
		seg:        seg,
		colMeta:    colMeta,
		start:      start,
		end:        end,
		rowIdx:     -1,
		blockCache: blockCache,
	}
}

// ensureDecoded lazily decodes all columns on first access.
// Uses sync.Once to guarantee thread-safe, idempotent initialization.
// On decode failure, decodedCols is set to an empty (non-nil) slice and err is recorded.
// 优先从 BlockCache 获取已解码的列数据，未命中时解码并写入缓存。
// decodeSegmentColumn 从 Segment 中解码单列数据，优先从 BlockCache 获取。
// 使用共享的 prepareEncodedColumn 和 decodeColumnFromEncoded 减少重复代码。
func (it *segmentIterator) decodeSegmentColumn(i int, decodedCols []decodedColumn) (bool, int) {
	cacheKey := CacheKey{SegmentID: it.seg.ID, ColumnIdx: uint32(i)}
	if dc, ok := it.blockCache.get(cacheKey); ok {
		decodedCols[i] = dc
		return true, 1
	}

	dc, err := decodeColumnFromEncoded(&it.seg.Columns[i], i)
	if err != nil {
		it.err = fmt.Errorf("segment: %w", err)
		it.decodedCols = make([]decodedColumn, 0)
		return false, 0
	}
	decodedCols[i] = dc
	it.blockCache.put(cacheKey, dc)
	return true, 0
}

func (it *segmentIterator) ensureDecoded() {
	it.decodeOnce.Do(func() {
		decodedCols := make([]decodedColumn, len(it.seg.Columns))
		cacheHitCount := 0

		for i := range it.seg.Columns {
			ok, hits := it.decodeSegmentColumn(i, decodedCols)
			if !ok {
				return
			}
			cacheHitCount += hits
		}

		it.decodedCols = decodedCols
	})
}

func (it *segmentIterator) Next() bool {
	if it.finished || it.err != nil {
		return false
	}

	it.ensureDecoded()
	if it.err != nil {
		return false
	}

	for {
		it.rowIdx++
		if it.rowIdx >= len(it.seg.Keys) {
			it.finished = true
			return false
		}

		key := it.seg.Keys[it.rowIdx]
		if key < it.start {
			continue
		}
		if key > it.end {
			it.finished = true
			return false
		}

		// 延迟物化：仅记录 key 和行索引，不构建 map
		it.currentKey = key
		it.started = true
		return true
	}
}

// buildRowMap 从解码后的列数据构建当前行的列值映射。
// 每次调用创建新 map，确保返回值可安全持有跨行引用。
func (it *segmentIterator) buildRowMap() map[string]common.Value {
	values := make(map[string]common.Value, len(it.colMeta))
	for colIdx, col := range it.colMeta {
		val := it.seg.getColumnValueFromDecoded(it.decodedCols, uint32(colIdx), uint32(it.rowIdx))
		values[col.Name] = val
	}
	return values
}

func (it *segmentIterator) Entry() ScanEntry {
	if !it.started {
		return ScanEntry{}
	}
	// 延迟物化：仅在 Entry() 被调用时构建行数据
	// 注意：返回的 map 是 rowBuf 的引用，调用方不应持有跨行引用
	rowMap := it.buildRowMap()
	return ScanEntry{Key: it.currentKey, Value: Row{Version: it.seg.ID, Columns: rowMap}}
}

func (it *segmentIterator) Err() error { return it.err }
func (it *segmentIterator) Close()     { it.finished = true }

// Key 返回当前行的主键，不触发列数据物化。
func (it *segmentIterator) Key() string {
	if !it.started {
		return ""
	}
	return it.currentKey
}
