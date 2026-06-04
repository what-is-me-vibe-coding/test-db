package storage

import (
	"os"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestSegmentSerializeDeserializeInt64(t *testing.T) {
	rowCount := uint32(100)
	ints := make([]int64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i) - 50
	}

	verifySegmentRoundTripInt64(t, ints, rowCount, nil, 1, "key-0", "key-99")
}

func TestSegmentSerializeDeserializeMultiColumn(t *testing.T) {
	rowCount := uint32(50)
	ints := make([]int64, rowCount)
	floats := make([]float64, rowCount)
	strs := make([]string, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i)
		floats[i] = float64(i) * 0.5
		strs[i] = "row-" + string(rune('A'+i%26))
	}

	builder := NewSegmentBuilder(2, "key-0", "key-49")
	addColumnOrFail(t, builder, common.TypeInt64, ints, rowCount, nil)
	addColumnOrFail(t, builder, common.TypeFloat64, floats, rowCount, nil)
	addColumnOrFail(t, builder, common.TypeString, strs, rowCount, nil)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	if len(restored.Columns) != 3 {
		t.Fatalf("Columns count: got %d, want 3", len(restored.Columns))
	}
	if len(restored.Footer.ColumnStats) != 3 {
		t.Fatalf("ColumnStats count: got %d, want 3", len(restored.Footer.ColumnStats))
	}

	verifyDecodedColumn(t, &restored.Columns[0], 0, int(rowCount))
	verifyDecodedColumn(t, &restored.Columns[1], 1, int(rowCount))
	verifyDecodedColumn(t, &restored.Columns[2], 2, int(rowCount))
}

func TestSegmentSerializeDeserializeWithNulls(t *testing.T) {
	rowCount := uint32(20)
	ints := make([]int64, rowCount)
	nulls := common.NewBitmap(rowCount)
	nulls.Set(0)
	nulls.Set(5)
	nulls.Set(10)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i) * 10
	}

	enc, err := EncodeColumn(common.TypeInt64, ints, rowCount, nulls)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(3, "key-0", "key-19")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	restoredCol := &restored.Columns[0]
	if err := DecompressColumn(restoredCol); err != nil {
		t.Fatalf("decompress: %v", err)
	}

	decoded, restoredNulls, err := DecodeColumn(restoredCol)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if restoredNulls == nil {
		t.Fatal("nulls not restored")
	}
	if restoredNulls.Count() != 3 {
		t.Errorf("null count: got %d, want 3", restoredNulls.Count())
	}
	for _, pos := range []uint32{0, 5, 10} {
		if !restoredNulls.Get(pos) {
			t.Errorf("expected null at position %d", pos)
		}
	}
	_ = decoded
}

func TestSegmentSerializeDeserializeBool(t *testing.T) {
	rowCount := uint32(16)
	bools := make([]uint64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		bools[i] = uint64(i % 2)
	}

	enc, err := EncodeColumn(common.TypeBool, bools, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(4, "key-0", "key-15")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	restoredCol := &restored.Columns[0]
	if err := DecompressColumn(restoredCol); err != nil {
		t.Fatalf("decompress: %v", err)
	}

	decoded, _, err := DecodeColumn(restoredCol)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decodedBools, ok := decoded.([]uint64)
	if !ok {
		t.Fatalf("decoded type: got %T, want []uint64", decoded)
	}
	for i := uint32(0); i < rowCount; i++ {
		if decodedBools[i] != bools[i] {
			t.Errorf("row %d: got %d, want %d", i, decodedBools[i], bools[i])
		}
	}
}

func TestSegmentDeserializeInvalidMagic(t *testing.T) {
	data := make([]byte, 22)
	_, err := DeserializeSegment(data)
	if err == nil {
		t.Error("expected error for invalid magic")
	}
}

func TestSegmentDeserializeTooShort(t *testing.T) {
	data := make([]byte, 10)
	_, err := DeserializeSegment(data)
	if err == nil {
		t.Error("expected error for too short data")
	}
}

func TestSegmentSerializeDeserializeEmpty(t *testing.T) {
	enc, err := EncodeColumn(common.TypeInt64, []int64{}, 0, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(6, "", "")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	if restored.RowCount != 0 {
		t.Errorf("RowCount: got %d, want 0", restored.RowCount)
	}
}

func TestSegmentSerializeDeserializeTimestamp(t *testing.T) {
	rowCount := uint32(10)
	times := make([]int64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		times[i] = int64(i) * 1000000000
	}

	enc, err := EncodeColumn(common.TypeTimestamp, times, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(7, "key-0", "key-9")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	restoredCol := &restored.Columns[0]
	if err := DecompressColumn(restoredCol); err != nil {
		t.Fatalf("decompress: %v", err)
	}

	decoded, _, err := DecodeColumn(restoredCol)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decodedTimes, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("decoded type: got %T, want []int64", decoded)
	}
	if len(decodedTimes) != int(rowCount) {
		t.Fatalf("decoded length: got %d, want %d", len(decodedTimes), rowCount)
	}
}

func TestSegmentWriteToFile(t *testing.T) {
	rowCount := uint32(100)
	ints := make([]int64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i)
	}

	enc, err := EncodeColumn(common.TypeInt64, ints, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(8, "key-0", "key-99")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "segment-*.dat")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := tmpFile.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	readData, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	restored, err := DeserializeSegment(readData)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	if restored.RowCount != rowCount {
		t.Errorf("RowCount: got %d, want %d", restored.RowCount, rowCount)
	}
	if len(restored.Columns) != 1 {
		t.Fatalf("Columns count: got %d, want 1", len(restored.Columns))
	}
}

func TestSegmentSerializeDeserializeString(t *testing.T) {
	rowCount := uint32(10)
	strs := make([]string, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		strs[i] = "hello-world-" + string(rune('A'+i))
	}

	enc, err := EncodeColumn(common.TypeString, strs, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(10, "key-0", "key-9")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	restoredCol := &restored.Columns[0]
	if err := DecompressColumn(restoredCol); err != nil {
		t.Fatalf("decompress: %v", err)
	}

	decoded, _, err := DecodeColumn(restoredCol)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decodedStrs, ok := decoded.([]string)
	if !ok {
		t.Fatalf("decoded type: got %T, want []string", decoded)
	}
	for i := uint32(0); i < rowCount; i++ {
		if decodedStrs[i] != strs[i] {
			t.Errorf("row %d: got %q, want %q", i, decodedStrs[i], strs[i])
		}
	}
}

func TestSegmentSerializeDeserializeLargeData(t *testing.T) {
	rowCount := uint32(1000)
	ints := make([]int64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i)
	}

	enc, err := EncodeColumn(common.TypeInt64, ints, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(11, "key-0", "key-999")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	if restored.RowCount != rowCount {
		t.Errorf("RowCount: got %d, want %d", restored.RowCount, rowCount)
	}

	restoredCol := &restored.Columns[0]
	if err := DecompressColumn(restoredCol); err != nil {
		t.Fatalf("decompress: %v", err)
	}

	decoded, _, err := DecodeColumn(restoredCol)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decodedInts, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("decoded type: got %T, want []int64", decoded)
	}
	for i := uint32(0); i < rowCount; i++ {
		if decodedInts[i] != ints[i] {
			t.Errorf("row %d: got %d, want %d", i, decodedInts[i], ints[i])
			break
		}
	}
}

func TestSegmentBuilderSetBloomFPRate(t *testing.T) {
	keys := []string{"a", "b", "c"}
	values := []int64{1, 2, 3}

	builder := NewSegmentBuilder(100, "a", "c")
	builder.SetKeys(keys)
	builder.SetBloomFPRate(0.001) // Custom FP rate

	enc, err := EncodeColumn(common.TypeInt64, values, 3, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(seg.Footer.BloomFilter) == 0 {
		t.Error("expected bloom filter to be built with custom FP rate")
	}
}

func TestSegmentGetColumnValueOutOfRangeIndex(t *testing.T) {
	seg := buildTestSegmentForSegment(t)

	// Request column index that doesn't exist
	_, err := seg.GetColumnValue(99, 0)
	if err == nil {
		t.Error("expected error for out-of-range column index")
	}
}

func TestSegmentFindRowByKeyNotFoundInList(t *testing.T) {
	seg := &Segment{Keys: []string{"a", "c", "e"}}
	_, found := seg.FindRowByKey("b")
	if found {
		t.Error("expected false for key not in sorted list")
	}
}

func TestSegmentForEachColumnStat(t *testing.T) {
	seg := buildTestSegmentForSegment(t)

	var colIDs []uint32
	seg.ForEachColumnStat(func(colID uint32, _ common.DataType, _, _ []byte, _ uint32) {
		colIDs = append(colIDs, colID)
	})
	if len(colIDs) == 0 {
		t.Error("expected at least one column stat")
	}
}

func TestSegmentGetAllColumnValuesFromBuilder(t *testing.T) {
	seg := buildTestSegmentForSegment(t)
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	vals, err := seg.GetAllColumnValues(0, colMeta)
	if err != nil {
		t.Fatalf("GetAllColumnValues: %v", err)
	}
	if len(vals) == 0 {
		t.Error("expected at least one column value")
	}
}

func buildTestSegmentForSegment(t *testing.T) *Segment {
	t.Helper()
	keys := []string{"a", "b", "c"}
	values := []int64{1, 2, 3}

	builder := NewSegmentBuilder(50, "a", "c")
	builder.SetKeys(keys)

	enc, err := EncodeColumn(common.TypeInt64, values, 3, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return seg
}
