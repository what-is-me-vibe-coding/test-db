package storage

import (
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestDictEncodingBoundaryRoundTrip 覆盖 dict 编码在基数边界的正确性：
// 历史 bug：当字符串列基数恰好为 256/65536 且无 NULL 时，encodeDict 与
// decodeDict 的索引宽度计算不一致（encode 用 hasNulls=false，decode 硬编码
// hasNulls=true），导致宽度不匹配 + 索引与 nullMarker 冲突，数据损坏。
// 修复后 indexWidth 始终为 nullMarker 预留槽位，encode/decode 宽度一致。
func TestDictEncodingBoundaryRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		dictSize int
		withNull bool
	}{
		{"255_no_null", 255, false},
		{"256_no_null", 256, false}, // 历史触发边界
		{"257_no_null", 257, false},
		{"255_with_null", 255, true},
		{"256_with_null", 256, true},
		{"1_no_null", 1, false},
		{"2_no_null", 2, false},
		{"65535_no_null", 65535, false},
		{"65536_no_null", 65536, false}, // 历史触发边界
		{"65537_no_null", 65537, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			strs, nulls := buildDictBoundaryInput(tc.dictSize, tc.withNull)
			enc, err := encodeDict(common.TypeString, strs, uint32(len(strs)), nulls)
			if err != nil {
				t.Fatalf("encodeDict: %v", err)
			}
			decoded, decNulls, err := decodeDict(enc)
			if err != nil {
				t.Fatalf("decodeDict: %v", err)
			}
			verifyDictDecoded(t, decoded, decNulls, strs, nulls)
		})
	}
}

// buildDictBoundaryInput 构造 dictSize 个不同字符串（可选追加一行 NULL）。
func buildDictBoundaryInput(dictSize int, withNull bool) ([]string, *common.Bitmap) {
	rowCount := dictSize
	var nulls *common.Bitmap
	if withNull {
		rowCount++
		nulls = common.NewBitmap(uint32(rowCount))
		nulls.Set(uint32(rowCount - 1))
	}
	strs := make([]string, rowCount)
	for i := 0; i < dictSize; i++ {
		strs[i] = fmt.Sprintf("v_%d", i)
	}
	return strs, nulls
}

// verifyDictDecoded 校验解码结果与原始输入一致（含 NULL 标记）。
func verifyDictDecoded(t *testing.T, decoded any, decNulls *common.Bitmap, strs []string, nulls *common.Bitmap) {
	t.Helper()
	got := decoded.([]string)
	for i := range strs {
		isNull := nulls != nil && nulls.Get(uint32(i))
		gotNull := decNulls != nil && decNulls.Get(uint32(i))
		if isNull != gotNull {
			t.Errorf("row %d null mismatch: got=%v want=%v", i, gotNull, isNull)
			continue
		}
		if isNull {
			continue
		}
		if got[i] != strs[i] {
			t.Errorf("row %d value mismatch: got=%q want=%q", i, got[i], strs[i])
		}
	}
}

// TestDictEncodingNullMarkerNoCollision 确保最大有效字典索引不会与
// nullMarker 冲突（历史 bug 的另一表现：索引 255 == nullMarker 0xFF）。
func TestDictEncodingNullMarkerNoCollision(t *testing.T) {
	const dictSize = 256
	strs := make([]string, dictSize)
	for i := 0; i < dictSize; i++ {
		strs[i] = fmt.Sprintf("k_%d", i)
	}
	enc, err := encodeDict(common.TypeString, strs, dictSize, nil)
	if err != nil {
		t.Fatalf("encodeDict: %v", err)
	}
	decoded, decNulls, err := decodeDict(enc)
	if err != nil {
		t.Fatalf("decodeDict: %v", err)
	}
	verifyDictDecoded(t, decoded, decNulls, strs, nil)
}

// TestDictEncodingBoundarySegmentRoundTrip 通过完整的 Segment 序列化/反序列化
// 路径验证 dict 编码边界修复：256 个不同字符串经 EncodeColumn（自动选择 Dict
// 编码）→ SegmentBuilder.Build → Serialize → DeserializeSegment → DecodeColumn，
// 确保落盘再读取后数据一致。这是真实数据路径的回归保护。
func TestDictEncodingBoundarySegmentRoundTrip(t *testing.T) {
	const rowCount = uint32(256)
	strs := makeStringColumn(rowCount)
	keys := makeStringKeys(rowCount)

	enc, err := EncodeColumn(common.TypeString, strs, rowCount, nil)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	if enc.Encoding != EncodingDict {
		t.Fatalf("encoding: got %v, want Dict", enc.Encoding)
	}

	restored := buildSerializeDeserializeSegment(t, 42, keys, enc)
	if len(restored.Columns) != 1 {
		t.Fatalf("columns: got %d, want 1", len(restored.Columns))
	}
	got, decNulls := decodeSegmentStringColumn(t, &restored.Columns[0], rowCount)
	for i := uint32(0); i < rowCount; i++ {
		if decNulls != nil && decNulls.Get(i) {
			t.Errorf("row %d falsely marked NULL", i)
		}
		if got[i] != strs[i] {
			t.Errorf("row %d: got=%q want=%q", i, got[i], strs[i])
		}
	}
}

// makeStringColumn 生成 rowCount 个不同字符串。
func makeStringColumn(rowCount uint32) []string {
	strs := make([]string, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		strs[i] = fmt.Sprintf("seg_val_%d", i)
	}
	return strs
}

// makeStringKeys 生成 rowCount 个主键字符串。
func makeStringKeys(rowCount uint32) []string {
	keys := make([]string, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		keys[i] = fmt.Sprintf("k%d", i)
	}
	return keys
}

// buildSerializeDeserializeSegment 构建含单列的 Segment，序列化后反序列化返回。
func buildSerializeDeserializeSegment(t *testing.T, segID uint64, keys []string, enc *EncodedColumn) *Segment {
	t.Helper()
	builder := NewSegmentBuilder(segID, keys[0], keys[len(keys)-1])
	builder.SetKeys(keys)
	builder.AddEncodedColumn(enc)
	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build segment: %v", err)
	}
	data, err := seg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	restored, err := DeserializeSegment(data)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	return restored
}

// decodeSegmentStringColumn 解压并解码 Segment 中的字符串列，返回值与 NULL 位图。
func decodeSegmentStringColumn(t *testing.T, col *EncodedColumn, rowCount uint32) ([]string, *common.Bitmap) {
	t.Helper()
	if err := DecompressColumn(col); err != nil {
		t.Fatalf("decompress: %v", err)
	}
	decoded, decNulls, err := DecodeColumn(col)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := decoded.([]string)
	if len(got) != int(rowCount) {
		t.Fatalf("decoded length: got %d, want %d", len(got), rowCount)
	}
	return got, decNulls
}
