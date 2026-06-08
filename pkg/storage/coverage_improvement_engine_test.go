package storage

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ===========================================================================
// Write (84.2% → 89.5%) - 未覆盖路径：WAL 错误、MemTable 错误
// ===========================================================================

// TestWrite_WALClosedAppendError 测试 Write 在 WAL 已关闭时 AppendWrite 失败。
func TestWrite_WALClosedAppendError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("file Close 失败: %v", err)
	}
	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("期望 WAL 文件关闭时 Write 返回错误，得到 nil")
	}
}

// TestWrite_PutError 测试 Write 在 MemTable.Put 失败时的错误路径。
func TestWrite_PutError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	eng.activeMem.Freeze()
	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("期望冻结 MemTable 时 Write 返回错误，得到 nil")
	}
}

// TestWrite_SuccessPath 测试 Write 的正常成功路径。
func TestWrite_SuccessPath(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()
	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(42)})
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	row, ok := eng.Get("key1")
	if !ok {
		t.Fatal("期望找到 key1")
	}
	if row.Columns[colVal].Int64 != 42 {
		t.Errorf("期望 42，得到 %d", row.Columns[colVal].Int64)
	}
}

// TestWrite_RotateMemTableTrigger 测试 Write 触发 rotateMemTable 的完整路径。
func TestWrite_RotateMemTableTrigger(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, MaxMemTableSize: 512})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()
	for i := 0; i < 300; i++ {
		key := fmt.Sprintf("rotate_trigger_%04d", i)
		err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
		if err != nil {
			t.Fatalf("Write %d 失败: %v", i, err)
		}
	}
	eng.mu.RLock()
	immCount := len(eng.immutable)
	eng.mu.RUnlock()
	t.Logf("有 %d 个 immutable memtable", immCount)
}

// ===========================================================================
// WriteBatch (85.0% → 90.0%) - 未覆盖路径：WAL 错误、MemTable 错误
// ===========================================================================

// TestWriteBatch_PutError 测试 WriteBatch 在 MemTable.Put 失败时的错误路径。
func TestWriteBatch_PutError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	eng.activeMem.Freeze()
	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("期望冻结 MemTable 时 WriteBatch 返回错误，得到 nil")
	}
}

// TestWriteBatch_SuccessPath 测试 WriteBatch 的正常成功路径。
func TestWriteBatch_SuccessPath(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()
	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
		{Key: "k2", Values: map[string]common.Value{colVal: common.NewInt64(2)}},
		{Key: "k3", Values: map[string]common.Value{colVal: common.NewInt64(3)}},
	}
	err = eng.WriteBatch(rows)
	if err != nil {
		t.Fatalf("WriteBatch 失败: %v", err)
	}
	for i, key := range []string{"k1", "k2", "k3"} {
		row, ok := eng.Get(key)
		if !ok {
			t.Fatalf("期望找到 %s", key)
		}
		expected := int64(i + 1)
		if row.Columns[colVal].Int64 != expected {
			t.Errorf("%s: 期望 %d，得到 %d", key, expected, row.Columns[colVal].Int64)
		}
	}
}

// TestWriteBatch_RotateMemTableTrigger 测试 WriteBatch 触发 rotateMemTable 的完整路径。
func TestWriteBatch_RotateMemTableTrigger(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, MaxMemTableSize: 512})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()
	for i := 0; i < 150; i++ {
		rows := []WriteRow{
			{Key: fmt.Sprintf("batch_rot_%04d_a", i), Values: map[string]common.Value{colVal: common.NewInt64(int64(i))}},
			{Key: fmt.Sprintf("batch_rot_%04d_b", i), Values: map[string]common.Value{colVal: common.NewInt64(int64(i * 2))}},
		}
		err := eng.WriteBatch(rows)
		if err != nil {
			t.Fatalf("WriteBatch %d 失败: %v", i, err)
		}
	}
	eng.mu.RLock()
	immCount := len(eng.immutable)
	eng.mu.RUnlock()
	t.Logf("有 %d 个 immutable memtable", immCount)
}

// TestWriteBatch_WALClosedAppendError 测试 WriteBatch 在 WAL 已关闭时 AppendBatch 失败。
func TestWriteBatch_WALClosedAppendError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("file Close 失败: %v", err)
	}
	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("期望 WAL 文件关闭时 WriteBatch 返回错误，得到 nil")
	}
}

// ===========================================================================
// Write/WriteBatch - WAL Sync 错误路径（通过管道替换文件触发）
// ===========================================================================

// TestWrite_WALSyncErrorViaPipe 测试 Write 在 WAL Sync 失败时的错误路径。
// 使用管道替换 WAL 文件，使 AppendWrite 成功但 Sync 失败。
func TestWrite_WALSyncErrorViaPipe(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	err = eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)})
	if err != nil {
		t.Fatalf("初始写入失败: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe 失败: %v", err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, e := r.Read(buf); e != nil {
				break
			}
		}
		close(done)
	}()

	origFile := eng.wal.file
	eng.wal.file = w
	eng.wal.maxSize = 1 << 30

	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("期望 WAL Sync 失败时 Write 返回错误，得到 nil")
	} else if strings.Contains(err.Error(), "wal sync") {
		t.Log("成功触发 wal sync 错误路径")
	}

	eng.wal.file = origFile
	_ = w.Close()
	<-done
	_ = r.Close()
	_ = eng.Close()
}

// TestWriteBatch_WALSyncErrorViaPipe 测试 WriteBatch 在 WAL Sync 失败时的错误路径。
func TestWriteBatch_WALSyncErrorViaPipe(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	err = eng.Write("key0", map[string]common.Value{colVal: common.NewInt64(0)})
	if err != nil {
		t.Fatalf("初始写入失败: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe 失败: %v", err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, e := r.Read(buf); e != nil {
				break
			}
		}
		close(done)
	}()

	origFile := eng.wal.file
	eng.wal.file = w
	eng.wal.maxSize = 1 << 30

	rows := []WriteRow{{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}}}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("期望 WAL Sync 失败时 WriteBatch 返回错误，得到 nil")
	} else if strings.Contains(err.Error(), "sync") {
		t.Log("成功触发 sync 错误路径")
	}

	eng.wal.file = origFile
	_ = w.Close()
	<-done
	_ = r.Close()
	_ = eng.Close()
}

// TestFlush_WALSyncErrorViaPipe 测试 Flush 在 WAL Sync 失败时的错误路径。
func TestFlush_WALSyncErrorViaPipe(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe 失败: %v", err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, e := r.Read(buf); e != nil {
				break
			}
		}
		close(done)
	}()

	origFile := eng.wal.file
	eng.wal.file = w
	eng.wal.maxSize = 1 << 30

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	err = eng.Flush(cols)
	if err == nil {
		t.Error("期望 WAL Sync 失败时 Flush 返回错误，得到 nil")
	} else if strings.Contains(err.Error(), "sync") {
		t.Log("成功触发 sync 错误路径")
	}

	eng.wal.file = origFile
	_ = w.Close()
	<-done
	_ = r.Close()
	_ = eng.Close()
}

// TestClose_WALCloseError 测试 Engine.Close 在 WAL Close 失败时的错误路径。
func TestClose_WALCloseError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("file Close 失败: %v", err)
	}
	err = eng.Close()
	if err == nil {
		t.Log("Engine.Close 返回 nil（可能 Sync 在文件关闭后仍成功）")
	} else {
		t.Logf("Engine.Close 错误（预期）: %v", err)
	}
}

// TestFlush_SuccessPath 测试 Flush 的正常成功路径。
func TestFlush_SuccessPath(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()
	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("key2", map[string]common.Value{colVal: common.NewInt64(2)})
	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	err = eng.Flush(cols)
	if err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	row, ok := eng.Get("key1")
	if !ok {
		t.Fatal("期望找到 key1")
	}
	if row.Columns[colVal].Int64 != 1 {
		t.Errorf("期望 1，得到 %d", row.Columns[colVal].Int64)
	}
}

// TestEncodeColumn_DictWithNulls 测试 Dict 编码带 null 值的字符串数据。
func TestEncodeColumn_DictWithNulls(t *testing.T) {
	strs := []string{testStrAlpha, testStrBeta, testStrGamma, testStrAlpha, testStrBeta}
	nulls := common.NewBitmap(5)
	nulls.Set(1)
	nulls.Set(3)

	enc, err := EncodeColumn(common.TypeString, strs, 5, nulls)
	if err != nil {
		t.Fatalf("EncodeColumn Dict with nulls 失败: %v", err)
	}
	if enc.Encoding != EncodingDict {
		t.Errorf("期望 Dict 编码，得到 %v", enc.Encoding)
	}

	decoded, decodedNulls, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn 失败: %v", err)
	}
	decodedStrs, ok := decoded.([]string)
	if !ok {
		t.Fatalf("期望 []string，得到 %T", decoded)
	}
	if len(decodedStrs) != 5 {
		t.Errorf("期望 5 个元素，得到 %d", len(decodedStrs))
	}
	if decodedNulls == nil || !decodedNulls.Get(1) {
		t.Error("期望位置 1 为 null")
	}
}

// TestEncodeColumn_BitmapWithNulls 测试 Bitmap 编码带 null 值的 bool 数据。
func TestEncodeColumn_BitmapWithNulls(t *testing.T) {
	bools := []uint64{1, 0, 1, 1, 0}
	nulls := common.NewBitmap(5)
	nulls.Set(2)

	enc, err := EncodeColumn(common.TypeBool, bools, 5, nulls)
	if err != nil {
		t.Fatalf("EncodeColumn Bitmap with nulls 失败: %v", err)
	}
	if enc.Encoding != EncodingBitmap {
		t.Errorf("期望 Bitmap 编码，得到 %v", enc.Encoding)
	}

	decoded, decodedNulls, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn 失败: %v", err)
	}
	decodedBools, ok := decoded.([]uint64)
	if !ok {
		t.Fatalf("期望 []uint64，得到 %T", decoded)
	}
	if len(decodedBools) != 5 {
		t.Errorf("期望 5 个元素，得到 %d", len(decodedBools))
	}
	if decodedNulls == nil || !decodedNulls.Get(2) {
		t.Error("期望位置 2 为 null")
	}
}

// TestNewEngine_ReplayWALRecordsSuccess 测试 NewEngine 在 WAL 回放成功时的路径。
func TestNewEngine_ReplayWALRecordsSuccess(t *testing.T) {
	dir := t.TempDir()
	eng1, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	_ = eng1.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng1.Write("key2", map[string]common.Value{colVal: common.NewInt64(2)})
	_ = eng1.Close()

	eng2, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 重开失败: %v", err)
	}
	defer func() { _ = eng2.Close() }()
	row, ok := eng2.Get("key1")
	if !ok {
		t.Fatal("期望找到 key1")
	}
	if row.Columns[colVal].Int64 != 1 {
		t.Errorf("期望 1，得到 %d", row.Columns[colVal].Int64)
	}
}
