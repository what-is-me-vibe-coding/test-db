package storage

import (
	"fmt"

	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
)

// registerSegmentIndexes 将 Segment 注册到所有索引（主键、布隆、稀疏），
// 并将列统计信息缓存到 IndexCache。
func (e *Engine) registerSegmentIndexes(seg *Segment, level int) error {
	segMeta := index.SegmentMeta{
		ID:     seg.ID,
		MinKey: seg.MinKey,
		MaxKey: seg.MaxKey,
		Level:  level,
	}
	if err := e.primaryIndex.RegisterSegment(segMeta); err != nil {
		return fmt.Errorf("engine: register primary index for segment %d: %w", seg.ID, err)
	}

	if len(seg.Footer.BloomFilter) > 0 {
		if err := e.bloomIndex.RegisterFromBytes(seg.ID, seg.Footer.BloomFilter); err != nil {
			return fmt.Errorf("engine: register bloom index for segment %d: %w", seg.ID, err)
		}
	}

	e.sparseIndex.LoadFromSegment(seg, seg.MinKey, seg.MaxKey, level)

	// 缓存列统计信息到 IndexCache
	if len(seg.Footer.ColumnStats) > 0 {
		stats := make([]ColumnStat, len(seg.Footer.ColumnStats))
		copy(stats, seg.Footer.ColumnStats)
		e.indexCache.PutColumnStats(seg.ID, stats)
	}

	return nil
}

// unregisterSegmentIndexes 从所有索引中注销 Segment，并清除相关缓存。
func (e *Engine) unregisterSegmentIndexes(segID uint64) {
	_ = e.primaryIndex.UnregisterSegment(segID)
	e.bloomIndex.Unregister(segID)
	e.sparseIndex.UnregisterSegment(segID)
	e.blockCache.Invalidate(segID)
	e.indexCache.Invalidate(segID)
}

// Segments 返回所有 Segment 的副本。
func (e *Engine) Segments() []*Segment {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*Segment, len(e.segments))
	copy(result, e.segments)
	return result
}

// SegmentCount 返回 Segment 的数量。
func (e *Engine) SegmentCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.segments)
}

// L0SegmentCount 返回 L0 层 Segment 的数量。
func (e *Engine) L0SegmentCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	count := 0
	for _, lvl := range e.segmentLevels {
		if lvl == 0 {
			count++
		}
	}
	return count
}

// MemTableSize 返回当前活跃 MemTable 的大小。
func (e *Engine) MemTableSize() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.activeMem.Size()
}

// PrimaryIndex 返回主键索引实例。
func (e *Engine) PrimaryIndex() *index.PrimaryIndex {
	return e.primaryIndex
}

// BloomIndex 返回布隆过滤器索引实例。
func (e *Engine) BloomIndex() *index.BloomIndex {
	return e.bloomIndex
}

// SparseIndex 返回稀疏索引实例。
func (e *Engine) SparseIndex() *index.SparseIndex {
	return e.sparseIndex
}

// ColumnMeta 返回列元数据的副本。
func (e *Engine) ColumnMeta() []ColumnMeta {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]ColumnMeta, len(e.columnMeta))
	copy(result, e.columnMeta)
	return result
}

func (e *Engine) rotateMemTable() error {
	if e.activeMem.Len() == 0 {
		return nil
	}
	e.activeMem.Freeze()
	e.immutable = append(e.immutable, e.activeMem)
	e.activeMem = NewMemTableWithSize(e.activeMem.maxSize)
	return nil
}

func (e *Engine) l0Count() int {
	count := 0
	for _, lvl := range e.segmentLevels {
		if lvl == 0 {
			count++
		}
	}
	return count
}

func (e *Engine) collectL0Segments() ([]*Segment, []int) {
	var segments []*Segment
	var indices []int
	for i, lvl := range e.segmentLevels {
		if lvl == 0 {
			segments = append(segments, e.segments[i])
			indices = append(indices, i)
		}
	}
	return segments, indices
}

func (e *Engine) collectL1Segments() ([]*Segment, []int) {
	var segments []*Segment
	var indices []int
	for i, lvl := range e.segmentLevels {
		if lvl == 1 {
			segments = append(segments, e.segments[i])
			indices = append(indices, i)
		}
	}
	return segments, indices
}
