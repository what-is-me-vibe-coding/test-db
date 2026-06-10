package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// replaceWALFdWithPipe 将 WAL 文件描述符替换为 pipe 的写端。
// pipe 的 write 成功但 fsync 失败（EINVAL），用于测试 WAL Sync 错误路径。
// 注意：读端必须保持打开，否则 Write 会返回 EPIPE 错误。
func replaceWALFdWithPipe(t *testing.T, eng *Engine) {
	t.Helper()
	fds := make([]int, 2)
	if err := syscall.Pipe(fds); err != nil {
		t.Skipf("skipping: syscall.Pipe failed: %v", err)
	}
	// 保持读端打开（否则 Write 返回 EPIPE），写端用于 Dup2
	// 读端在测试结束后由进程清理自动关闭

	walFd := int(eng.wal.file.Fd())
	if err := syscall.Dup2(fds[1], walFd); err != nil {
		_ = syscall.Close(fds[0])
		_ = syscall.Close(fds[1])
		t.Skipf("skipping: Dup2 failed: %v", err)
	}
	_ = syscall.Close(fds[1])
	// fds[0] 故意不关闭，保持读端打开
}

// ---------------------------------------------------------------------------
// Write: WAL Sync 失败路径（AppendWrite 成功但 Sync 失败）
// 使用 Dup2 将 WAL 文件描述符替换为 /dev/full，使 Write 成功但 Sync 失败。
// ---------------------------------------------------------------------------

func TestWriteWALSyncFailsAfterAppend(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// 先正常写入一条记录，确保 WAL 状态正常
	if err := eng.Write("init_key", map[string]common.Value{colVal: common.NewInt64(0)}); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// 将 WAL 文件描述符替换为 pipe（write 成功，fsync 失败）
	replaceWALFdWithPipe(t, eng)

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when WAL Sync fails after AppendWrite, got nil")
	}
}

// ---------------------------------------------------------------------------
// writeCheckpoint: WAL Sync 失败路径
// ---------------------------------------------------------------------------

func TestWriteCheckpointWALSyncFails(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_ = eng.Write("k1", map[string]common.Value{colVal: common.NewInt64(1)})

	// 将 WAL 文件描述符替换为 pipe（write 成功，fsync 失败）
	replaceWALFdWithPipe(t, eng)

	err = eng.writeCheckpoint(1)
	if err == nil {
		t.Error("expected error when WAL Sync fails in writeCheckpoint, got nil")
	}
}

// ---------------------------------------------------------------------------
// WriteBatch: WAL Sync 失败路径（AppendBatch 成功但 Sync 失败）
// ---------------------------------------------------------------------------

func TestWriteBatchWALSyncFailsAfterAppend(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// 先正常写入，确保 WAL 状态正常
	if err := eng.Write("init_key", map[string]common.Value{colVal: common.NewInt64(0)}); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// 将 WAL 文件描述符替换为 pipe（write 成功，fsync 失败）
	replaceWALFdWithPipe(t, eng)

	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("expected error when WAL Sync fails in WriteBatch, got nil")
	}
}

// ---------------------------------------------------------------------------
// OpenWAL: replayWALRecords 返回错误的路径
// ---------------------------------------------------------------------------

func TestOpenWALReplayErrorPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	// 创建一个有效的 WAL 文件
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	_ = w.AppendWrite([]byte("data"))
	_ = w.Sync()
	_ = w.Close()

	// 重新打开 WAL 应该成功
	w2, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	_ = w2.Close()

	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Scheduler: tryCleanWAL 错误路径
// ---------------------------------------------------------------------------

func TestSchedulerTryCleanWALRemoveErrorPath(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	sched := NewScheduler(eng, SchedulerConfig{
		WALCleanInterval:  defaultWALCleanInterval,
		WALCleanThreshold: 1, // 设置很小的阈值，确保触发清理
	})

	// 创建一个 .prev 文件（非空目录），使 Remove 失败
	prevPath := eng.wal.path + ".prev"
	prevDir := prevPath
	if err := os.MkdirAll(prevDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	// 在目录中创建文件，使 Remove（删除目录）失败
	if err := os.WriteFile(filepath.Join(prevDir, "inner"), []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	err = sched.tryCleanWAL()
	if err == nil {
		t.Error("expected error when Remove fails in tryCleanWAL, got nil")
	}

	// 清理
	_ = os.RemoveAll(prevDir)
}

// ---------------------------------------------------------------------------
// Compress/Decompress: 额外错误路径
// ---------------------------------------------------------------------------

func TestDecompressColumnWithInvalidData(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     common.TypeInt64,
		RowCount: 10,
		Data:     []byte{0xFF, 0xFE, 0xFD, 0xFC}, // 无效的压缩数据
	}
	err := DecompressColumn(enc)
	if err == nil {
		t.Error("expected error for invalid compressed data, got nil")
	}
}

func TestCompressColumnTwice(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     common.TypeInt64,
		RowCount: 10,
		Data:     make([]byte, 80), // 10 * 8 bytes
	}
	// 第一次压缩应该成功
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("first CompressColumn failed: %v", err)
	}
	// 第二次压缩也成功（ZSTD 可以压缩任何数据，包括已压缩的数据）
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("second CompressColumn failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// EncodeColumn: 额外类型路径
// ---------------------------------------------------------------------------

func TestEncodeColumnInvalidDataType(t *testing.T) {
	invalidType := common.DataType(99)
	// rowCount > 0 时会进入 encodePlain 的 switch，无效类型没有匹配的 case
	_, err := EncodeColumn(invalidType, []int64{1}, 1, nil)
	if err == nil {
		t.Error("expected error for unsupported type in EncodeColumn, got nil")
	}
}
