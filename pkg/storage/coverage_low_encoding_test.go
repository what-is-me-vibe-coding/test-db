package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestEncodingTypeStringUnknown_V7 测试 EncodingType.String() 对未知类型的输出。
func TestEncodingTypeStringUnknown_V7(t *testing.T) {
	unknown := EncodingType(42)
	got := unknown.String()
	want := "Unknown(42)"
	if got != want {
		t.Errorf("期望 %q, 实际 %q", want, got)
	}
}

// TestDecodeColumnUnknownEncoding_V7 测试 DecodeColumn 对未知编码类型返回错误。
func TestDecodeColumnUnknownEncoding_V7(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingType(99),
		Type:     common.TypeInt64,
		RowCount: 1,
		Data:     []byte{0, 0, 0, 0, 0, 0, 0, 0},
	}
	_, _, err := DecodeColumn(enc)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestEncodeColumnUnknownEncoding_V7 测试 EncodeColumn 的 default 分支。
// selectEncoding 对 TypeNull 返回 EncodingPlain，但 encodePlain 对 TypeNull 返回错误。
func TestEncodeColumnUnknownEncoding_V7(t *testing.T) {
	_, err := EncodeColumn(common.TypeNull, nil, 1, nil)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestEncodePlainUnsupportedType_V7 测试 encodePlain 对不支持类型的错误路径。
func TestEncodePlainUnsupportedType_V7(t *testing.T) {
	_, err := encodePlain(common.DataType(99), nil, 1, nil)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestEncodeDictNonStringType_V7 测试 encodeDict 对非字符串类型的错误路径。
func TestEncodeDictNonStringType_V7(t *testing.T) {
	_, err := encodeDict(common.TypeInt64, []int64{1, 2}, 2, nil)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestEncodeRLENonInt64Type_V7 测试 encodeRLE 对非 int64 类型的错误路径。
func TestEncodeRLENonInt64Type_V7(t *testing.T) {
	_, err := encodeRLE(common.TypeString, []string{"a"}, 1, nil)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestEncodeBitmapWrongDataType_V7 测试 encodeBitmap 对错误数据类型的处理。
func TestEncodeBitmapWrongDataType_V7(t *testing.T) {
	_, err := encodeBitmap([]int64{1, 0}, 2, nil)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestDecodePlainUnsupportedType_V7 测试 decodePlain 对不支持类型的错误路径。
func TestDecodePlainUnsupportedType_V7(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     common.DataType(99),
		RowCount: 1,
		Data:     []byte{0, 0, 0, 0, 0, 0, 0, 0},
	}
	_, _, err := decodePlain(enc)
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
}

// TestDecodeDictOutOfRangeIndex_V7 测试 decodeDict 对越界索引的错误路径。
func TestDecodeDictOutOfRangeIndex_V7(t *testing.T) {
	// 构造一个字典编码列，其中索引指向不存在的字典条目
	enc := &EncodedColumn{
		Encoding: EncodingDict,
		Type:     common.TypeString,
		RowCount: 1,
		Data:     []byte{0x02}, // 索引 2，但字典只有 1 个条目
		Dict:     []string{"hello"},
	}
	_, _, err := DecodeColumn(enc)
	if err == nil {
		t.Fatal("期望越界索引错误，实际返回 nil")
	}
}

// TestReadIndexDefaultWidth_V7 测试 readIndex 对无效 width 的默认返回值。
func TestReadIndexDefaultWidth_V7(t *testing.T) {
	buf := []byte{0xAB, 0xCD, 0xEF, 0x01}
	result := readIndex(buf, 0, 3) // width=3 不在 1/2/4 中
	if result != 0 {
		t.Errorf("期望 0，实际 %d", result)
	}
}

// TestSelectEncodingFloat64_V7 测试 selectEncoding 对 float64 类型返回 Plain。
func TestSelectEncodingFloat64_V7(t *testing.T) {
	enc := selectEncoding(common.TypeFloat64, nil, 10)
	if enc != EncodingPlain {
		t.Errorf("期望 EncodingPlain，实际 %v", enc)
	}
}

// TestSelectEncodingTimestamp_V7 测试 selectEncoding 对 timestamp 类型返回 Plain。
func TestSelectEncodingTimestamp_V7(t *testing.T) {
	enc := selectEncoding(common.TypeTimestamp, nil, 10)
	if enc != EncodingPlain {
		t.Errorf("期望 EncodingPlain，实际 %v", enc)
	}
}

// TestIsRLEInt64ShortData_V7 测试 isRLEInt64 对少于 2 个元素的数据返回 false。
func TestIsRLEInt64ShortData_V7(t *testing.T) {
	if isRLEInt64([]int64{1}, 1) {
		t.Error("期望 false，实际 true")
	}
	if isRLEInt64(nil, 0) {
		t.Error("期望 false，实际 true")
	}
}

// TestIsRLEInt64WrongType_V7 测试 isRLEInt64 对非 []int64 类型返回 false。
func TestIsRLEInt64WrongType_V7(t *testing.T) {
	if isRLEInt64("not ints", 1) {
		t.Error("期望 false，实际 true")
	}
}

// TestEncodeDictWithNulls_V7 测试 encodeDict 带 null 值的编码。
func TestEncodeDictWithNulls_V7(t *testing.T) {
	strs := []string{"hello", "world", "hello"}
	nulls := common.NewBitmap(3)
	nulls.Set(1) // 第二行为 null

	enc, err := encodeDict(common.TypeString, strs, 3, nulls)
	if err != nil {
		t.Fatalf("encodeDict 失败: %v", err)
	}
	if enc.Encoding != EncodingDict {
		t.Errorf("期望 EncodingDict，实际 %v", enc.Encoding)
	}
}

// TestEncodeRLEWithNulls_V7 测试 encodeRLE 带 null 值的编码。
func TestEncodeRLEWithNulls_V7(t *testing.T) {
	ints := []int64{1, 1, 1, 2, 2}
	nulls := common.NewBitmap(5)
	nulls.Set(2) // 第三行为 null

	enc, err := encodeRLE(common.TypeInt64, ints, 5, nulls)
	if err != nil {
		t.Fatalf("encodeRLE 失败: %v", err)
	}
	if enc.Encoding != EncodingRLE {
		t.Errorf("期望 EncodingRLE，实际 %v", enc.Encoding)
	}
}
