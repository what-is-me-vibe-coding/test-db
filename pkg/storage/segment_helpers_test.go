package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func verifySegmentRoundTripInt64(t *testing.T, ints []int64, rowCount uint32, nulls *common.Bitmap, id uint64, minKey, maxKey string) {
	t.Helper()
	enc, err := EncodeColumn(common.TypeInt64, ints, rowCount, nulls)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	builder := NewSegmentBuilder(id, minKey, maxKey)
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

	verifyRestoredInt64(t, restored, ints, rowCount, nulls)
}

func verifyRestoredInt64(t *testing.T, restored *Segment, ints []int64, rowCount uint32, nulls *common.Bitmap) {
	t.Helper()
	if restored.RowCount != rowCount {
		t.Errorf("RowCount mismatch: got %d, want %d", restored.RowCount, rowCount)
	}
	if len(restored.Columns) != 1 {
		t.Fatalf("Columns count: got %d, want 1", len(restored.Columns))
	}

	restoredCol := &restored.Columns[0]
	if err := DecompressColumn(restoredCol); err != nil {
		t.Fatalf("decompress: %v", err)
	}

	decoded, decodedNulls, err := DecodeColumn(restoredCol)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if nulls == nil && decodedNulls != nil && !decodedNulls.IsEmpty() {
		t.Error("unexpected nulls")
	}

	decodedInts, ok := decoded.([]int64)
	if !ok {
		t.Fatalf("decoded type: got %T, want []int64", decoded)
	}
	if len(decodedInts) != int(rowCount) {
		t.Fatalf("decoded length: got %d, want %d", len(decodedInts), rowCount)
	}
	for i := uint32(0); i < rowCount; i++ {
		if decodedInts[i] != ints[i] {
			t.Errorf("row %d: got %d, want %d", i, decodedInts[i], ints[i])
		}
	}
}

func addColumnOrFail(t *testing.T, builder *SegmentBuilder, typ common.DataType, data interface{}, rowCount uint32, nulls *common.Bitmap) {
	t.Helper()
	enc, err := EncodeColumn(typ, data, rowCount, nulls)
	if err != nil {
		t.Fatalf("encode %v: %v", typ, err)
	}
	builder.AddEncodedColumn(enc)
}

func verifyDecodedColumn(t *testing.T, col *EncodedColumn, idx, expectedLen int) {
	t.Helper()
	if err := DecompressColumn(col); err != nil {
		t.Fatalf("decompress column %d: %v", idx, err)
	}
	decoded, _, err := DecodeColumn(col)
	if err != nil {
		t.Fatalf("decode column %d: %v", idx, err)
	}
	switch idx {
	case 0:
		di, ok := decoded.([]int64)
		if !ok {
			t.Fatalf("column %d type: got %T", idx, decoded)
		}
		if len(di) != expectedLen {
			t.Errorf("column %d length: got %d, want %d", idx, len(di), expectedLen)
		}
	case 1:
		df, ok := decoded.([]float64)
		if !ok {
			t.Fatalf("column %d type: got %T", idx, decoded)
		}
		if len(df) != expectedLen {
			t.Errorf("column %d length: got %d, want %d", idx, len(df), expectedLen)
		}
	case 2:
		ds, ok := decoded.([]string)
		if !ok {
			t.Fatalf("column %d type: got %T", idx, decoded)
		}
		if len(ds) != expectedLen {
			t.Errorf("column %d length: got %d, want %d", idx, len(ds), expectedLen)
		}
	}
}
