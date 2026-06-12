package storage

import (
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestScanRangeUnlockedEmpty_V7 测试 scanRangeUnlocked 在没有迭代器时返回 nil。
func TestScanRangeUnlockedEmpty_V7(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 空引擎，无数据，scanRangeUnlocked 应返回空结果
	eng.mu.RLock()
	results := eng.scanRangeUnlocked("a", "z")
	eng.mu.RUnlock()

	if len(results) != 0 {
		t.Errorf("期望 0 条结果，实际 %d 条", len(results))
	}
}

// TestScanRangeWithMultipleSegments_V7 测试 scanRangeUnlocked 跨多个 segment 的扫描。
func TestScanRangeWithMultipleSegments_V7(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// 写入第一批数据并刷盘
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 1: %v", err)
	}

	// 写入第二批数据并刷盘
	for i := 5; i < 10; i++ {
		key := string(rune('a' + i))
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 2: %v", err)
	}

	// 扫描全部范围
	results := eng.ScanRange("a", "z")
	if len(results) != 10 {
		t.Errorf("期望 10 条结果，实际 %d", len(results))
	}
}

// TestMergeIteratorErrorPropagation_V7 测试 MergeIterator 传播迭代器错误。
func TestMergeIteratorErrorPropagation_V7(t *testing.T) {
	// 创建一个返回错误的迭代器
	errIt := &errIterV7{err: fmt.Errorf("迭代错误")}

	mi := NewMergeIterator(errIt)
	defer mi.Close()

	// Next 应返回 false
	if mi.Next() {
		t.Error("期望 Next 返回 false")
	}
	// Err 应返回迭代器的错误
	if mi.Err() == nil {
		t.Error("期望错误，实际 nil")
	}
}

// errIterV7 是一个总是返回错误的 ScanIterator 实现。
type errIterV7 struct {
	err error
}

func (it *errIterV7) Next() bool       { return false }
func (it *errIterV7) Entry() ScanEntry { return ScanEntry{} }
func (it *errIterV7) Err() error       { return it.err }
func (it *errIterV7) Close()           {}

// TestSliceIteratorEntryBeforeNext_V7 测试 sliceIterator 在 Next 之前调用 Entry 返回空。
func TestSliceIteratorEntryBeforeNext_V7(t *testing.T) {
	entries := []ScanEntry{
		{Key: "a", Value: Row{Columns: map[string]common.Value{"v": common.NewInt64(1)}}},
	}
	it := newSliceIterator(entries)
	defer it.Close()

	// 未调用 Next 时 Entry 应返回空
	entry := it.Entry()
	if entry.Key != "" {
		t.Errorf("期望空 Key，实际 %q", entry.Key)
	}
}

// TestSliceIteratorExhausted_V7 测试 sliceIterator 超出范围后返回 false。
func TestSliceIteratorExhausted_V7(t *testing.T) {
	entries := []ScanEntry{
		{Key: "a", Value: Row{Columns: map[string]common.Value{"v": common.NewInt64(1)}}},
	}
	it := newSliceIterator(entries)

	if !it.Next() {
		t.Fatal("期望第一个 Next 返回 true")
	}
	if it.Next() {
		t.Error("期望第二个 Next 返回 false（已耗尽）")
	}
	it.Close()
}

// TestMemTableIteratorEntryOutOfRange_V7 测试 memTableIterator 越界访问。
func TestMemTableIteratorEntryOutOfRange_V7(t *testing.T) {
	mem := NewMemTable()
	it := newMemTableIterator(mem, "a", "z")
	defer it.Close()

	// 空 memtable，Next 返回 false，Entry 应返回空
	entry := it.Entry()
	if entry.Key != "" {
		t.Errorf("期望空 Key，实际 %q", entry.Key)
	}
}

// TestSegmentIteratorEntryBeforeStart_V7 测试 segmentIterator 未开始时 Entry 返回空。
func TestSegmentIteratorEntryBeforeStart_V7(t *testing.T) {
	seg := buildTestSegment(t, []string{"a", "b"}, []int64{1, 2})
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	it := newSegmentIterator(seg, colMeta, "a", "b", nil)

	// 未调用 Next 时 Entry 应返回空
	entry := it.Entry()
	if entry.Key != "" {
		t.Errorf("期望空 Key，实际 %q", entry.Key)
	}
	it.Close()
}

// TestMergeIteratorAdvanceNext_V7 测试 MergeIterator 的 advanceNext 路径。
func TestMergeIteratorAdvanceNext_V7(t *testing.T) {
	it1 := newSliceIterator([]ScanEntry{
		{Key: "a", Value: Row{Columns: map[string]common.Value{"v": common.NewInt64(1)}}},
		{Key: "c", Value: Row{Columns: map[string]common.Value{"v": common.NewInt64(3)}}},
	})
	it2 := newSliceIterator([]ScanEntry{
		{Key: "a", Value: Row{Columns: map[string]common.Value{"v": common.NewInt64(10)}}},
		{Key: "b", Value: Row{Columns: map[string]common.Value{"v": common.NewInt64(20)}}},
	})

	mi := NewMergeIterator(it1, it2)
	defer mi.Close()

	var keys []string
	for mi.Next() {
		keys = append(keys, mi.Entry().Key)
	}

	expected := []string{"a", "b", "c"}
	if len(keys) != len(expected) {
		t.Fatalf("期望 %d 个 key，实际 %d: %v", len(expected), len(keys), keys)
	}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("key[%d]: 期望 %q, 实际 %q", i, expected[i], k)
		}
	}
}
