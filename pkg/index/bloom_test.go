package index

import (
	"fmt"
	"math"
	"testing"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

func TestNewSegmentBloomManager(t *testing.T) {
	bm := NewSegmentBloomManager()
	if bm == nil {
		t.Fatal("expected non-nil SegmentBloomManager")
	}
	if bm.Count() != 0 {
		t.Errorf("expected 0 filters, got %d", bm.Count())
	}
}

func TestBuildBloomFilter(t *testing.T) {
	keys := []string{"bf-key-1", "bf-key-2", "bf-key-3", "bf-key-4", "bf-key-5"}

	bf := BuildBloomFilter(keys, 100, 0.01)
	if bf == nil {
		t.Fatal("expected non-nil BloomFilter")
	}

	for _, k := range keys {
		if !bf.Test([]byte(k)) {
			t.Errorf("expected key %q to be present", k)
		}
	}

	if bf.Test([]byte("nonexistent")) {
		t.Error("unexpected positive for non-existent key")
	}
}

func TestBuildBloomFilterDefaultParams(t *testing.T) {
	keys := []string{"a", "b", "c"}
	bf := BuildBloomFilter(keys, 0, 0)
	if bf == nil {
		t.Fatal("expected non-nil BloomFilter")
	}
	for _, k := range keys {
		if !bf.Test([]byte(k)) {
			t.Errorf("expected key %q to be present", k)
		}
	}
}

func TestBuildBloomFilterEmptyKeys(t *testing.T) {
	bf := BuildBloomFilter(nil, 0, 0.01)
	if bf == nil {
		t.Fatal("expected non-nil BloomFilter even with empty keys")
	}
	if bf.Test([]byte("anything")) {
		t.Error("expected no keys in empty bloom filter")
	}
}

func TestTestBloom(t *testing.T) {
	keys := []string{"alice", "bob", "charlie"}
	bf := BuildBloomFilter(keys, 100, 0.01)

	data, err := bf.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal binary: %v", err)
	}

	for _, k := range keys {
		if !TestBloom(data, k) {
			t.Errorf("expected key %q to be present", k)
		}
	}

	if TestBloom(data, "david") {
		t.Error("expected non-existent key to return false")
	}
}

func TestTestBloomEmptyData(t *testing.T) {
	if TestBloom(nil, "key") {
		t.Error("expected false for nil data")
	}
	if TestBloom([]byte{}, "key") {
		t.Error("expected false for empty data")
	}
}

func TestRegisterAndMayContain(t *testing.T) {
	bm := NewSegmentBloomManager()

	keys := []string{"reg-k1", "reg-k2", "reg-k3"}
	bf := BuildBloomFilter(keys, 100, 0.01)
	data, err := bf.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal binary: %v", err)
	}

	if err := bm.Register(1, data); err != nil {
		t.Fatalf("register: %v", err)
	}

	for _, k := range keys {
		if !bm.MayContain(1, k) {
			t.Errorf("expected key %q to be present in segment 1", k)
		}
	}

	if bm.MayContain(1, "nonexistent") {
		t.Error("expected non-existent key to return false")
	}

	if !bm.MayContain(999, "reg-k1") {
		t.Error("expected true for unknown segment (no filter = may contain)")
	}
}

func TestRegisterEmptyData(t *testing.T) {
	bm := NewSegmentBloomManager()

	if err := bm.Register(1, nil); err != nil {
		t.Fatalf("register empty data: %v", err)
	}

	if bm.Count() != 1 {
		t.Errorf("expected 1 filter, got %d", bm.Count())
	}

	if !bm.MayContain(1, "anything") {
		t.Error("empty filter should return true (may contain)")
	}
}

func TestBloomUnregister(t *testing.T) {
	bm := NewSegmentBloomManager()

	bf := BuildBloomFilter([]string{"unreg-key"}, 10, 0.01)
	data, _ := bf.MarshalBinary()
	_ = bm.Register(1, data)
	_ = bm.Register(2, data)

	if bm.Count() != 2 {
		t.Fatalf("expected 2 filters, got %d", bm.Count())
	}

	bm.Unregister(1)

	if bm.Count() != 1 {
		t.Errorf("expected 1 filter after unregister, got %d", bm.Count())
	}

	if !bm.MayContain(1, "unreg-key") {
		t.Error("unknown segment should return true (may contain)")
	}
	if !bm.MayContain(2, "unreg-key") {
		t.Error("existing segment should still contain key")
	}
}

func TestBloomClear(t *testing.T) {
	bm := NewSegmentBloomManager()

	bf := BuildBloomFilter([]string{"clear-key"}, 10, 0.01)
	data, _ := bf.MarshalBinary()
	_ = bm.Register(1, data)
	_ = bm.Register(2, data)

	bm.Clear()
	if bm.Count() != 0 {
		t.Errorf("expected 0 filters after clear, got %d", bm.Count())
	}
}

func TestBloomFalsePositiveRate(t *testing.T) {
	n := uint(10000)
	fpRate := 0.01
	bf := bloom.NewWithEstimates(n, fpRate)

	for i := uint(0); i < n; i++ {
		bf.Add([]byte(fmt.Sprintf("key-%d", i)))
	}

	falsePositives := 0
	testCount := 100000
	for i := uint(0); i < uint(testCount); i++ {
		key := fmt.Sprintf("nonexistent-%d", i)
		if bf.Test([]byte(key)) {
			falsePositives++
		}
	}

	actualFPR := float64(falsePositives) / float64(testCount)
	// 允许一定余量，实际误判率通常接近理论值
	if actualFPR > 0.03 {
		t.Errorf("false positive rate too high: %.4f (expected ~%.4f)", actualFPR, fpRate)
	}
	t.Logf("False positive rate: %.4f (target: %.4f)", actualFPR, fpRate)
}

func TestBloomSkipRate(t *testing.T) {
	n := uint(10000)
	bf := bloom.NewWithEstimates(n, 0.01)

	for i := uint(0); i < n; i++ {
		bf.Add([]byte(fmt.Sprintf("key-%d", i)))
	}

	skipped := 0
	testCount := 100000
	for i := uint(0); i < uint(testCount); i++ {
		key := fmt.Sprintf("nonexistent-%d", i)
		if !bf.Test([]byte(key)) {
			skipped++
		}
	}

	skipRate := float64(skipped) / float64(testCount)
	if skipRate < 0.95 {
		t.Errorf("skip rate too low: %.4f (expected > 0.95)", skipRate)
	}
	t.Logf("Skip rate for non-existent keys: %.4f", skipRate)
}

func TestSegmentBuilderBloomFilter(t *testing.T) {
	builder := storage.NewSegmentBuilder(1, "a", "z")

	enc, err := storage.EncodeColumn(common.TypeInt64, []int64{10, 20, 30}, 3, nil)
	if err != nil {
		t.Fatalf("encode column: %v", err)
	}
	builder.AddEncodedColumn(enc)

	bloomKeys := []string{"key-a", "key-b", "key-c"}
	builder.SetBloomKeys(bloomKeys)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(seg.Footer.BloomFilter) == 0 {
		t.Fatal("expected bloom filter data in footer")
	}

	for _, k := range bloomKeys {
		if !TestBloom(seg.Footer.BloomFilter, k) {
			t.Errorf("expected key %q to be in bloom filter", k)
		}
	}

	if TestBloom(seg.Footer.BloomFilter, "key-z") {
		t.Error("expected non-existent key to be absent")
	}
}

func TestSegmentBuilderNoBloomKeys(t *testing.T) {
	builder := storage.NewSegmentBuilder(1, "a", "z")

	enc, err := storage.EncodeColumn(common.TypeInt64, []int64{10, 20, 30}, 3, nil)
	if err != nil {
		t.Fatalf("encode column: %v", err)
	}
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(seg.Footer.BloomFilter) != 0 {
		t.Error("expected empty bloom filter when no keys are set")
	}
}

func TestConcurrentBloomAccess(_ *testing.T) {
	bm := NewSegmentBloomManager()

	bf := BuildBloomFilter([]string{"conc-k1", "conc-k2", "conc-k3"}, 100, 0.01)
	data, _ := bf.MarshalBinary()
	_ = bm.Register(1, data)

	done := make(chan bool, 10)
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				bm.MayContain(1, fmt.Sprintf("key%d", j%4))
			}
			done <- true
		}()
	}

	for i := 0; i < 5; i++ {
		go func(id uint64) {
			for j := 0; j < 100; j++ {
				bf := BuildBloomFilter([]string{fmt.Sprintf("k-%d-%d", id, j)}, 10, 0.01)
				d, _ := bf.MarshalBinary()
				_ = bm.Register(id*100+uint64(j), d)
			}
			done <- true
		}(uint64(i))
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestBloomLargeKeySet(t *testing.T) {
	n := 50000
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = fmt.Sprintf("large-key-%08d", i)
	}

	bf := BuildBloomFilter(keys, uint(n), 0.01)

	data, err := bf.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal binary: %v", err)
	}

	bm := NewSegmentBloomManager()
	if err := bm.Register(1, data); err != nil {
		t.Fatalf("register: %v", err)
	}

	for i := 0; i < n; i++ {
		if !bm.MayContain(1, keys[i]) {
			t.Fatalf("expected key %s to be present at index %d", keys[i], i)
		}
	}

	misses := 0
	checkCount := 10000
	for i := 0; i < checkCount; i++ {
		if !bm.MayContain(1, fmt.Sprintf("missing-%08d", i)) {
			misses++
		}
	}
	skipRate := float64(misses) / float64(checkCount)
	if skipRate < 0.95 {
		t.Errorf("skip rate too low: %.4f (expected > 0.95)", skipRate)
	}
}

func TestBloomFilterSerializeRoundTrip(t *testing.T) {
	keys := []string{"alpha", "beta", "gamma", "delta"}
	bf := BuildBloomFilter(keys, 100, 0.01)

	data, err := bf.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	restored := &bloom.BloomFilter{}
	if err := restored.UnmarshalBinary(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, k := range keys {
		if !restored.Test([]byte(k)) {
			t.Errorf("restored filter should contain key %q", k)
		}
	}

	if restored.Test([]byte("epsilon")) {
		t.Error("restored filter should not contain non-existent key")
	}
}

func TestBloomFilterWithDuplicateKeys(t *testing.T) {
	const dk = "dkey-a"
	keys := []string{dk, dk, "dkey-b"}
	bf := BuildBloomFilter(keys, 100, 0.01)

	if !bf.Test([]byte(dk)) {
		t.Errorf("expected %q to be present", dk)
	}
	if !bf.Test([]byte("dkey-b")) {
		t.Error("expected 'unique' to be present")
	}
}

func TestBloomFilterCapacity(t *testing.T) {
	bf := BuildBloomFilter([]string{"key"}, 1000000, 0.01)
	if bf.Cap() == 0 {
		t.Error("expected non-zero capacity")
	}
	if !bf.Test([]byte("key")) {
		t.Error("expected 'key' to be present")
	}
}

func TestBloomFilterApproximatedSize(t *testing.T) {
	n := uint(10000)
	bf := bloom.NewWithEstimates(n, 0.01)
	for i := uint(0); i < n; i++ {
		bf.Add([]byte(fmt.Sprintf("key-%d", i)))
	}

	data, err := bf.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	expectedBits := int(math.Ceil(-float64(n) * math.Log(0.01) / (math.Ln2 * math.Ln2)))
	expectedBytes := expectedBits / 8
	actualBytes := len(data)

	t.Logf("Bloom filter: %d keys, %d bytes (theoretical ~%d bytes)", n, actualBytes, expectedBytes)

	if actualBytes > expectedBytes*3 {
		t.Errorf("bloom filter too large: %d bytes (expected ~%d bytes)", actualBytes, expectedBytes)
	}
}
