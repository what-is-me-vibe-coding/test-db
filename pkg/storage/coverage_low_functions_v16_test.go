package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestEngineWriteGroupCommitWithClosedWAL 验证 GroupCommit 模式下 WAL 关闭后写入返回错误。
func TestEngineWriteGroupCommitWithClosedWAL(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir:      t.TempDir(),
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	// 先正常写入一条数据确保 groupCommitter 已初始化
	_ = eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)})

	// 关闭 WAL 以触发 AppendWrite 错误路径
	_ = eng.wal.Close()

	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when writing with closed WAL in GroupCommit mode")
	}
}

// TestEngineFlushWithGroupCommit 验证 GroupCommit 模式下 Flush 触发 writeCheckpoint 中的 SyncNow 路径。
func TestEngineFlushWithGroupCommit(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir:      t.TempDir(),
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("key2", map[string]common.Value{colVal: common.NewInt64(2)})

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush with group commit: %v", err)
	}

	segs := eng.Segments()
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
}

// TestEngineFlushWithClosedWAL 验证 Flush 在 WAL 关闭后返回错误（覆盖 writeCheckpoint 错误路径）。
func TestEngineFlushWithClosedWAL(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	// 关闭 WAL 以触发 writeCheckpoint 中的 AppendCheckpoint 错误
	_ = eng.wal.Close()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	err = eng.Flush(cols)
	if err == nil {
		t.Error("expected error when flushing with closed WAL")
	}
}

// TestOpenWALFileNotExist 验证 OpenWAL 在文件不存在时返回错误。
func TestOpenWALFileNotExist(t *testing.T) {
	_, _, err := OpenWAL(filepath.Join(t.TempDir(), "nonexistent.log"))
	if err == nil {
		t.Error("expected error when opening non-existent WAL file")
	}
}

// TestOpenWALWithCorruptedData 验证 OpenWAL 能处理损坏的 WAL 数据。
func TestOpenWALWithCorruptedData(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")

	// 创建一个包含损坏数据的 WAL 文件
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	// 写入一些无效数据
	_, _ = f.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00})
	_ = f.Close()

	wal, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("open WAL with corrupted data should not error: %v", err)
	}
	defer func() { _ = wal.Close() }()

	// 损坏记录应被跳过，不应返回任何有效记录
	if len(records) != 0 {
		t.Errorf("expected 0 records from corrupted WAL, got %d", len(records))
	}
}

// TestOpenWALWithValidAndCorruptedData 验证 OpenWAL 能正确回放有效记录并跳过损坏部分。
func TestOpenWALWithValidAndCorruptedData(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")

	// 先创建一个有效的 WAL 并写入记录
	wal, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("create WAL: %v", err)
	}
	_ = wal.AppendWrite([]byte("valid_record"))
	_ = wal.Sync()
	_ = wal.Close()

	// 在有效数据后追加损坏数据
	f, err := os.OpenFile(walPath, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	_, _ = f.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00})
	_ = f.Close()

	// 重新打开 WAL，应能回放有效记录
	wal2, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	defer func() { _ = wal2.Close() }()

	if len(records) != 1 {
		t.Errorf("expected 1 valid record, got %d", len(records))
	}
}

// TestGroupCommitterDoubleClose 验证 GroupCommitter 重复关闭不会 panic。
func TestGroupCommitterDoubleClose(t *testing.T) {
	wal, err := CreateWAL(t.TempDir() + "/wal.log")
	if err != nil {
		t.Fatalf("create wal: %v", err)
	}
	defer func() { _ = wal.Close() }()

	gc := NewGroupCommitter(wal, 1*time.Millisecond)

	// 第一次关闭
	gc.Close()

	// 第二次关闭应安全返回（覆盖已关闭路径）
	gc.Close()
}

// TestEngineCloseWithScheduler 验证引擎关闭时正确停止调度器。
func TestEngineCloseWithScheduler(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	eng.StartScheduler(SchedulerConfig{
		FlushInterval:    1 * time.Hour,
		CompactInterval:  1 * time.Hour,
		WALCleanInterval: 1 * time.Hour,
	})

	// 写入数据后关闭，调度器应被正确停止
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	if err := eng.Close(); err != nil {
		t.Fatalf("close engine with scheduler: %v", err)
	}
}

// TestEngineCloseWithGroupCommitAndData 验证 GroupCommit 模式下引擎关闭时数据正确持久化。
func TestEngineCloseWithGroupCommitAndData(t *testing.T) {
	dir := t.TempDir()

	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(42)})

	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	// 重新打开验证数据
	eng2, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	defer func() { _ = eng2.Close() }()

	row, ok := eng2.Get("key1")
	if !ok {
		t.Fatal("key1 not found after recovery")
	}
	if v, exists := row.Columns[colVal]; !exists || v.Int64 != 42 {
		t.Errorf("expected val=42, got %v", v)
	}
}

// TestEngineWriteRotateMemTableInGroupCommit 验证 GroupCommit 模式下 MemTable 旋转正常工作。
func TestEngineWriteRotateMemTableInGroupCommit(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir:         t.TempDir(),
		MaxMemTableSize: 1,
		SyncMode:        SyncGroupCommit,
		SyncInterval:    1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入足够数据触发 MemTable 旋转
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		err := eng.Write(key, map[string]common.Value{
			colVal: common.NewString("long string to trigger rotation quickly"),
		})
		if err != nil {
			t.Fatalf("write %s: %v", key, err)
		}
	}
}

// TestEngineWriteWithWALSyncError 验证 Write 在 WAL sync 失败时返回错误（SyncEveryWrite 模式）。
func TestEngineWriteWithWALSyncError(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	// 正常写入一条
	_ = eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)})

	// 关闭 WAL 文件描述符以触发 sync 错误
	_ = eng.wal.Close()

	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when writing with closed WAL (SyncEveryWrite mode)")
	}
}

// TestOpenWALWithEmptyFileCoverage 验证 OpenWAL 能正确处理空文件。
func TestOpenWALWithEmptyFileCoverage(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")

	// 创建空文件
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_ = f.Close()

	wal, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("open empty WAL: %v", err)
	}
	defer func() { _ = wal.Close() }()

	if len(records) != 0 {
		t.Errorf("expected 0 records from empty WAL, got %d", len(records))
	}
}

// TestEngineFlushWithGroupCommitClosedWAL 验证 GroupCommit 模式下 Flush 在 WAL 关闭后返回错误。
func TestEngineFlushWithGroupCommitClosedWAL(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir:      t.TempDir(),
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	// 关闭 WAL 以触发 writeCheckpoint 错误
	_ = eng.wal.Close()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	err = eng.Flush(cols)
	if err == nil {
		t.Error("expected error when flushing with closed WAL in GroupCommit mode")
	}
}

// TestEngineStartSchedulerIdempotent 验证重复启动调度器不会创建多个实例。
func TestEngineStartSchedulerIdempotent(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cfg := SchedulerConfig{
		FlushInterval:    1 * time.Hour,
		CompactInterval:  1 * time.Hour,
		WALCleanInterval: 1 * time.Hour,
	}

	eng.StartScheduler(cfg)
	eng.StartScheduler(cfg) // 重复启动应被忽略

	_, ok := eng.SchedulerStats()
	if !ok {
		t.Error("expected scheduler to be running")
	}
}
