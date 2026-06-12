package storage

import (
	"bytes"
	"testing"
)

const crCol1 = "col1"
const crCol0 = "col0"
const crCol = "col"

// TestCompressEmptyReturnsNil_V7 测试 Compress 对空数据返回 nil, nil。
func TestCompressEmptyReturnsNil_V7(t *testing.T) {
	result, err := Compress([]byte{})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if result != nil {
		t.Errorf("期望 nil，实际 %d 字节", len(result))
	}
}

// TestCompressColumnNil_V7 测试 CompressColumn 对 nil EncodedColumn 返回错误。
func TestCompressColumnNil_V7(t *testing.T) {
	err := CompressColumn(nil)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestDecompressColumnNil_V7 测试 DecompressColumn 对 nil EncodedColumn 返回错误。
func TestDecompressColumnNil_V7(t *testing.T) {
	err := DecompressColumn(nil)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestDecompressColumnInvalidData_V7 测试 DecompressColumn 对无效压缩数据返回错误。
func TestDecompressColumnInvalidData_V7(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     0,
		RowCount: 1,
		Data:     []byte{0xFF, 0xFE, 0xFD, 0xFC}, // 无效的 zstd 数据
	}
	err := DecompressColumn(enc)
	if err == nil {
		t.Fatal("期望解压错误，实际返回 nil")
	}
}

// TestCompressDecompressRoundTripSmall_V7 测试小数据的压缩/解压往返。
func TestCompressDecompressRoundTripSmall_V7(t *testing.T) {
	data := []byte("hello zstd")
	compressed, err := Compress(data)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress 失败: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Errorf("往返不匹配: 期望 %q, 实际 %q", string(data), string(decompressed))
	}
}

// TestCompressDecompressRoundTripMedium_V7 测试中等大小数据的压缩/解压往返。
func TestCompressDecompressRoundTripMedium_V7(t *testing.T) {
	data := bytes.Repeat([]byte("medium data block "), 500)
	compressed, err := Compress(data)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress 失败: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Errorf("往返不匹配: 长度 期望 %d, 实际 %d", len(data), len(decompressed))
	}
}

// TestEncoderDecoderPoolReuse_V7 测试编码器/解码器池的复用。
func TestEncoderDecoderPoolReuse_V7(t *testing.T) {
	// 第一次压缩/解压：创建新的编码器/解码器
	data1 := []byte("first compression")
	compressed1, err := Compress(data1)
	if err != nil {
		t.Fatalf("第一次 Compress 失败: %v", err)
	}
	_, err = Decompress(compressed1)
	if err != nil {
		t.Fatalf("第一次 Decompress 失败: %v", err)
	}

	// 第二次压缩/解压：应从池中复用编码器/解码器
	data2 := []byte("second compression test data")
	compressed2, err := Compress(data2)
	if err != nil {
		t.Fatalf("第二次 Compress 失败: %v", err)
	}
	decompressed2, err := Decompress(compressed2)
	if err != nil {
		t.Fatalf("第二次 Decompress 失败: %v", err)
	}
	if !bytes.Equal(decompressed2, data2) {
		t.Errorf("池复用后往返不匹配")
	}
}

// TestDecompressInvalidZstdData_V7 测试 Decompress 对无效 zstd 数据返回错误。
func TestDecompressInvalidZstdData_V7(t *testing.T) {
	_, err := Decompress([]byte{0x00, 0x01, 0x02, 0x03})
	if err == nil {
		t.Fatal("期望解压错误，实际返回 nil")
	}
}

// TestCompressDecompressSingleByte_V7 测试单字节数据的压缩/解压。
func TestCompressDecompressSingleByte_V7(t *testing.T) {
	data := []byte{0x42}
	compressed, err := Compress(data)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress 失败: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Errorf("单字节往返不匹配: 期望 %v, 实际 %v", data, decompressed)
	}
}

// TestCompressColumnWithEmptyData_V7 测试 CompressColumn 对空数据列的处理。
func TestCompressColumnWithEmptyData_V7(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     0,
		RowCount: 0,
		Data:     []byte{},
	}
	err := CompressColumn(enc)
	if err != nil {
		t.Fatalf("CompressColumn 空数据不应报错: %v", err)
	}
	// 空数据压缩后 Data 应为 nil
	if enc.Data != nil {
		t.Errorf("期望 Data 为 nil，实际 %d 字节", len(enc.Data))
	}
}
