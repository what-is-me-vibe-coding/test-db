package storage

import (
	"container/heap"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

const (
	defaultL0CompactionThreshold = 4
	defaultLevelSizeMultiplier   = 2
)

// Compactor 负责将多个 Segment 合并为更少的 Segment。
type Compactor struct {
	mu      sync.Mutex
	dataDir string
	idGen   *segmentIDGen
}

// NewCompactor 创建一个 Compactor 实例。
func NewCompactor(dataDir string, idGen *segmentIDGen) *Compactor {
	return &Compactor{dataDir: dataDir, idGen: idGen}
}

// Compact 将输入的 segments 合并为一个新的 Segment。
func (c *Compactor) Compact(segments []*Segment, cols []ColumnMeta) (*Segment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(segments) == 0 {
		return nil, fmt.Errorf("compactor: no segments to compact")
	}

	rows, err := c.mergeSegments(segments, cols)
	if err != nil {
		return nil, fmt.Errorf("compactor: merge segments: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("compactor: merged result is empty")
	}

	seg, err := c.buildSegment(rows, cols)
	if err != nil {
		return nil, fmt.Errorf("compactor: build segment: %w", err)
	}

	return seg, nil
}

// CompactToLevel 将 L0 的 segments 合并到 L1，或将 Ln 合并到 Ln+1。
func (c *Compactor) CompactToLevel(segments []*Segment, _ int, cols []ColumnMeta) (*Segment, error) {
	seg, err := c.Compact(segments, cols)
	if err != nil {
		return nil, err
	}
	return seg, nil
}

// segReader 跟踪单个 Segment 在 k-way merge 中的读取位置。
// 流式归并优化：不再预物化所有行为 []memRow，而是持有解码后的列数据，
// 按需从列数据中提取当前行的值，避免同时持有所有行的 memRow 对象。
// 峰值内存从 O(总行数 × 列数) 降至 O(段数 + 输出行数 × 列数)。
type segReader struct {
	seg         *Segment
	decodedCols []decodedColumn
	pos         int
	rowCount    int
	segIdx      int // 在 sortedSegs 中的索引，用于去重优先级
}

// currentKey 返回当前行的主键。
func (r *segReader) currentKey() string {
	if r.pos < len(r.seg.Keys) {
		return r.seg.Keys[r.pos]
	}
	return fmt.Sprintf("row_%d_%d", r.seg.ID, r.pos)
}

// currentRow 从解码后的列数据中按需提取当前行的值，构建 memRow。
// 每次调用分配一个新的 []common.Value 切片，确保返回值可安全持有。
func (r *segReader) currentRow() memRow {
	numCols := len(r.decodedCols)
	values := make([]common.Value, numCols)
	for i := range r.decodedCols {
		values[i] = extractValue(r.decodedCols[i], uint32(r.pos))
	}
	return memRow{
		Key:    r.currentKey(),
		Values: values,
	}
}

// advance 推进读取位置到下一行，返回是否还有更多行。
func (r *segReader) advance() bool {
	r.pos++
	return r.pos < r.rowCount
}

// compactionEntry 是 k-way merge 堆中的条目。
type compactionEntry struct {
	key    string
	segIdx int
	reader *segReader
}

// compactionHeap 实现堆接口，按 key 升序，key 相同时 segIdx 降序（最新优先）。
type compactionHeap []*compactionEntry

func (h compactionHeap) Len() int { return len(h) }
func (h compactionHeap) Less(i, j int) bool {
	if h[i].key != h[j].key {
		return h[i].key < h[j].key
	}
	// key 相同时，segIdx 大的排在堆顶（优先处理）
	return h[i].segIdx > h[j].segIdx
}
func (h compactionHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *compactionHeap) Push(x any)   { *h = append(*h, x.(*compactionEntry)) }
func (h *compactionHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// sortSegsByID 按 Segment ID 升序排序（ID 越小越旧）。
func sortSegsByID(segs []*Segment) {
	sort.Slice(segs, func(i, j int) bool { return segs[i].ID < segs[j].ID })
}

func (c *Compactor) mergeSegments(segments []*Segment, _ []ColumnMeta) ([]memRow, error) {
	// 使用 k-way merge 替代全量排序：各 Segment 内行已按 key 有序，
	// 通过最小堆归并，复杂度 O(n log k) 优于 O(n log n) 的全量排序。
	// 同时在归并过程中去重，同一 key 保留最高 segment ID（最新版本）。
	//
	// 流式归并优化：每个 segReader 仅持有解码后的列数据，按需提取当前行值，
	// 不再预物化所有行为 []memRow，避免同时持有所有输入行的内存开销。

	// 先按 Segment ID 排序，确保 ID 更大的 segment 在堆中优先级更高
	sortedSegs := make([]*Segment, len(segments))
	copy(sortedSegs, segments)
	sortSegsByID(sortedSegs)

	readers := make([]*segReader, 0, len(sortedSegs))
	estimatedRows := 0
	for i, seg := range sortedSegs {
		reader, err := c.newSegmentReader(seg, i)
		if err != nil {
			return nil, fmt.Errorf("compactor: read segment %d: %w", seg.ID, err)
		}
		if reader.rowCount > 0 {
			readers = append(readers, reader)
			estimatedRows += reader.rowCount
		}
	}

	if len(readers) == 0 {
		return nil, nil
	}

	// 最小堆：按 key 排序，key 相同时 segIdx 大的优先（最新数据）
	h := &compactionHeap{}
	heap.Init(h)
	for _, r := range readers {
		heap.Push(h, &compactionEntry{
			key:    r.currentKey(),
			segIdx: r.segIdx,
			reader: r,
		})
	}

	deduped := make([]memRow, 0, estimatedRows)
	var prevKey string

	for h.Len() > 0 {
		entry := (*h)[0]
		key := entry.key
		// 按需从列数据提取当前行，避免预物化所有行
		row := entry.reader.currentRow()

		// 推进该 reader 的位置
		if entry.reader.advance() {
			entry.key = entry.reader.currentKey()
			heap.Fix(h, 0)
		} else {
			heap.Pop(h)
		}

		// 去重：同一 key 只保留第一个遇到的（segIdx 最大，即最新版本）
		if key == prevKey {
			// 跳过旧版本
			continue
		}
		deduped = append(deduped, row)
		prevKey = key
	}

	return deduped, nil
}

// newSegmentReader 创建一个流式段读取器，解码所有列数据但不预物化行。
// 列数据按列式存储，必须一次性解码；行值通过 currentRow() 按需提取，
// 避免同时持有所有行的 []common.Value 切片，显著降低 Compaction 峰值内存。
func (c *Compactor) newSegmentReader(seg *Segment, segIdx int) (*segReader, error) {
	if seg.RowCount == 0 {
		return &segReader{seg: seg, rowCount: 0, segIdx: segIdx}, nil
	}

	numCols := len(seg.Columns)
	decodedCols := make([]decodedColumn, numCols)
	for i := range seg.Columns {
		cd, err := decodeSegmentColumn(&seg.Columns[i], i)
		if err != nil {
			return nil, err
		}
		decodedCols[i] = cd
	}

	return &segReader{
		seg:         seg,
		decodedCols: decodedCols,
		pos:         0,
		rowCount:    int(seg.RowCount),
		segIdx:      segIdx,
	}, nil
}

// decodeSegmentColumn 解码单个 Segment 列用于 Compaction。
// 使用共享的 decodeColumnFromEncoded 函数，避免重复的列解码逻辑。
func decodeSegmentColumn(src *EncodedColumn, colIdx int) (decodedColumn, error) {
	dc, err := decodeColumnFromEncoded(src, colIdx)
	if err != nil {
		return decodedColumn{}, fmt.Errorf("compactor: %w", err)
	}
	return dc, nil
}

func (c *Compactor) buildSegment(rows []memRow, cols []ColumnMeta) (*Segment, error) {
	rowCount := uint32(len(rows))
	minKey := rows[0].Key
	maxKey := rows[len(rows)-1].Key

	segID := c.idGen.Next()
	builder := NewSegmentBuilder(segID, minKey, maxKey)

	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.Key
	}
	builder.SetKeys(keys)

	for colIdx, colMeta := range cols {
		enc, err := buildColumnEncoded(rows, colIdx, colMeta, rowCount)
		if err != nil {
			return nil, err
		}
		builder.AddEncodedColumn(enc)
	}

	seg, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("compactor: build segment: %w", err)
	}

	fileName, err := writeSegmentFile(c.dataDir, seg)
	if err != nil {
		return nil, fmt.Errorf("compactor: %w", err)
	}

	seg.FilePath = fileName
	return seg, nil
}

// buildColumnEncoded 将行数据中指定列编码为 EncodedColumn。
func buildColumnEncoded(rows []memRow, colIdx int, colMeta ColumnMeta, rowCount uint32) (*EncodedColumn, error) {
	return encodeColumnFromProvider(colMeta, rowCount, func(rowIdx int) (common.Value, bool) {
		if colIdx >= len(rows[rowIdx].Values) {
			return common.Value{}, false
		}
		return rows[rowIdx].Values[colIdx], true
	})
}

// CleanupSegments 删除旧 Segment 文件。
func (c *Compactor) CleanupSegments(segments []*Segment) error {
	for _, seg := range segments {
		if seg.FilePath != "" {
			if err := os.Remove(seg.FilePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("compactor: remove segment %s: %w", seg.FilePath, err)
			}
		}
	}
	return nil
}

type memRow struct {
	Key    string
	Values []common.Value
}

// Compact 执行 Tiered Compaction，将 L0 合并到 L1。
func (e *Engine) Compact(cols []ColumnMeta) error {
	e.mu.Lock()

	l0Segments, _ := e.collectSegmentsByLevel(0)
	if len(l0Segments) == 0 {
		e.mu.Unlock()
		return nil
	}

	l1Segments, _ := e.collectSegmentsByLevel(1)

	allSegments := make([]*Segment, 0, len(l0Segments)+len(l1Segments))
	allSegments = append(allSegments, l0Segments...)
	allSegments = append(allSegments, l1Segments...)

	// 记录待删除的 segment ID，而非索引，避免并发操作导致索引失效
	compactIDs := make(map[uint64]struct{}, len(allSegments))
	for _, seg := range allSegments {
		compactIDs[seg.ID] = struct{}{}
	}

	e.mu.Unlock()

	newSeg, err := e.compactor.Compact(allSegments, cols)
	if err != nil {
		return fmt.Errorf("engine compact: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 先注册新 segment 的索引，再注销旧 segment 的索引，
	// 确保任何时刻索引中都有数据可用，避免部分失败导致数据丢失。
	if err := e.addSegment(newSeg, 1); err != nil {
		cleanupSegmentFile(newSeg)
		return fmt.Errorf("engine compact: %w", err)
	}

	// 新 segment 注册成功后，再注销旧 segment 的索引并清理
	e.removeCompactedSegments(compactIDs)

	if err := e.compactor.CleanupSegments(allSegments); err != nil {
		return fmt.Errorf("engine compact: cleanup: %w", err)
	}

	return nil
}

// removeCompactedSegments 从引擎中移除已合并的旧 Segment，包括索引注销和数据结构清理。
// 调用者必须持有 e.mu 写锁。
func (e *Engine) removeCompactedSegments(compactIDs map[uint64]struct{}) {
	for _, seg := range e.segments {
		if _, ok := compactIDs[seg.ID]; ok {
			e.unregisterSegmentIndexes(seg.ID)
			delete(e.segmentMap, seg.ID)
		}
	}

	remaining := make([]*Segment, 0, len(e.segments))
	remainingLevels := make([]int, 0, len(e.segmentLevels))
	l0Count := 0
	for i, seg := range e.segments {
		if _, ok := compactIDs[seg.ID]; !ok {
			remaining = append(remaining, seg)
			remainingLevels = append(remainingLevels, e.segmentLevels[i])
			if e.segmentLevels[i] == 0 {
				l0Count++
			}
		}
	}
	e.segments = remaining
	e.segmentLevels = remainingLevels
	e.l0SegmentCount = l0Count
}

// ShouldCompact 判断是否需要执行 Compaction。
func (e *Engine) ShouldCompact() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.l0Count() >= defaultL0CompactionThreshold
}
