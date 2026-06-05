package storage

import (
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestDeserializeBatchWriteRecordTruncated(t *testing.T) {
	// 空数据
	if _, err := deserializeBatchWriteRecord(nil); err == nil {
		t.Error("expected error for nil data")
	}
	// 只有行数头部，没有行数据
	if _, err := deserializeBatchWriteRecord([]byte{1, 0}); err == nil {
		t.Error("expected error for truncated data")
	}
	// 完全空
	if _, err := deserializeBatchWriteRecord([]byte{}); err == nil {
		t.Error("expected error for empty data")
	}
}

func TestEngineWriteBatchWALClosed(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	// Close the WAL to trigger errors on WriteBatch
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("close WAL: %v", err)
	}

	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("expected error when WAL is closed, got nil")
	}
}

func TestEngineWriteBatchTriggersRotation(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir:         t.TempDir(),
		MaxMemTableSize: 1, // Set very small to trigger ShouldFlush quickly
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Write enough rows to trigger memtable rotation
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("rot_key_%03d", i)
		rows := []WriteRow{
			{Key: key, Values: map[string]common.Value{colVal: common.NewInt64(int64(i))}},
		}
		if err := eng.WriteBatch(rows); err != nil {
			t.Fatalf("WriteBatch %d: %v", i, err)
		}
	}

	// Verify data is still readable after rotation
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("rot_key_%03d", i)
		got, ok := eng.Get(key)
		if !ok {
			t.Errorf("key %s not found after rotation", key)
			continue
		}
		if got.Columns[colVal].Int64 != int64(i) {
			t.Errorf("key %s: expected %d, got %d", key, i, got.Columns[colVal].Int64)
		}
	}
}

func TestEngineWriteBatchWALSyncError(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	// Close the WAL file to trigger sync error
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("close WAL: %v", err)
	}

	rows := []WriteRow{
		{Key: "sync_key", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("expected error when WAL sync fails, got nil")
	}
}

func TestApplyBatchWriteRecordCorruptPayload(t *testing.T) {
	mem := NewMemTable()

	// Call applyBatchWriteRecord with invalid/corrupt payload bytes
	maxVer, ok := applyBatchWriteRecord([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 0, mem)
	if ok {
		t.Error("expected ok=false for corrupt payload, got true")
	}
	if maxVer != 0 {
		t.Errorf("expected maxVer=0 for corrupt payload, got %d", maxVer)
	}
}

func TestApplyBatchWriteRecordSkipsOldVersions(t *testing.T) {
	mem := NewMemTable()

	// Serialize a batch write record with version starting at 1
	rows := []WriteRow{
		{Key: "old1", Values: map[string]common.Value{colVal: common.NewInt64(10)}},
		{Key: "old2", Values: map[string]common.Value{colVal: common.NewInt64(20)}},
	}
	data, err := serializeBatchWriteRecord(rows, 1)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	// Call applyBatchWriteRecord with lastFlushedVersion=100 (higher than all record versions)
	maxVer, ok := applyBatchWriteRecord(data, 100, mem)
	if !ok {
		t.Error("expected ok=true for valid payload, got false")
	}
	if maxVer != 0 {
		t.Errorf("expected maxVer=0 when all rows skipped, got %d", maxVer)
	}

	// Verify no rows were inserted into the memtable
	if _, found := mem.Get("old1"); found {
		t.Error("old1 should have been skipped")
	}
	if _, found := mem.Get("old2"); found {
		t.Error("old2 should have been skipped")
	}
}

func TestReadTypedValueTruncatedBool(t *testing.T) {
	_, n, err := readTypedValue([]byte{}, common.TypeBool)
	if err == nil {
		t.Error("expected error for truncated bool, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadTypedValueTruncatedInt64(t *testing.T) {
	_, n, err := readTypedValue([]byte{1, 2, 3, 4}, common.TypeInt64)
	if err == nil {
		t.Error("expected error for truncated int64, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadTypedValueTruncatedFloat64(t *testing.T) {
	_, n, err := readTypedValue([]byte{1, 2, 3, 4}, common.TypeFloat64)
	if err == nil {
		t.Error("expected error for truncated float64, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadTypedValueTruncatedStringLen(t *testing.T) {
	_, n, err := readTypedValue([]byte{1}, common.TypeString)
	if err == nil {
		t.Error("expected error for truncated string len, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadTypedValueTruncatedStringValue(t *testing.T) {
	// len=5 but only 3 bytes after the length field
	_, n, err := readTypedValue([]byte{5, 0, 'a', 'b', 'c'}, common.TypeString)
	if err == nil {
		t.Error("expected error for truncated string value, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadTypedValueTruncatedTimestamp(t *testing.T) {
	_, n, err := readTypedValue([]byte{1, 2, 3, 4}, common.TypeTimestamp)
	if err == nil {
		t.Error("expected error for truncated timestamp, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadValueBinaryTruncatedColNameLen(t *testing.T) {
	_, _, n, err := readValueBinary([]byte{})
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadValueBinaryTruncatedColName(t *testing.T) {
	// col name len = 5 but no name data
	_, _, n, err := readValueBinary([]byte{5, 0})
	if err == nil {
		t.Error("expected error for truncated col name, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestReadValueBinaryTruncatedTypeValid(t *testing.T) {
	// col name present but no type/valid bytes
	_, _, n, err := readValueBinary([]byte{1, 0, 'a'})
	if err == nil {
		t.Error("expected error for truncated type/valid, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}
}

func TestEngineRegisterSegmentIndexesNoBloomFilter(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Write some data
	rows := []WriteRow{
		{Key: "bloom_key1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
		{Key: "bloom_key2", Values: map[string]common.Value{colVal: common.NewInt64(2)}},
	}
	if err := eng.WriteBatch(rows); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	// Flush to create a segment
	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Manually remove bloom filter data from the segment to test the no-bloom path
	eng.mu.Lock()
	for _, seg := range eng.segments {
		seg.Footer.BloomFilter = nil
	}
	eng.mu.Unlock()

	// Re-register indexes without bloom filter - should not error
	eng.mu.Lock()
	for i, seg := range eng.segments {
		if err := eng.registerSegmentIndexes(seg, eng.segmentLevels[i]); err != nil {
			eng.mu.Unlock()
			t.Fatalf("registerSegmentIndexes without bloom: %v", err)
		}
	}
	eng.mu.Unlock()

	// Verify the engine still works (Get falls through to segment scan)
	got, ok := eng.Get("bloom_key1")
	if !ok {
		t.Error("bloom_key1 not found after re-registering indexes without bloom")
	}
	if got.Columns[colVal].Int64 != 1 {
		t.Errorf("bloom_key1: expected 1, got %d", got.Columns[colVal].Int64)
	}
}
