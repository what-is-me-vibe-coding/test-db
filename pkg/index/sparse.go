package index

import (
	"encoding/binary"
	"math"
	"sync"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// PredicateOp 表示比较操作类型。
type PredicateOp int

// 比较操作常量。
const (
	OpEqual PredicateOp = iota
	OpNotEqual
	OpLess
	OpLessEqual
	OpGreater
	OpGreaterEqual
)

// ColumnSparseStat 存储单个列的稀疏索引信息。
type ColumnSparseStat struct {
	MinValue  common.Value
	MaxValue  common.Value
	NullCount uint32
	HasValues bool
}

// SparseIndex 管理所有 Segment 的列级稀疏索引。
type SparseIndex struct {
	mu    sync.RWMutex
	stats map[colStatKey]ColumnSparseStat
}

type colStatKey struct {
	SegID uint64
	ColID uint32
}

// NewSparseIndex 创建 SparseIndex。
func NewSparseIndex() *SparseIndex {
	return &SparseIndex{
		stats: make(map[colStatKey]ColumnSparseStat),
	}
}

// RegisterColumnStat 注册单列的统计信息。
func (si *SparseIndex) RegisterColumnStat(segID uint64, colID uint32, stat storage.ColumnStat, dataType common.DataType) {
	si.mu.Lock()
	defer si.mu.Unlock()

	key := colStatKey{SegID: segID, ColID: colID}
	css := ColumnSparseStat{
		NullCount: stat.NullCount,
	}

	if len(stat.Min) > 0 && len(stat.Max) > 0 {
		css.MinValue = bytesToValue(stat.Min, dataType)
		css.MaxValue = bytesToValue(stat.Max, dataType)
		css.HasValues = true
	}

	si.stats[key] = css
}

// GetColumnStat 获取指定列统计信息。
func (si *SparseIndex) GetColumnStat(segID uint64, colID uint32) (ColumnSparseStat, bool) {
	si.mu.RLock()
	defer si.mu.RUnlock()

	css, ok := si.stats[colStatKey{SegID: segID, ColID: colID}]
	return css, ok
}

// UnregisterSegment 移除指定 Segment 的所有统计信息。
func (si *SparseIndex) UnregisterSegment(segID uint64) {
	si.mu.Lock()
	defer si.mu.Unlock()

	for key := range si.stats {
		if key.SegID == segID {
			delete(si.stats, key)
		}
	}
}

// CanSkip 判断指定 Segment 是否可以跳过某个列上的谓词。
// CanSkip 返回 true 表示该 Segment 不可能包含匹配结果，可以跳过。
// 返回 false 表示可能包含匹配结果，需要继续读取。
func (si *SparseIndex) CanSkip(segID uint64, colID uint32, op PredicateOp, value common.Value) bool {
	css, ok := si.GetColumnStat(segID, colID)
	if !ok || !css.HasValues {
		return false
	}

	minVal := css.MinValue
	maxVal := css.MaxValue

	// 匹配条件判断：
	// 可以跳过 ↔ 区间 [min, max] 中不存在任何一个满足条件的值
	switch op {
	case OpEqual:
		// value 不在区间 → 全部不满足 → 可以跳过
		return value.Less(minVal) || maxVal.Less(value)
	case OpNotEqual:
		// 即使 min/max 不等于 value，中间可能存在等于 value → 无法判定跳过
		return false
	case OpLess:
		// 查找 v < value → 若所有值 >= value 即 min >= value → 可跳过
		// min >= value → !(min < value) → 可以跳过
		return !minVal.Less(value)
	case OpLessEqual:
		// 查找 v <= value → 若所有值 > value 即 min > value → min.Less(value) 为 false → value.Less(min) 为 true → 可以跳过
		return value.Less(minVal)
	case OpGreater:
		// 查找 v > value → 若所有值 <= value 即 max <= value → value < max 为 false → !(value.Less(maxVal)) → 可以跳过
		return !value.Less(maxVal)
	case OpGreaterEqual:
		// 查找 v >= value → 若所有值 < value 即 max < value → max.Less(value) → 可以跳过
		return maxVal.Less(value)
	default:
		return false
	}
}

// LoadFromSegment 从 Segment 的 Footer 加载所有列统计信息。
func (si *SparseIndex) LoadFromSegment(seg *storage.Segment, _, _ string, _ int) {
	if seg == nil {
		return
	}

	for _, stat := range seg.Footer.ColumnStats {
		var dt common.DataType
		if int(stat.ColumnID) < len(seg.Columns) {
			dt = seg.Columns[stat.ColumnID].Type
		}
		si.RegisterColumnStat(seg.ID, stat.ColumnID, stat, dt)
	}
}

// BuildFromColumnVector 从列向量构建统计信息并注册。
func (si *SparseIndex) BuildFromColumnVector(segID uint64, colID uint32, cv *storage.ColumnVector) {
	if cv == nil || cv.Len() == 0 {
		return
	}

	stat := ColumnSparseStat{}
	nullBitmap := cv.NullBitmap()

	first := true
	for i := uint32(0); i < cv.Len(); i++ {
		if nullBitmap != nil && nullBitmap.Get(i) {
			stat.NullCount++
			continue
		}

		val := cv.GetValue(i)
		if val.IsNull() {
			continue
		}

		if first {
			stat.MinValue = val
			stat.MaxValue = val
			stat.HasValues = true
			first = false
		} else {
			if val.Less(stat.MinValue) {
				stat.MinValue = val
			}
			if stat.MaxValue.Less(val) {
				stat.MaxValue = val
			}
		}
	}

	si.mu.Lock()
	si.stats[colStatKey{SegID: segID, ColID: colID}] = stat
	si.mu.Unlock()
}

// StatCount 返回统计信息条目数。
func (si *SparseIndex) StatCount() int {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return len(si.stats)
}

// Clear 清空所有统计信息。
func (si *SparseIndex) Clear() {
	si.mu.Lock()
	defer si.mu.Unlock()
	si.stats = make(map[colStatKey]ColumnSparseStat)
}

func bytesToValue(b []byte, dataType common.DataType) common.Value {
	switch dataType {
	case common.TypeInt64:
		if len(b) >= 8 {
			v := int64(binary.LittleEndian.Uint64(b))
			return common.NewInt64(v)
		}
	case common.TypeFloat64:
		if len(b) >= 8 {
			v := math.Float64frombits(binary.LittleEndian.Uint64(b))
			return common.NewFloat64(v)
		}
	case common.TypeBool:
		if len(b) > 0 {
			return common.NewBool(b[0] != 0)
		}
	case common.TypeTimestamp:
		if len(b) >= 8 {
			v := int64(binary.LittleEndian.Uint64(b))
			return common.NewInt64(v)
		}
	case common.TypeString:
		return common.NewString(string(b))
	}
	return common.NewNull()
}
