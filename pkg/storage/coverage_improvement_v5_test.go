package storage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
)

// ---------------------------------------------------------------------------
// OpenWAL error paths (76.5% → higher)
// ---------------------------------------------------------------------------

// TestOpenWALNonExistentFilePath verifies OpenWAL returns error for a path
// in a non-existent directory.
func TestOpenWALNonExistentFilePath(t *testing.T) {
	_, _, err := OpenWAL("/no/such/directory/wal.log")
	if err == nil {
		t.Fatal("expected error for non-existent file path, got nil")
	}
}

// TestOpenWALReplayRecordsCorrectly verifies that OpenWAL correctly replays
// multiple records of different types (write, commit, checkpoint).
func TestOpenWALReplayRecordsCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}

	if err := w.AppendWrite([]byte("write_record")); err != nil {
		t.Fatalf("AppendWrite failed: %v", err)
	}
	if err := w.AppendCommit([]byte("commit_record")); err != nil {
		t.Fatalf("AppendCommit failed: %v", err)
	}
	if err := w.AppendCheckpoint([]byte("checkpoint_record")); err != nil {
		t.Fatalf("AppendCheckpoint failed: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	opened, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = opened.Close() }()

	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	if recs[0].Type != walTypeWrite || string(recs[0].Payload) != "write_record" {
		t.Errorf("record 0: expected write/write_record, got type=%d payload=%q", recs[0].Type, string(recs[0].Payload))
	}
	if recs[1].Type != walTypeCommit || string(recs[1].Payload) != "commit_record" {
		t.Errorf("record 1: expected commit/commit_record, got type=%d payload=%q", recs[1].Type, string(recs[1].Payload))
	}
	if recs[2].Type != walTypeCheckpoint || string(recs[2].Payload) != "checkpoint_record" {
		t.Errorf("record 2: expected checkpoint/checkpoint_record, got type=%d payload=%q", recs[2].Type, string(recs[2].Payload))
	}
}

// TestOpenWALCorruptedBadCRC verifies OpenWAL stops replay when a record
// has a bad CRC and truncates the file to the last valid record.
func TestOpenWALCorruptedBadCRC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	if err := w.AppendWrite([]byte("valid_data")); err != nil {
		t.Fatalf("AppendWrite failed: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	totalLen := uint32(walTypeSize + 4 + walCRCSize)
	fakeRecord := make([]byte, walHeaderSize+totalLen)
	binary.LittleEndian.PutUint32(fakeRecord[0:], totalLen)
	fakeRecord[4] = walTypeWrite
	copy(fakeRecord[5:], []byte("bad!"))
	binary.LittleEndian.PutUint32(fakeRecord[5+4:], 0xDEADBEEF)

	modified := make([]byte, len(data)+len(fakeRecord))
	copy(modified, data)
	copy(modified[len(data):], fakeRecord)
	if err := os.WriteFile(path, modified, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	opened, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = opened.Close() }()

	if len(recs) != 1 {
		t.Fatalf("expected 1 valid record before bad CRC, got %d", len(recs))
	}
	if string(recs[0].Payload) != "valid_data" {
		t.Errorf("record 0: expected 'valid_data', got %q", string(recs[0].Payload))
	}
}

// TestOpenWALPartialHeaderTruncation verifies OpenWAL truncates the file
// when there is a partial header at the end (simulating a crash mid-write).
func TestOpenWALPartialHeaderTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL failed: %v", err)
	}
	if err := w.AppendWrite([]byte("good")); err != nil {
		t.Fatalf("AppendWrite failed: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	validSize := len(data)
	modified := make([]byte, validSize+2)
	copy(modified, data)
	modified[validSize] = 0x0A
	modified[validSize+1] = 0x00

	if err := os.WriteFile(path, modified, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	opened, recs, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("OpenWAL failed: %v", err)
	}
	defer func() { _ = opened.Close() }()

	if len(recs) != 1 {
		t.Fatalf("expected 1 valid record, got %d", len(recs))
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if fi.Size() != int64(validSize) {
		t.Errorf("expected file size %d after truncation, got %d", validSize, fi.Size())
	}
}

// ---------------------------------------------------------------------------
// Engine.getFromSegments paths (82.6% → higher)
// ---------------------------------------------------------------------------

// TestGetFromSegmentsKeyNotInPrimaryIndex tests that getFromSegments returns
// empty when the key is not found in the primary index.
func TestGetFromSegmentsKeyNotInPrimaryIndex(t *testing.T) {
	eng := &Engine{
		activeMem:    NewMemTable(),
		primaryIndex: index.NewPrimaryIndex(),
		bloomIndex:   index.NewBloomIndex(),
		sparseIndex:  index.NewSparseIndex(),
		segmentMap:   make(map[uint64]*Segment),
	}

	row, ok := eng.getFromSegments("nonexistent_key")
	if ok {
		t.Error("expected false for key not in primary index")
	}
	if len(row.Columns) != 0 {
		t.Error("expected empty columns for key not in primary index")
	}
}

// TestGetFromSegmentsBloomFilterSaysNo tests that getFromSegments skips a
// segment when the bloom filter says the key is not present.
func TestGetFromSegmentsBloomFilterSaysNo(t *testing.T) {
	pi := index.NewPrimaryIndex()
	_ = pi.RegisterSegment(index.SegmentMeta{ID: 1, MinKey: "a", MaxKey: "z"})

	bi := index.NewBloomIndex()
	_ = bi.BuildAndRegister(1, []string{"present_key"}, 0.01)

	eng := &Engine{
		activeMem:    NewMemTable(),
		primaryIndex: pi,
		bloomIndex:   bi,
		sparseIndex:  index.NewSparseIndex(),
		segmentMap:   make(map[uint64]*Segment),
		columnMeta:   []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}},
	}

	row, ok := eng.getFromSegments("missing_key")
	if ok {
		t.Error("expected false when bloom filter says key is not present")
	}
	if len(row.Columns) != 0 {
		t.Error("expected empty columns when bloom filter says no")
	}
}

// TestGetFromSegmentsSegmentNotInMap tests that getFromSegments skips a
// segment when the bloom filter says yes but the segment is not in segmentMap.
func TestGetFromSegmentsSegmentNotInMap(t *testing.T) {
	pi := index.NewPrimaryIndex()
	_ = pi.RegisterSegment(index.SegmentMeta{ID: 1, MinKey: "a", MaxKey: "z"})

	bi := index.NewBloomIndex()
	_ = bi.BuildAndRegister(1, []string{"some_key"}, 0.01)

	eng := &Engine{
		activeMem:    NewMemTable(),
		primaryIndex: pi,
		bloomIndex:   bi,
		sparseIndex:  index.NewSparseIndex(),
		segmentMap:   make(map[uint64]*Segment),
		columnMeta:   []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}},
	}

	row, ok := eng.getFromSegments("some_key")
	if ok {
		t.Error("expected false when segment not found in segmentMap")
	}
	if len(row.Columns) != 0 {
		t.Error("expected empty columns when segment not in map")
	}
}

// TestGetFromSegmentsFindRowByKeyReturnsFalse tests that getFromSegments
// returns false when the segment is found but FindRowByKey returns false.
func TestGetFromSegmentsFindRowByKeyReturnsFalse(t *testing.T) {
	pi := index.NewPrimaryIndex()
	_ = pi.RegisterSegment(index.SegmentMeta{ID: 1, MinKey: "a", MaxKey: "z"})

	bi := index.NewBloomIndex()
	_ = bi.BuildAndRegister(1, []string{"some_key"}, 0.01)

	seg := &Segment{
		ID:       1,
		MinKey:   "a",
		MaxKey:   "z",
		RowCount: 1,
		Keys:     []string{"other_key"},
		Columns: []EncodedColumn{
			{Encoding: EncodingPlain, Type: common.TypeInt64, RowCount: 1, Data: make([]byte, 8)},
		},
	}

	eng := &Engine{
		activeMem:    NewMemTable(),
		primaryIndex: pi,
		bloomIndex:   bi,
		sparseIndex:  index.NewSparseIndex(),
		segmentMap:   map[uint64]*Segment{1: seg},
		columnMeta:   []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}},
	}

	row, ok := eng.getFromSegments("some_key")
	if ok {
		t.Error("expected false when FindRowByKey returns false")
	}
	if len(row.Columns) != 0 {
		t.Error("expected empty columns when FindRowByKey returns false")
	}
}

// TestGetFromSegmentsGetColumnValueError tests that getFromSegments skips
// a column when GetColumnValue returns an error, but still returns the row
// with remaining columns.
func TestGetFromSegmentsGetColumnValueError(t *testing.T) {
	pi := index.NewPrimaryIndex()
	_ = pi.RegisterSegment(index.SegmentMeta{ID: 1, MinKey: "a", MaxKey: "z"})

	bi := index.NewBloomIndex()
	_ = bi.BuildAndRegister(1, []string{"target_key"}, 0.01)

	seg := &Segment{
		ID:       1,
		MinKey:   "a",
		MaxKey:   "z",
		RowCount: 1,
		Keys:     []string{"target_key"},
		Columns: []EncodedColumn{
			{Encoding: EncodingType(99), Type: common.TypeInt64, RowCount: 1, Data: make([]byte, 8)},
		},
	}

	eng := &Engine{
		activeMem:    NewMemTable(),
		primaryIndex: pi,
		bloomIndex:   bi,
		sparseIndex:  index.NewSparseIndex(),
		segmentMap:   map[uint64]*Segment{1: seg},
		columnMeta:   []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}},
	}

	row, ok := eng.getFromSegments("target_key")
	if !ok {
		t.Fatal("expected true when key is found in segment")
	}
	if len(row.Columns) != 0 {
		t.Errorf("expected 0 columns (all errored), got %d", len(row.Columns))
	}
}
