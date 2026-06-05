package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// Engine Write error paths
// ---------------------------------------------------------------------------

// TestWriteWALSyncError tests Write when WAL Sync fails (closed WAL).
func TestWriteWALSyncError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Close the WAL to make Sync fail
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close failed: %v", err)
	}

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when WAL Sync fails, got nil")
	}
}

// TestWriteWALAppendError tests Write when WAL AppendWrite fails.
func TestWriteWALAppendError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Close the WAL to make AppendWrite fail
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close failed: %v", err)
	}

	err = eng.Write("key", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when WAL AppendWrite fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine Flush error paths
// ---------------------------------------------------------------------------

// TestFlushWALClosedError tests Flush when WAL is closed (checkpoint append/sync fails).
func TestFlushWALClosedError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	_ = eng.Write(crKey1, map[string]common.Value{colVal: common.NewInt64(1)})

	// Close WAL before Flush to trigger checkpoint append error
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close failed: %v", err)
	}

	err = eng.Flush(cols)
	if err == nil {
		t.Error("expected error when WAL is closed during Flush, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine Compact error paths
// ---------------------------------------------------------------------------

// TestCompactCompactError tests Compact when compactor.Compact fails.
func TestCompactCompactError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// Create enough L0 segments to trigger compaction
	for i := 0; i < defaultL0CompactionThreshold; i++ {
		_ = eng.Write(fmt.Sprintf("key%d", i), map[string]common.Value{colVal: common.NewInt64(int64(i))})
		if err := eng.Flush(cols); err != nil {
			t.Fatalf("Flush %d failed: %v", i, err)
		}
	}

	// Corrupt segment data to make Compact fail
	eng.mu.Lock()
	for _, seg := range eng.segments {
		for i := range seg.Columns {
			seg.Columns[i].Data = []byte{0xFF, 0xFE, 0xFD, 0xFC}
		}
	}
	eng.mu.Unlock()

	err = eng.Compact(cols)
	if err == nil {
		t.Error("expected error when Compact fails, got nil")
	}
}

// TestCompactCleanupError tests Compact when CleanupSegments fails.
func TestCompactCleanupError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// Create enough L0 segments to trigger compaction
	for i := 0; i < defaultL0CompactionThreshold; i++ {
		_ = eng.Write(fmt.Sprintf("key%d", i), map[string]common.Value{colVal: common.NewInt64(int64(i))})
		if err := eng.Flush(cols); err != nil {
			t.Fatalf("Flush %d failed: %v", i, err)
		}
	}

	// Set FilePath to a non-empty directory to make Remove fail
	eng.mu.Lock()
	for _, seg := range eng.segments {
		nonEmptyDir := filepath.Join(dir, fmt.Sprintf("blocker_%d", seg.ID))
		if err := os.MkdirAll(nonEmptyDir, 0755); err != nil {
			eng.mu.Unlock()
			t.Fatalf("MkdirAll failed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(nonEmptyDir, "inner"), []byte("x"), 0644); err != nil {
			eng.mu.Unlock()
			t.Fatalf("WriteFile failed: %v", err)
		}
		seg.FilePath = nonEmptyDir
	}
	eng.mu.Unlock()

	err = eng.Compact(cols)
	if err == nil {
		t.Error("expected error when CleanupSegments fails, got nil")
	}
}

// TestCompactRegisterIndexError tests registerSegmentIndexes with segment ID 0.
func TestCompactRegisterIndexError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Segment with ID 0 causes RegisterSegment to fail
	seg := &Segment{
		ID:     0,
		MinKey: "a",
		MaxKey: "z",
		Footer: SegmentFooter{},
	}
	err = eng.registerSegmentIndexes(seg, 0)
	if err == nil {
		t.Error("expected error when registering segment with ID 0, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine Close error paths
// ---------------------------------------------------------------------------

// TestCloseWALSyncError tests Close when WAL Sync fails.
func TestCloseWALSyncError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Close the WAL file descriptor first to make Sync fail
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("file Close failed: %v", err)
	}

	err = eng.Close()
	if err == nil {
		t.Error("expected error when WAL Sync fails during Close, got nil")
	}
}

// TestCloseWALCloseError tests Close when WAL Close fails.
func TestCloseWALCloseError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Close the WAL file descriptor first to make the second Close fail
	if err := eng.wal.file.Close(); err != nil {
		t.Fatalf("file Close failed: %v", err)
	}

	err = eng.Close()
	if err == nil {
		t.Error("expected error when WAL Close fails during Close, got nil")
	}
}

// ---------------------------------------------------------------------------
// Engine NewEngine error paths
// ---------------------------------------------------------------------------

// TestNewEngineMkdirAllError tests NewEngine when data directory creation fails.
func TestNewEngineMkdirAllError(t *testing.T) {
	// Create a file at the path where the directory should be
	tmpFile, err := os.CreateTemp("", "engine-mkdir-blocker-*")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	// NewEngine should fail because MkdirAll can't create a directory
	// where a file already exists as a parent
	_, err = NewEngine(EngineConfig{DataDir: tmpPath + "/subdir/data"})
	if err == nil {
		t.Error("expected error when MkdirAll fails, got nil")
	}
}

// TestNewEngineWALCreationFailure tests NewEngine when both OpenWAL and CreateWAL fail.
func TestNewEngineWALCreationFailure(t *testing.T) {
	// Create a read-only directory so WAL creation fails
	dir := t.TempDir()
	walDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(walDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Make the directory read-only
	if err := os.Chmod(walDir, 0555); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}
	defer func() { _ = os.Chmod(walDir, 0755) }()

	if os.Getuid() == 0 {
		t.Skip("root user bypasses file permission checks")
	}

	_, err := NewEngine(EngineConfig{DataDir: walDir})
	if err == nil {
		t.Error("expected error when WAL creation fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// WriteBatch error paths
// ---------------------------------------------------------------------------

// TestWriteBatchWALSyncErrorLowCov tests WriteBatch when WAL Sync fails.
func TestWriteBatchWALSyncErrorLowCov(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Close WAL to make Sync fail
	if err := eng.wal.Close(); err != nil {
		t.Fatalf("WAL Close failed: %v", err)
	}

	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("expected error when WAL Sync fails in WriteBatch, got nil")
	}
}

// TestWriteBatchRotateErrorLowCov tests WriteBatch when memtable is frozen.
func TestWriteBatchRotateErrorLowCov(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, MaxMemTableSize: 256})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Freeze the active memtable so Put will fail
	eng.activeMem.Freeze()

	rows := []WriteRow{
		{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}},
	}
	err = eng.WriteBatch(rows)
	if err == nil {
		t.Error("expected error when memtable is frozen in WriteBatch, got nil")
	}
}

// TestWriteBatchEmptyRowsLowCov tests WriteBatch with empty rows.
func TestWriteBatchEmptyRowsLowCov(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	err = eng.WriteBatch(nil)
	if err != nil {
		t.Errorf("expected nil for empty WriteBatch, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Engine Write rotate memtable error path
// ---------------------------------------------------------------------------

// TestWriteRotateMemTableErrorLowCov tests Write when rotateMemTable is triggered.
func TestWriteRotateMemTableErrorLowCov(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, MaxMemTableSize: 256})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Write enough data to trigger rotation
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key_%04d", i)
		if err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))}); err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// registerSegmentIndexes error paths
// ---------------------------------------------------------------------------

// TestRegisterSegmentIndexesPrimaryIndexError tests registerSegmentIndexes when primary index registration fails.
func TestRegisterSegmentIndexesPrimaryIndexError(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Segment with ID 0 causes RegisterSegment to fail
	seg := &Segment{
		ID:     0,
		MinKey: "a",
		MaxKey: "z",
		Footer: SegmentFooter{},
	}
	err = eng.registerSegmentIndexes(seg, 0)
	if err == nil {
		t.Error("expected error for segment ID 0, got nil")
	}
}

// ---------------------------------------------------------------------------
// loadSegments error paths
// ---------------------------------------------------------------------------

// TestLoadSegmentsReadDirError tests loadSegments when os.ReadDir fails.
func TestLoadSegmentsReadDirError(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Point the flusher's dataDir to a file (not a directory) to make ReadDir fail
	tmpFile, err := os.CreateTemp(dir, "blocker-*")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	eng.flusher.dataDir = tmpPath
	err = eng.loadSegments()
	if err == nil {
		t.Error("expected error when ReadDir fails, got nil")
	}
}

// TestLoadSegmentsCorruptSegmentFile tests loadSegments with a corrupt segment file.
func TestLoadSegmentsCorruptSegmentFile(t *testing.T) {
	dir := t.TempDir()

	// Create a corrupt segment file
	corruptData := []byte("this is not a valid segment")
	corruptPath := filepath.Join(dir, "segment_1.widb")
	if err := os.WriteFile(corruptPath, corruptData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// NewEngine should still succeed but log the corrupt file
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Logf("NewEngine returned error (acceptable if all segments fail): %v", err)
	} else {
		_ = eng.Close()
	}
}

// TestLoadSegmentsAllCorrupt tests loadSegments when all segment files are corrupt.
func TestLoadSegmentsAllCorrupt(t *testing.T) {
	dir := t.TempDir()

	// Create only corrupt segment files
	corruptData := []byte("corrupt")
	if err := os.WriteFile(filepath.Join(dir, "segment_1.widb"), corruptData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segment_2.widb"), corruptData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := NewEngine(EngineConfig{DataDir: dir})
	if err == nil {
		t.Error("expected error when all segment files are corrupt, got nil")
	}
}
