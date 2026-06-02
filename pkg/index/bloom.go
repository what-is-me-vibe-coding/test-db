package index

import (
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
)

const defaultFalsePositiveRate = 0.01

// BuildBloomFilter 使用主键列表构建布隆过滤器。
// n 为预估元素数量，默认为 len(keys)。
// 序列化后可直接存入 SegmentFooter.BloomFilter。
func BuildBloomFilter(keys []string, n uint, fpRate float64) *bloom.BloomFilter {
	if fpRate <= 0 {
		fpRate = defaultFalsePositiveRate
	}
	if n == 0 {
		n = uint(len(keys))
	}
	if n < 1 {
		n = 1
	}

	bf := bloom.NewWithEstimates(n, fpRate)
	for _, k := range keys {
		bf.Add([]byte(k))
	}
	return bf
}

// TestBloom 从序列化的布隆过滤器数据中测试 key 是否存在。
func TestBloom(data []byte, key string) bool {
	if len(data) == 0 {
		return false
	}
	bf := &bloom.BloomFilter{}
	if err := bf.UnmarshalBinary(data); err != nil {
		return false
	}
	return bf.Test([]byte(key))
}

// SegmentBloomManager 管理所有 Segment 的布隆过滤器。
type SegmentBloomManager struct {
	mu      sync.RWMutex
	filters map[uint64]*bloom.BloomFilter
}

// NewSegmentBloomManager 创建 SegmentBloomManager。
func NewSegmentBloomManager() *SegmentBloomManager {
	return &SegmentBloomManager{
		filters: make(map[uint64]*bloom.BloomFilter),
	}
}

// Register 注册一个 Segment 的布隆过滤器。
func (bm *SegmentBloomManager) Register(segID uint64, data []byte) error {
	bf := &bloom.BloomFilter{}
	if len(data) > 0 {
		if err := bf.UnmarshalBinary(data); err != nil {
			return err
		}
	}

	bm.mu.Lock()
	bm.filters[segID] = bf
	bm.mu.Unlock()
	return nil
}

// MayContain 测试指定 Segment 是否可能包含该 key。
// 返回 false 表示一定不包含，可以跳过该 Segment。
func (bm *SegmentBloomManager) MayContain(segID uint64, key string) bool {
	bm.mu.RLock()
	bf, ok := bm.filters[segID]
	bm.mu.RUnlock()

	if !ok || bf == nil {
		return true
	}
	return bf.Test([]byte(key))
}

// Unregister 移除指定 Segment 的布隆过滤器。
func (bm *SegmentBloomManager) Unregister(segID uint64) {
	bm.mu.Lock()
	delete(bm.filters, segID)
	bm.mu.Unlock()
}

// Count 返回已注册的布隆过滤器数量。
func (bm *SegmentBloomManager) Count() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return len(bm.filters)
}

// Clear 清空所有布隆过滤器。
func (bm *SegmentBloomManager) Clear() {
	bm.mu.Lock()
	bm.filters = make(map[uint64]*bloom.BloomFilter)
	bm.mu.Unlock()
}
