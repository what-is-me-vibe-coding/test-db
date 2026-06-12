package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	if w.Size() != 0 {
		t.Errorf("expected size 0, got %d", w.Size())
	}

	_ = w.Close()

	_, err = os.Stat(path)
	if err != nil {
		t.Fatalf("wal file not created: %v", err)
	}
}

func TestWALAppendWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	payload := []byte("hello wal")
	if err := w.AppendWrite(payload); err != nil {
		t.Fatalf("AppendWrite failed: %v", err)
	}

	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if w.Size() == 0 {
		t.Fatal("expected non-zero size after write")
	}
}

func TestWALAppendCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	if err := w.AppendCommit([]byte("commit data")); err != nil {
		t.Fatalf("AppendCommit failed: %v", err)
	}
}

func TestWALAppendCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	if err := w.AppendCheckpoint([]byte("checkpoint data")); err != nil {
		t.Fatalf("AppendCheckpoint failed: %v", err)
	}
}

func TestWALLargePayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	largePayload := make([]byte, maxRecordPayload+1)
	err = w.AppendWrite(largePayload)
	if err == nil {
		t.Fatal("expected error for oversized payload")
	}
}

func TestWALRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	w.maxSize = walMetaSize + 100

	for i := 0; i < 10; i++ {
		payload := []byte("test data for rotation")
		if err := w.AppendWrite(payload); err != nil {
			t.Fatalf("AppendWrite #%d failed: %v", i, err)
		}
	}

	_, err = os.Stat(path + ".prev")
	if err != nil {
		t.Fatalf("previous WAL file not created: %v", err)
	}
}

func TestWALConcurrentWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	const goroutines = 10
	const writesPerRoutine = 100
	done := make(chan bool)

	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < writesPerRoutine; j++ {
				if err := w.AppendWrite([]byte("concurrent")); err != nil {
					t.Errorf("concurrent write failed: %v", err)
				}
			}
			done <- true
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}

	_ = w.Sync()
	_ = w.Close()

	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	expected := goroutines * writesPerRoutine
	if len(recs) != expected {
		t.Errorf("expected %d records, got %d", expected, len(recs))
	}
}

func TestCreateWALInvalidDir(t *testing.T) {
	// Try to create WAL in a non-existent directory
	_, err := CreateWAL("/nonexistent/dir/test.wal")
	if err == nil {
		t.Error("expected error creating WAL in invalid directory")
	}
}

func TestWALMaybeRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	// Set a very small max size to trigger rotation quickly
	w.maxSize = walMetaSize + 50

	// Write enough data to trigger rotation
	for i := 0; i < 5; i++ {
		if err := w.AppendWrite([]byte("test data for rotation")); err != nil {
			t.Fatalf("AppendWrite #%d failed: %v", i, err)
		}
	}

	_ = w.Close()

	// Verify the .prev file was created (rotation happened)
	_, err = os.Stat(path + ".prev")
	if err != nil {
		t.Fatalf("previous WAL file not created after rotation: %v", err)
	}

	// Verify the current WAL file still exists
	_, err = os.Stat(path)
	if err != nil {
		t.Fatalf("current WAL file not found: %v", err)
	}
}

func TestWALTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	// Write some data
	_ = w.AppendWrite([]byte("data to be truncated"))
	_ = w.Sync()

	if w.Size() == 0 {
		t.Fatal("expected non-zero size before truncate")
	}

	// Truncate the WAL
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	if w.Size() != 0 {
		t.Errorf("expected size 0 after truncate, got %d", w.Size())
	}

	// Verify we can still write after truncation
	if err := w.AppendWrite([]byte("after truncate")); err != nil {
		t.Fatalf("AppendWrite after truncate failed: %v", err)
	}

	_ = w.Close()
}

func TestWALSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	initialSize := w.Size()
	if initialSize != 0 {
		t.Errorf("expected initial size 0, got %d", initialSize)
	}

	_ = w.AppendWrite([]byte("test"))

	afterSize := w.Size()
	if afterSize <= initialSize {
		t.Errorf("expected size to increase after write, got %d", afterSize)
	}
}

// TestOpenWALPermissionError 测试打开目录路径作为 WAL 文件（应得到非 NotExist 错误）
func TestOpenWALPermissionError(t *testing.T) {
	dir := t.TempDir()
	// 尝试打开目录路径作为 WAL 文件，应得到非 NotExist 错误
	_, _, err := OpenWAL(dir)
	if err == nil {
		t.Fatal("expected error when opening directory as WAL file")
	}
	// 确保不是 NotExist 错误
	if os.IsNotExist(err) {
		t.Errorf("expected non-NotExist error, got NotExist: %v", err)
	}
}

// TestOpenWALWithValidOffsetRecovery 测试 OpenWAL 恢复后偏移量正确设置，且可以继续追加
func TestOpenWALWithValidOffsetRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	// 创建 WAL 并写入多条记录
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	_ = w.AppendWrite([]byte("record1"))
	_ = w.AppendWrite([]byte("record2"))
	_ = w.AppendWrite([]byte("record3"))
	_ = w.Sync()

	sizeAfterWrite := w.Size()
	if sizeAfterWrite == 0 {
		t.Fatal("expected non-zero size after writes")
	}
	_ = w.Close()

	// 重新打开 WAL，验证偏移量正确恢复
	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}

	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}

	// 验证恢复后的偏移量与写入后的一致
	if recovered.Size() != sizeAfterWrite {
		t.Errorf("expected offset %d after recovery, got %d", sizeAfterWrite, recovered.Size())
	}

	// 验证恢复后可以继续追加记录
	if err := recovered.AppendWrite([]byte("record4")); err != nil {
		t.Fatalf("AppendWrite after recovery failed: %v", err)
	}
	_ = recovered.Sync()
	_ = recovered.Close()

	// 再次打开验证所有 4 条记录
	recovered2, recs2, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("second OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered2.Close() }()

	if len(recs2) != 4 {
		t.Fatalf("expected 4 records, got %d", len(recs2))
	}
}

// TestOpenWALNotExistErrMsg 测试打开不存在的 WAL 文件应返回包含 "wal open" 的错误
func TestOpenWALNotExistErrMsg(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.wal")

	_, _, err := OpenWAL(path)
	if err == nil {
		t.Fatal("expected error when opening non-existent WAL file")
	}

	// 验证错误信息包含 "wal open"
	if !strings.Contains(err.Error(), "wal open") {
		t.Errorf("expected error containing 'wal open', got: %v", err)
	}
}

// TestOpenWALCorruptedFile 测试打开包含损坏数据的 WAL 文件，应只返回有效记录
func TestOpenWALCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.wal")

	// 创建 WAL 并写入有效记录
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	_ = w.AppendWrite([]byte("valid1"))
	_ = w.AppendWrite([]byte("valid2"))
	_ = w.Sync()
	_ = w.Close()

	// 在文件末尾追加垃圾数据模拟损坏
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("open for corruption failed: %v", err)
	}
	_, _ = f.Write([]byte("garbage data that is not a valid WAL record"))
	_ = f.Close()

	// 重新打开 WAL，应成功且只返回有效记录
	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL on corrupted file should succeed, got error: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	if len(recs) != 2 {
		t.Fatalf("expected 2 valid records, got %d", len(recs))
	}

	if string(recs[0].Payload) != "valid1" {
		t.Errorf("expected first record 'valid1', got %q", string(recs[0].Payload))
	}
	if string(recs[1].Payload) != "valid2" {
		t.Errorf("expected second record 'valid2', got %q", string(recs[1].Payload))
	}
}

// TestMaybeRotateNoRotationNeeded 测试当 offset 小于 maxSize 时，maybeRotate 不执行任何操作
func TestMaybeRotateNoRotationNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	// maxSize 保持默认的 64MB，写入少量数据不会触发轮转
	sizeBefore := w.Size()
	_ = w.AppendWrite([]byte("small data"))
	sizeAfter := w.Size()

	if sizeAfter <= sizeBefore {
		t.Errorf("expected size to increase after write, got before=%d after=%d", sizeBefore, sizeAfter)
	}

	// 验证没有产生 .prev 文件（未触发轮转）
	if _, err := os.Stat(path + ".prev"); err == nil {
		t.Error("expected no .prev file when rotation is not needed")
	}
}
