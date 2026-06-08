package storage

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

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
