package storage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// --- OpenWAL 错误路径补充测试 ---

// TestOpenWALPartialBodyAfterValid 测试有效记录后跟部分 body 时的恢复
func TestOpenWALPartialBodyAfterValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	_ = w.AppendWrite([]byte("data1"))
	_ = w.Sync()
	_ = w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// 在有效数据后追加部分头部+部分 body（模拟崩溃）
	partialHeader := make([]byte, walHeaderSize)
	binary.LittleEndian.PutUint32(partialHeader, uint32(walTypeSize+walCRCSize+5))
	modifiedData := make([]byte, len(data)+len(partialHeader)+2)
	copy(modifiedData, data)
	copy(modifiedData[len(data):], partialHeader)
	// 只追加 2 字节 body（不足 totalLen 要求的长度）
	modifiedData = modifiedData[:len(data)+len(partialHeader)+2]

	if err := os.WriteFile(path, modifiedData, 0644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	if len(recs) != 1 {
		t.Errorf("expected 1 valid record, got %d", len(recs))
	}
}

// TestOpenWALRecoveryAndContinueAppendV2 测试恢复后继续追加记录
func TestOpenWALRecoveryAndContinueAppendV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	_ = w.AppendWrite([]byte("record1"))
	_ = w.AppendWrite([]byte("record2"))
	_ = w.Sync()
	_ = w.Close()

	recovered, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}

	if err := recovered.AppendWrite([]byte("record3")); err != nil {
		t.Fatalf("AppendWrite after recovery failed: %v", err)
	}
	_ = recovered.Sync()

	recovered2, recs2, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("second OpenWAL failed: %v", err)
	}
	defer func() { _ = recovered2.Close() }()

	if len(recs2) != 3 {
		t.Errorf("expected 3 records after second recovery, got %d", len(recs2))
	}
}

// --- Compress/Decompress 错误路径补充测试 ---

// TestCompressColumnNilInputV2 测试 CompressColumn 传入 nil 时的错误
func TestCompressColumnNilInputV2(t *testing.T) {
	err := CompressColumn(nil)
	if err == nil {
		t.Fatal("expected error for nil EncodedColumn, got nil")
	}
}

// TestDecompressColumnNilInputV2 测试 DecompressColumn 传入 nil 时的错误
func TestDecompressColumnNilInputV2(t *testing.T) {
	err := DecompressColumn(nil)
	if err == nil {
		t.Fatal("expected error for nil EncodedColumn, got nil")
	}
}

// TestDecompressInvalidZstdData 测试解压无效 ZSTD 数据时的错误
func TestDecompressInvalidZstdData(t *testing.T) {
	_, err := Decompress([]byte{0xFF, 0xFE, 0xFD, 0xFC})
	if err == nil {
		t.Fatal("expected error for invalid compressed data, got nil")
	}
}

// TestDecompressColumnInvalidCompressedData 测试解压列数据失败时的错误
func TestDecompressColumnInvalidCompressedData(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     common.TypeInt64,
		RowCount: 1,
		Data:     []byte{0xFF, 0xFE, 0xFD, 0xFC}, // 无效的 ZSTD 数据
	}
	err := DecompressColumn(enc)
	if err == nil {
		t.Fatal("expected error for invalid compressed column data, got nil")
	}
}

// --- GetColumnValue 错误路径补充测试 ---

// TestGetColumnValueColIdxOutOfRange 测试列索引越界时的错误
func TestGetColumnValueColIdxOutOfRange(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{},
		Keys:    []string{},
	}

	_, err := seg.GetColumnValue(0, 0)
	if err == nil {
		t.Fatal("expected error for column index out of range, got nil")
	}
}

// TestGetColumnValueDecompressFailure 测试 GetColumnValue 解压失败时的错误
func TestGetColumnValueDecompressFailure(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{
			{
				Encoding: EncodingPlain,
				Type:     common.TypeInt64,
				RowCount: 1,
				Data:     []byte{0xFF, 0xFE, 0xFD, 0xFC}, // 无效压缩数据
			},
		},
		Keys: []string{"key1"},
	}

	_, err := seg.GetColumnValue(0, 0)
	if err == nil {
		t.Fatal("expected error for decompress failure in GetColumnValue, got nil")
	}
}

// --- DeserializeColumnBlock 错误路径补充测试 ---

// TestDeserializeColumnBlockDataTooShort 测试反序列化数据过短时的错误
func TestDeserializeColumnBlockDataTooShort(t *testing.T) {
	_, err := DeserializeColumnBlock([]byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Fatal("expected error for too short data, got nil")
	}
}

// TestDeserializeColumnBlockNullsOverflow 测试 nulls 数据超出缓冲区时的错误
func TestDeserializeColumnBlockNullsOverflow(t *testing.T) {
	data := make([]byte, 20)
	pos := 0
	binary.LittleEndian.PutUint32(data[pos:], 0)
	pos += 4
	data[pos] = byte(EncodingPlain)
	pos += 2
	data[pos] = byte(common.TypeInt64)
	pos++
	binary.LittleEndian.PutUint32(data[pos:], 1)
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 1000) // nullsLen 超出数据长度

	_, err := DeserializeColumnBlock(data)
	if err == nil {
		t.Fatal("expected error for truncated nulls data, got nil")
	}
}

// TestDeserializeColumnBlockDataOverflow 测试列数据超出缓冲区时的错误
func TestDeserializeColumnBlockDataOverflow(t *testing.T) {
	data := make([]byte, 20)
	pos := 0
	binary.LittleEndian.PutUint32(data[pos:], 0)
	pos += 4
	data[pos] = byte(EncodingPlain)
	pos += 2
	data[pos] = byte(common.TypeInt64)
	pos++
	binary.LittleEndian.PutUint32(data[pos:], 1)
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 0) // nullsLen = 0
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 1000) // dataLen 超出数据长度

	_, err := DeserializeColumnBlock(data)
	if err == nil {
		t.Fatal("expected error for truncated column data, got nil")
	}
}

// TestDeserializeColumnBlockOffsetsOverflow 测试 offsets 数据超出缓冲区时的错误
func TestDeserializeColumnBlockOffsetsOverflow(t *testing.T) {
	data := make([]byte, 40)
	pos := 0
	binary.LittleEndian.PutUint32(data[pos:], 0)
	pos += 4
	data[pos] = byte(EncodingPlain)
	pos += 2
	data[pos] = byte(common.TypeInt64)
	pos++
	binary.LittleEndian.PutUint32(data[pos:], 1)
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 0) // nullsLen = 0
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 8) // dataLen = 8
	pos += 4
	pos += 8                                        // skip data
	binary.LittleEndian.PutUint32(data[pos:], 1000) // offsetsLen 超出数据长度

	_, err := DeserializeColumnBlock(data)
	if err == nil {
		t.Fatal("expected error for truncated offsets data, got nil")
	}
}

// TestDeserializeColumnBlockDictOverflow 测试 dict 数据超出缓冲区时的错误
func TestDeserializeColumnBlockDictOverflow(t *testing.T) {
	data := make([]byte, 30)
	pos := 0
	binary.LittleEndian.PutUint32(data[pos:], 0)
	pos += 4
	data[pos] = byte(EncodingDict)
	pos += 2
	data[pos] = byte(common.TypeString)
	pos++
	binary.LittleEndian.PutUint32(data[pos:], 1)
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 0) // nullsLen = 0
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 1) // dataLen = 1
	pos += 4
	data[pos] = 0 // 1 字节数据
	pos++
	binary.LittleEndian.PutUint32(data[pos:], 0) // offsetsLen = 0
	pos += 4
	binary.LittleEndian.PutUint32(data[pos:], 1000) // dictLen 超出数据长度

	_, err := DeserializeColumnBlock(data)
	if err == nil {
		t.Fatal("expected error for truncated dict data, got nil")
	}
}

// --- EncodeColumn 错误路径补充测试 ---

// TestEncodeColumnEmptyDataV2 测试编码空数据
func TestEncodeColumnEmptyDataV2(t *testing.T) {
	enc, err := EncodeColumn(common.TypeInt64, []int64{}, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.RowCount != 0 {
		t.Errorf("expected 0 rowCount, got %d", enc.RowCount)
	}
}

// TestDecodeColumnUnknownEncodingType 测试解码未知编码类型时的错误
func TestDecodeColumnUnknownEncodingType(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingType(99),
		Type:     common.TypeInt64,
		RowCount: 1,
	}
	_, _, err := DecodeColumn(enc)
	if err == nil {
		t.Fatal("expected error for unknown encoding in DecodeColumn, got nil")
	}
}

// --- encodePlain 错误路径补充测试 ---

// TestEncodePlainUnsupportedDataType 测试编码不支持的类型时的错误
func TestEncodePlainUnsupportedDataType(t *testing.T) {
	_, err := encodePlain(common.DataType(99), nil, 1, nil)
	if err == nil {
		t.Fatal("expected error for unsupported type in encodePlain, got nil")
	}
}

// TestEncodePlainInt64TypeMismatch 测试编码时 int64 数据类型不匹配的错误
func TestEncodePlainInt64TypeMismatch(t *testing.T) {
	_, err := encodePlain(common.TypeInt64, []string{"not_int"}, 1, nil)
	if err == nil {
		t.Fatal("expected error for type mismatch in encodePlain, got nil")
	}
}

// TestEncodePlainFloat64TypeMismatch 测试 float64 类型不匹配
func TestEncodePlainFloat64TypeMismatch(t *testing.T) {
	_, err := encodePlain(common.TypeFloat64, []string{"not_float"}, 1, nil)
	if err == nil {
		t.Fatal("expected error for float type mismatch, got nil")
	}
}

// TestEncodePlainTimestampTypeMismatch 测试 timestamp 类型不匹配
func TestEncodePlainTimestampTypeMismatch(t *testing.T) {
	_, err := encodePlain(common.TypeTimestamp, []string{"not_timestamp"}, 1, nil)
	if err == nil {
		t.Fatal("expected error for timestamp type mismatch, got nil")
	}
}

// TestEncodePlainStringTypeMismatch 测试 string 类型不匹配
func TestEncodePlainStringTypeMismatch(t *testing.T) {
	_, err := encodePlain(common.TypeString, []int64{1, 2}, 2, nil)
	if err == nil {
		t.Fatal("expected error for string type mismatch, got nil")
	}
}

// --- encodeDict/encodeRLE 错误路径补充测试 ---

// TestEncodeDictNonStringType 测试字典编码非字符串类型时的错误
func TestEncodeDictNonStringType(t *testing.T) {
	_, err := encodeDict(common.TypeInt64, []int64{1, 2}, 2, nil)
	if err == nil {
		t.Fatal("expected error for non-string type in encodeDict, got nil")
	}
}

// TestEncodeDictTypeMismatch 测试字典编码数据类型不匹配
func TestEncodeDictTypeMismatch(t *testing.T) {
	_, err := encodeDict(common.TypeString, []int64{1, 2}, 2, nil)
	if err == nil {
		t.Fatal("expected error for type mismatch in encodeDict, got nil")
	}
}

// TestEncodeRLENonInt64Type 测试 RLE 编码非 int64 类型时的错误
func TestEncodeRLENonInt64Type(t *testing.T) {
	_, err := encodeRLE(common.TypeString, []string{"a", "b"}, 2, nil)
	if err == nil {
		t.Fatal("expected error for non-int64 type in encodeRLE, got nil")
	}
}

// TestEncodeRLETypeMismatch 测试 RLE 编码数据类型不匹配
func TestEncodeRLETypeMismatch(t *testing.T) {
	_, err := encodeRLE(common.TypeInt64, []string{"not_int"}, 1, nil)
	if err == nil {
		t.Fatal("expected error for type mismatch in encodeRLE, got nil")
	}
}

// TestEncodeBitmapTypeMismatch 测试 bitmap 编码数据类型不匹配
func TestEncodeBitmapTypeMismatch(t *testing.T) {
	_, err := encodeBitmap([]int64{1, 2}, 2, nil)
	if err == nil {
		t.Fatal("expected error for type mismatch in encodeBitmap, got nil")
	}
}

// --- extractValue 默认分支测试 ---

// TestExtractValueUnknownDataType 测试 extractValue 对未知类型的处理
func TestExtractValueUnknownDataType(t *testing.T) {
	cd := columnData{
		data:  nil,
		nulls: nil,
		typ:   common.DataType(99),
	}
	val := extractValue(cd, 0)
	if val.Valid {
		t.Error("expected null value for unknown type, got valid value")
	}
}

// --- Compaction 错误路径补充测试 ---

// TestCompactEmptySegmentsList 测试合并空 segment 列表时的错误
func TestCompactEmptySegmentsList(t *testing.T) {
	c := NewCompactor(t.TempDir())
	_, err := c.Compact(nil, nil)
	if err == nil {
		t.Fatal("expected error for empty segments, got nil")
	}
}

// TestCompactBuildSegmentColAppendError 测试 compaction 中列追加失败
func TestCompactBuildSegmentColAppendError(t *testing.T) {
	c := NewCompactor(t.TempDir())

	rows := []memRow{
		{Key: "a", Values: []common.Value{common.NewInt64(1)}},
	}

	// 使用不匹配的列元数据触发 Append 类型错误
	cols := []ColumnMeta{
		{ID: 0, Name: "col1", Type: common.TypeString}, // 期望 string，但数据是 int64
	}

	_, err := c.buildSegment(rows, cols)
	if err == nil {
		t.Fatal("expected error for column type mismatch in buildSegment, got nil")
	}
}

// --- Engine 错误路径补充测试 ---

// TestEngineFlushNoImmutable 测试没有 immutable MemTable 时 Flush 的行为
func TestEngineFlushNoImmutable(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: "col1", Type: common.TypeInt64}}
	err = eng.Flush(cols)
	if err != nil {
		t.Fatalf("Flush with no data should succeed, got: %v", err)
	}
}

// TestEngineCompactNoL0 测试没有 L0 Segment 时 Compact 的行为
func TestEngineCompactNoL0(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: "col1", Type: common.TypeInt64}}
	err = eng.Compact(cols)
	if err != nil {
		t.Fatalf("Compact with no L0 segments should succeed, got: %v", err)
	}
}

// TestEngineShouldCompactEmpty 测试不需要 Compaction 时的判断
func TestEngineShouldCompactEmpty(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	if eng.ShouldCompact() {
		t.Error("expected ShouldCompact=false for empty engine")
	}
}

// TestEngineL0SegmentCountNoSegments 测试空引擎的 L0SegmentCount
func TestEngineL0SegmentCountNoSegments(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	count := eng.L0SegmentCount()
	if count != 0 {
		t.Errorf("expected 0 L0 segments, got %d", count)
	}
}

// TestEngineSegmentCountNoSegments 测试空引擎的 SegmentCount
func TestEngineSegmentCountNoSegments(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	count := eng.SegmentCount()
	if count != 0 {
		t.Errorf("expected 0 segments, got %d", count)
	}
}

// TestEngineMemTableSizeNoData 测试空引擎的 MemTableSize
func TestEngineMemTableSizeNoData(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	size := eng.MemTableSize()
	if size != 0 {
		t.Errorf("expected 0 MemTable size, got %d", size)
	}
}

// TestEngineColumnMetaNoData 测试空引擎的 ColumnMeta
func TestEngineColumnMetaNoData(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	meta := eng.ColumnMeta()
	if len(meta) != 0 {
		t.Errorf("expected empty ColumnMeta, got %d items", len(meta))
	}
}

// TestEngineGetMissingKey 测试查询不存在的键
func TestEngineGetMissingKey(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_, ok := eng.Get("nonexistent_key")
	if ok {
		t.Error("expected false for non-existent key")
	}
}

// --- readIndex 默认分支测试 ---

// TestReadIndexInvalidWidthValue 测试 readIndex 对无效 width 的处理
func TestReadIndexInvalidWidthValue(t *testing.T) {
	buf := []byte{0x01, 0x02, 0x03, 0x04}
	result := readIndex(buf, 0, 3) // width=3 不在 1/2/4 范围内
	if result != 0 {
		t.Errorf("expected 0 for invalid width, got %d", result)
	}
}

// --- decodePlain 默认分支测试 ---

// TestDecodePlainUnsupportedDataType 测试解码不支持的类型
func TestDecodePlainUnsupportedDataType(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain,
		Type:     common.DataType(99),
		RowCount: 1,
		Data:     []byte{0x01},
	}
	_, _, err := decodePlain(enc)
	if err == nil {
		t.Fatal("expected error for unsupported type in decodePlain, got nil")
	}
}

// --- Compaction readSegmentRows 边界测试 ---

// TestCompactorReadSegmentRowsNoRows 测试读取空 Segment 的行
func TestCompactorReadSegmentRowsNoRows(t *testing.T) {
	c := NewCompactor(t.TempDir())
	seg := &Segment{
		ID:       1,
		RowCount: 0,
		Columns:  []EncodedColumn{},
		Keys:     []string{},
	}

	rows, err := c.readSegmentRows(seg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// --- Compaction mergeSegments 去重测试 ---

// TestCompactorMergeSegmentsDedupV2 测试合并时正确去重（保留最新版本）
func TestCompactorMergeSegmentsDedupV2(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("key2", map[string]common.Value{colVal: common.NewInt64(2)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 1: %v", err)
	}

	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(10)})
	_ = eng.Write("key3", map[string]common.Value{colVal: common.NewInt64(3)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 2: %v", err)
	}

	if err := eng.Compact(cols); err != nil {
		t.Fatalf("compact: %v", err)
	}

	row, ok := eng.Get("key1")
	if !ok {
		t.Fatal("key1 not found after compaction")
	}
	if row.Columns[colVal].Int64 != 10 {
		t.Errorf("key1: expected 10 (latest version), got %d", row.Columns[colVal].Int64)
	}

	row2, ok2 := eng.Get("key2")
	if !ok2 || row2.Columns[colVal].Int64 != 2 {
		t.Errorf("key2: expected 2, got %d, ok=%v", row2.Columns[colVal].Int64, ok2)
	}

	row3, ok3 := eng.Get("key3")
	if !ok3 || row3.Columns[colVal].Int64 != 3 {
		t.Errorf("key3: expected 3, got %d, ok=%v", row3.Columns[colVal].Int64, ok3)
	}
}

// --- Engine Flush 后 SegmentCount 测试 ---

// TestEngineSegmentCountAfterFlushV2 测试 Flush 后 SegmentCount 正确
func TestEngineSegmentCountAfterFlushV2(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if eng.SegmentCount() != 1 {
		t.Errorf("expected 1 segment after flush, got %d", eng.SegmentCount())
	}
	if eng.L0SegmentCount() != 1 {
		t.Errorf("expected 1 L0 segment after flush, got %d", eng.L0SegmentCount())
	}
}

// --- Engine Flush 设置 ColumnMeta 测试 ---

// TestEngineFlushSetsColumnMetaV2 测试 Flush 正确设置 ColumnMeta
func TestEngineFlushSetsColumnMetaV2(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{
		{ID: 0, Name: "name", Type: common.TypeString},
		{ID: 1, Name: "age", Type: common.TypeInt64},
	}

	_ = eng.Write("a", map[string]common.Value{"name": common.NewString("alice"), "age": common.NewInt64(30)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	meta := eng.ColumnMeta()
	if len(meta) != 2 {
		t.Fatalf("expected 2 column metas, got %d", len(meta))
	}
	if meta[0].Name != "name" || meta[1].Name != "age" {
		t.Errorf("unexpected column meta: %+v", meta)
	}
}

// --- Compaction CompactToLevel 测试 ---

// TestCompactorCompactToLevelV2 测试 CompactToLevel 方法
func TestCompactorCompactToLevelV2(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("b", map[string]common.Value{colVal: common.NewInt64(2)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	segments := eng.Segments()
	if len(segments) == 0 {
		t.Fatal("expected at least 1 segment")
	}

	c := NewCompactor(dir)
	c.SetNextID(eng.flusher.NextID())

	seg, err := c.CompactToLevel(segments, 1, cols)
	if err != nil {
		t.Fatalf("CompactToLevel failed: %v", err)
	}
	if seg == nil {
		t.Fatal("expected non-nil segment")
	}
}

// --- Compactor CleanupSegments 测试 ---

// TestCompactorCleanupMissingFile 测试清理不存在的文件时不报错
func TestCompactorCleanupMissingFile(t *testing.T) {
	c := NewCompactor(t.TempDir())
	segments := []*Segment{
		{ID: 999, FilePath: "/nonexistent/path/segment_999.widb"},
	}
	err := c.CleanupSegments(segments)
	if err != nil {
		t.Errorf("expected no error for non-existent file cleanup, got: %v", err)
	}
}

// --- Engine Close 测试 ---

// TestEngineCloseDoubleCloseV2 测试引擎重复关闭
func TestEngineCloseDoubleCloseV2(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if err := eng.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	err = eng.Close()
	if err == nil {
		t.Log("double close did not error (acceptable)")
	}
}

// --- Engine Segments 测试 ---

// TestEngineSegmentsAfterFlushV2 测试 Flush 后 Segments 返回正确
func TestEngineSegmentsAfterFlushV2(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	segs := eng.Segments()
	if len(segs) != 1 {
		t.Errorf("expected 1 segment, got %d", len(segs))
	}
}

// --- Engine Index Accessors 测试 ---

// TestEngineIndexAccessorsV2 测试引擎索引访问器
func TestEngineIndexAccessorsV2(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	if eng.PrimaryIndex() == nil {
		t.Error("expected non-nil PrimaryIndex")
	}
	if eng.BloomIndex() == nil {
		t.Error("expected non-nil BloomIndex")
	}
	if eng.SparseIndex() == nil {
		t.Error("expected non-nil SparseIndex")
	}
}

// --- WAL Truncate 测试 ---

// TestWALTruncateAndContinue 测试 WAL Truncate 正常流程
func TestWALTruncateAndContinue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	_ = w.AppendWrite([]byte("data1"))
	_ = w.Sync()

	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	if w.Size() != 0 {
		t.Errorf("expected size 0 after truncate, got %d", w.Size())
	}

	if err := w.AppendWrite([]byte("data2")); err != nil {
		t.Fatalf("AppendWrite after truncate failed: %v", err)
	}

	_ = w.Close()
}

// --- WAL Append payload 过大测试 ---

// TestWALAppendOversizedPayload 测试追加超大 payload 时的错误
func TestWALAppendOversizedPayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	largePayload := make([]byte, maxRecordPayload+1)
	err = w.AppendWrite(largePayload)
	if err == nil {
		t.Fatal("expected error for oversized payload, got nil")
	}
}

// --- readBloomFilter / readRawKeys 边界测试 ---

// TestReadBloomFilterTruncatedData 测试 readBloomFilter 数据截断
func TestReadBloomFilterTruncatedData(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 100) // bloomLen 超出数据长度
	_, _, err := readBloomFilter(data, 0)
	if err == nil {
		t.Fatal("expected error for truncated bloom filter data, got nil")
	}
}

// TestReadRawKeysTruncatedData 测试 readRawKeys 数据截断
func TestReadRawKeysTruncatedData(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 100) // rawKeysLen 超出数据长度
	_, _, err := readRawKeys(data, 0)
	if err == nil {
		t.Fatal("expected error for truncated raw keys data, got nil")
	}
}

// TestReadDictStringOverflow 测试 readDict 字符串长度超出数据
func TestReadDictStringOverflow(t *testing.T) {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data[0:], 1)   // dictLen = 1
	binary.LittleEndian.PutUint32(data[4:], 100) // strLen 超出数据长度
	_, err := readDict(data, 0, &EncodedColumn{})
	if err == nil {
		t.Fatal("expected error for truncated dict string data, got nil")
	}
}

// TestReadDictLengthFieldTruncated 测试 readDict 数据长度字段不足
func TestReadDictLengthFieldTruncated(t *testing.T) {
	data := make([]byte, 2) // 不足 4 字节读取 dictLen
	_, err := readDict(data, 0, &EncodedColumn{})
	if err == nil {
		t.Fatal("expected error for truncated dict length field, got nil")
	}
}

// --- deserializeFooter 边界测试 ---

// TestDeserializeFooterTooShortV2 测试反序列化过短的 footer
func TestDeserializeFooterTooShortV2(t *testing.T) {
	_, err := deserializeFooter([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for too short footer, got nil")
	}
}

// --- DeserializeSegment 边界测试 ---

// TestDeserializeSegmentDataTooShort 测试反序列化过短的 segment 数据
func TestDeserializeSegmentDataTooShort(t *testing.T) {
	_, err := DeserializeSegment([]byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Fatal("expected error for too short segment data, got nil")
	}
}

// TestDeserializeSegmentBadMagic 测试反序列化 magic 不匹配
func TestDeserializeSegmentBadMagic(t *testing.T) {
	data := make([]byte, 22)
	binary.LittleEndian.PutUint32(data[0:], 0xDEADBEEF) // 无效 magic
	_, err := DeserializeSegment(data)
	if err == nil {
		t.Fatal("expected error for invalid magic, got nil")
	}
}

// --- readOffsets 边界测试 ---

// TestReadOffsetsLengthFieldTruncated 测试 readOffsets 长度字段超出数据
func TestReadOffsetsLengthFieldTruncated(t *testing.T) {
	data := make([]byte, 2) // 不足 4 字节
	_, err := readOffsets(data, 0, &EncodedColumn{})
	if err == nil {
		t.Fatal("expected error for truncated offsets length, got nil")
	}
}

// --- readNulls 边界测试 ---

// TestReadNullsDataOverflow 测试 readNulls 数据超出缓冲区
func TestReadNullsDataOverflow(t *testing.T) {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:], 100) // nullsLen 超出数据
	_, err := readNulls(data, 0, &EncodedColumn{})
	if err == nil {
		t.Fatal("expected error for truncated nulls data, got nil")
	}
}

// --- readColumnData 边界测试 ---

// TestReadColumnDataLengthFieldTruncated 测试 readColumnData 长度字段超出数据
func TestReadColumnDataLengthFieldTruncated(t *testing.T) {
	data := make([]byte, 2) // 不足 4 字节
	_, err := readColumnData(data, 0, &EncodedColumn{})
	if err == nil {
		t.Fatal("expected error for truncated column data length, got nil")
	}
}

// --- Engine Write 触发 MemTable 轮转测试 ---

// TestEngineWriteTriggersRotation 测试写入触发 MemTable 轮转
func TestEngineWriteTriggersRotation(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir:         t.TempDir(),
		MaxMemTableSize: 256, // 很小的 MemTable
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	for i := 0; i < 100; i++ {
		key := string(rune('a' + i%26))
		if err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))}); err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}
}

// --- ColumnVector SetValue 类型不匹配测试 ---

// TestColumnVectorSetValueWrongType 测试 SetValue 类型不匹配时的错误
func TestColumnVectorSetValueWrongType(t *testing.T) {
	cv := NewColumnVector(0, common.TypeInt64, 10)
	err := cv.SetValue(0, common.NewString("not_int"))
	if err == nil {
		t.Fatal("expected error for type mismatch in SetValue, got nil")
	}
}

// --- ColumnVector Append 类型不匹配测试 ---

// TestColumnVectorAppendWrongType 测试 Append 类型不匹配时的错误
func TestColumnVectorAppendWrongType(t *testing.T) {
	cv := NewColumnVector(0, common.TypeInt64, 10)
	err := cv.Append(common.NewString("not_int"))
	if err == nil {
		t.Fatal("expected error for type mismatch in Append, got nil")
	}
}

// --- ColumnVector GetValue 未知类型测试 ---

// TestColumnVectorGetValueUnknownDataType 测试 GetValue 对未知类型的处理
func TestColumnVectorGetValueUnknownDataType(t *testing.T) {
	cv := &ColumnVector{
		Typ:      common.DataType(99),
		capacity: 10,
		nulls:    common.NewBitmap(10),
	}
	val := cv.GetValue(0)
	if val.Valid {
		t.Error("expected null value for unknown type, got valid value")
	}
}
