package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestSegmentBuilderEmpty(t *testing.T) {
	builder := NewSegmentBuilder(5, "a", "b")
	_, err := builder.Build()
	if err == nil {
		t.Error("expected error for empty columns")
	}
}

func TestSegmentFooterStats(t *testing.T) {
	rowCount := uint32(100)
	ints := make([]int64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i) - 50
	}

	enc, err := EncodeColumn(common.TypeInt64, ints, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(9, "key-0", "key-99")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(seg.Footer.ColumnStats) != 1 {
		t.Fatalf("ColumnStats count: got %d, want 1", len(seg.Footer.ColumnStats))
	}

	stat := seg.Footer.ColumnStats[0]
	if stat.NullCount != 0 {
		t.Errorf("NullCount: got %d, want 0", stat.NullCount)
	}
}

func TestSegmentRLEStats(t *testing.T) {
	rowCount := uint32(100)
	ints := make([]int64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i / 10)
	}

	enc, err := EncodeColumn(common.TypeInt64, ints, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if enc.Encoding != EncodingRLE {
		t.Skip("data did not trigger RLE encoding")
	}

	builder := NewSegmentBuilder(12, "key-0", "key-99")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(seg.Footer.ColumnStats) != 1 {
		t.Fatalf("ColumnStats count: got %d, want 1", len(seg.Footer.ColumnStats))
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
	decodedInts, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("decoded type: got %T, want []int64", decoded)
	}
	if len(decodedInts) != int(rowCount) {
		t.Fatalf("decoded length: got %d, want %d", len(decodedInts), rowCount)
	}
}

func TestSegmentPlainStringStats(t *testing.T) {
	rowCount := uint32(10)
	strs := []string{"banana", testStrApple, testStrCherry, "date", "elderberry", "fig", "grape", "honeydew", "kiwi", "lemon"}

	enc, err := encodePlain(common.TypeString, strs, rowCount, nil)
	if err != nil {
		t.Fatalf("encode plain: %v", err)
	}

	builder := NewSegmentBuilder(13, "key-0", "key-9")
	builder.AddEncodedColumn(enc)

	seg, err := builder.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(seg.Footer.ColumnStats) != 1 {
		t.Fatalf("ColumnStats count: got %d, want 1", len(seg.Footer.ColumnStats))
	}

	stat := seg.Footer.ColumnStats[0]
	if string(stat.Min) != "apple" {
		t.Errorf("min: got %q, want %q", string(stat.Min), "apple")
	}
	if string(stat.Max) != "lemon" {
		t.Errorf("max: got %q, want %q", string(stat.Max), "lemon")
	}
}
