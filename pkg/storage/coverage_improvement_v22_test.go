package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// Write GroupCommit sync 路径
// ---------------------------------------------------------------------------

// TestCoverageV20_Write_GroupCommitSync 测试 Write 在 GroupCommit 模式下的 syncCh 等待路径
func TestCoverageV20_Write_GroupCommitSync(t *testing.T) {
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

	// 写入数据，应走 GroupCommit 的 syncCh 等待路径
	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(42)})
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 等待 GroupCommitter 处理
	time.Sleep(50 * time.Millisecond)

	// 验证数据已写入
	row, ok := eng.activeMem.Get("key1")
	if !ok {
		t.Fatal("期望找到 key1")
	}
	if row.Columns[colVal].Int64 != 42 {
		t.Errorf("期望值 42，得到 %d", row.Columns[colVal].Int64)
	}
}

// TestCoverageV20_Write_MultipleGroupCommit 测试多次写入共享 GroupCommit sync
func TestCoverageV20_Write_MultipleGroupCommit(t *testing.T) {
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

	// 连续写入多条数据
	for i := 0; i < 10; i++ {
		err := eng.Write("key", map[string]common.Value{colVal: common.NewInt64(int64(i))})
		if err != nil {
			t.Fatalf("Write %d 失败: %v", i, err)
		}
	}

	// 等待 GroupCommitter 处理
	time.Sleep(50 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// writeCheckpoint GroupCommit SyncNow 路径
// ---------------------------------------------------------------------------

// TestCoverageV20_WriteCheckpoint_GroupCommit 测试 writeCheckpoint 在 GroupCommit 模式下调用 SyncNow
func TestCoverageV20_WriteCheckpoint_GroupCommit(t *testing.T) {
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

	// 写入数据
	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// Flush 触发 writeCheckpoint，内部会调用 gc.SyncNow()
	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runCompactLoop / runWALCleanLoop 错误记录路径
// ---------------------------------------------------------------------------

// TestCoverageV20_RunCompactLoop_ErrorRecording 测试 runCompactLoop 错误记录路径
func TestCoverageV20_RunCompactLoop_ErrorRecording(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 创建足够多的 L0 segment 以触发 compaction
	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	for i := 0; i < defaultL0CompactionThreshold; i++ {
		if err := eng.Write(fmtKey(i), map[string]common.Value{colVal: common.NewInt64(int64(i))}); err != nil {
			t.Fatalf("Write %d 失败: %v", i, err)
		}
		if err := eng.Flush(cols); err != nil {
			t.Fatalf("Flush %d 失败: %v", i, err)
		}
	}

	// 破坏某个 segment 的列数据，使 compaction 失败
	eng.mu.Lock()
	if len(eng.segments) > 0 {
		for i := range eng.segments[0].Columns {
			eng.segments[0].Columns[i].Data = []byte{0xFF, 0xFE, 0xFD}
		}
	}
	eng.mu.Unlock()

	// 使用短间隔启动调度器
	sched := NewScheduler(eng, SchedulerConfig{
		CompactInterval:  10 * time.Millisecond,
		FlushInterval:    1 * time.Hour,
		WALCleanInterval: 1 * time.Hour,
	})
	sched.Start()
	defer sched.Stop()

	// 等待 compaction 尝试和错误记录
	time.Sleep(200 * time.Millisecond)

	// 验证错误被记录
	stats := sched.Stats()
	if stats.LastError == "" {
		t.Error("期望 compaction 错误被记录，但 LastError 为空")
	}
}

// TestCoverageV20_RunWALCleanLoop_ErrorRecording 测试 runWALCleanLoop 错误记录路径
func TestCoverageV20_RunWALCleanLoop_ErrorRecording(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 创建 .prev 文件
	prevPath := eng.wal.path + ".prev"
	if err := os.WriteFile(prevPath, []byte("prev wal data"), 0644); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}

	// 启动调度器
	sched := NewScheduler(eng, SchedulerConfig{
		WALCleanInterval:  10 * time.Millisecond,
		WALCleanThreshold: 1, // 极小阈值以触发清理
		FlushInterval:     1 * time.Hour,
		CompactInterval:   1 * time.Hour,
	})
	sched.Start()

	// 等待 WAL 清理成功
	time.Sleep(200 * time.Millisecond)

	// 验证 WALCleanCount 被递增
	stats := sched.Stats()
	if stats.WALCleanCount == 0 {
		t.Error("期望 WALCleanCount > 0")
	}

	sched.Stop()
	_ = eng.Close()
}

// ---------------------------------------------------------------------------
// WriteBatch GroupCommit 路径
// ---------------------------------------------------------------------------

// TestCoverageV20_WriteBatch_GroupCommit 测试 WriteBatch 在 GroupCommit 模式下的 syncCh 等待路径
func TestCoverageV20_WriteBatch_GroupCommit(t *testing.T) {
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
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
		{Key: "k2", Values: map[string]common.Value{colVal: common.NewInt64(2)}},
		{Key: "k3", Values: map[string]common.Value{colVal: common.NewInt64(3)}},
	}

	if err := eng.WriteBatch(rows); err != nil {
		t.Fatalf("WriteBatch 失败: %v", err)
	}

	// 等待 GroupCommitter 处理
	time.Sleep(50 * time.Millisecond)

	// 验证数据已写入
	for i := 1; i <= 3; i++ {
		key := "k" + string(rune('0'+i))
		row, ok := eng.activeMem.Get(key)
		if !ok {
			t.Errorf("期望找到 %s", key)
			continue
		}
		if row.Columns[colVal].Int64 != int64(i) {
			t.Errorf("key %s: 期望值 %d，得到 %d", key, i, row.Columns[colVal].Int64)
		}
	}
}

// ---------------------------------------------------------------------------
// GroupCommitter SyncNow 路径
// ---------------------------------------------------------------------------

// TestCoverageV20_GroupCommitter_SyncNow 测试 GroupCommitter.SyncNow 直接同步
func TestCoverageV20_GroupCommitter_SyncNow(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	defer func() { _ = w.Close() }()

	gc := NewGroupCommitter(w, 1*time.Millisecond)
	defer gc.Close()

	// 写入数据
	if err := w.AppendWrite([]byte("test-data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}

	// 提交 sync 请求
	syncCh := gc.Submit()

	// 调用 SyncNow 应立即同步
	gc.SyncNow()

	// 等待 syncCh
	select {
	case <-syncCh:
		// 成功
	case <-time.After(2 * time.Second):
		t.Fatal("SyncNow 后 syncCh 未关闭")
	}
}

// ---------------------------------------------------------------------------
// Engine writeCheckpoint 完整路径
// ---------------------------------------------------------------------------

// TestCoverageV20_WriteCheckpoint_NormalMode 测试 writeCheckpoint 在普通模式下的路径
func TestCoverageV20_WriteCheckpoint_NormalMode(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:  dir,
		SyncMode: SyncEveryWrite,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入数据
	if err := eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// Flush 触发 writeCheckpoint
	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Engine 完整 GroupCommit 写入和恢复
// ---------------------------------------------------------------------------

// TestCoverageV20_Engine_GroupCommitRecovery 测试 GroupCommit 模式下的写入和恢复
func TestCoverageV20_Engine_GroupCommitRecovery(t *testing.T) {
	dir := t.TempDir()

	// 第一个引擎实例
	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 写入数据
	if err := eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(100)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 等待 GroupCommitter 处理
	time.Sleep(50 * time.Millisecond)

	// 关闭引擎
	if err := eng.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 重新打开引擎验证数据恢复
	eng2, err := NewEngine(EngineConfig{
		DataDir:  dir,
		SyncMode: SyncEveryWrite,
	})
	if err != nil {
		t.Fatalf("NewEngine 恢复失败: %v", err)
	}
	defer func() { _ = eng2.Close() }()

	row, ok := eng2.activeMem.Get("key1")
	if !ok {
		t.Fatal("恢复后期望找到 key1")
	}
	if row.Columns[colVal].Int64 != 100 {
		t.Errorf("恢复后期望值 100，得到 %d", row.Columns[colVal].Int64)
	}
}

// ---------------------------------------------------------------------------
// Scheduler 错误记录完整路径
// ---------------------------------------------------------------------------

// TestCoverageV20_Scheduler_FlushErrorRecording 测试调度器刷盘错误记录路径
func TestCoverageV20_Scheduler_FlushErrorRecording(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 写入数据并手动将 memtable 移到 immutable
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	eng.mu.Lock()
	eng.activeMem.Freeze()
	eng.immutable = append(eng.immutable, eng.activeMem)
	eng.activeMem = NewMemTableWithSize(eng.activeMem.maxSize)
	eng.mu.Unlock()

	// 关闭 WAL 使 Flush 失败
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close 失败: %v", err)
	}

	sched := NewScheduler(eng, SchedulerConfig{
		FlushInterval:    10 * time.Millisecond,
		CompactInterval:  1 * time.Hour,
		WALCleanInterval: 1 * time.Hour,
	})
	sched.Start()

	// 等待 flush 尝试和错误记录
	time.Sleep(200 * time.Millisecond)

	stats := sched.Stats()
	if stats.LastError == "" {
		t.Error("期望 flush 错误被记录，但 LastError 为空")
	}

	sched.Stop()
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

// fmtKey 生成格式化的键名
func fmtKey(i int) string {
	return "key" + string(rune('0'+i))
}

// ---------------------------------------------------------------------------
// Engine Close 在 GroupCommit 模式下
// ---------------------------------------------------------------------------

// TestCoverageV20_Engine_CloseGroupCommit 测试 GroupCommit 模式下关闭引擎
func TestCoverageV20_Engine_CloseGroupCommit(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:      dir,
		SyncMode:     SyncGroupCommit,
		SyncInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 写入数据
	if err := eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 关闭引擎应正确停止 GroupCommitter
	if err := eng.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Engine StartScheduler 重复启动
// ---------------------------------------------------------------------------

// TestCoverageV20_Engine_StartSchedulerTwice 测试重复启动调度器
func TestCoverageV20_Engine_StartSchedulerTwice(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cfg := SchedulerConfig{
		FlushInterval:    1 * time.Hour,
		CompactInterval:  1 * time.Hour,
		WALCleanInterval: 1 * time.Hour,
	}

	// 第一次启动
	eng.StartScheduler(cfg)

	// 第二次启动应不做任何操作
	eng.StartScheduler(cfg)

	// 验证调度器正在运行
	stats, ok := eng.SchedulerStats()
	if !ok {
		t.Error("期望调度器正在运行")
	}
	_ = stats
}
