package storage

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// Flush (engine): 未覆盖的错误路径
// ---------------------------------------------------------------------------

// TestFlushV13_NoImmutableEarlyReturn 测试 Flush 在没有 immutable memtable 时的提前返回路径。
// 当 activeMem 为空且 immutable 也为空时，Flush 应直接返回 nil。
func TestFlushV13_NoImmutableEarlyReturn(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// 不写入任何数据，activeMem 为空，immutable 也为空
	err = eng.Flush(cols)
	if err != nil {
		t.Errorf("空 Flush 不应返回错误: %v", err)
	}

	// 验证没有产生 segment
	if count := eng.SegmentCount(); count != 0 {
		t.Errorf("期望 0 个 segment，得到 %d", count)
	}
}

// TestFlushV13_ErrorRecoveryPutBackImmutable 测试 Flush 失败时将 immutable memtable 放回。
// 当 flusher.Flush 失败时，未刷写的 immutable memtable 应被放回 e.immutable。
func TestFlushV13_ErrorRecoveryPutBackImmutable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// 写入数据
	if err := eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 手动将 activeMem 移到 immutable，并设置 flusher 的 dataDir 为无效路径
	eng.mu.Lock()
	eng.activeMem.Freeze()
	eng.immutable = append(eng.immutable, eng.activeMem)
	eng.activeMem = NewMemTableWithSize(eng.activeMem.maxSize)
	// 将 flusher 的 dataDir 指向一个文件（非目录），使 writeSegment 失败
	tmpFile, tmpErr := os.CreateTemp(dir, "blocker-*")
	if tmpErr != nil {
		eng.mu.Unlock()
		t.Fatalf("CreateTemp 失败: %v", tmpErr)
	}
	blockerPath := tmpFile.Name()
	_ = tmpFile.Close()
	eng.flusher.dataDir = blockerPath
	eng.mu.Unlock()

	err = eng.Flush(cols)
	if err == nil {
		t.Error("期望 Flush 失败返回错误，得到 nil")
	}

	// 验证 immutable memtable 被放回
	eng.mu.Lock()
	immutableCount := len(eng.immutable)
	eng.mu.Unlock()

	if immutableCount == 0 {
		t.Error("期望 Flush 失败后 immutable memtable 被放回，但 immutable 为空")
	}

	// 恢复 flusher 的 dataDir 以便 Close 成功
	eng.mu.Lock()
	eng.flusher.dataDir = dir
	eng.immutable = nil
	eng.mu.Unlock()
	_ = eng.Close()
}

// TestFlushV13_RegisterSegmentIndexesFailure 测试 Flush 时 registerSegmentIndexes 失败的路径。
// 通过让 flusher 产生 ID=0 的 segment（uint64 溢出），使 primaryIndex.RegisterSegment 失败。
// 验证失败后剩余的 immutable memtable 被放回。
func TestFlushV13_RegisterSegmentIndexesFailure(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// 写入数据并手动创建两个 immutable memtable
	if err := eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write key1 失败: %v", err)
	}
	// 手动将 activeMem 移到 immutable
	eng.mu.Lock()
	eng.activeMem.Freeze()
	eng.immutable = append(eng.immutable, eng.activeMem)
	eng.activeMem = NewMemTableWithSize(eng.activeMem.maxSize)
	eng.mu.Unlock()

	if err := eng.Write("key2", map[string]common.Value{colVal: common.NewInt64(2)}); err != nil {
		t.Fatalf("Write key2 失败: %v", err)
	}
	// 再次手动将 activeMem 移到 immutable
	eng.mu.Lock()
	eng.activeMem.Freeze()
	eng.immutable = append(eng.immutable, eng.activeMem)
	eng.activeMem = NewMemTableWithSize(eng.activeMem.maxSize)
	// 设置 flusher.nextID 为 uint64 最大值，下次 Flush 会产生 ID=0 的 segment
	eng.flusher.nextID = ^uint64(0)
	eng.mu.Unlock()

	err = eng.Flush(cols)
	if err == nil {
		t.Error("期望 registerSegmentIndexes 失败返回错误，得到 nil")
	}

	// 验证剩余 immutable memtable 被放回（第二个 memtable 应被放回）
	eng.mu.Lock()
	immutableCount := len(eng.immutable)
	eng.mu.Unlock()

	if immutableCount == 0 {
		t.Error("期望 registerSegmentIndexes 失败后剩余 immutable memtable 被放回")
	}

	// 恢复以便 Close 成功
	eng.mu.Lock()
	eng.flusher.nextID = 0
	eng.immutable = nil
	eng.mu.Unlock()
	_ = eng.Close()
}

// ---------------------------------------------------------------------------
// decodeSegmentColumn: 未覆盖的错误路径
// ---------------------------------------------------------------------------

// TestDecodeSegmentColumnV13_DecompressError 测试 decodeAllColumns 在 DecompressColumn 失败时的行为。
// 使用损坏的压缩数据（非有效 zstd 格式）触发 DecompressColumn 错误。
func TestDecodeSegmentColumnV13_DecompressError(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{
			{
				Encoding: EncodingPlain,
				Type:     common.TypeInt64,
				RowCount: 1,
				Data:     []byte{0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8},
			},
		},
		Keys: []string{crKey1},
	}

	_, err := seg.decodeAllColumns()
	if err == nil {
		t.Error("期望 DecompressColumn 失败返回错误，得到 nil")
	}
}

// TestDecodeSegmentColumnV13_DecodeColumnError 测试 decodeAllColumns 在 DecodeColumn 失败时的行为。
// 使用空数据（DecompressColumn 成功）+ 无效编码类型（DecodeColumn 失败）来触发错误。
func TestDecodeSegmentColumnV13_DecodeColumnError(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{
			{
				Encoding: EncodingType(99), // 无效编码类型
				Type:     common.TypeInt64,
				RowCount: 1,
				Data:     []byte{}, // 空数据，DecompressColumn 会成功（Decompress 对空数据返回 nil）
			},
		},
		Keys: []string{crKey1},
	}

	_, err := seg.decodeAllColumns()
	if err == nil {
		t.Error("期望 DecodeColumn 失败返回错误，得到 nil")
	}
}

// ---------------------------------------------------------------------------
// CompressColumn/DecompressColumn: 未覆盖的错误路径
// ---------------------------------------------------------------------------

// TestCompressColumnV13_NilInput 测试 CompressColumn 对 nil 输入返回错误。
func TestCompressColumnV13_NilInput(t *testing.T) {
	err := CompressColumn(nil)
	if err == nil {
		t.Error("期望 CompressColumn(nil) 返回错误，得到 nil")
	}
}

// TestDecompressColumnV13_NilInput 测试 DecompressColumn 对 nil 输入返回错误。
func TestDecompressColumnV13_NilInput(t *testing.T) {
	err := DecompressColumn(nil)
	if err == nil {
		t.Error("期望 DecompressColumn(nil) 返回错误，得到 nil")
	}
}

// TestCompressColumnV13_EmptyData 测试 CompressColumn 对空 Data 的处理。
// Compress 对空数据返回 nil,nil，CompressColumn 应将 enc.Data 设为 nil。
func TestCompressColumnV13_EmptyData(t *testing.T) {
	enc := &EncodedColumn{Data: []byte{}}
	err := CompressColumn(enc)
	if err != nil {
		t.Errorf("CompressColumn 空数据不应返回错误: %v", err)
	}
	if enc.Data != nil {
		t.Errorf("期望空数据压缩后 Data 为 nil，得到 %v", enc.Data)
	}
}

// TestDecompressColumnV13_CorruptedData 测试 DecompressColumn 对损坏压缩数据的处理。
func TestDecompressColumnV13_CorruptedData(t *testing.T) {
	enc := &EncodedColumn{Data: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}}
	err := DecompressColumn(enc)
	if err == nil {
		t.Error("期望 DecompressColumn 损坏数据返回错误，得到 nil")
	}
}

// ---------------------------------------------------------------------------
// Write (engine): 未覆盖的错误路径
// ---------------------------------------------------------------------------

// TestWriteV13_WALAppendFailure 测试 Write 在 WAL 追加失败时的行为。
// 通过关闭 WAL 文件描述符来触发 AppendWrite 失败。
func TestWriteV13_WALAppendFailure(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 关闭 WAL 文件描述符使 AppendWrite 失败
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("WAL file Close 失败: %v", err)
	}

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("期望 WAL 追加失败返回错误，得到 nil")
	}
}

// TestWriteV13_WALSyncFailure 测试 Write 在 WAL 同步失败时的行为。
// 通过关闭 WAL 使 Sync 失败。
func TestWriteV13_WALSyncFailure(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 关闭 WAL 使 Sync 失败
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close 失败: %v", err)
	}

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("期望 WAL 同步失败返回错误，得到 nil")
	}
}

// TestWriteV13_RotateMemTableTrigger 测试 Write 触发 MemTable 轮转的路径。
// 使用很小的 MaxMemTableSize 使 ShouldFlush 返回 true，触发 rotateMemTable。
func TestWriteV13_RotateMemTableTrigger(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, MaxMemTableSize: 256})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入足够多的数据以触发 MemTable 轮转
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key_%04d", i)
		if err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))}); err != nil {
			t.Fatalf("Write %d 失败: %v", i, err)
		}
	}

	// 验证数据可读
	row, ok := eng.Get("key_0000")
	if !ok {
		t.Error("期望能读取 key_0000")
	} else if row.Columns[colVal] != common.NewInt64(0) {
		t.Errorf("key_0000: 期望 0，得到 %v", row.Columns[colVal])
	}
}

// ---------------------------------------------------------------------------
// WriteBatch: 未覆盖的错误路径
// ---------------------------------------------------------------------------

// TestWriteBatchV13_EmptyBatch 测试 WriteBatch 空 batch 直接返回 nil。
func TestWriteBatchV13_EmptyBatch(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	err = eng.WriteBatch(nil)
	if err != nil {
		t.Errorf("WriteBatch(nil) 不应返回错误: %v", err)
	}

	err = eng.WriteBatch([]WriteRow{})
	if err != nil {
		t.Errorf("WriteBatch([]) 不应返回错误: %v", err)
	}
}

// TestWriteBatchV13_WALAppendFailure 测试 WriteBatch 在 WAL 追加失败时的行为。
// 通过关闭 WAL 使 AppendBatch 失败。
func TestWriteBatchV13_WALAppendFailure(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}

	// 关闭 WAL 使 AppendBatch 失败
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close 失败: %v", err)
	}

	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
		{Key: "k2", Values: map[string]common.Value{colVal: common.NewInt64(2)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("期望 WAL 追加失败返回错误，得到 nil")
	}
}

// ---------------------------------------------------------------------------
// DeserializeSegment: 未覆盖的错误路径
// ---------------------------------------------------------------------------

// TestDeserializeSegmentV13_InvalidMagic 测试 DeserializeSegment 在 magic number 无效时的行为。
func TestDeserializeSegmentV13_InvalidMagic(t *testing.T) {
	// 创建一个有足够长度但 magic number 无效的数据
	data := make([]byte, 22)
	binary.LittleEndian.PutUint32(data[0:], 0xDEADBEEF) // 无效的 magic number
	// footer offset（在末尾 8 字节）
	binary.LittleEndian.PutUint64(data[len(data)-8:], 14)

	_, err := DeserializeSegment(data)
	if err == nil {
		t.Error("期望无效 magic number 返回错误，得到 nil")
	}
}

// TestDeserializeSegmentV13_TruncatedFile 测试 DeserializeSegment 在文件截断时的行为。
func TestDeserializeSegmentV13_TruncatedFile(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "数据太短（小于 22 字节）",
			data: make([]byte, 10),
		},
		{
			name: "只有 magic（4 字节）",
			data: make([]byte, 4),
		},
		{
			name: "空数据",
			data: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DeserializeSegment(tt.data)
			if err == nil {
				t.Error("期望截断文件返回错误，得到 nil")
			}
		})
	}
}
