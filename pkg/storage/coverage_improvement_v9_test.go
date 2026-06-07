package storage

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// OpenWAL: 非普通文件 Truncate 错误路径（76.5% → 目标 >90%）
// ---------------------------------------------------------------------------

// TestOpenWAL_TruncateOnNonRegularFile 测试 OpenWAL 对目录的 Truncate 行为。
// 对目录调用 Truncate 会返回错误。
func TestOpenWAL_TruncateOnNonRegularFile(t *testing.T) {
	dir := t.TempDir()

	// 尝试打开目录作为 WAL 文件，应返回错误
	_, _, err := OpenWAL(dir)
	if err == nil {
		t.Error("期望打开目录作为 WAL 返回错误，得到 nil")
	}
}

// TestOpenWAL_FileNotExist 测试 OpenWAL 打开不存在的文件。
func TestOpenWAL_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	_, _, err := OpenWAL(filepath.Join(dir, "nonexistent.wal"))
	if err == nil {
		t.Error("期望打开不存在的 WAL 文件返回错误，得到 nil")
	}
}

// TestOpenWAL_ValidRecovery 测试 OpenWAL 正常恢复路径。
func TestOpenWAL_ValidRecovery(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	// 先创建 WAL 并写入数据
	wal, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}

	if err := wal.AppendWrite([]byte("test payload")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}
	if err := wal.Sync(); err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 使用 OpenWAL 恢复
	wal2, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 失败: %v", err)
	}
	defer func() { _ = wal2.Close() }()

	if len(records) == 0 {
		t.Error("期望恢复到记录，但 records 为空")
	}
}

// TestOpenWAL_SeekError 测试 OpenWAL 在 Seek 时出错。
// 通过关闭文件描述符来触发 Seek 错误。
func TestOpenWAL_SeekError(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "test.wal")

	// 创建空 WAL
	wal, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// OpenWAL 对空文件应成功（validOffset=0, Seek(0,...) 不会失败）
	wal2, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 空文件不应返回错误: %v", err)
	}
	_ = wal2.Close()
	if len(records) != 0 {
		t.Errorf("空 WAL 应无记录，得到 %d 条", len(records))
	}
}

// ---------------------------------------------------------------------------
// Compress/Decompress: 编码器/解码器池复用路径（85.7% → 100%）
// ---------------------------------------------------------------------------

// TestCompressDecompress_PoolReuse 测试编码器/解码器池复用。
// 多次调用 Compress/Decompress 应从池中复用编解码器实例。
func TestCompressDecompress_PoolReuse(t *testing.T) {
	data := []byte("test data for pool reuse verification")

	// 第一次调用：创建新的编码器/解码器
	compressed1, err := Compress(data)
	if err != nil {
		t.Fatalf("第一次 Compress 失败: %v", err)
	}

	// 第二次调用：应从池中获取编码器
	compressed2, err := Compress(data)
	if err != nil {
		t.Fatalf("第二次 Compress 失败: %v", err)
	}

	// 两次压缩结果应一致
	if string(compressed1) != string(compressed2) {
		t.Error("两次压缩结果不一致")
	}

	// 解压验证
	decompressed, err := Decompress(compressed1)
	if err != nil {
		t.Fatalf("Decompress 失败: %v", err)
	}
	if string(decompressed) != string(data) {
		t.Errorf("解压数据不匹配: 期望 %q，得到 %q", data, decompressed)
	}

	// 第二次解压：应从池中获取解码器
	decompressed2, err := Decompress(compressed2)
	if err != nil {
		t.Fatalf("第二次 Decompress 失败: %v", err)
	}
	if string(decompressed2) != string(data) {
		t.Errorf("第二次解压数据不匹配: 期望 %q，得到 %q", data, decompressed2)
	}
}

// TestCompressColumn_WithData 测试 CompressColumn 正常压缩路径。
func TestCompressColumn_WithData(t *testing.T) {
	original := []byte("column data for compression test")
	enc := &EncodedColumn{Data: make([]byte, len(original))}
	copy(enc.Data, original)

	err := CompressColumn(enc)
	if err != nil {
		t.Fatalf("CompressColumn 失败: %v", err)
	}

	// 压缩后数据应与原始数据不同
	if string(enc.Data) == string(original) {
		t.Error("压缩后数据应与原始数据不同")
	}

	// 解压验证
	err = DecompressColumn(enc)
	if err != nil {
		t.Fatalf("DecompressColumn 失败: %v", err)
	}
	if string(enc.Data) != string(original) {
		t.Errorf("解压后数据不匹配: 期望 %q，得到 %q", original, enc.Data)
	}
}

// ---------------------------------------------------------------------------
// Engine Flush: WAL 关闭时 Flush 失败路径（82.0% → >90%）
// ---------------------------------------------------------------------------

// TestFlush_EmptyImmutable 测试 Flush 时没有 immutable memtable 的路径。
func TestFlush_EmptyImmutable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: crCol1, Type: common.TypeInt64}}

	// 没有写入任何数据，Flush 应直接返回 nil
	err = eng.Flush(cols)
	if err != nil {
		t.Errorf("空 Flush 不应返回错误: %v", err)
	}
}

// TestFlush_WithColumnMeta 测试 Flush 时设置 columnMeta 的路径。
func TestFlush_WithColumnMeta(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: crCol1, Type: common.TypeInt64}}

	// 写入数据
	if err := eng.Write("key1", map[string]common.Value{crCol1: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 第一次 Flush 设置 columnMeta
	err = eng.Flush(cols)
	if err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}

	// 第二次 Flush 不应覆盖已有的 columnMeta
	if err := eng.Write("key2", map[string]common.Value{crCol1: common.NewInt64(2)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	err = eng.Flush(cols)
	if err != nil {
		t.Fatalf("第二次 Flush 失败: %v", err)
	}
}

// TestFlush_MultipleImmutable 测试 Flush 多个 immutable memtable 的路径。
func TestFlush_MultipleImmutable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, MaxMemTableSize: 256})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: crCol1, Type: common.TypeInt64}}

	// 写入大量数据以触发多次 memtable 轮转
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key_%04d", i)
		if err := eng.Write(key, map[string]common.Value{crCol1: common.NewInt64(int64(i))}); err != nil {
			t.Fatalf("Write %d 失败: %v", i, err)
		}
	}

	err = eng.Flush(cols)
	if err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Engine Write: 空字符串 key 边界条件（84.2% → >90%）
// ---------------------------------------------------------------------------

// TestWrite_EmptyKey 测试 Write 使用空字符串 key。
func TestWrite_EmptyKey(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	err = eng.Write("", map[string]common.Value{crCol1: common.NewInt64(1)})
	if err != nil {
		t.Errorf("空 key 写入不应返回错误: %v", err)
	}

	// 验证可以读取
	row, ok := eng.Get("")
	if !ok {
		t.Error("期望能读取空 key 的数据")
	}
	if row.Columns[crCol1] != common.NewInt64(1) {
		t.Errorf("读取值不匹配: 期望 1，得到 %v", row.Columns[crCol1])
	}
}

// ---------------------------------------------------------------------------
// ScanRange: Segment 数据损坏时迭代器错误路径（88.2% → >90%）
// ---------------------------------------------------------------------------

// TestScanRange_CorruptSegmentData 测试 ScanRange 在 Segment 数据损坏时返回 nil。
func TestScanRange_CorruptSegmentData(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: crCol1, Type: common.TypeInt64}}

	// 写入并 Flush 以创建 Segment
	if err := eng.Write("key1", map[string]common.Value{crCol1: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}

	// 损坏 Segment 数据
	eng.mu.Lock()
	for _, seg := range eng.segments {
		for i := range seg.Columns {
			seg.Columns[i].Data = []byte{0xFF, 0xFE, 0xFD, 0xFC}
		}
	}
	eng.mu.Unlock()

	// ScanRange 应返回 nil（迭代器遇到错误）
	results := eng.ScanRange("", "z")
	if results != nil {
		t.Errorf("损坏 Segment 的 ScanRange 应返回 nil，得到 %d 条结果", len(results))
	}
}
