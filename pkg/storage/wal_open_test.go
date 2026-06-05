package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenWALTruncateAfterPartialData 测试 WAL 文件末尾有垃圾数据时，OpenWAL 会截断到有效偏移量
func TestOpenWALTruncateAfterPartialData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	// 创建 WAL 并写入记录
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	_ = w.AppendWrite([]byte("valid1"))
	_ = w.AppendWrite([]byte("valid2"))
	_ = w.Sync()
	_ = w.Close()

	// 读取文件内容，获取有效数据的长度
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read WAL file: %v", err)
	}
	validSize := len(data)

	// 在末尾追加垃圾数据（模拟崩溃时的部分写入）
	garbage := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04}
	modifiedData := make([]byte, validSize+len(garbage))
	copy(modifiedData, data)
	copy(modifiedData[validSize:], garbage)

	if err := os.WriteFile(path, modifiedData, 0644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	// 打开 WAL，验证文件被截断到有效偏移量
	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	// 应该恢复 2 条有效记录
	if len(recs) != 2 {
		t.Fatalf("expected 2 valid records, got %d", len(recs))
	}
	if string(recs[0].Payload) != "valid1" {
		t.Errorf("record 0: expected 'valid1', got %q", string(recs[0].Payload))
	}
	if string(recs[1].Payload) != "valid2" {
		t.Errorf("record 1: expected 'valid2', got %q", string(recs[1].Payload))
	}

	// 验证文件已被截断到有效偏移量
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat WAL file: %v", err)
	}
	if fileInfo.Size() != int64(validSize) {
		t.Errorf("expected file size %d after truncation, got %d", validSize, fileInfo.Size())
	}

	// 验证恢复后可以继续追加
	if err := recovered.AppendWrite([]byte("after_truncate")); err != nil {
		t.Fatalf("AppendWrite after truncate recovery failed: %v", err)
	}
}

// TestWALMaybeRotateMaxSizeExceeded 测试 WAL 文件超过 maxSize 时正确触发轮转
func TestWALMaybeRotateMaxSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	// 设置一个很小的 maxSize，写入多条记录后触发轮转
	w.maxSize = walMetaSize + 10

	// 写入足够多的记录以触发轮转
	for i := 0; i < 5; i++ {
		if err := w.AppendWrite([]byte("trigger rotation data")); err != nil {
			t.Fatalf("AppendWrite #%d failed: %v", i, err)
		}
	}

	// 验证轮转后 offset 被重置（新文件写入了一条或多条记录）
	if w.Size() == 0 {
		t.Error("expected non-zero size after rotation and write")
	}

	// 验证 .prev 文件存在
	_, err = os.Stat(path + ".prev")
	if err != nil {
		t.Fatalf("expected .prev file after rotation: %v", err)
	}

	_ = w.Close()
}

func TestWALAppendBatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	records := []BatchRecord{
		{Type: walTypeWrite, Payload: []byte("batch_record_1")},
		{Type: walTypeWrite, Payload: []byte("batch_record_2")},
		{Type: walTypeWrite, Payload: []byte("batch_record_3")},
	}
	if err := w.AppendBatch(records); err != nil {
		t.Fatalf("AppendBatch failed: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	_ = w.Close()

	// Verify by OpenWAL
	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	for i, rec := range recs {
		expected := fmt.Sprintf("batch_record_%d", i+1)
		if string(rec.Payload) != expected {
			t.Errorf("record %d: expected %q, got %q", i, expected, string(rec.Payload))
		}
	}
}

func TestWALAppendBatchPayloadTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	largePayload := make([]byte, maxRecordPayload+1)
	records := []BatchRecord{
		{Type: walTypeWrite, Payload: largePayload},
	}
	err = w.AppendBatch(records)
	if err == nil {
		t.Fatal("expected error for oversized payload in AppendBatch")
	}
}

func TestWALAppendBatchWriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	// Close the WAL file first to trigger write error
	_ = w.Close()

	records := []BatchRecord{
		{Type: walTypeWrite, Payload: []byte("should fail")},
	}
	err = w.AppendBatch(records)
	if err == nil {
		t.Fatal("expected error when writing to closed WAL file")
	}
}

func TestOpenWALFileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.wal")

	_, _, err := OpenWAL(path)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	// The error is wrapped by fmt.Errorf, so we check the error message contains "no such file"
	if !os.IsNotExist(err) {
		// Wrapped errors don't match os.IsNotExist, so check the error chain
		if !strings.Contains(err.Error(), "no such file") && !strings.Contains(err.Error(), "cannot find") {
			t.Errorf("expected file-not-found error, got: %v", err)
		}
	}
}

func TestOpenWALWithCorruptHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.wal")

	// Create a valid WAL with some records first
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	_ = w.AppendWrite([]byte("valid_record"))
	_ = w.Sync()
	_ = w.Close()

	// Read the valid file content
	validData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// Append corrupt data: a header with totalLen too small (less than walTypeSize+walCRCSize)
	// totalLen = 1 (too small, should be at least walTypeSize+walCRCSize = 5)
	corruptData := make([]byte, len(validData), len(validData)+4)
	copy(corruptData, validData)
	corruptData = append(corruptData, []byte{1, 0, 0, 0}...) // totalLen=1 (invalid)
	if err := os.WriteFile(path, corruptData, 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	// OpenWAL should return records up to the corruption point
	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	if len(recs) != 1 {
		t.Fatalf("expected 1 valid record before corruption, got %d", len(recs))
	}
	if string(recs[0].Payload) != "valid_record" {
		t.Errorf("record 0: expected 'valid_record', got %q", string(recs[0].Payload))
	}
}

// TestOpenWALNotExist tests that OpenWAL returns an error wrapping os.ErrNotExist
// when the file path is in a non-existent directory.
func TestOpenWALNotExist(t *testing.T) {
	_, _, err := OpenWAL("/nonexistent/directory/test.wal")
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
	// The error is wrapped by fmt.Errorf, so os.IsNotExist may not match.
	// Verify the error chain contains a "no such file" indicator.
	if !os.IsNotExist(err) && !strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected file-not-found error, got: %v", err)
	}
}

// TestOpenWALReadOnlyFile tests that OpenWAL fails when the WAL file
// is read-only (cannot be opened with O_RDWR for truncate/seek).
func TestOpenWALReadOnlyFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: test requires non-root user")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.wal")

	// Create a WAL file with valid records
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	_ = w.AppendWrite([]byte("record1"))
	_ = w.AppendWrite([]byte("record2"))
	_ = w.Sync()
	_ = w.Close()

	// Make the file read-only
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	// Try to open the WAL - should fail because OpenWAL uses O_RDWR
	_, _, err = OpenWAL(path)
	if err == nil {
		t.Fatal("expected error when opening read-only WAL file, got nil")
	}
	// The error should NOT be os.IsNotExist (the file exists, just not writable)
	if os.IsNotExist(err) {
		t.Errorf("expected non-NotExist error for read-only file, got NotExist: %v", err)
	}
}
