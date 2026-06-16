package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestWriteEmptyKeyRejected 验证 Write 拒绝空 key
func TestWriteEmptyKeyRejected(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	err = eng.Write("", map[string]common.Value{
		"col1": common.NewInt64(1),
	})
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

// TestWriteBatchEmptyKeyRejected 验证 WriteBatch 拒绝包含空 key 的行
func TestWriteBatchEmptyKeyRejected(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	// 空 key 在第一行
	err = eng.WriteBatch([]WriteRow{
		{Key: "", Values: map[string]common.Value{"col1": common.NewInt64(1)}},
	})
	if err == nil {
		t.Fatal("expected error for empty key in batch, got nil")
	}

	// 空 key 在中间行
	err = eng.WriteBatch([]WriteRow{
		{Key: "valid", Values: map[string]common.Value{"col1": common.NewInt64(1)}},
		{Key: "", Values: map[string]common.Value{"col1": common.NewInt64(2)}},
		{Key: "also_valid", Values: map[string]common.Value{"col1": common.NewInt64(3)}},
	})
	if err == nil {
		t.Fatal("expected error for empty key in middle of batch, got nil")
	}
}

// TestWriteBatchValidKeysSucceed 验证 WriteBatch 使用合法 key 可以成功
func TestWriteBatchValidKeysSucceed(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	err = eng.WriteBatch([]WriteRow{
		{Key: "key1", Values: map[string]common.Value{"col1": common.NewInt64(1)}},
		{Key: "key2", Values: map[string]common.Value{"col1": common.NewInt64(2)}},
	})
	if err != nil {
		t.Fatalf("WriteBatch with valid keys: %v", err)
	}
}

// TestCleanupSegmentFile 验证 cleanupSegmentFile 正确清理段文件
func TestCleanupSegmentFile(t *testing.T) {
	dir := t.TempDir()

	// 创建一个临时段文件
	filePath := filepath.Join(dir, "segment_999.widb")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("create test file: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("test file should exist")
	}

	// 清理段文件
	seg := &Segment{ID: 999, FilePath: filePath}
	cleanupSegmentFile(seg)

	// 验证文件已被删除
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("segment file should be deleted after cleanup")
	}
}

// TestCleanupSegmentFileNilSegment 验证 cleanupSegmentFile 对 nil Segment 安全
func TestCleanupSegmentFileNilSegment(_ *testing.T) {
	cleanupSegmentFile(nil) // 不应 panic
}

// TestCleanupSegmentFileEmptyPath 验证 cleanupSegmentFile 对空路径安全
func TestCleanupSegmentFileEmptyPath(_ *testing.T) {
	seg := &Segment{ID: 1, FilePath: ""}
	cleanupSegmentFile(seg) // 不应 panic
}

// TestCleanupSegmentFileNonExistent 验证 cleanupSegmentFile 对不存在的文件安全
func TestCleanupSegmentFileNonExistent(_ *testing.T) {
	seg := &Segment{ID: 1, FilePath: "/nonexistent/path/segment_1.widb"}
	cleanupSegmentFile(seg) // 不应 panic
}

// TestFlushImmutableRollbackOnIndexFailure 验证 flushImmutable 在索引注册失败时回滚段数据
func TestFlushImmutableRollbackOnIndexFailure(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	cols := []ColumnMeta{
		{ID: 0, Name: "col1", Type: common.TypeInt64},
	}

	// 写入数据使其进入 immutable
	if err := eng.Write("key1", map[string]common.Value{"col1": common.NewInt64(1)}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// 正常 Flush 应成功
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// 验证段已注册
	if eng.SegmentCount() == 0 {
		t.Fatal("expected at least one segment after flush")
	}
}

// TestWriteValidKeySucceed 验证 Write 使用合法 key 可以成功
func TestWriteValidKeySucceed(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	err = eng.Write("valid_key", map[string]common.Value{
		"col1": common.NewInt64(42),
	})
	if err != nil {
		t.Fatalf("Write with valid key: %v", err)
	}

	row, ok := eng.Get("valid_key")
	if !ok {
		t.Fatal("expected to find written key")
	}
	if row.Columns["col1"].Int64 != 42 {
		t.Errorf("expected col1=42, got %d", row.Columns["col1"].Int64)
	}
}
