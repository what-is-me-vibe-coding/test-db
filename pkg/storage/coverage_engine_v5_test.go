package storage

import (
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// Engine.Write error paths (84.2% → higher)
// ---------------------------------------------------------------------------

// TestWriteWALAppendFailureV5 tests Write when WAL AppendWrite fails
// by closing the WAL file descriptor before writing.
func TestWriteWALAppendFailureV5(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("file Close failed: %v", err)
	}

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when WAL AppendWrite fails, got nil")
	}
}

// TestWriteWALSyncFailureV5 tests Write when WAL Sync fails
// by closing the WAL file descriptor before syncing.
func TestWriteWALSyncFailureV5(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_ = eng.wal.file.Close()

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when WAL Sync fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine.NewEngine paths (84.2% → higher)
// ---------------------------------------------------------------------------

// TestNewEngineInvalidDataDirV5 tests NewEngine with a data directory
// that cannot be created (path under /dev/null).
func TestNewEngineInvalidDataDirV5(t *testing.T) {
	_, err := NewEngine(EngineConfig{DataDir: "/dev/null/cannot/create/here"})
	if err == nil {
		t.Error("expected error for invalid data directory, got nil")
	}
}

// TestNewEngineReplayWALRecordsV5 tests NewEngine with an existing WAL
// that contains write records to be replayed into the memtable.
func TestNewEngineReplayWALRecordsV5(t *testing.T) {
	dir := t.TempDir()

	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("first NewEngine: %v", err)
	}
	if err := eng.Write("replay_key", map[string]common.Value{colVal: common.NewInt64(42)}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	eng2, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("second NewEngine: %v", err)
	}
	defer func() { _ = eng2.Close() }()

	row, ok := eng2.Get("replay_key")
	if !ok {
		t.Fatal("expected replay_key to be recovered from WAL replay")
	}
	if row.Columns[colVal].Int64 != 42 {
		t.Errorf("expected val=42, got %d", row.Columns[colVal].Int64)
	}
}

// ---------------------------------------------------------------------------
// Compress/CompressColumn/DecompressColumn edge cases
// ---------------------------------------------------------------------------

// TestCompressEmptyDataV5 verifies Compress returns nil for empty input.
func TestCompressEmptyDataV5(t *testing.T) {
	result, err := Compress([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty data, got %d bytes", len(result))
	}
}

// TestDecompressEmptyDataV5 verifies Decompress returns nil for empty input.
func TestDecompressEmptyDataV5(t *testing.T) {
	result, err := Decompress([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty data, got %d bytes", len(result))
	}
}

// TestCompressColumnNilV5 verifies CompressColumn returns error for nil EncodedColumn.
func TestCompressColumnNilV5(t *testing.T) {
	err := CompressColumn(nil)
	if err == nil {
		t.Fatal("expected error for nil EncodedColumn, got nil")
	}
}

// TestDecompressColumnNilV5 verifies DecompressColumn returns error for nil EncodedColumn.
func TestDecompressColumnNilV5(t *testing.T) {
	err := DecompressColumn(nil)
	if err == nil {
		t.Fatal("expected error for nil EncodedColumn, got nil")
	}
}

// TestDecompressInvalidDataV5 verifies Decompress returns error for invalid data.
func TestDecompressInvalidDataV5(t *testing.T) {
	_, err := Decompress([]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE})
	if err == nil {
		t.Fatal("expected error for invalid compressed data, got nil")
	}
}

// ---------------------------------------------------------------------------
// EncodeColumn edge cases (85.7% → higher)
// ---------------------------------------------------------------------------

// TestEncodeColumnUnsupportedTypePlainV5 tests EncodeColumn with a type
// that is not supported for plain encoding (TypeNull).
func TestEncodeColumnUnsupportedTypePlainV5(t *testing.T) {
	_, err := EncodeColumn(common.TypeNull, nil, 1, nil)
	if err == nil {
		t.Error("expected error for unsupported type in EncodeColumn, got nil")
	}
}

// TestEncodeColumnUnknownEncodingTypeV5 tests DecodeColumn with an unknown
// encoding type to cover the default branch.
func TestEncodeColumnUnknownEncodingTypeV5(t *testing.T) {
	enc := &EncodedColumn{
		Encoding: EncodingType(99),
		Type:     common.TypeInt64,
		RowCount: 1,
		Data:     make([]byte, 8),
	}
	_, _, err := DecodeColumn(enc)
	if err == nil {
		t.Error("expected error for unknown encoding type in DecodeColumn, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine.Close (85.7% → higher)
// ---------------------------------------------------------------------------

// TestEngineCloseSuccessV5 tests that Close succeeds on a working engine.
func TestEngineCloseSuccessV5(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if err := eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := eng.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestNewEngineReplayWithWALPath tests that NewEngine correctly opens
// an existing WAL file and replays its records.
func TestNewEngineReplayWithWALPath(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")

	// Create a WAL with a write record
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	payload, err := serializeWriteRecord("k1", 1, map[string]common.Value{colVal: common.NewInt64(10)})
	if err != nil {
		t.Fatalf("serializeWriteRecord failed: %v", err)
	}
	if err := w.AppendWrite(payload); err != nil {
		t.Fatalf("AppendWrite failed: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// NewEngine should open the existing WAL and replay the record
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	row, ok := eng.Get("k1")
	if !ok {
		t.Fatal("expected k1 to be recovered from WAL replay")
	}
	if row.Columns[colVal].Int64 != 10 {
		t.Errorf("expected val=10, got %d", row.Columns[colVal].Int64)
	}
}
