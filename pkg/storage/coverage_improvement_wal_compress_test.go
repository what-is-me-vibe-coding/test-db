package storage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ===========================================================================
// OpenWAL (76.5%) - 未覆盖路径
// ===========================================================================

// TestOpenWAL_ReplayIOError 测试 OpenWAL 在 replayWAL 遇到部分写入时的行为。
func TestOpenWAL_ReplayIOError(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "replay_io.wal")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	_ = w.AppendWrite([]byte("record1"))
	_ = w.AppendWrite([]byte("record2"))
	_ = w.Sync()
	_ = w.Close()
	// 追加部分头部数据（4 字节，模拟崩溃期间的部分写入）
	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("OpenFile 失败: %v", err)
	}
	_, _ = f.Write([]byte{0x05, 0x00, 0x00, 0x00}) // totalLen=5，但缺少消息体
	_ = f.Close()
	opened, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 不应因部分写入返回错误: %v", err)
	}
	defer func() { _ = opened.Close() }()
	if len(records) != 2 {
		t.Errorf("期望 2 条记录，得到 %d", len(records))
	}
}

// TestOpenWAL_InvalidRecordLength 测试 OpenWAL 遇到无效记录长度时的行为。
func TestOpenWAL_InvalidRecordLength(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "invalid_len.wal")

	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	_ = w.AppendWrite([]byte("valid"))
	_ = w.Sync()
	_ = w.Close()

	// 追加无效记录长度（totalLen=0）
	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("OpenFile 失败: %v", err)
	}
	_, _ = f.Write([]byte{0x00, 0x00, 0x00, 0x00})
	_ = f.Close()

	opened, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 不应因无效记录长度返回错误: %v", err)
	}
	defer func() { _ = opened.Close() }()

	if len(records) != 1 {
		t.Errorf("期望 1 条有效记录，得到 %d", len(records))
	}
}

// TestOpenWAL_CRCMismatch 测试 OpenWAL 遇到 CRC 不匹配时的行为。
func TestOpenWAL_CRCMismatch(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "crc_mismatch.wal")

	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	_ = w.AppendWrite([]byte("valid"))
	_ = w.Sync()
	_ = w.Close()

	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if len(data) > 0 {
		data[len(data)-1] ^= 0xFF
	}
	if err := os.WriteFile(walPath, data, 0644); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}

	opened, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 不应因 CRC 不匹配返回错误: %v", err)
	}
	defer func() { _ = opened.Close() }()

	if len(records) != 0 {
		t.Errorf("期望 0 条记录（CRC 不匹配），得到 %d", len(records))
	}
}

// TestOpenWAL_OversizedRecordLength 测试 OpenWAL 遇到超大记录长度时的行为。
func TestOpenWAL_OversizedRecordLength(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "oversized.wal")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	_ = w.AppendWrite([]byte("valid"))
	_ = w.Sync()
	_ = w.Close()
	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("OpenFile 失败: %v", err)
	}
	_, _ = f.Write([]byte{0xFF, 0xFF, 0xFF, 0x00})
	_ = f.Close()
	opened, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 不应因超大记录长度返回错误: %v", err)
	}
	defer func() { _ = opened.Close() }()
	if len(records) != 1 {
		t.Errorf("期望 1 条有效记录，得到 %d", len(records))
	}
}

// TestOpenWAL_PartialBody 测试 OpenWAL 遇到部分消息体时的行为。
func TestOpenWAL_PartialBody(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "partial_body.wal")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	_ = w.AppendWrite([]byte("valid"))
	_ = w.Sync()
	_ = w.Close()
	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("OpenFile 失败: %v", err)
	}
	_, _ = f.Write([]byte{0x0A, 0x00, 0x00, 0x00}) // totalLen=10
	_, _ = f.Write([]byte{0x01, 0x02})             // 只有 2 字节消息体
	_ = f.Close()
	opened, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 不应因部分消息体返回错误: %v", err)
	}
	defer func() { _ = opened.Close() }()
	if len(records) != 1 {
		t.Errorf("期望 1 条有效记录，得到 %d", len(records))
	}
}

// TestOpenWAL_ValidRecordRecovery 测试 OpenWAL 在有各种记录类型时的恢复行为。
func TestOpenWAL_ValidRecordRecovery(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "valid_recovery.wal")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	_ = w.AppendWrite([]byte("write_data"))
	_ = w.AppendCommit([]byte("commit_data"))
	_ = w.AppendCheckpoint([]byte("checkpoint_data"))
	_ = w.Sync()
	_ = w.Close()
	opened, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 失败: %v", err)
	}
	defer func() { _ = opened.Close() }()
	if len(records) != 3 {
		t.Fatalf("期望 3 条记录，得到 %d", len(records))
	}
	if records[0].Type != walTypeWrite {
		t.Errorf("记录 0: 期望类型 %d，得到 %d", walTypeWrite, records[0].Type)
	}
	if records[1].Type != walTypeCommit {
		t.Errorf("记录 1: 期望类型 %d，得到 %d", walTypeCommit, records[1].Type)
	}
	if records[2].Type != walTypeCheckpoint {
		t.Errorf("记录 2: 期望类型 %d，得到 %d", walTypeCheckpoint, records[2].Type)
	}
}

// TestOpenWAL_NonExistentFile 测试 OpenWAL 在文件不存在时的行为。
func TestOpenWAL_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	_, _, err := OpenWAL(filepath.Join(dir, "nonexistent.wal"))
	if err == nil {
		t.Error("期望不存在的文件时 OpenWAL 返回错误，得到 nil")
	}
}

// TestOpenWAL_PartialHeader 测试 replayWAL 遇到部分头部时的行为。
func TestOpenWAL_PartialHeader(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "partial_header.wal")
	if err := os.WriteFile(walPath, []byte{0x01, 0x02}, 0644); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	w, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 不应因部分头部返回错误: %v", err)
	}
	defer func() { _ = w.Close() }()
	if len(records) != 0 {
		t.Errorf("期望 0 条记录，得到 %d", len(records))
	}
}

// TestOpenWAL_InvalidHeader 测试 replayWAL 遇到无效头部时的行为。
func TestOpenWAL_InvalidHeader(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "invalid_header.wal")
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 1) // totalLen=1
	if err := os.WriteFile(walPath, data, 0644); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	w, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL 不应因无效头部返回错误: %v", err)
	}
	defer func() { _ = w.Close() }()
	if len(records) != 0 {
		t.Errorf("期望 0 条记录，得到 %d", len(records))
	}
}

// ===========================================================================
// maybeRotate (80.8% → 100%)
// ===========================================================================

// TestMaybeRotate_TruncateSyncError 测试 WAL Truncate 中 Sync 失败的路径。
func TestMaybeRotate_TruncateSyncError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")
	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL 失败: %v", err)
	}
	if err := w.AppendWrite([]byte("data")); err != nil {
		t.Fatalf("AppendWrite 失败: %v", err)
	}
	if err := w.file.Close(); err != nil {
		t.Fatalf("关闭底层文件失败: %v", err)
	}
	err = w.Truncate()
	if err == nil {
		_ = w.Close()
		t.Fatal("期望 Truncate 因 Sync 失败返回错误，得到 nil")
	}
	if !strings.Contains(err.Error(), "wal truncate sync") {
		t.Errorf("错误消息应包含 'wal truncate sync'，得到: %v", err)
	}
	_ = w.Close()
}

// ===========================================================================
// Compress / getEncoder / getDecoder - 编码器池复用路径
// ===========================================================================

// TestCompress_EncoderPoolReuse 测试 Compress 中编码器池的复用路径。
func TestCompress_EncoderPoolReuse(t *testing.T) {
	data1 := []byte("first compression data")
	compressed1, err := Compress(data1)
	if err != nil {
		t.Fatalf("第一次 Compress 失败: %v", err)
	}
	decompressed1, err := Decompress(compressed1)
	if err != nil {
		t.Fatalf("第一次 Decompress 失败: %v", err)
	}
	if string(decompressed1) != string(data1) {
		t.Errorf("第一次往返不匹配")
	}

	// 第二次调用：应从池中获取编码器
	data2 := []byte("second compression data with different content")
	compressed2, err := Compress(data2)
	if err != nil {
		t.Fatalf("第二次 Compress 失败: %v", err)
	}
	decompressed2, err := Decompress(compressed2)
	if err != nil {
		t.Fatalf("第二次 Decompress 失败: %v", err)
	}
	if string(decompressed2) != string(data2) {
		t.Errorf("第二次往返不匹配")
	}
}

// TestDecompress_DecoderPoolReuse 测试 Decompress 中解码器池的复用路径。
func TestDecompress_DecoderPoolReuse(t *testing.T) {
	original := []byte("test data for decoder pool reuse")
	compressed, err := Compress(original)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	result1, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("第一次 Decompress 失败: %v", err)
	}
	if string(result1) != string(original) {
		t.Errorf("第一次解压不匹配")
	}
	result2, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("第二次 Decompress 失败: %v", err)
	}
	if string(result2) != string(original) {
		t.Errorf("第二次解压不匹配")
	}
}

// TestCompress_LargeData 测试 Compress 对较大数据的处理。
func TestCompress_LargeData(t *testing.T) {
	data := make([]byte, 1024*64)
	for i := range data {
		data[i] = byte(i % 256)
	}
	compressed, err := Compress(data)
	if err != nil {
		t.Fatalf("Compress 大数据失败: %v", err)
	}
	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress 大数据失败: %v", err)
	}
	if len(decompressed) != len(data) {
		t.Errorf("解压后长度不匹配: 得到 %d，期望 %d", len(decompressed), len(data))
	}
}

// TestDecompress_InvalidData 测试 Decompress 对无效数据的处理。
func TestDecompress_InvalidData(t *testing.T) {
	_, err := Decompress([]byte{0xFF, 0xFE, 0xFD, 0xFC})
	if err == nil {
		t.Error("期望无效数据返回错误，得到 nil")
	}
}

// TestCompressColumn_NilColumn 测试 CompressColumn 对 nil 列的处理。
func TestCompressColumn_NilColumn(t *testing.T) {
	err := CompressColumn(nil)
	if err == nil {
		t.Error("期望 nil 列返回错误，得到 nil")
	}
}

// ===========================================================================
// EncodeColumn (85.7%) - 各种编码类型和类型不匹配
// ===========================================================================

// TestEncodeColumn_TypeMismatches 测试 EncodeColumn 对类型不匹配的处理。
func TestEncodeColumn_TypeMismatches(t *testing.T) {
	tests := []struct {
		name    string
		typ     common.DataType
		data    interface{}
		wantErr bool
	}{
		{"string as int64", common.TypeInt64, []string{"not_int64"}, true},
		{"int64 as float64", common.TypeFloat64, []int64{1}, true},
		{"string as timestamp", common.TypeTimestamp, []string{"not_ts"}, true},
		{"int64 as string", common.TypeString, []int64{1}, true},
		{"string as bool", common.TypeBool, []string{"not_bool"}, true},
		{"int64 plain", common.TypeInt64, []int64{1, 2, 3}, false},
		{"float64 plain", common.TypeFloat64, []float64{1.1, 2.2}, false},
		{"timestamp plain", common.TypeTimestamp, []int64{1000, 2000}, false},
		{"string dict", common.TypeString, []string{"a", "b"}, false},
		{"bool bitmap", common.TypeBool, []uint64{1, 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := EncodeColumn(tt.typ, tt.data, 2, nil)
			if tt.wantErr {
				if err == nil {
					t.Error("期望返回错误，得到 nil")
				}
			} else {
				if err != nil {
					t.Fatalf("不期望错误: %v", err)
				}
				if enc == nil {
					t.Fatal("期望非 nil EncodedColumn")
				}
			}
		})
	}
}

// TestEncodeColumn_RLEWithNulls 测试 RLE 编码带 null 值的 int64 数据。
func TestEncodeColumn_RLEWithNulls(t *testing.T) {
	ints := make([]int64, 100)
	for i := range ints {
		ints[i] = int64(i / 50)
	}
	nulls := common.NewBitmap(100)
	nulls.Set(10)
	nulls.Set(60)

	enc, err := EncodeColumn(common.TypeInt64, ints, 100, nulls)
	if err != nil {
		t.Fatalf("EncodeColumn RLE with nulls 失败: %v", err)
	}
	if enc.Encoding != EncodingRLE {
		t.Errorf("期望 RLE 编码，得到 %v", enc.Encoding)
	}

	decoded, decodedNulls, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn 失败: %v", err)
	}
	decodedInts, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("期望 []int64，得到 %T", decoded)
	}
	if len(decodedInts) != 100 {
		t.Errorf("期望 100 个元素，得到 %d", len(decodedInts))
	}
	if decodedNulls == nil || !decodedNulls.Get(10) {
		t.Error("期望位置 10 为 null")
	}
}
