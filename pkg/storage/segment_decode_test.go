package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestDecodeAllColumnsEmptySegment(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{},
	}
	columns, err := seg.decodeAllColumns()
	if err != nil {
		t.Fatalf("decodeAllColumns on empty segment: %v", err)
	}
	if len(columns) != 0 {
		t.Errorf("expected 0 columns, got %d", len(columns))
	}
}

func TestDecodeAllColumnsWithCorruptData(t *testing.T) {
	// Create a segment with a column that has corrupt compressed data
	seg := &Segment{
		Columns: []EncodedColumn{
			{
				Encoding: EncodingPlain,
				Type:     common.TypeInt64,
				RowCount: 2,
				Data:     []byte{0xDE, 0xAD, 0xBE, 0xEF}, // corrupt data that can't be decompressed
			},
		},
	}
	_, err := seg.decodeAllColumns()
	if err == nil {
		t.Error("expected error for corrupt compressed data, got nil")
	}
}

// TestDecodeAllColumnsWithNullOnlyColumn tests decoding a column that has only
// a Nulls bitmap but no Data, Offsets, or Dict.
func TestDecodeAllColumnsWithNullOnlyColumn(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{
			{
				Encoding: EncodingPlain,
				Type:     common.TypeInt64,
				RowCount: 3,
				Nulls:    []byte{0x07}, // bits 0,1,2 set = all 3 rows are null
			},
		},
	}
	columns, err := seg.decodeAllColumns()
	if err != nil {
		t.Fatalf("decodeAllColumns with null-only column: %v", err)
	}
	if len(columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(columns))
	}
	if columns[0].nulls == nil {
		t.Error("expected nulls bitmap to be set for null-only column")
	}
}

// TestDecodeAllColumnsWithDecompressError tests that decodeAllColumns returns
// an error when a column has invalid compressed data that cannot be decompressed.
func TestDecodeAllColumnsWithDecompressError(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{
			{
				Encoding: EncodingPlain,
				Type:     common.TypeInt64,
				RowCount: 2,
				Data:     []byte{0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8}, // invalid zstd data
			},
		},
	}
	_, err := seg.decodeAllColumns()
	if err == nil {
		t.Error("expected error for invalid compressed data, got nil")
	}
}

// TestDecodeAllColumnsWithDecodeError tests that decodeAllColumns returns
// an error when a column has an unknown encoding type that cannot be decoded.
func TestDecodeAllColumnsWithDecodeError(t *testing.T) {
	seg := &Segment{
		Columns: []EncodedColumn{
			{
				Encoding: EncodingType(99), // unknown encoding type
				Type:     common.TypeInt64,
				RowCount: 0,
			},
		},
	}
	_, err := seg.decodeAllColumns()
	if err == nil {
		t.Error("expected error for unknown encoding type, got nil")
	}
}
