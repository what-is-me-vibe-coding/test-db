package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestScanErrorPath tests that Engine.Scan silently logs errors from
// ScanWithError and returns nil entries when the underlying scan fails.
func TestScanErrorPath(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Inject a corrupted segment that will cause decode errors during scan.
	corruptedSeg := &Segment{
		ID:       999,
		MinKey:   "a",
		MaxKey:   "z",
		RowCount: 1,
		Keys:     []string{crKey1},
		Columns: []EncodedColumn{
			{
				Encoding: EncodingType(99), // invalid encoding triggers decode error
				Type:     common.TypeInt64,
				RowCount: 1,
				Data:     make([]byte, 8),
			},
		},
	}

	eng.mu.Lock()
	eng.segments = append(eng.segments, corruptedSeg)
	eng.segmentMap[corruptedSeg.ID] = corruptedSeg
	eng.segmentLevels = append(eng.segmentLevels, 0)
	eng.columnMeta = []ColumnMeta{{ID: 0, Name: crCol0, Type: common.TypeInt64}}
	eng.mu.Unlock()

	// Scan should handle the error gracefully: log it and return nil/empty entries.
	entries := eng.Scan("a", "z")
	if len(entries) != 0 {
		t.Errorf("expected empty entries from scan with corrupted segment, got %d", len(entries))
	}
}

// TestScanWithErrorDecodingFailure tests that ScanWithError returns an error
// when segment decoding fails.
func TestScanWithErrorDecodingFailure(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	corruptedSeg := &Segment{
		ID:       998,
		MinKey:   "a",
		MaxKey:   "z",
		RowCount: 1,
		Keys:     []string{crKey1},
		Columns: []EncodedColumn{
			{
				Encoding: EncodingType(99),
				Type:     common.TypeInt64,
				RowCount: 1,
				Data:     make([]byte, 8),
			},
		},
	}

	eng.mu.Lock()
	eng.segments = append(eng.segments, corruptedSeg)
	eng.segmentMap[corruptedSeg.ID] = corruptedSeg
	eng.segmentLevels = append(eng.segmentLevels, 0)
	eng.columnMeta = []ColumnMeta{{ID: 0, Name: crCol0, Type: common.TypeInt64}}
	eng.mu.Unlock()

	entries, err := eng.ScanWithError("a", "z")
	if err == nil {
		t.Error("expected error from ScanWithError with corrupted segment, got nil")
	}
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(entries))
	}
}
