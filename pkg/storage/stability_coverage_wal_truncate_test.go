package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// WAL.Truncate: 完整覆盖
// ---------------------------------------------------------------------------

func TestWALTruncate_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}

	// 写入一些数据
	for i := 0; i < 10; i++ {
		if err := w.AppendWrite([]byte("data")); err != nil {
			t.Fatalf("AppendWrite: %v", err)
		}
	}

	sizeBefore := w.Size()
	if sizeBefore == 0 {
		t.Fatal("expected non-zero size before truncate")
	}

	// 执行 Truncate
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate: %v", err)
	}

	sizeAfter := w.Size()
	if sizeAfter != 0 {
		t.Errorf("expected size 0 after truncate, got %d", sizeAfter)
	}

	// Truncate 后仍可写入
	if err := w.AppendWrite([]byte("after-truncate")); err != nil {
		t.Fatalf("AppendWrite after truncate: %v", err)
	}
	if w.Size() == 0 {
		t.Error("expected non-zero size after writing post-truncate")
	}

	_ = w.Close()
}

func TestWALTruncate_EmptyWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}

	// 空文件也可以 Truncate
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate empty WAL: %v", err)
	}

	if w.Size() != 0 {
		t.Errorf("expected size 0, got %d", w.Size())
	}

	_ = w.Close()
}

func TestWALTruncate_MultipleTimes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}
	defer func() { _ = w.Close() }()

	// 第一次写入 + Truncate
	w.AppendWrite([]byte("batch1"))
	w.Truncate()

	// 第二次写入 + Truncate
	w.AppendWrite([]byte("batch2"))
	w.Truncate()

	// 第三次写入
	w.AppendWrite([]byte("batch3"))
	if w.Size() == 0 {
		t.Error("expected non-zero size after third write")
	}
}

func TestWALTruncate_DataRecoverable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}

	// 写入并 Truncate
	w.AppendWrite([]byte("old-data"))
	w.Truncate()

	// 写入新数据
	w.AppendWrite([]byte("new-data"))
	w.AppendCommit([]byte("commit"))
	_ = w.Close()

	// 重新打开，验证只有新数据
	w2, records, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL: %v", err)
	}
	defer func() { _ = w2.Close() }()

	if len(records) != 2 {
		t.Fatalf("expected 2 records after truncate+write, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// cleanupSegmentFile: 覆盖 nil 段和空路径
// ---------------------------------------------------------------------------

func TestCleanupSegmentFile_NilSegment(_ *testing.T) {
	// 不应 panic
	cleanupSegmentFile(nil)
}

func TestCleanupSegmentFile_EmptyPath(_ *testing.T) {
	seg := &Segment{FilePath: ""}
	cleanupSegmentFile(seg) // 不应 panic 或报错
}

func TestCleanupSegmentFile_NonExistentFile(_ *testing.T) {
	seg := &Segment{FilePath: "/nonexistent/path/segment.dat"}
	cleanupSegmentFile(seg) // 文件不存在，应忽略错误
}

func TestCleanupSegmentFile_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "segment.dat")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	seg := &Segment{FilePath: filePath}
	cleanupSegmentFile(seg)

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

// ---------------------------------------------------------------------------
// encodeUint64Batch / encodeFloat64Batch: 边界情况
// ---------------------------------------------------------------------------

func TestEncodeUint64Batch_Empty(t *testing.T) {
	result := encodeUint64Batch(nil, 0)
	if len(result) != 0 {
		t.Errorf("expected empty slice for 0 rows, got %d bytes", len(result))
	}
}

func TestEncodeUint64Batch_SingleValue(t *testing.T) {
	vals := []int64{42}
	result := encodeUint64Batch(vals, 1)
	if len(result) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(result))
	}
	// 验证小端编码
	le := uint64(vals[0])
	if result[0] != byte(le) || result[7] != byte(le>>56) {
		t.Errorf("unexpected encoding result: %v", result)
	}
}

func TestEncodeUint64Batch_MultipleValues(t *testing.T) {
	vals := []int64{1, 2, 3, 256}
	result := encodeUint64Batch(vals, 4)
	if len(result) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(result))
	}
}

func TestEncodeFloat64Batch_Empty(t *testing.T) {
	result := encodeFloat64Batch(nil, 0)
	if len(result) != 0 {
		t.Errorf("expected empty slice for 0 rows, got %d bytes", len(result))
	}
}

func TestEncodeFloat64Batch_SingleValue(t *testing.T) {
	vals := []float64{3.14}
	result := encodeFloat64Batch(vals, 1)
	if len(result) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(result))
	}
}

func TestEncodeFloat64Batch_MultipleValues(t *testing.T) {
	vals := []float64{1.0, 2.5, -3.14, 0.0}
	result := encodeFloat64Batch(vals, 4)
	if len(result) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(result))
	}
}
