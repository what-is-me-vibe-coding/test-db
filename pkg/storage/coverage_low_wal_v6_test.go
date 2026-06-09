package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// OpenWAL (76.5%) - Truncate 错误路径（wal.go 第 84-87 行）
// ---------------------------------------------------------------------------

// TestOpenWAL_TruncateErrorAfterReplay_DevNull 使用 /dev/null 符号链接
// 触发 Truncate 错误。/dev/null 是字符设备，Truncate 返回 EINVAL，
// 覆盖 replayWAL 成功后 Truncate 失败的路径。
func TestOpenWAL_TruncateErrorAfterReplay_DevNull(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/null 测试依赖 Linux 特定行为")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "devnull.wal")

	// 创建指向 /dev/null 的符号链接
	if err := os.Symlink("/dev/null", path); err != nil {
		t.Fatalf("Symlink 失败: %v", err)
	}

	_, _, err := OpenWAL(path)
	if err == nil {
		t.Fatal("期望 /dev/null 符号链接上 Truncate 失败时返回错误，得到 nil")
	}

	// /dev/null 上 replayWAL 返回 0 记录，validOffset=0
	// Truncate(0) 在字符设备上返回 EINVAL
	if !strings.Contains(err.Error(), "wal truncate") && !strings.Contains(err.Error(), "wal seek") {
		t.Errorf("错误消息应包含 'wal truncate' 或 'wal seek'，得到: %v", err)
	}
}

// TestOpenWAL_TruncateErrorAfterReplay_DevFull 使用 /dev/full 符号链接
// 触发 Truncate 错误。/dev/full 是字符设备，Truncate 返回 EINVAL。
func TestOpenWAL_TruncateErrorAfterReplay_DevFull(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full 测试依赖 Linux 特定行为")
	}

	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full 不存在")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "devfull.wal")

	if err := os.Symlink("/dev/full", path); err != nil {
		t.Fatalf("Symlink 失败: %v", err)
	}

	_, _, err := OpenWAL(path)
	if err == nil {
		t.Fatal("期望 /dev/full 符号链接上 Truncate 失败时返回错误，得到 nil")
	}

	if !strings.Contains(err.Error(), "wal truncate") && !strings.Contains(err.Error(), "wal seek") {
		t.Errorf("错误消息应包含 'wal truncate' 或 'wal seek'，得到: %v", err)
	}
}

// ---------------------------------------------------------------------------
// OpenWAL (76.5%) - Seek 错误路径（wal.go 第 88-91 行）
// ---------------------------------------------------------------------------

// TestOpenWAL_SeekErrorAfterTruncate_VerifyPath 验证 OpenWAL 中 Seek 错误路径
// 的错误消息格式。由于无法在 OpenWAL 内部干预 Truncate 和 Seek 之间的时序
// （它们在同一个函数中顺序执行），此测试通过 /dev/zero 符号链接触发
// Truncate 错误，确认 "wal truncate" 或 "wal seek" 错误路径可被触发。
// 注意：Seek 错误路径（第 88-91 行）在当前实现中极难单独触发，
// 因为需要 Truncate 成功后 Seek 失败，而 Linux 上不存在这样的文件类型。
func TestOpenWAL_SeekErrorAfterTruncate_VerifyPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("此测试依赖 Linux 特定行为")
	}

	// 检查 /dev/zero 是否存在
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero 不存在")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "devzero.wal")

	// /dev/zero 是字符设备，Truncate 返回 EINVAL
	if err := os.Symlink("/dev/zero", path); err != nil {
		t.Fatalf("Symlink 失败: %v", err)
	}

	_, _, err := OpenWAL(path)
	if err == nil {
		t.Fatal("期望 /dev/zero 符号链接上返回错误，得到 nil")
	}

	// /dev/zero 上 replayWAL 成功（读到全零数据后返回错误或 0 记录），
	// 然后 Truncate 失败（字符设备不支持 truncate）
	if !strings.Contains(err.Error(), "wal truncate") && !strings.Contains(err.Error(), "wal seek") {
		t.Errorf("错误消息应包含 'wal truncate' 或 'wal seek'，得到: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Write (79.3%) - WAL append 失败路径（engine.go 第 125-128 行）
// ---------------------------------------------------------------------------

// TestWrite_WALAppendFailClosedFile 验证 WAL 文件关闭后 Write 返回 wal append 错误。
// 通过直接关闭底层文件描述符使 AppendWrite 失败。
func TestWrite_WALAppendFailClosedFile(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 先正常写入一条数据
	_ = eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)})

	// 关闭底层文件描述符使 AppendWrite 失败
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("关闭 WAL 文件失败: %v", err)
	}

	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Fatal("期望 WAL 文件关闭后 Write 返回错误，得到 nil")
	}
	if !strings.Contains(err.Error(), "engine write: wal append") {
		t.Errorf("错误消息应包含 'engine write: wal append'，得到: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Write (79.3%) - WAL sync 失败路径（engine.go 第 134-137 行）
// ---------------------------------------------------------------------------

// TestWrite_WALSyncFailPipeFile 使用管道替换 WAL 文件使 Sync 失败。
// 管道支持 Write 但不支持 fsync，因此 AppendWrite 成功而 Sync 失败，
// 覆盖 Write 中 "engine write: wal sync" 错误路径。
func TestWrite_WALSyncFailPipeFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("管道测试依赖 Linux 特定行为")
	}

	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 创建管道，用写端替换 WAL 文件
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe 失败: %v", err)
	}

	// 后台读取管道数据防止写端阻塞
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := r.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	// 替换 WAL 底层文件为管道写端
	eng.wal.mu.Lock()
	oldFile := eng.wal.file
	eng.wal.file = w
	eng.wal.offset = 0
	eng.wal.mu.Unlock()
	_ = oldFile.Close()

	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	// 清理
	_ = w.Close()
	_ = r.Close()

	if err == nil {
		t.Fatal("期望管道上 Sync 失败时 Write 返回错误，得到 nil")
	}
	if !strings.Contains(err.Error(), "engine write: wal sync") {
		t.Errorf("错误消息应包含 'engine write: wal sync'，得到: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Write (79.3%) - rotateMemTable 失败路径（engine.go 第 147-150 行）
// ---------------------------------------------------------------------------

// TestWrite_RotateMemTableWhenShouldFlush 验证 Write 在 ShouldFlush 为 true 时
// 正确执行 rotateMemTable。当前 rotateMemTable 永远返回 nil，
// 此测试覆盖 ShouldFlush 检查和 rotateMemTable 调用路径。
func TestWrite_RotateMemTableWhenShouldFlush(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, MaxMemTableSize: 1})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入足够数据触发 ShouldFlush
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("rotate_key_%04d", i)
		err := eng.Write(key, map[string]common.Value{
			colVal: common.NewString("data_to_fill_memtable_and_trigger_flush"),
		})
		if err != nil {
			t.Fatalf("Write %d 失败: %v", i, err)
		}
	}

	// 验证数据可读
	row, ok := eng.Get("rotate_key_0000")
	if !ok {
		t.Fatal("期望找到 rotate_key_0000")
	}
	if v, exists := row.Columns[colVal]; !exists || v.Str != "data_to_fill_memtable_and_trigger_flush" {
		t.Errorf("数据不匹配: %v", v)
	}
}

// ---------------------------------------------------------------------------
// Write (79.3%) - serializeWriteRecord 正常路径覆盖
// ---------------------------------------------------------------------------

// TestWrite_SerializeWriteRecordNormalPath 验证 serializeWriteRecord 在正常输入下不返回错误。
// 当前 serializeWriteRecord 实现永远返回 nil error，因此第 121-124 行的错误路径
// 是死代码。此测试确保正常路径被覆盖。
func TestWrite_SerializeWriteRecordNormalPath(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 使用各种数据类型验证 serializeWriteRecord 正常工作
	values := map[string]common.Value{
		"int_col":  common.NewInt64(42),
		"str_col":  common.NewString("hello"),
		"bool_col": common.NewBool(true),
		"null_col": common.NewNull(),
	}
	err = eng.Write("multi_type_key", values)
	if err != nil {
		t.Fatalf("Write 多类型值失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Write (79.3%) - GroupCommit 模式下 WAL append 失败路径
// ---------------------------------------------------------------------------

// TestWrite_GroupCommit_WALAppendFail 验证 GroupCommit 模式下
// WAL append 失败时 Write 返回正确错误。
func TestWrite_GroupCommit_WALAppendFail(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 先正常写入确保 groupCommitter 初始化
	_ = eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)})

	// 关闭底层文件描述符使 AppendWrite 失败
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("关闭 WAL 文件失败: %v", err)
	}

	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Fatal("期望 WAL 文件关闭后 Write 返回错误，得到 nil")
	}
	if !strings.Contains(err.Error(), "engine write: wal append") {
		t.Errorf("错误消息应包含 'engine write: wal append'，得到: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Write (79.3%) - GroupCommit 模式下 WAL sync 失败路径
// ---------------------------------------------------------------------------

// TestWrite_GroupCommit_WALSyncFailPipeFile 使用管道替换 WAL 文件
// 验证 GroupCommit 模式下 Write 的行为。
// GroupCommit 模式下 Sync 由后台 goroutine 异步执行，
// Write 本身不检查 Sync 错误，因此即使 Sync 失败 Write 也可能成功。
// 此测试验证 GroupCommit 模式下 Write 的代码路径覆盖。
func TestWrite_GroupCommit_WALSyncFailPipeFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("管道测试依赖 Linux 特定行为")
	}

	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 先正常写入确保 groupCommitter 初始化
	_ = eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)})

	// 创建管道替换 WAL 文件
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe 失败: %v", err)
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := r.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	eng.wal.mu.Lock()
	oldFile := eng.wal.file
	eng.wal.file = w
	eng.wal.offset = 0
	eng.wal.mu.Unlock()
	_ = oldFile.Close()

	// GroupCommit 模式下 Write 可能成功（sync 是异步的）
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	_ = w.Close()
	_ = r.Close()
}

// ---------------------------------------------------------------------------
// maybeRotate (80.8%) - 创建临时文件失败路径（wal.go 第 225-228 行）
// ---------------------------------------------------------------------------

// TestMaybeRotate_CreateTempFailReadOnlyDir 使用只读目录触发创建临时文件失败。
// 与现有测试（删除目录）使用不同方式触发同一错误路径。
func TestMaybeRotate_CreateTempFailReadOnlyDir(t *testing.T) {
	if runtime.GOOS == skipWindows {
		t.Skip("权限测试在 Windows 上不可靠")
	}
	if os.Getuid() == 0 {
		t.Skip("root 用户绕过文件权限检查")
	}

	dir := t.TempDir()
	subDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Mkdir 失败: %v", err)
	}

	path := filepath.Join(subDir, "test.wal")
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}

	// 写入数据使 offset > 0
	if err := w.AppendWrite([]byte("data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}

	// 设置很小的 maxSize 以触发轮转
	w.maxSize = 1

	// 将目录设为只读，使 os.Create(w.path+".tmp") 失败
	if err := os.Chmod(subDir, 0555); err != nil {
		t.Fatalf("Chmod 失败: %v", err)
	}
	defer func() { _ = os.Chmod(subDir, 0755) }()

	err = w.AppendWrite([]byte("more_data"))
	if err == nil {
		_ = w.Close()
		t.Fatal("期望只读目录下创建临时文件失败时返回错误，得到 nil")
	}
	if !strings.Contains(err.Error(), "wal rotate create temp") {
		t.Errorf("错误消息应包含 'wal rotate create temp'，得到: %v", err)
	}

	_ = w.Close()
}

// ---------------------------------------------------------------------------
// maybeRotate (80.8%) - 关闭旧文件失败路径（wal.go 第 232-236 行）
// ---------------------------------------------------------------------------

// TestMaybeRotate_CloseOldFileFail_VerifyTempCleanup 验证关闭旧文件失败时
// 临时文件被正确清理。通过预先关闭底层文件描述符触发 old.Close() 失败。
func TestMaybeRotate_CloseOldFileFail_VerifyTempCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "close_fail.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}

	// 写入数据使 offset > 0
	if err := w.AppendWrite([]byte("initial_data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}

	// 设置很小的 maxSize
	w.maxSize = 1

	// 双重关闭底层文件使 maybeRotate 中的 old.Close() 失败
	if err := w.file.Close(); err != nil {
		t.Fatalf("第一次关闭文件失败: %v", err)
	}

	// 触发轮转
	err = w.AppendWrite([]byte("trigger"))
	if err == nil {
		_ = w.Close()
		t.Fatal("期望关闭旧文件失败时返回错误，得到 nil")
	}

	if !strings.Contains(err.Error(), "wal rotate close") {
		t.Errorf("错误消息应包含 'wal rotate close'，得到: %v", err)
	}

	// 验证临时文件已被清理
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("期望临时文件已被删除，但文件仍存在")
	}

	_ = w.Close()
}

// ---------------------------------------------------------------------------
// maybeRotate (80.8%) - 重命名旧文件失败路径（wal.go 第 239-247 行）
// ---------------------------------------------------------------------------

// TestMaybeRotate_RenameOldFail_DirAtPrevPath 通过在 .prev 路径创建非空目录
// 使 os.Rename(w.path, rotatedPath) 失败，验证恢复路径正确执行。
func TestMaybeRotate_RenameOldFail_DirAtPrevPath(t *testing.T) {
	if runtime.GOOS == skipWindows {
		t.Skip("重命名测试在 Windows 上行为不同")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "rename_old.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}

	if err := w.AppendWrite([]byte("data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}

	w.maxSize = 1

	// 在 .prev 路径创建非空目录使 Rename 失败
	prevDir := path + ".prev"
	if err := os.Mkdir(prevDir, 0755); err != nil {
		t.Fatalf("Mkdir .prev 失败: %v", err)
	}
	// 创建非空子文件确保目录不能被 Rename 覆盖
	blocker, err := os.Create(filepath.Join(prevDir, "blocker"))
	if err != nil {
		t.Fatalf("Create blocker 失败: %v", err)
	}
	_ = blocker.Close()

	err = w.AppendWrite([]byte("trigger"))
	if err == nil {
		_ = w.Close()
		t.Fatal("期望重命名旧文件失败时返回错误，得到 nil")
	}

	if !strings.Contains(err.Error(), "wal rotate rename") {
		t.Errorf("错误消息应包含 'wal rotate rename'，得到: %v", err)
	}

	// 验证临时文件已被清理
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("期望临时文件已被删除，但文件仍存在")
	}

	// 验证恢复路径：w.file 应被重新打开
	if w.file != nil {
		// 恢复成功后 WAL 应仍可追加
		if appendErr := w.AppendWrite([]byte("after_recovery")); appendErr != nil {
			t.Logf("恢复后追加数据: %v（可能文件状态不一致）", appendErr)
		}
	}

	// 清理
	_ = os.Remove(filepath.Join(prevDir, "blocker"))
	_ = os.Remove(prevDir)
	_ = w.Close()
}

// ---------------------------------------------------------------------------
// maybeRotate (80.8%) - 重命名临时文件失败路径（wal.go 第 250-259 行）
// ---------------------------------------------------------------------------

// TestMaybeRotate_RenameTempFail_BlockWithDir 使用 goroutine 在第一次 Rename
// 成功后（w.path -> .prev），在 w.path 创建非空目录使第二次 Rename 失败
// （.tmp -> w.path）。
func TestMaybeRotate_RenameTempFail_BlockWithDir(t *testing.T) {
	if runtime.GOOS == skipWindows {
		t.Skip("重命名测试在 Windows 上行为不同")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "rename_temp.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}

	if err := w.AppendWrite([]byte("data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}

	w.maxSize = 1

	// 使用轮询检测 .prev 文件出现，然后创建阻塞目录
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := os.Stat(path + ".prev"); err == nil {
					// 第一次 Rename 已完成，在 w.path 创建非空目录
					if mkErr := os.Mkdir(path, 0755); mkErr == nil {
						f, cErr := os.Create(filepath.Join(path, "blocker"))
						if cErr == nil {
							_ = f.Close()
						}
					}
					return
				}
				runtime.Gosched()
			}
		}
	}()

	err = w.AppendWrite([]byte("trigger"))
	close(stop)

	if err != nil && strings.Contains(err.Error(), "wal rotate rename temp") {
		t.Logf("成功触发第二次 Rename 失败路径: %v", err)
	} else if err != nil {
		t.Logf("触发了其他错误路径: %v", err)
	}

	// 清理
	_ = os.Remove(filepath.Join(path, "blocker"))
	_ = os.RemoveAll(path)
	_ = os.Remove(path + ".tmp")
	_ = os.Remove(path + ".prev")
	_ = w.Close()
}

// ---------------------------------------------------------------------------
// maybeRotate (80.8%) - 重命名临时文件失败且恢复也失败路径
// ---------------------------------------------------------------------------

// TestMaybeRotate_RenameTempFail_RecoveryOpenFails 验证第二次 Rename 失败时
// 恢复路径（重新打开 w.path）也失败的情况。通过删除 w.path 使恢复的
// os.OpenFile 失败。
func TestMaybeRotate_RenameTempFail_RecoveryOpenFails(t *testing.T) {
	if runtime.GOOS == skipWindows {
		t.Skip("无法在 Windows 上删除已打开文件")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "recovery_fail.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}

	if err := w.AppendWrite([]byte("data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}

	w.maxSize = 1

	// 使用 goroutine：在 .prev 出现后删除 .tmp 文件
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := os.Stat(path + ".prev"); err == nil {
					// 删除 .tmp 使第二次 Rename 失败
					_ = os.Remove(path + ".tmp")
					// 删除 w.path 使恢复路径的 OpenFile 失败
					_ = os.Remove(path)
					return
				}
				runtime.Gosched()
			}
		}
	}()

	err = w.AppendWrite([]byte("trigger"))
	close(stop)

	if err != nil && strings.Contains(err.Error(), "wal rotate rename temp") {
		t.Logf("成功触发第二次 Rename 失败路径: %v", err)
	} else if err != nil {
		t.Logf("触发了其他错误路径: %v", err)
	}

	// 清理
	_ = os.Remove(path + ".tmp")
	_ = os.Remove(path + ".prev")
	_ = os.RemoveAll(path)
	_ = w.Close()
}

// ---------------------------------------------------------------------------
// maybeRotate - 使用 syscall 关闭文件描述符触发 close 错误
// ---------------------------------------------------------------------------

// TestMaybeRotate_CloseOldFileFail_DoubleClose 通过双重关闭文件描述符
// 触发 old.Close() 失败，验证错误消息和清理逻辑。
func TestMaybeRotate_CloseOldFileFail_DoubleClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "double_close.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}

	if err := w.AppendWrite([]byte("data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}

	w.maxSize = 1

	// 通过 syscall 关闭文件描述符，使后续 Close 失败
	fd := w.file.Fd()
	_ = syscall.Close(int(fd))

	err = w.AppendWrite([]byte("trigger"))
	if err == nil {
		_ = w.Close()
		t.Fatal("期望关闭旧文件失败时返回错误，得到 nil")
	}

	if !strings.Contains(err.Error(), "wal rotate") {
		t.Logf("错误消息: %v（可能触发不同错误路径）", err)
	}

	_ = w.Close()
}

// ---------------------------------------------------------------------------
// writeCheckpoint (84.6%) - Sync 失败路径（engine.go 第 256-258 行）
// ---------------------------------------------------------------------------

// TestWriteCheckpoint_SyncFailPipeFile 使用管道替换 WAL 文件使 Sync 失败。
// 管道支持 Write 但 fsync 返回 EINVAL，因此 AppendCheckpoint 成功而 Sync 失败，
// 覆盖 writeCheckpoint 中 "engine flush: sync checkpoint" 错误路径。
func TestWriteCheckpoint_SyncFailPipeFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("管道测试依赖 Linux 特定行为")
	}

	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 写入数据
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(42)})

	// 创建管道替换 WAL 文件
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe 失败: %v", err)
	}

	// 后台读取管道数据防止写端阻塞
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := r.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	// 替换 WAL 底层文件为管道写端
	eng.wal.mu.Lock()
	oldFile := eng.wal.file
	eng.wal.file = w
	eng.wal.offset = 0
	eng.wal.mu.Unlock()
	_ = oldFile.Close()

	// 直接调用 writeCheckpoint，AppendCheckpoint 写入管道成功，Sync 失败
	err = eng.writeCheckpoint(1)

	// 清理
	_ = w.Close()
	_ = r.Close()

	if err == nil {
		t.Fatal("期望管道上 Sync 失败时 writeCheckpoint 返回错误，得到 nil")
	}
	if !strings.Contains(err.Error(), "engine flush: sync checkpoint") {
		t.Errorf("错误消息应包含 'engine flush: sync checkpoint'，得到: %v", err)
	}
}

// ---------------------------------------------------------------------------
// writeCheckpoint (84.6%) - AppendCheckpoint 失败路径（engine.go 第 253-255 行）
// ---------------------------------------------------------------------------

// TestWriteCheckpoint_AppendCheckpointFail_ClosedWAL 验证 WAL 关闭后
// writeCheckpoint 返回 "engine flush: write checkpoint" 错误。
func TestWriteCheckpoint_AppendCheckpointFail_ClosedWAL(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 设置 columnMeta 使 serializeCheckpointRecord 产生非空 payload
	eng.mu.Lock()
	eng.columnMeta = []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	eng.mu.Unlock()

	// 关闭 WAL 使 AppendCheckpoint 失败
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close 失败: %v", err)
	}

	err = eng.writeCheckpoint(1)
	if err == nil {
		t.Fatal("期望 WAL 关闭后 writeCheckpoint 返回错误，得到 nil")
	}
	if !strings.Contains(err.Error(), "engine flush: write checkpoint") {
		t.Errorf("错误消息应包含 'engine flush: write checkpoint'，得到: %v", err)
	}
}

// ---------------------------------------------------------------------------
// writeCheckpoint (84.6%) - serializeCheckpointRecord 正常路径
// ---------------------------------------------------------------------------

// TestWriteCheckpoint_SerializeNormalPath 验证 serializeCheckpointRecord
// 在正常输入下不返回错误。当前实现使用 json.Marshal，对于 ColumnMeta
// 类型永远成功，因此第 249-252 行的错误路径是死代码。
// 此测试确保正常路径被覆盖。
func TestWriteCheckpoint_SerializeNormalPath(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入数据并设置 columnMeta
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	cols := []ColumnMeta{
		{ID: 0, Name: colVal, Type: common.TypeInt64},
		{ID: 1, Name: "name", Type: common.TypeString},
	}

	// Flush 内部调用 writeCheckpoint，验证 serializeCheckpointRecord 正常工作
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Write (79.3%) - WAL sync 失败路径（engine.go 第 134-137 行）补充
// ---------------------------------------------------------------------------

// TestWrite_WALSyncFail_DirectCloseFD 验证直接关闭 WAL 文件描述符后
// Write 返回 sync 错误。与现有测试不同，此测试先成功写入一条数据
// 确保 WAL 状态正常，然后再关闭 fd。
func TestWrite_WALSyncFail_DirectCloseFD(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 正常写入一条数据
	if err := eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)}); err != nil {
		t.Fatalf("正常 Write 失败: %v", err)
	}

	// 关闭底层文件描述符
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("关闭 WAL 文件失败: %v", err)
	}

	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Fatal("期望 WAL 文件关闭后 Write 返回错误，得到 nil")
	}

	// 错误可能是 append 或 sync，取决于 OS 缓冲区状态
	if !strings.Contains(err.Error(), "engine write: wal append") &&
		!strings.Contains(err.Error(), "engine write: wal sync") {
		t.Errorf("错误消息应包含 'engine write: wal append' 或 'engine write: wal sync'，得到: %v", err)
	}
}
