package storage

import (
	"math"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestEncodingTypeString(t *testing.T) {
	tests := []struct {
		enc  EncodingType
		want string
	}{
		{EncodingPlain, "Plain"},
		{EncodingDict, "Dict"},
		{EncodingRLE, "RLE"},
		{EncodingBitmap, "Bitmap"},
		{EncodingType(99), "Unknown(99)"},
	}
	for _, tt := range tests {
		got := tt.enc.String()
		if got != tt.want {
			t.Errorf("EncodingType(%d).String() = %q, want %q", tt.enc, got, tt.want)
		}
	}
}

func TestEncodeDecodePlainInt64(t *testing.T) {
	data := []int64{0, 1, -1, math.MaxInt64, math.MinInt64, 42, -100}
	enc, err := EncodeColumn(common.TypeInt64, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingPlain {
		t.Errorf("encoding = %v, want Plain", enc.Encoding)
	}
	if enc.RowCount != uint32(len(data)) {
		t.Errorf("rowCount = %d, want %d", enc.RowCount, len(data))
	}

	decoded, nulls, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	if nulls != nil {
		t.Error("expected no nulls in plain int64 decode")
	}
	ints, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("expected []int64, got %T", decoded)
	}
	if len(ints) != len(data) {
		t.Fatalf("len = %d, want %d", len(ints), len(data))
	}
	for i, v := range data {
		if ints[i] != v {
			t.Errorf("row %d = %d, want %d", i, ints[i], v)
		}
	}
}

func TestEncodeDecodePlainFloat64(t *testing.T) {
	data := []float64{0.0, 1.5, -3.14, math.MaxFloat64, math.SmallestNonzeroFloat64}
	enc, err := EncodeColumn(common.TypeFloat64, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingPlain {
		t.Errorf("encoding = %v, want Plain", enc.Encoding)
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	floats, ok := decoded.([]float64)
	if !ok {
		t.Fatalf("expected []float64, got %T", decoded)
	}
	for i, v := range data {
		if floats[i] != v {
			t.Errorf("row %d = %f, want %f", i, floats[i], v)
		}
	}
}

func TestEncodeDecodePlainTimestamp(t *testing.T) {
	data := []int64{0, 1, 1620000000000000000, -1}
	enc, err := EncodeColumn(common.TypeTimestamp, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingPlain {
		t.Errorf("encoding = %v, want Plain", enc.Encoding)
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	times, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("expected []int64, got %T", decoded)
	}
	for i, v := range data {
		if times[i] != v {
			t.Errorf("row %d = %d, want %d", i, times[i], v)
		}
	}
}

const (
	testStrHello = "hello"
	testStrApple = "apple"
	testStrWorld = "world"
)

func TestEncodeDecodePlainString(t *testing.T) {
	data := []string{testStrHello, testStrWorld, "", "test", "foo"}
	enc, err := EncodeColumn(common.TypeString, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingDict {
		t.Errorf("encoding = %v, want Dict", enc.Encoding)
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	strs, ok := decoded.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", decoded)
	}
	for i, v := range data {
		if strs[i] != v {
			t.Errorf("row %d = %q, want %q", i, strs[i], v)
		}
	}
}

func TestEncodeDecodeDictString(t *testing.T) {
	data := []string{testStrApple, "banana", testStrApple, testStrApple, "banana", "cherry", testStrApple}
	enc, err := EncodeColumn(common.TypeString, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingDict {
		t.Errorf("encoding = %v, want Dict", enc.Encoding)
	}
	if len(enc.Dict) != 3 {
		t.Errorf("dict size = %d, want 3", len(enc.Dict))
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	strs, ok := decoded.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", decoded)
	}
	for i, v := range data {
		if strs[i] != v {
			t.Errorf("row %d = %q, want %q", i, strs[i], v)
		}
	}
}

func TestEncodeDecodeDictStringWithNulls(t *testing.T) {
	data := []string{"a", "b", "a", "c", "b"}
	nulls := common.NewBitmap(5)
	nulls.Set(1)
	nulls.Set(3)

	enc, err := EncodeColumn(common.TypeString, data, uint32(len(data)), nulls)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}

	decoded, decodedNulls, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	strs, ok := decoded.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", decoded)
	}

	for i := uint32(0); i < 5; i++ {
		if nulls.Get(i) != decodedNulls.Get(i) {
			t.Errorf("row %d null mismatch: expected %v, got %v", i, nulls.Get(i), decodedNulls.Get(i))
		}
		if !nulls.Get(i) && strs[i] != data[i] {
			t.Errorf("row %d = %q, want %q", i, strs[i], data[i])
		}
	}
}

func TestEncodeDecodeDictLargeIndex(t *testing.T) {
	const n = 300
	data := make([]string, n)
	for i := 0; i < n; i++ {
		data[i] = "value_" + string(rune('a'+i%26))
	}
	enc, err := EncodeColumn(common.TypeString, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	strs, ok := decoded.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", decoded)
	}
	for i := 0; i < n; i++ {
		if strs[i] != data[i] {
			t.Errorf("row %d = %q, want %q", i, strs[i], data[i])
		}
	}
}

func TestEncodeDecodeRLEInt64(t *testing.T) {
	data := []int64{1, 1, 1, 1, 1, 2, 2, 3, 3, 3}
	enc, err := EncodeColumn(common.TypeInt64, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingRLE {
		t.Errorf("encoding = %v, want RLE", enc.Encoding)
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	ints, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("expected []int64, got %T", decoded)
	}
	for i, v := range data {
		if ints[i] != v {
			t.Errorf("row %d = %d, want %d", i, ints[i], v)
		}
	}
}

func TestEncodeDecodeRLEInt64WithNulls(t *testing.T) {
	data := []int64{1, 1, 0, 2, 2, 0, 3, 3, 3, 3}
	nulls := common.NewBitmap(10)
	nulls.Set(2)
	nulls.Set(5)

	enc, err := EncodeColumn(common.TypeInt64, data, uint32(len(data)), nulls)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingRLE {
		t.Errorf("encoding = %v, want RLE", enc.Encoding)
	}

	decoded, decodedNulls, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	ints, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("expected []int64, got %T", decoded)
	}

	for i := uint32(0); i < 10; i++ {
		if nulls.Get(i) != decodedNulls.Get(i) {
			t.Errorf("row %d null mismatch: expected %v, got %v", i, nulls.Get(i), decodedNulls.Get(i))
		}
		if !nulls.Get(i) && ints[i] != data[i] {
			t.Errorf("row %d = %d, want %d", i, ints[i], data[i])
		}
	}
}

func TestEncodeDecodeBitmap(t *testing.T) {
	data := []uint64{1, 0, 1, 1, 0, 0, 1, 0}
	enc, err := EncodeColumn(common.TypeBool, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.Encoding != EncodingBitmap {
		t.Errorf("encoding = %v, want Bitmap", enc.Encoding)
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	bools, ok := decoded.([]uint64)
	if !ok {
		t.Fatalf("expected []uint64, got %T", decoded)
	}
	for i, v := range data {
		if bools[i] != v {
			t.Errorf("row %d = %d, want %d", i, bools[i], v)
		}
	}
}

func TestEncodeDecodeEmpty(t *testing.T) {
	enc, err := EncodeColumn(common.TypeInt64, []int64{}, 0, nil)
	if err != nil {
		t.Fatalf("EncodeColumn failed: %v", err)
	}
	if enc.RowCount != 0 {
		t.Errorf("rowCount = %d, want 0", enc.RowCount)
	}

	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn failed: %v", err)
	}
	ints, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("expected []int64, got %T", decoded)
	}
	if len(ints) != 0 {
		t.Errorf("len = %d, want 0", len(ints))
	}
}

func TestSelectEncodingInt64RLE(t *testing.T) {
	data := []int64{1, 1, 1, 1, 1, 2, 2, 3, 3, 3}
	enc := selectEncoding(common.TypeInt64, data, uint32(len(data)))
	if enc != EncodingRLE {
		t.Errorf("encoding = %v, want RLE for repetitive data", enc)
	}
}

func TestSelectEncodingInt64Plain(t *testing.T) {
	data := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	enc := selectEncoding(common.TypeInt64, data, uint32(len(data)))
	if enc != EncodingPlain {
		t.Errorf("encoding = %v, want Plain for unique data", enc)
	}
}

func TestSelectEncodingTypeBool(t *testing.T) {
	enc := selectEncoding(common.TypeBool, nil, 10)
	if enc != EncodingBitmap {
		t.Errorf("encoding = %v, want Bitmap for bool type", enc)
	}
}

func TestSelectEncodingTypeString(t *testing.T) {
	enc := selectEncoding(common.TypeString, nil, 10)
	if enc != EncodingDict {
		t.Errorf("encoding = %v, want Dict for string type", enc)
	}
}

func TestIndexWidth(t *testing.T) {
	tests := []struct {
		size     uint32
		hasNulls bool
		want     int
	}{
		{0, false, 1},
		{1, false, 1},
		{256, false, 1},
		{256, true, 2},
		{255, true, 1},
		{257, false, 2},
		{65535, false, 2},
		{65536, false, 2},
		{65536, true, 4},
		{65537, false, 4},
	}
	for _, tt := range tests {
		got := indexWidth(tt.size, tt.hasNulls)
		if got != tt.want {
			t.Errorf("indexWidth(%d, %v) = %d, want %d", tt.size, tt.hasNulls, got, tt.want)
		}
	}
}

func TestNullMarkerForWidth(t *testing.T) {
	tests := []struct {
		width int
		want  uint32
	}{
		{1, 0xFF},
		{2, 0xFFFF},
		{4, 0xFFFFFFFF},
	}
	for _, tt := range tests {
		got := nullMarkerForWidth(tt.width)
		if got != tt.want {
			t.Errorf("nullMarkerForWidth(%d) = %d, want %d", tt.width, got, tt.want)
		}
	}
}

func TestReadWriteIndex(t *testing.T) {
	tests := []struct {
		width int
		idx   uint32
	}{
		{1, 0},
		{1, 255},
		{2, 0},
		{2, 65535},
		{4, 0},
		{4, math.MaxUint32},
	}

	for _, tt := range tests {
		buf := make([]byte, tt.width)
		writeIndex(buf, 0, tt.width, tt.idx)
		got := readIndex(buf, 0, tt.width)
		if got != tt.idx {
			t.Errorf("width=%d: readWriteIndex = %d, want %d", tt.width, got, tt.idx)
		}
	}
}

func TestEncodeDecodeRoundTripInt64(t *testing.T) {
	data := []int64{10, 20, 30, 40, 50}
	enc, err := EncodeColumn(common.TypeInt64, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn: %v", err)
	}
	ints := decoded.([]int64)
	for i, v := range data {
		if ints[i] != v {
			t.Errorf("row %d = %d, want %d", i, ints[i], v)
		}
	}
}

func TestEncodeDecodeRoundTripFloat64(t *testing.T) {
	data := []float64{1.1, 2.2, 3.3}
	enc, err := EncodeColumn(common.TypeFloat64, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn: %v", err)
	}
	floats := decoded.([]float64)
	for i, v := range data {
		if floats[i] != v {
			t.Errorf("row %d = %f, want %f", i, floats[i], v)
		}
	}
}

func TestEncodeDecodeRoundTripString(t *testing.T) {
	data := []string{testStrHello, testStrWorld, testStrHello, "test"}
	enc, err := EncodeColumn(common.TypeString, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn: %v", err)
	}
	strs := decoded.([]string)
	for i, v := range data {
		if strs[i] != v {
			t.Errorf("row %d = %q, want %q", i, strs[i], v)
		}
	}
}

func TestEncodeDecodeRoundTripBool(t *testing.T) {
	data := []uint64{1, 0, 1, 1, 0}
	enc, err := EncodeColumn(common.TypeBool, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn: %v", err)
	}
	bools := decoded.([]uint64)
	for i, v := range data {
		if bools[i] != v {
			t.Errorf("row %d = %d, want %d", i, bools[i], v)
		}
	}
}

func TestEncodeDecodeRoundTripTimestamp(t *testing.T) {
	data := []int64{100, 200, 300}
	enc, err := EncodeColumn(common.TypeTimestamp, data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn: %v", err)
	}
	times := decoded.([]int64)
	for i, v := range data {
		if times[i] != v {
			t.Errorf("row %d = %d, want %d", i, times[i], v)
		}
	}
}

func TestEncodeColumnUnsupportedType(t *testing.T) {
	_, err := EncodeColumn(common.TypeNull, nil, 1, nil)
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestEncodeColumnInvalidData(t *testing.T) {
	_, err := EncodeColumn(common.TypeInt64, "not ints", 1, nil)
	if err == nil {
		t.Error("expected error for invalid data type")
	}
}

func TestDecodeColumnCorruptedData(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingDict,
		Type:     common.TypeString,
		RowCount: 1,
		Data:     []byte{0x02},
		Dict:     []string{"a", "b"},
	}
	_, _, err := DecodeColumn(enc)
	if err == nil {
		t.Error("expected error for corrupted dict index")
	}
}

func TestNullBitmapRoundTrip(t *testing.T) {
	t.Run("Int64", func(t *testing.T) {
		data := []int64{1, 0, 2, 0, 3}
		nulls := common.NewBitmap(5)
		nulls.Set(1)
		nulls.Set(3)

		enc, err := EncodeColumn(common.TypeInt64, data, 5, nulls)
		if err != nil {
			t.Fatalf("EncodeColumn: %v", err)
		}
		decoded, decodedNulls, err := DecodeColumn(enc)
		if err != nil {
			t.Fatalf("DecodeColumn: %v", err)
		}
		if decodedNulls == nil {
			t.Fatal("expected non-nil nulls")
		}
		for i := uint32(0); i < 5; i++ {
			if nulls.Get(i) != decodedNulls.Get(i) {
				t.Errorf("row %d null mismatch: %v vs %v", i, nulls.Get(i), decodedNulls.Get(i))
			}
		}
		_ = decoded
	})

	t.Run("Float64", func(t *testing.T) {
		data := []float64{1.0, 0.0, 2.0}
		nulls := common.NewBitmap(3)
		nulls.Set(1)

		enc, err := EncodeColumn(common.TypeFloat64, data, 3, nulls)
		if err != nil {
			t.Fatalf("EncodeColumn: %v", err)
		}
		_, decodedNulls, err := DecodeColumn(enc)
		if err != nil {
			t.Fatalf("DecodeColumn: %v", err)
		}
		if decodedNulls == nil {
			t.Fatal("expected non-nil nulls")
		}
		if !decodedNulls.Get(1) {
			t.Error("row 1 should be null")
		}
	})
}

func TestEncodePlainStrings(t *testing.T) {
	data := []string{testStrHello, testStrWorld, "", "foo"}
	enc, err := encodePlainStrings(data, uint32(len(data)), nil)
	if err != nil {
		t.Fatalf("encodePlainStrings: %v", err)
	}
	if enc.Encoding != EncodingPlain {
		t.Errorf("encoding = %v, want Plain", enc.Encoding)
	}
	if len(enc.Offsets) != 5 {
		t.Errorf("offsets len = %d, want 5", len(enc.Offsets))
	}
}

func TestEncodePlainStringsWithNulls(t *testing.T) {
	data := []string{"a", "b", "c"}
	nulls := common.NewBitmap(3)
	nulls.Set(1)

	enc, err := encodePlainStrings(data, 3, nulls)
	if err != nil {
		t.Fatalf("encodePlainStrings: %v", err)
	}
	if len(enc.Nulls) == 0 {
		t.Error("expected nulls in encoded column")
	}
}

func TestEncodePlainInvalidTimestamp(t *testing.T) {
	_, err := encodePlain(common.TypeTimestamp, "not ints", 1, nil)
	if err == nil {
		t.Error("expected error for invalid timestamp data")
	}
}

func TestEncodeBitmapWithNulls(t *testing.T) {
	data := []uint64{1, 0, 1}
	nulls := common.NewBitmap(3)
	nulls.Set(1)

	enc, err := encodeBitmap(data, 3, nulls)
	if err != nil {
		t.Fatalf("encodeBitmap: %v", err)
	}
	if len(enc.Nulls) == 0 {
		t.Error("expected nulls in encoded column")
	}
}

func TestDecodeBitmapWithNulls(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingBitmap,
		Type:     common.TypeBool,
		RowCount: 3,
		Data:     common.NewBitmap(3).ToBytes(),
		Nulls:    common.NewBitmap(3).ToBytes(),
	}
	_, nulls, err := decodeBitmap(enc)
	if err != nil {
		t.Fatalf("decodeBitmap: %v", err)
	}
	if nulls == nil {
		t.Error("expected non-nil nulls")
	}
}

func TestDecodePlainString(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     common.TypeString,
		RowCount: 2,
		Data:     []byte("ab"),
		Offsets:  []uint32{0, 1, 2},
	}
	decoded, _, err := decodePlain(enc)
	if err != nil {
		t.Fatalf("decodePlain: %v", err)
	}
	strs := decoded.([]string)
	if strs[0] != "a" || strs[1] != "b" {
		t.Errorf("got %q, %q", strs[0], strs[1])
	}
}

func TestEncodeRLEInvalidType(t *testing.T) {
	_, err := encodeRLE(common.TypeFloat64, []float64{1.0}, 1, nil)
	if err == nil {
		t.Error("expected error for non-int64 RLE")
	}
}

func TestEncodeDictInvalidType(t *testing.T) {
	_, err := encodeDict(common.TypeInt64, []int64{1}, 1, nil)
	if err == nil {
		t.Error("expected error for non-string dict")
	}
}

func TestEncodingTypeUnknown(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: 99,
		Type:     common.TypeInt64,
		RowCount: 1,
		Data:     make([]byte, 8),
	}
	_, _, err := DecodeColumn(enc)
	if err == nil {
		t.Error("expected error for unknown encoding")
	}
}

func TestEncodeBitmapInvalidData(t *testing.T) {
	_, err := encodeBitmap("not bools", 1, nil)
	if err == nil {
		t.Error("expected error for invalid bitmap data")
	}
}

func TestDecodePlainUnsupportedType(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     common.TypeNull,
		RowCount: 1,
		Data:     []byte{0},
	}
	_, _, err := decodePlain(enc)
	if err == nil {
		t.Error("expected error for unsupported type in plain decode")
	}
}
