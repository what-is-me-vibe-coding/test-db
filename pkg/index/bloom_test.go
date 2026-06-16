package index

import (
	"fmt"
	"sync"
	"testing"

	"github.com/bits-and-blooms/bloom/v3"
)

const testBloomKey1 = "key1"
const testBloomKey2 = "key2"
const testBloomKey3 = "key3"
const testAlpha = "alpha"
const testBeta = "beta"
const testGamma = "gamma"

func TestBloomIndexRegisterAndMayContain(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{testBloomKey1, testBloomKey2, testBloomKey3, "key4", "key5"}
	data, err := BuildFromKeys(keys, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildFromKeys: %v", err)
	}

	err = bi.RegisterFromBytes(1, data)
	if err != nil {
		t.Fatalf("RegisterFromBytes: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true", k)
		}
	}

	if bi.MayContain(1, []byte("nonexistent")) {
		t.Log("MayContain: false positive for nonexistent key (expected with 1%% FP rate)")
	}

	hit, miss := bi.Stats()
	t.Logf("Stats: hit=%d, miss=%d", hit, miss)
}

func TestBloomIndexNoFilter(t *testing.T) {
	bi := NewBloomIndex()

	if !bi.MayContain(99, []byte("any")) {
		t.Error("MayContain should return true when no filter registered")
	}
}

func TestBloomIndexUnregister(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"a", "b", "c"}
	data, err := BuildFromKeys(keys, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildFromKeys: %v", err)
	}

	err = bi.RegisterFromBytes(1, data)
	if err != nil {
		t.Fatalf("RegisterFromBytes: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	bi.Unregister(1)

	if bi.Len() != 0 {
		t.Errorf("Len after unregister: got %d, want 0", bi.Len())
	}

	if !bi.MayContain(1, []byte("a")) {
		t.Error("MayContain should return true after unregister (no filter)")
	}
}

func TestBloomIndexClear(t *testing.T) {
	bi := NewBloomIndex()

	for i := 0; i < 5; i++ {
		keys := []string{fmt.Sprintf("k%d", i)}
		data, err := BuildFromKeys(keys, DefaultBloomFPRate)
		if err != nil {
			t.Fatalf("BuildFromKeys: %v", err)
		}
		err = bi.RegisterFromBytes(uint64(i+1), data)
		if err != nil {
			t.Fatalf("RegisterFromBytes: %v", err)
		}
	}

	if bi.Len() != 5 {
		t.Errorf("Len: got %d, want 5", bi.Len())
	}

	bi.Clear()

	if bi.Len() != 0 {
		t.Errorf("Len after clear: got %d, want 0", bi.Len())
	}

	hit, miss := bi.Stats()
	if hit != 0 || miss != 0 {
		t.Errorf("Stats after clear: hit=%d, miss=%d, want 0,0", hit, miss)
	}
}

func TestBloomIndexEmptyKeys(t *testing.T) {
	data, err := BuildFromKeys([]string{}, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildFromKeys: %v", err)
	}
	if data != nil {
		t.Error("BuildFromKeys with empty keys should return nil")
	}
}

func TestBloomIndexNilRegister(t *testing.T) {
	bi := NewBloomIndex()
	err := bi.Register(1, nil)
	if err == nil {
		t.Error("Register with nil filter should return error")
	}
}

func TestBloomIndexRegisterFromEmptyBytes(t *testing.T) {
	bi := NewBloomIndex()
	err := bi.RegisterFromBytes(1, nil)
	if err != nil {
		t.Fatalf("RegisterFromBytes with nil: %v", err)
	}
	if bi.Len() != 0 {
		t.Errorf("Len: got %d, want 0", bi.Len())
	}
}

func TestBloomIndexBuildAndRegister(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"k1", "k2", "k3"}
	err := bi.BuildAndRegister(1, keys, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true", k)
		}
	}
}

func TestBuildFromKeysDefaultFPRate(t *testing.T) {
	keys := []string{"x", "y", "z"}
	data, err := BuildFromKeys(keys, 0)
	if err != nil {
		t.Fatalf("BuildFromKeys: %v", err)
	}
	if data == nil {
		t.Fatal("BuildFromKeys should return non-nil data")
	}
}

func TestBloomIndexConcurrentAccess(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"c1", "c2", "c3", "c4", "c5"}
	data, err := BuildFromKeys(keys, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildFromKeys: %v", err)
	}
	err = bi.RegisterFromBytes(1, data)
	if err != nil {
		t.Fatalf("RegisterFromBytes: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("c%d", idx%5+1)
			bi.MayContain(1, []byte(key))
		}(i)
	}
	wg.Wait()

	hit, miss := bi.Stats()
	t.Logf("Concurrent access stats: hit=%d, miss=%d", hit, miss)
}

func TestBloomIndexFalsePositiveRate(t *testing.T) {
	n := 10000
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	data, err := BuildFromKeys(keys, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildFromKeys: %v", err)
	}

	bi := NewBloomIndex()
	err = bi.RegisterFromBytes(1, data)
	if err != nil {
		t.Fatalf("RegisterFromBytes: %v", err)
	}

	falsePositives := 0
	nonExistentKeys := 10000
	for i := 0; i < nonExistentKeys; i++ {
		if bi.MayContain(1, []byte(fmt.Sprintf("nonexistent-%d", i))) {
			falsePositives++
		}
	}

	fpRate := float64(falsePositives) / float64(nonExistentKeys)
	t.Logf("False positive rate: %.4f (expected ~0.01), FP count: %d/%d", fpRate, falsePositives, nonExistentKeys)

	if fpRate > 0.05 {
		t.Errorf("False positive rate %.4f exceeds 5%% threshold", fpRate)
	}
}

func TestBloomIndexMultipleSegments(t *testing.T) {
	bi := NewBloomIndex()

	for segID := uint64(1); segID <= 3; segID++ {
		keys := []string{
			fmt.Sprintf("seg%d-a", segID),
			fmt.Sprintf("seg%d-b", segID),
			fmt.Sprintf("seg%d-c", segID),
		}
		err := bi.BuildAndRegister(segID, keys, DefaultBloomFPRate)
		if err != nil {
			t.Fatalf("BuildAndRegister seg %d: %v", segID, err)
		}
	}

	if bi.Len() != 3 {
		t.Errorf("Len: got %d, want 3", bi.Len())
	}

	if !bi.MayContain(2, []byte("seg2-b")) {
		t.Error("seg2 should contain seg2-b")
	}

	if bi.MayContain(2, []byte("seg1-a")) {
		t.Log("seg2 false positive for seg1-a")
	}
}

// TestBloomIndexRegisterNormal 测试 Register 方法的正常注册路径。
// 创建真实的 BloomFilter 对象并注册，验证 MayContain 可以正常工作。
func TestBloomIndexRegisterNormal(t *testing.T) {
	bi := NewBloomIndex()

	// 创建一个真实的布隆过滤器并添加一些 key
	filter := bloom.NewWithEstimates(100, DefaultBloomFPRate)
	keys := []string{"apple", "banana", "cherry"}
	for _, k := range keys {
		filter.Add([]byte(k))
	}

	// 正常注册
	err := bi.Register(1, filter)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	// 验证已注册的 key 可以通过 MayContain 找到
	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true after Register", k)
		}
	}
}

// TestBloomIndexRegisterOverwrite 测试 Register 覆盖已存在的过滤器。
func TestBloomIndexRegisterOverwrite(t *testing.T) {
	bi := NewBloomIndex()

	// 注册第一个过滤器
	filter1 := bloom.NewWithEstimates(10, DefaultBloomFPRate)
	filter1.Add([]byte("old-key"))
	err := bi.Register(1, filter1)
	if err != nil {
		t.Fatalf("Register first: %v", err)
	}

	if !bi.MayContain(1, []byte("old-key")) {
		t.Error("old-key should be found in first filter")
	}

	// 用新的过滤器覆盖
	filter2 := bloom.NewWithEstimates(10, DefaultBloomFPRate)
	filter2.Add([]byte("new-key"))
	err = bi.Register(1, filter2)
	if err != nil {
		t.Fatalf("Register overwrite: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1 after overwrite", bi.Len())
	}

	// 新 key 应该能找到
	if !bi.MayContain(1, []byte("new-key")) {
		t.Error("new-key should be found after overwrite")
	}
}

// TestBloomIndexRegisterMultipleSegments 测试 Register 注册多个 Segment。
func TestBloomIndexRegisterMultipleSegments(t *testing.T) {
	bi := NewBloomIndex()

	for segID := uint64(1); segID <= 5; segID++ {
		filter := bloom.NewWithEstimates(10, DefaultBloomFPRate)
		filter.Add([]byte(fmt.Sprintf("seg%d-key", segID)))
		err := bi.Register(segID, filter)
		if err != nil {
			t.Fatalf("Register seg %d: %v", segID, err)
		}
	}

	if bi.Len() != 5 {
		t.Errorf("Len: got %d, want 5", bi.Len())
	}

	// 验证每个 Segment 的 key 都能找到
	for segID := uint64(1); segID <= 5; segID++ {
		key := fmt.Sprintf("seg%d-key", segID)
		if !bi.MayContain(segID, []byte(key)) {
			t.Errorf("MayContain seg %d key %q: expected true", segID, key)
		}
	}
}

// TestBuildAndRegisterEmptyKeys 测试 BuildAndRegister 空 keys 时返回 nil 不注册的场景。
func TestBuildAndRegisterEmptyKeys(t *testing.T) {
	bi := NewBloomIndex()

	// 空 keys 不应注册任何过滤器
	err := bi.BuildAndRegister(1, []string{}, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister with empty keys: %v", err)
	}

	if bi.Len() != 0 {
		t.Errorf("Len: got %d, want 0 after empty keys", bi.Len())
	}

	// nil keys 也不应注册
	err = bi.BuildAndRegister(2, nil, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister with nil keys: %v", err)
	}

	if bi.Len() != 0 {
		t.Errorf("Len: got %d, want 0 after nil keys", bi.Len())
	}

	// 验证 MayContain 对未注册 Segment 返回 true（无过滤器时默认不跳过）
	if !bi.MayContain(1, []byte("any")) {
		t.Error("MayContain should return true for unregistered segment")
	}
	if !bi.MayContain(2, []byte("any")) {
		t.Error("MayContain should return true for unregistered segment with nil keys")
	}
}

// TestBuildAndRegisterWithKeys 测试 BuildAndRegister 正常路径后能查到 key。
func TestBuildAndRegisterWithKeys(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"x1", "x2", "x3"}
	err := bi.BuildAndRegister(10, keys, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(10, []byte(k)) {
			t.Errorf("MayContain(%q): expected true", k)
		}
	}
}

func TestBloomBuildAndRegister(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{testBloomKey1, testBloomKey2, testBloomKey3}
	if err := bi.BuildAndRegister(1, keys, DefaultBloomFPRate); err != nil {
		t.Fatalf("BuildAndRegister: %v", err)
	}

	for _, k := range keys {
		if !bi.MayContainString(1, k) {
			t.Errorf("MayContainString(%q): expected true after BuildAndRegister", k)
		}
	}
}

func TestBloomBuildAndRegisterEmpty(t *testing.T) {
	bi := NewBloomIndex()

	if err := bi.BuildAndRegister(1, nil, DefaultBloomFPRate); err != nil {
		t.Fatalf("BuildAndRegister with nil keys: %v", err)
	}

	if err := bi.BuildAndRegister(2, []string{}, DefaultBloomFPRate); err != nil {
		t.Fatalf("BuildAndRegister with empty keys: %v", err)
	}
}

func TestBloomBuildFromKeysInvalidFPRate(t *testing.T) {
	keys := []string{testBloomKey1, testBloomKey2}

	tests := []struct {
		name   string
		fpRate float64
	}{
		{"zero", 0},
		{"negative", -0.1},
		{"one", 1.0},
		{"above_one", 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := BuildFromKeys(keys, tt.fpRate)
			if err != nil {
				t.Fatalf("BuildFromKeys with fpRate=%v: %v", tt.fpRate, err)
			}
			if data == nil {
				t.Error("expected non-nil data for non-empty keys")
			}
		})
	}
}

// TestBuildAndRegisterEmptyKeys_CovV2 测试 BuildAndRegister 空 keys 时返回 nil 不注册
// 覆盖 bloom.go:165-167 行的 data==nil 分支
func TestBuildAndRegisterEmptyKeys_CovV2(t *testing.T) {
	bi := NewBloomIndex()

	// 空 slice：BuildFromKeys 返回 nil, nil → data==nil → 直接返回 nil
	err := bi.BuildAndRegister(1, []string{}, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister 空 keys 不应返回错误: %v", err)
	}
	if bi.Len() != 0 {
		t.Errorf("空 keys 后 Len = %d，期望 0", bi.Len())
	}

	// nil keys：同样走 data==nil 路径
	err = bi.BuildAndRegister(2, nil, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister nil keys 不应返回错误: %v", err)
	}
	if bi.Len() != 0 {
		t.Errorf("nil keys 后 Len = %d，期望 0", bi.Len())
	}

	// 验证未注册的 Segment 查询返回 true（保守策略：无过滤器时不跳过）
	if !bi.MayContain(1, []byte("any")) {
		t.Error("未注册 Segment 的 MayContain 应返回 true")
	}
}

// TestBuildAndRegisterNormalKeys_CovV2 测试 BuildAndRegister 正常 keys 的完整路径
// 覆盖 bloom.go:161-168 行，包括 BuildFromKeys 成功和 RegisterFromBytes 调用
func TestBuildAndRegisterNormalKeys_CovV2(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"key-a", "key-b", "key-c"}
	err := bi.BuildAndRegister(1, keys, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister 正常 keys 不应返回错误: %v", err)
	}
	if bi.Len() != 1 {
		t.Errorf("正常 keys 后 Len = %d，期望 1", bi.Len())
	}

	// 验证注册的 key 可以被查到
	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): 期望 true", k)
		}
	}

	// 验证 MayContainString 也能正常工作
	for _, k := range keys {
		if !bi.MayContainString(1, k) {
			t.Errorf("MayContainString(%q): 期望 true", k)
		}
	}
}

// TestRegisterFromBytesCorruptData_CovV2 测试 RegisterFromBytes 对损坏数据的错误处理
// 覆盖 bloom.go:53-54 行的 UnmarshalBinary 错误分支
func TestRegisterFromBytesCorruptData_CovV2(t *testing.T) {
	bi := NewBloomIndex()

	// 构建有效过滤器并截断数据以触发反序列化错误
	validData, err := BuildFromKeys([]string{"a", "b"}, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildFromKeys 失败: %v", err)
	}
	if len(validData) < 4 {
		t.Fatalf("有效数据太短: %d 字节", len(validData))
	}

	// 使用截断的数据触发 UnmarshalBinary 错误
	truncatedData := validData[:len(validData)/2]
	err = bi.RegisterFromBytes(1, truncatedData)
	if err == nil {
		t.Error("RegisterFromBytes 截断数据应返回错误")
	}

	// 验证失败注册后索引中没有过滤器
	if bi.Len() != 0 {
		t.Errorf("失败注册后 Len = %d，期望 0", bi.Len())
	}

	// 使用完全随机的无效数据测试
	randomData := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	err = bi.RegisterFromBytes(2, randomData)
	if err == nil {
		t.Error("RegisterFromBytes 随机数据应返回错误")
	}
}

// TestLookupEmptyIndex_CovV2 测试 PrimaryIndex.Lookup 在空索引上返回 nil
// 覆盖 primary.go:66-68 行的空 segments 分支
func TestLookupEmptyIndex_CovV2(t *testing.T) {
	pi := NewPrimaryIndex()

	// 空索引上 Lookup 应返回 nil
	result := pi.Lookup("any-key")
	if result != nil {
		t.Errorf("空索引 Lookup 应返回 nil，得到 %v", result)
	}

	// 验证 SegmentCount 为 0
	if pi.SegmentCount() != 0 {
		t.Errorf("空索引 SegmentCount = %d，期望 0", pi.SegmentCount())
	}

	// 注册后再移除，验证 Lookup 仍返回 nil
	_ = pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "a", MaxKey: "z"})
	_ = pi.UnregisterSegment(1)

	result = pi.Lookup("m")
	if result != nil {
		t.Errorf("移除所有 segment 后 Lookup 应返回 nil，得到 %v", result)
	}
}

// --- BuildAndRegister: valid keys with custom fpRate ---

func TestBuildAndRegister_CustomFPRate(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"custom1", "custom2", "custom3"}
	err := bi.BuildAndRegister(1, keys, 0.001)
	if err != nil {
		t.Fatalf("BuildAndRegister with custom fpRate: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true with custom fpRate", k)
		}
	}
}

// --- BuildAndRegister: valid keys with fpRate=0 (should use default) ---

func TestBuildAndRegister_ZeroFPRate(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"zero1", "zero2"}
	err := bi.BuildAndRegister(1, keys, 0)
	if err != nil {
		t.Fatalf("BuildAndRegister with fpRate=0: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (fpRate=0 should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: valid keys with negative fpRate (should use default) ---

func TestBuildAndRegister_NegativeFPRate(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"neg1", "neg2"}
	err := bi.BuildAndRegister(1, keys, -0.5)
	if err != nil {
		t.Fatalf("BuildAndRegister with negative fpRate: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (negative fpRate should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: valid keys with fpRate=1.0 (should use default) ---

func TestBuildAndRegister_FPRateOne(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"one1", "one2"}
	err := bi.BuildAndRegister(1, keys, 1.0)
	if err != nil {
		t.Fatalf("BuildAndRegister with fpRate=1.0: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (fpRate=1.0 should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: valid keys with fpRate>1 (should use default) ---

func TestBuildAndRegister_FPRateGreaterThanOne(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"gt1_a", "gt1_b"}
	err := bi.BuildAndRegister(1, keys, 2.0)
	if err != nil {
		t.Fatalf("BuildAndRegister with fpRate>1: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (fpRate>1 should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: empty keys should not register (nil keys variant) ---

func TestBuildAndRegister_NilKeys(t *testing.T) {
	bi := NewBloomIndex()

	err := bi.BuildAndRegister(1, nil, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister with nil keys: %v", err)
	}

	if bi.Len() != 0 {
		t.Errorf("Len: got %d, want 0 after nil keys", bi.Len())
	}
}

// --- BuildFromKeys: fpRate exactly at boundary (0 and 1) ---

func TestBuildFromKeys_FPRateBoundaryZero(t *testing.T) {
	keys := []string{"b1", "b2"}
	data, err := BuildFromKeys(keys, 0)
	if err != nil {
		t.Fatalf("BuildFromKeys with fpRate=0: %v", err)
	}
	if data == nil {
		t.Fatal("BuildFromKeys with fpRate=0 should return non-nil data (falls back to default)")
	}
}

func TestBuildFromKeys_FPRateBoundaryOne(t *testing.T) {
	keys := []string{"b1", "b2"}
	data, err := BuildFromKeys(keys, 1.0)
	if err != nil {
		t.Fatalf("BuildFromKeys with fpRate=1.0: %v", err)
	}
	if data == nil {
		t.Fatal("BuildFromKeys with fpRate=1.0 should return non-nil data (falls back to default)")
	}
}

func TestBuildFromKeys_NegativeFPRate(t *testing.T) {
	keys := []string{"b1", "b2"}
	data, err := BuildFromKeys(keys, -1.0)
	if err != nil {
		t.Fatalf("BuildFromKeys with negative fpRate: %v", err)
	}
	if data == nil {
		t.Fatal("BuildFromKeys with negative fpRate should return non-nil data (falls back to default)")
	}
}
