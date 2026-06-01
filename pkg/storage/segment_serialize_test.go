package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestSegmentColumnBlockRoundTrip(t *testing.T) {
	rowCount := uint32(10)
	ints := make([]int64, rowCount)
	for i := uint32(0); i < rowCount; i++ {
		ints[i] = int64(i)
	}

	enc, err := EncodeColumn(common.TypeInt64, ints, rowCount, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	serialized := SerializeColumnBlock(enc)
	restored, err := DeserializeColumnBlock(serialized)
	if err != nil {
		t.Fatalf("deserialize column block: %v", err)
	}

	if restored.Encoding != enc.Encoding {
		t.Errorf("encoding: got %v, want %v", restored.Encoding, enc.Encoding)
	}
	if restored.Type != enc.Type {
		t.Errorf("type: got %v, want %v", restored.Type, enc.Type)
	}
	if restored.RowCount != enc.RowCount {
		t.Errorf("rowCount: got %d, want %d", restored.RowCount, enc.RowCount)
	}

	decoded, _, err := DecodeColumn(restored)
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
		}
	}
}

func TestDeserializeColumnBlockTooShort(t *testing.T) {
	_, err := DeserializeColumnBlock([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestSegmentFooterRoundTrip(t *testing.T) {
	footer := &SegmentFooter{
		ColumnStats: []ColumnStat{
			{ColumnID: 0, Min: int64ToBytes(1), Max: int64ToBytes(100), NullCount: 5},
			{ColumnID: 1, Min: []byte("abc"), Max: []byte("xyz"), NullCount: 3},
		},
		BloomFilter: []byte{0x01, 0x02, 0x03},
		IndexOffset: 12345,
	}

	serialized := serializeFooter(footer)
	restored, err := deserializeFooter(serialized)
	if err != nil {
		t.Fatalf("deserialize footer: %v", err)
	}

	if len(restored.ColumnStats) != 2 {
		t.Fatalf("ColumnStats count: got %d, want 2", len(restored.ColumnStats))
	}
	if restored.ColumnStats[0].ColumnID != 0 {
		t.Errorf("ColumnStats[0].ColumnID: got %d, want 0", restored.ColumnStats[0].ColumnID)
	}
	if restored.ColumnStats[0].NullCount != 5 {
		t.Errorf("ColumnStats[0].NullCount: got %d, want 5", restored.ColumnStats[0].NullCount)
	}
	if restored.ColumnStats[1].ColumnID != 1 {
		t.Errorf("ColumnStats[1].ColumnID: got %d, want 1", restored.ColumnStats[1].ColumnID)
	}
	if string(restored.ColumnStats[1].Min) != "abc" {
		t.Errorf("ColumnStats[1].Min: got %q, want %q", string(restored.ColumnStats[1].Min), "abc")
	}
	if string(restored.ColumnStats[1].Max) != "xyz" {
		t.Errorf("ColumnStats[1].Max: got %q, want %q", string(restored.ColumnStats[1].Max), "xyz")
	}
	if len(restored.BloomFilter) != 3 {
		t.Errorf("BloomFilter length: got %d, want 3", len(restored.BloomFilter))
	}
	if restored.IndexOffset != 12345 {
		t.Errorf("IndexOffset: got %d, want 12345", restored.IndexOffset)
	}
}
