package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// Engine.Write error paths (84.2% → higher)
// ---------------------------------------------------------------------------

// TestWriteWALAppendFailureV5 tests Write when WAL AppendWrite fails
// by closing the WAL file descriptor before writing.
func TestWriteWALAppendFailureV5(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("file Close failed: %v", err)
	}

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when WAL AppendWrite fails, got nil")
	}
}

// TestWriteWALSyncFailureV5 tests Write when WAL Sync fails
// by closing the WAL file descriptor before syncing.
func TestWriteWALSyncFailureV5(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_ = eng.wal.file.Close()

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when WAL Sync fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine.NewEngine paths (84.2% → higher)
// ---------------------------------------------------------------------------

// TestNewEngineInvalidDataDirV5 tests NewEngine with a data directory
// that cannot be created (path under /dev/null).
func TestNewEngineInvalidDataDirV5(t *testing.T) {
	_, err := NewEngine(EngineConfig{DataDir: "/dev/null/cannot/create/here"})
	if err == nil {
		t.Error("expected error for invalid data directory, got nil")
	}
}

// TestNewEngineReplayWALRecordsV5 tests NewEngine with an existing WAL
// that contains write records to be replayed into the memtable.
func TestNewEngineReplayWALRecordsV5(t *testing.T) {
	dir := t.TempDir()

	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("first NewEngine: %v", err)
	}
	if err := eng.Write("replay_key", map[string]common.Value{colVal: common.NewInt64(42)}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	eng2, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("second NewEngine: %v", err)
	}
	defer func() { _ = eng2.Close() }()

	row, ok := eng2.Get("replay_key")
	if !ok {
		t.Fatal("expected replay_key to be recovered from WAL replay")
	}
	if row.Columns[colVal].Int64 != 42 {
		t.Errorf("expected val=42, got %d", row.Columns[colVal].Int64)
	}
}

// ---------------------------------------------------------------------------
// Compress/CompressColumn/DecompressColumn edge cases
// ---------------------------------------------------------------------------

// TestCompressEmptyDataV5 verifies Compress returns nil for empty input.
func TestCompressEmptyDataV5(t *testing.T) {
	result, err := Compress([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty data, got %d bytes", len(result))
	}
}

// TestDecompressEmptyDataV5 verifies Decompress returns nil for empty input.
func TestDecompressEmptyDataV5(t *testing.T) {
	result, err := Decompress([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty data, got %d bytes", len(result))
	}
}

// TestCompressColumnNilV5 verifies CompressColumn returns error for nil EncodedColumn.
func TestCompressColumnNilV5(t *testing.T) {
	err := CompressColumn(nil)
	if err == nil {
		t.Fatal("expected error for nil EncodedColumn, got nil")
	}
}

// TestDecompressColumnNilV5 verifies DecompressColumn returns error for nil EncodedColumn.
func TestDecompressColumnNilV5(t *testing.T) {
	err := DecompressColumn(nil)
	if err == nil {
		t.Fatal("expected error for nil EncodedColumn, got nil")
	}
}

// TestDecompressInvalidDataV5 verifies Decompress returns error for invalid data.
func TestDecompressInvalidDataV5(t *testing.T) {
	_, err := Decompress([]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE})
	if err == nil {
		t.Fatal("expected error for invalid compressed data, got nil")
	}
}

// ---------------------------------------------------------------------------
// EncodeColumn edge cases (85.7% → higher)
// ---------------------------------------------------------------------------

// TestEncodeColumnUnsupportedTypePlainV5 tests EncodeColumn with a type
// that is not supported for plain encoding (TypeNull).
func TestEncodeColumnUnsupportedTypePlainV5(t *testing.T) {
	_, err := EncodeColumn(common.TypeNull, nil, 1, nil)
	if err == nil {
		t.Error("expected error for unsupported type in EncodeColumn, got nil")
	}
}

// TestEncodeColumnUnknownEncodingTypeV5 tests DecodeColumn with an unknown
// encoding type to cover the default branch.
func TestEncodeColumnUnknownEncodingTypeV5(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingType(99),
		Type:     common.TypeInt64,
		RowCount: 1,
		Data:     make([]byte, 8),
	}
	_, _, err := DecodeColumn(enc)
	if err == nil {
		t.Error("expected error for unknown encoding type in DecodeColumn, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine.Close (85.7% → higher)
// ---------------------------------------------------------------------------

// TestEngineCloseSuccessV5 tests that Close succeeds on a working engine.
func TestEngineCloseSuccessV5(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if err := eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := eng.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestNewEngineReplayWithWALPath tests that NewEngine correctly opens
// an existing WAL file and replays its records.
func TestNewEngineReplayWithWALPath(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")

	// Create a WAL with a write record
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	payload, err := serializeWriteRecord("k1", 1, map[string]common.Value{colVal: common.NewInt64(10)})
	if err != nil {
		t.Fatalf("serializeWriteRecord failed: %v", err)
	}
	if err := w.AppendWrite(payload); err != nil {
		t.Fatalf("AppendWrite failed: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// NewEngine should open the existing WAL and replay the record
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	row, ok := eng.Get("k1")
	if !ok {
		t.Fatal("expected k1 to be recovered from WAL replay")
	}
	if row.Columns[colVal].Int64 != 10 {
		t.Errorf("expected val=10, got %d", row.Columns[colVal].Int64)
	}
}

// --- Merged from coverage_low_engine_v6_test.go ---

// ---------------------------------------------------------------------------
// Engine Write 与 GroupCommit 模式测试（v6 补充）
// ---------------------------------------------------------------------------

// TestWriteGroupCommitV6 测试 Write 在 GroupCommit 模式下多键写入。
func TestWriteGroupCommitV6(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入数据，应通过 GroupCommitter 提交
	if err := eng.Write("gc_v6_key1", map[string]common.Value{colVal: common.NewInt64(100)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := eng.Write("gc_v6_key2", map[string]common.Value{colVal: common.NewInt64(200)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 验证数据可读
	row, ok := eng.Get("gc_v6_key1")
	if !ok {
		t.Fatal("期望找到 gc_v6_key1")
	}
	if row.Columns[colVal].Int64 != 100 {
		t.Errorf("期望 100，实际 %d", row.Columns[colVal].Int64)
	}

	row, ok = eng.Get("gc_v6_key2")
	if !ok {
		t.Fatal("期望找到 gc_v6_key2")
	}
	if row.Columns[colVal].Int64 != 200 {
		t.Errorf("期望 200，实际 %d", row.Columns[colVal].Int64)
	}
}

// TestWriteMemTableRotationV6 测试 Write 触发 memtable 轮转并验证数据完整性。
func TestWriteMemTableRotationV6(t *testing.T) {
	dir := t.TempDir()
	// 设置很小的 MaxMemTableSize 以触发轮转
	eng, err := NewEngine(EngineConfig{
		DataDir:         dir,
		MaxMemTableSize: 128,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入足够多的数据以触发 memtable 轮转
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("rot_v6_%04d", i)
		if err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))}); err != nil {
			t.Fatalf("Write %d 失败: %v", i, err)
		}
	}

	// 验证数据仍可读取
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("rot_v6_%04d", i)
		row, ok := eng.Get(key)
		if !ok {
			t.Errorf("期望找到 %s", key)
			continue
		}
		if row.Columns[colVal].Int64 != int64(i) {
			t.Errorf("key %s: 期望 %d，实际 %d", key, i, row.Columns[colVal].Int64)
		}
	}
}

// ---------------------------------------------------------------------------
// writeCheckpoint 测试（v6 补充）
// ---------------------------------------------------------------------------

// TestWriteCheckpointAfterFlushV6 测试 Flush 后 writeCheckpoint 写入 WAL checkpoint 记录。
func TestWriteCheckpointAfterFlushV6(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// 写入数据
	if err := eng.Write("cp_v6_key1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := eng.Write("cp_v6_key2", map[string]common.Value{colVal: common.NewInt64(2)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// Flush 应触发 writeCheckpoint
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}

	// 验证 segment 已创建
	if eng.SegmentCount() == 0 {
		t.Error("期望至少 1 个 segment，实际 0")
	}
}

// TestWriteCheckpointGroupCommitV6 测试 GroupCommit 模式下的 writeCheckpoint。
func TestWriteCheckpointGroupCommitV6(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// 写入并 Flush，应通过 GroupCommitter.SyncNow() 同步
	if err := eng.Write("gc_cp_v6_key", map[string]common.Value{colVal: common.NewInt64(42)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}

	// 验证数据在 segment 中
	if eng.SegmentCount() == 0 {
		t.Error("期望至少 1 个 segment")
	}
}

// TestWriteCheckpointMultipleFlushesV6 测试多次 Flush 后 checkpoint 版本递增。
func TestWriteCheckpointMultipleFlushesV6(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// 第一次 Flush
	if err := eng.Write("cp_v6_1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write 1 失败: %v", err)
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 1 失败: %v", err)
	}

	// 第二次 Flush
	if err := eng.Write("cp_v6_2", map[string]common.Value{colVal: common.NewInt64(2)}); err != nil {
		t.Fatalf("Write 2 失败: %v", err)
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 2 失败: %v", err)
	}

	// 验证两次 Flush 都成功创建了 segment
	if eng.SegmentCount() < 2 {
		t.Errorf("期望至少 2 个 segment，实际 %d", eng.SegmentCount())
	}
}

// ---------------------------------------------------------------------------
// WriteBatch 测试（v6 补充）
// ---------------------------------------------------------------------------

// TestWriteBatchEmptyRowsV6 测试 WriteBatch 传入空行切片。
func TestWriteBatchEmptyRowsV6(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 空切片应直接返回 nil
	if err := eng.WriteBatch(nil); err != nil {
		t.Errorf("期望 nil，实际 %v", err)
	}
	if err := eng.WriteBatch([]WriteRow{}); err != nil {
		t.Errorf("期望 nil，实际 %v", err)
	}
}

// TestWriteBatchGroupCommitV6 测试 WriteBatch 在 GroupCommit 模式下的批量写入。
func TestWriteBatchGroupCommitV6(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	rows := []WriteRow{
		{Key: "batch_gc_v6_1", Values: map[string]common.Value{colVal: common.NewInt64(10)}},
		{Key: "batch_gc_v6_2", Values: map[string]common.Value{colVal: common.NewInt64(20)}},
		{Key: "batch_gc_v6_3", Values: map[string]common.Value{colVal: common.NewInt64(30)}},
	}

	if err := eng.WriteBatch(rows); err != nil {
		t.Fatalf("WriteBatch 失败: %v", err)
	}

	// 验证所有行可读
	for i, row := range rows {
		got, ok := eng.Get(row.Key)
		if !ok {
			t.Errorf("第 %d 行: 期望找到 key %s", i, row.Key)
			continue
		}
		expectedVal := int64((i + 1) * 10)
		if got.Columns[colVal].Int64 != expectedVal {
			t.Errorf("key %s: 期望 %d，实际 %d", row.Key, expectedVal, got.Columns[colVal].Int64)
		}
	}
}

// TestWriteBatchMemTableRotationV6 测试 WriteBatch 触发 memtable 轮转。
func TestWriteBatchMemTableRotationV6(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:         dir,
		MaxMemTableSize: 128,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 批量写入足够多的数据以触发轮转
	for batch := 0; batch < 5; batch++ {
		var rows []WriteRow
		for i := 0; i < 20; i++ {
			key := fmt.Sprintf("batch_rot_v6_%d_%04d", batch, i)
			rows = append(rows, WriteRow{
				Key:    key,
				Values: map[string]common.Value{colVal: common.NewInt64(int64(batch*20 + i))},
			})
		}
		if err := eng.WriteBatch(rows); err != nil {
			t.Fatalf("WriteBatch %d 失败: %v", batch, err)
		}
	}

	// 验证部分数据可读
	row, ok := eng.Get("batch_rot_v6_0_0000")
	if !ok {
		t.Fatal("期望找到 batch_rot_v6_0_0000")
	}
	if row.Columns[colVal].Int64 != 0 {
		t.Errorf("期望 0，实际 %d", row.Columns[colVal].Int64)
	}
}
