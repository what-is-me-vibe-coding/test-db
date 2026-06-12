package storage

import (
	"path/filepath"
	"testing"
)

// TestWALRecoverOpen_FailurePath 测试 recoverOpen 在文件无法打开时的失败路径。
// 通过设置一个不存在的路径来触发 os.OpenFile 失败。
func TestWALRecoverOpen_FailurePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	_ = w.Close()

	// 将 WAL 的路径修改为不存在的目录
	w.path = filepath.Join(dir, "nonexistent_dir", "fail.wal")

	// 关闭当前文件句柄
	_ = w.file.Close()

	// recoverOpen 应该失败但不 panic
	w.recoverOpen()
	// 验证 file 被重新赋值（失败时仍为 nil 或旧值）
	// 关键是不 panic
}

// TestWALRecoverOpen_SuccessPath 测试 recoverOpen 成功路径。
func TestWALRecoverOpen_SuccessPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	// 关闭文件后，recoverOpen 应该能重新打开
	_ = w.file.Close()
	w.recoverOpen()

	// 验证文件已重新打开（可以写入）
	if err := w.AppendWrite([]byte("after_recover")); err != nil {
		t.Fatalf("AppendWrite after recoverOpen failed: %v", err)
	}
	_ = w.Close()
}
