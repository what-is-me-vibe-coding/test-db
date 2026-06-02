package index

import (
	"fmt"
	"sync"
	"testing"
)

func TestBloomIndexRegisterAndMayContain(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"key1", "key2", "key3", "key4", "key5"}
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
