package storage

import (
	"strings"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
)

// ===========================================================================
// iterator.go:97 decodeSegmentColumn (82.1% → 100%)
// 未覆盖路径：Offsets 拷贝、Dict 拷贝、Nulls 拷贝、DecompressColumn 错误、DecodeColumn 错误
// ===========================================================================

// TestDecodeSegmentColumn_WithOffsets 测试含 Offsets 的 Plain 编码字符串列。
// 覆盖 iterator.go 第 114-116 行（src.Offsets 拷贝路径）。
func TestDecodeSegmentColumn_WithOffsets(t *testing.T) {
	strs := []string{testStrAlpha, testStrBeta, testStrGamma}
	enc, err := EncodeColumn(common.TypeString, strs, 3, nil)
	if err != nil {
		t.Fatalf("EncodeColumn 失败: %v", err)
	}
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("CompressColumn 失败: %v", err)
	}

	seg := &Segment{
		ID: 1, MinKey: "a", MaxKey: "c", RowCount: 3,
		Keys:    []string{"a", "b", "c"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeString}}
	bc := NewBlockCache(256 * 1024 * 1024)

	it := newSegmentIterator(seg, colMeta, "a", "c", bc)
	it.ensureDecoded()
	if it.err != nil {
		t.Fatalf("ensureDecoded 失败: %v", it.err)
	}

	count := 0
	for it.Next() {
		if it.Entry().Key == "" {
			t.Error("期望非空键")
		}
		count++
	}
	if count != 3 {
		t.Errorf("期望 3 条记录，得到 %d", count)
	}
}

// TestDecodeSegmentColumn_WithDict 测试含 Dict 数据的字符串列。
// 覆盖 iterator.go 第 120-122 行（src.Dict 拷贝路径）。
func TestDecodeSegmentColumn_WithDict(t *testing.T) {
	strs := []string{testStrHello, testStrWorld, testStrHello}
	enc, err := EncodeColumn(common.TypeString, strs, 3, nil)
	if err != nil {
		t.Fatalf("EncodeColumn 失败: %v", err)
	}
	if enc.Encoding != EncodingDict {
		t.Fatalf("期望 Dict 编码，得到 %v", enc.Encoding)
	}
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("CompressColumn 失败: %v", err)
	}

	seg := &Segment{
		ID: 2, MinKey: "a", MaxKey: "c", RowCount: 3,
		Keys:    []string{"a", "b", "c"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeString}}
	bc := NewBlockCache(256 * 1024 * 1024)

	it := newSegmentIterator(seg, colMeta, "a", "c", bc)
	it.ensureDecoded()
	if it.err != nil {
		t.Fatalf("ensureDecoded 失败: %v", it.err)
	}
	count := 0
	for it.Next() {
		count++
	}
	if count != 3 {
		t.Errorf("期望 3 条记录，得到 %d", count)
	}
}

// TestDecodeSegmentColumn_WithNulls 测试含 Nulls 数据的列。
// 覆盖 iterator.go 第 124-126 行（src.Nulls 拷贝路径）。
func TestDecodeSegmentColumn_WithNulls(t *testing.T) {
	ints := []int64{10, 20, 30}
	nulls := common.NewBitmap(3)
	nulls.Set(1)

	enc, err := EncodeColumn(common.TypeInt64, ints, 3, nulls)
	if err != nil {
		t.Fatalf("EncodeColumn 失败: %v", err)
	}
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("CompressColumn 失败: %v", err)
	}

	seg := &Segment{
		ID: 3, MinKey: "a", MaxKey: "c", RowCount: 3,
		Keys:    []string{"a", "b", "c"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	bc := NewBlockCache(256 * 1024 * 1024)

	it := newSegmentIterator(seg, colMeta, "a", "c", bc)
	it.ensureDecoded()
	if it.err != nil {
		t.Fatalf("ensureDecoded 失败: %v", it.err)
	}
	count := 0
	for it.Next() {
		count++
	}
	if count != 3 {
		t.Errorf("期望 3 条记录，得到 %d", count)
	}
}

// TestDecodeSegmentColumn_DecompressErrorV2 测试解压失败时的错误路径。
// 覆盖 iterator.go 第 123-127 行。
func TestDecodeSegmentColumn_DecompressErrorV2(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain, Type: common.TypeInt64, RowCount: 1,
		Data: []byte{0xFF, 0xFE, 0xFD, 0xFC},
	}
	seg := &Segment{
		ID: 4, MinKey: "a", MaxKey: "a", RowCount: 1,
		Keys:    []string{"a"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	bc := NewBlockCache(256 * 1024 * 1024)

	it := newSegmentIterator(seg, colMeta, "a", "a", bc)
	it.ensureDecoded()
	if it.err == nil {
		t.Fatal("期望解压失败时设置错误，得到 nil")
	}
}

// TestDecodeSegmentColumn_DecodeError 测试解压成功但解码失败时的错误路径。
// 覆盖 iterator.go 第 129-133 行。
func TestDecodeSegmentColumn_DecodeError(t *testing.T) {
	// 创建有效压缩数据但使用未知编码类型
	originalData := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	compressed, err := Compress(originalData)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}

	enc := &EncodedColumn{
		Encoding: EncodingType(99), Type: common.TypeInt64, RowCount: 1,
		Data: compressed,
	}
	seg := &Segment{
		ID: 5, MinKey: "a", MaxKey: "a", RowCount: 1,
		Keys:    []string{"a"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	bc := NewBlockCache(256 * 1024 * 1024)

	it := newSegmentIterator(seg, colMeta, "a", "a", bc)
	it.ensureDecoded()
	if it.err == nil {
		t.Fatal("期望解码失败时设置错误，得到 nil")
	}
	if !strings.Contains(it.err.Error(), "decode column") {
		t.Errorf("错误消息应包含 'decode column'，得到: %v", it.err)
	}
	// 验证 Next() 在有错误时返回 false
	if it.Next() {
		t.Error("期望有错误时 Next() 返回 false")
	}
}

// TestDecodeSegmentColumn_PlainStringWithOffsets 测试手动创建的含 Offsets 的 Plain 编码字符串列。
// 覆盖 iterator.go 第 114-116 行。
func TestDecodeSegmentColumn_PlainStringWithOffsets(t *testing.T) {
	strs := []string{testStrAlpha, testStrBeta, testStrGamma}
	var dataBuf []byte
	offsets := make([]uint32, len(strs)+1)
	for i, s := range strs {
		offsets[i] = uint32(len(dataBuf))
		dataBuf = append(dataBuf, []byte(s)...)
	}
	offsets[len(strs)] = uint32(len(dataBuf))

	compressed, err := Compress(dataBuf)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}

	enc := &EncodedColumn{
		Encoding: EncodingPlain, Type: common.TypeString, RowCount: uint32(len(strs)),
		Data: compressed, Offsets: offsets,
	}
	seg := &Segment{
		ID: 100, MinKey: "a", MaxKey: "c", RowCount: uint32(len(strs)),
		Keys:    []string{"a", "b", "c"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeString}}
	bc := NewBlockCache(256 * 1024 * 1024)

	it := newSegmentIterator(seg, colMeta, "a", "c", bc)
	it.ensureDecoded()
	if it.err != nil {
		t.Fatalf("ensureDecoded 失败: %v", it.err)
	}
	count := 0
	for it.Next() {
		count++
	}
	if count != 3 {
		t.Errorf("期望 3 条记录，得到 %d", count)
	}
}

// TestDecodeSegmentColumn_CacheHitV2 测试从 BlockCache 命中的路径。
func TestDecodeSegmentColumn_CacheHitV2(t *testing.T) {
	ints := []int64{100, 200, 300}
	enc, err := EncodeColumn(common.TypeInt64, ints, 3, nil)
	if err != nil {
		t.Fatalf("EncodeColumn 失败: %v", err)
	}
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("CompressColumn 失败: %v", err)
	}

	seg := &Segment{
		ID: 6, MinKey: "a", MaxKey: "c", RowCount: 3,
		Keys:    []string{"a", "b", "c"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	bc := NewBlockCache(256 * 1024 * 1024)

	// 第一次迭代：填充缓存
	it1 := newSegmentIterator(seg, colMeta, "a", "c", bc)
	it1.ensureDecoded()
	it1.Close()

	// 第二次迭代：应从缓存命中
	it2 := newSegmentIterator(seg, colMeta, "a", "c", bc)
	it2.ensureDecoded()
	count := 0
	for it2.Next() {
		count++
	}
	if count != 3 {
		t.Errorf("期望 3 条记录，得到 %d", count)
	}
	stats := bc.Stats()
	if stats.Hits == 0 {
		t.Error("期望至少 1 次缓存命中")
	}
}

// TestSegmentIterator_NextWithDecompressError 测试 Next() 在解压错误时的行为。
func TestSegmentIterator_NextWithDecompressError(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain, Type: common.TypeInt64, RowCount: 1,
		Data: []byte{0xFF, 0xFE, 0xFD, 0xFC},
	}
	seg := &Segment{
		ID: 102, MinKey: "a", MaxKey: "a", RowCount: 1,
		Keys:    []string{"a"},
		Columns: []EncodedColumn{*enc},
	}
	colMeta := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	bc := NewBlockCache(256 * 1024 * 1024)

	it := newSegmentIterator(seg, colMeta, "a", "a", bc)
	if it.Next() {
		t.Error("期望解压失败时 Next() 返回 false")
	}
	if it.err == nil {
		t.Fatal("期望设置错误，得到 nil")
	}
	// 第二次 Next() 应该因 it.err != nil 而直接返回 false
	if it.Next() {
		t.Error("期望有错误时 Next() 返回 false")
	}
}

// ===========================================================================
// iterator.go:412-414, 426-428, 432-434 - buildScanIterators 和 ScanRange
// ===========================================================================

// TestBuildScanIterators_WithImmutableMemTables 测试有 immutable memtable 时的行为。
func TestBuildScanIterators_WithImmutableMemTables(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 失败: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})

	// 手动将 activeMem 移到 immutable
	eng.mu.Lock()
	eng.activeMem.Freeze()
	eng.immutable = append(eng.immutable, eng.activeMem)
	eng.activeMem = NewMemTableWithSize(eng.activeMem.maxSize)
	eng.mu.Unlock()

	_ = eng.Write("key2", map[string]common.Value{colVal: common.NewInt64(2)})

	eng.mu.RLock()
	results := eng.ScanRange("a", "z")
	eng.mu.RUnlock()

	if len(results) != 2 {
		t.Errorf("期望 2 条记录，得到 %d", len(results))
	}
}

// TestScanRange_NoIterators 测试 ScanRange 在没有匹配数据时返回空结果。
func TestScanRange_NoIterators(t *testing.T) {
	eng := &Engine{
		activeMem:    NewMemTable(),
		segmentMap:   make(map[uint64]*Segment),
		primaryIndex: index.NewPrimaryIndex(),
		bloomIndex:   index.NewBloomIndex(),
		sparseIndex:  index.NewSparseIndex(),
		blockCache:   NewBlockCache(256 * 1024 * 1024),
		indexCache:   NewIndexCache(1000),
	}
	results := eng.ScanRange("a", "z")
	if len(results) != 0 {
		t.Errorf("期望 0 条记录，得到 %d", len(results))
	}
}

// ===========================================================================
// compaction.go:238 decodeSegmentColumn - 补充覆盖
// ===========================================================================

// TestCompactionDecodeSegmentColumn_WithOffsets 测试含 Offsets 的 Plain 编码字符串列。
func TestCompactionDecodeSegmentColumn_WithOffsets(t *testing.T) {
	strs := []string{testStrHello, testStrWorld}
	var dataBuf []byte
	offsets := make([]uint32, len(strs)+1)
	for i, s := range strs {
		offsets[i] = uint32(len(dataBuf))
		dataBuf = append(dataBuf, []byte(s)...)
	}
	offsets[len(strs)] = uint32(len(dataBuf))

	compressed, err := Compress(dataBuf)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}

	enc := &EncodedColumn{
		Encoding: EncodingPlain, Type: common.TypeString, RowCount: uint32(len(strs)),
		Data: compressed, Offsets: offsets,
	}
	dc, err := decodeSegmentColumn(enc, 0)
	if err != nil {
		t.Fatalf("decodeSegmentColumn 失败: %v", err)
	}
	strResult, ok := dc.data.([]string)
	if !ok {
		t.Fatalf("期望 []string，得到 %T", dc.data)
	}
	if len(strResult) != 2 {
		t.Errorf("期望 2 个元素，得到 %d", len(strResult))
	}
}

// TestCompactionDecodeSegmentColumn_WithDict 测试含 Dict 数据的列。
func TestCompactionDecodeSegmentColumn_WithDict(t *testing.T) {
	strs := []string{testStrHello, testStrWorld, testStrHello}
	enc, err := EncodeColumn(common.TypeString, strs, 3, nil)
	if err != nil {
		t.Fatalf("EncodeColumn 失败: %v", err)
	}
	if enc.Encoding != EncodingDict {
		t.Fatalf("期望 Dict 编码，得到 %v", enc.Encoding)
	}
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("CompressColumn 失败: %v", err)
	}
	dc, err := decodeSegmentColumn(enc, 0)
	if err != nil {
		t.Fatalf("decodeSegmentColumn 失败: %v", err)
	}
	strResult, ok := dc.data.([]string)
	if !ok {
		t.Fatalf("期望 []string，得到 %T", dc.data)
	}
	if len(strResult) != 3 {
		t.Errorf("期望 3 个元素，得到 %d", len(strResult))
	}
}

// TestCompactionDecodeSegmentColumn_WithNulls 测试含 Nulls 数据的列。
func TestCompactionDecodeSegmentColumn_WithNulls(t *testing.T) {
	ints := []int64{10, 20, 30}
	nulls := common.NewBitmap(3)
	nulls.Set(1)
	enc, err := EncodeColumn(common.TypeInt64, ints, 3, nulls)
	if err != nil {
		t.Fatalf("EncodeColumn 失败: %v", err)
	}
	if err := CompressColumn(enc); err != nil {
		t.Fatalf("CompressColumn 失败: %v", err)
	}
	dc, err := decodeSegmentColumn(enc, 0)
	if err != nil {
		t.Fatalf("decodeSegmentColumn 失败: %v", err)
	}
	if dc.nulls == nil {
		t.Fatal("期望非 nil nulls")
	}
	if !dc.nulls.Get(1) {
		t.Error("期望位置 1 为 null")
	}
}

// TestCompactionDecodeSegmentColumn_DecompressError 测试解压失败的错误路径。
func TestCompactionDecodeSegmentColumn_DecompressError(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingPlain, Type: common.TypeInt64, RowCount: 1,
		Data: []byte{0xFF, 0xFE, 0xFD, 0xFC},
	}
	_, err := decodeSegmentColumn(enc, 0)
	if err == nil {
		t.Fatal("期望解压失败返回错误，得到 nil")
	}
}

// TestCompactionDecodeSegmentColumn_DecodeError 测试解码失败的错误路径。
func TestCompactionDecodeSegmentColumn_DecodeError(t *testing.T) {
	originalData := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	compressed, err := Compress(originalData)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	enc := &EncodedColumn{
		Encoding: EncodingType(99), Type: common.TypeInt64, RowCount: 1,
		Data: compressed,
	}
	_, err = decodeSegmentColumn(enc, 0)
	if err == nil {
		t.Fatal("期望解码失败返回错误，得到 nil")
	}
}
