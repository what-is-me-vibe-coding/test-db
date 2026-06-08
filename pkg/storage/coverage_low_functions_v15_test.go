package storage

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
)

// --- OpenWAL error paths (76.5% -> target: file not exist, replayWAL error, Truncate/Seek) ---

func TestV15_OpenWAL_FileNotExist(t *testing.T) {
	_, _, err := OpenWAL(filepath.Join(t.TempDir(), "nonexistent.wal"))
	if err == nil {
		t.Error("expected error when opening non-existent WAL file")
	}
}

func TestV15_OpenWAL_ValidRecordReplay(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tp := walTypeWrite
	payload := []byte("test-data")
	totalLen := uint32(1 + len(payload) + 4)
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, totalLen)
	body := make([]byte, totalLen)
	body[0] = tp
	copy(body[1:], payload)
	crcVal := crc32.Checksum(body[:1+len(payload)], crcTable)
	binary.LittleEndian.PutUint32(body[1+len(payload):], crcVal)
	if _, err := f.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := f.Write(body); err != nil {
		t.Fatalf("write body: %v", err)
	}
	_ = f.Close()

	w, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL: %v", err)
	}
	defer func() { _ = w.Close() }()
	if len(records) != 1 || string(records[0].Payload) != "test-data" {
		t.Errorf("expected 1 record with 'test-data', got %d records", len(records))
	}
}

func TestV15_OpenWAL_TruncateCorruptTail(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}
	if err := w.AppendWrite([]byte("valid")); err != nil {
		t.Fatalf("AppendWrite: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	_ = w.Close()

	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = f.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	_ = f.Close()

	w2, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL: %v", err)
	}
	defer func() { _ = w2.Close() }()
	if len(records) != 1 || string(records[0].Payload) != "valid" {
		t.Errorf("expected 1 valid record after truncate, got %d", len(records))
	}
	if err := w2.AppendWrite([]byte("after-reopen")); err != nil {
		t.Fatalf("AppendWrite after reopen: %v", err)
	}
}

func TestV15_OpenWAL_PartialHeaderTail(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := w.AppendWrite([]byte("r")); err != nil {
			t.Fatalf("AppendWrite %d: %v", i, err)
		}
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	offset := w.Size()
	_ = w.Close()

	f, err := os.OpenFile(walPath, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.Seek(offset, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	_, _ = f.Write([]byte{0x00, 0x00}) // partial header
	_ = f.Close()

	w2, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL: %v", err)
	}
	defer func() { _ = w2.Close() }()
	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}
}

func TestV15_ReplayWAL_InvalidRecordLength(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, 2) // too small
	_, _ = f.Write(header)
	_ = f.Close()

	w, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL: %v", err)
	}
	defer func() { _ = w.Close() }()
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestV15_ReplayWAL_CRCMismatch(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	payload := []byte("data")
	totalLen := uint32(1 + len(payload) + 4)
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, totalLen)
	body := make([]byte, totalLen)
	body[0] = walTypeWrite
	copy(body[1:], payload)
	binary.LittleEndian.PutUint32(body[1+len(payload):], 0xDEADBEEF)
	_, _ = f.Write(header)
	_, _ = f.Write(body)
	_ = f.Close()

	w, records, err := OpenWAL(walPath)
	if err != nil {
		t.Fatalf("OpenWAL: %v", err)
	}
	defer func() { _ = w.Close() }()
	if len(records) != 0 {
		t.Errorf("expected 0 records for CRC mismatch, got %d", len(records))
	}
}

// --- Compress/Decompress empty data (85.7% -> target: empty input returns nil) ---

func TestV15_Compress_EmptyInput(t *testing.T) {
	for _, input := range [][]byte{nil, {}} {
		result, err := Compress(input)
		if err != nil || result != nil {
			t.Errorf("Compress(%v): got result=%v err=%v, want nil,nil", input, result, err)
		}
	}
}

func TestV15_Decompress_EmptyInput(t *testing.T) {
	for _, input := range [][]byte{nil, {}} {
		result, err := Decompress(input)
		if err != nil || result != nil {
			t.Errorf("Decompress(%v): got result=%v err=%v, want nil,nil", input, result, err)
		}
	}
}

func TestV15_Decompress_InvalidData(t *testing.T) {
	_, err := Decompress([]byte{0xFF, 0xFE, 0xFD, 0xFC})
	if err == nil {
		t.Error("expected error for invalid compressed data")
	}
}

func TestV15_CompressDecompress_RoundTrip(t *testing.T) {
	original := []byte("hello world, compress and decompress test")
	compressed, err := Compress(original)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if string(decompressed) != string(original) {
		t.Errorf("round-trip mismatch: got %q, want %q", string(decompressed), string(original))
	}
}

// --- CompressColumn/DecompressColumn nil input (85.7% -> target: nil EncodedColumn) ---

func TestV15_CompressColumn_NilInput(t *testing.T) {
	if err := CompressColumn(nil); err == nil {
		t.Error("expected error for CompressColumn(nil)")
	}
}

func TestV15_DecompressColumn_NilInput(t *testing.T) {
	if err := DecompressColumn(nil); err == nil {
		t.Error("expected error for DecompressColumn(nil)")
	}
}

func TestV15_CompressColumn_EmptyData(t *testing.T) {
	enc := &EncodedColumn{Encoding: EncodingPlain, Type: common.TypeInt64, RowCount: 0, Data: []byte{}}
	if err := CompressColumn(enc); err != nil {
		t.Errorf("CompressColumn with empty data: %v", err)
	}
}

func TestV15_DecompressColumn_EmptyData(t *testing.T) {
	enc := &EncodedColumn{Encoding: EncodingPlain, Type: common.TypeInt64, RowCount: 0, Data: []byte{}}
	if err := DecompressColumn(enc); err != nil {
		t.Errorf("DecompressColumn with empty data: %v", err)
	}
}

// --- EncodeColumn/DecodeColumn unknown encoding (85.7% -> target: unknown encoding type) ---

func TestV15_DecodeColumn_UnknownEncoding(t *testing.T) {
	enc := &EncodedColumn{Encoding: EncodingType(99), Type: common.TypeInt64, RowCount: 1, Data: make([]byte, 8)}
	if _, _, err := DecodeColumn(enc); err == nil {
		t.Error("expected error for unknown encoding type in DecodeColumn")
	}
}

func TestV15_EncodeColumn_PlainUnsupportedType(t *testing.T) {
	// TypeNull with rowCount > 0 hits the default branch in encodePlain
	if _, err := EncodeColumn(common.TypeNull, nil, 1, nil); err == nil {
		t.Error("expected error for unsupported type in EncodeColumn")
	}
}

func TestV15_EncodeColumn_PlainEmptyRow(t *testing.T) {
	enc, err := EncodeColumn(common.TypeInt64, []int64{}, 0, nil)
	if err != nil || enc.RowCount != 0 {
		t.Errorf("expected RowCount=0, got %d, err=%v", enc.RowCount, err)
	}
}

func TestV15_EncodeColumn_RLEInt64(t *testing.T) {
	data := make([]int64, 200)
	for i := range data {
		data[i] = 42
	}
	enc, err := EncodeColumn(common.TypeInt64, data, 200, nil)
	if err != nil {
		t.Fatalf("EncodeColumn RLE: %v", err)
	}
	if enc.Encoding != EncodingRLE {
		t.Fatalf("expected RLE, got %v", enc.Encoding)
	}
	decoded, _, err := DecodeColumn(enc)
	if err != nil {
		t.Fatalf("DecodeColumn RLE: %v", err)
	}
	ints := decoded.([]int64)
	for i, v := range ints {
		if v != 42 {
			t.Errorf("row %d: got %d, want 42", i, v)
		}
	}
}

func TestV15_EncodeColumn_BoolWithNulls(t *testing.T) {
	data := []uint64{1, 0, 1, 0, 1}
	nulls := common.NewBitmap(5)
	nulls.Set(1)
	nulls.Set(3)
	enc, err := EncodeColumn(common.TypeBool, data, 5, nulls)
	if err != nil {
		t.Fatalf("EncodeColumn bool with nulls: %v", err)
	}
	if enc.Encoding != EncodingBitmap || len(enc.Nulls) == 0 {
		t.Errorf("expected Bitmap with nulls, got encoding=%v nullsLen=%d", enc.Encoding, len(enc.Nulls))
	}
}

// --- BuildAndRegister (83.3% -> target: empty keys returns nil) ---

func TestV15_BuildAndRegister_EmptyKeys(t *testing.T) {
	bi := index.NewBloomIndex()
	if err := bi.BuildAndRegister(1, []string{}, 0.01); err != nil {
		t.Errorf("BuildAndRegister with empty keys should return nil, got: %v", err)
	}
}

func TestV15_BuildAndRegister_NilKeys(t *testing.T) {
	bi := index.NewBloomIndex()
	if err := bi.BuildAndRegister(2, nil, 0.01); err != nil {
		t.Errorf("BuildAndRegister with nil keys should return nil, got: %v", err)
	}
}

func TestV15_BuildAndRegister_ValidKeys(t *testing.T) {
	bi := index.NewBloomIndex()
	if err := bi.BuildAndRegister(3, []string{crKey1, crKey2}, 0.01); err != nil {
		t.Errorf("BuildAndRegister with valid keys: %v", err)
	}
	if !bi.MayContainString(3, crKey1) {
		t.Error("expected BloomIndex to contain key1")
	}
}

func TestV15_BuildAndRegister_EdgeFPRates(t *testing.T) {
	bi := index.NewBloomIndex()
	for _, tc := range []struct {
		segID  uint64
		fpRate float64
	}{
		{10, 0}, {11, -0.1}, {12, 1.0},
	} {
		if err := bi.BuildAndRegister(tc.segID, []string{"a"}, tc.fpRate); err != nil {
			t.Errorf("BuildAndRegister(segID=%d, fpRate=%v): %v", tc.segID, tc.fpRate, err)
		}
	}
}

// --- Write error paths (84.2% -> target: WAL append fail, WAL sync fail) ---

func TestV15_Write_WALAppendFail(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	_ = eng.wal.Close()
	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when Write with closed WAL (append)")
	}
}

func TestV15_Write_WALSyncFail(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	_ = eng.wal.file.Close()
	err = eng.Write("key1", map[string]common.Value{colVal: common.NewInt64(1)})
	if err == nil {
		t.Error("expected error when Write with closed WAL file (sync)")
	}
}

// --- WriteBatch error paths (85% -> target: WAL append fail, WAL sync fail, MemTable Put fail) ---

func TestV15_WriteBatch_WALAppendFail(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	_ = eng.wal.Close()
	rows := []WriteRow{{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}}}
	if err := eng.WriteBatch(rows); err == nil {
		t.Error("expected error when WriteBatch with closed WAL (append)")
	}
}

func TestV15_WriteBatch_WALSyncFail(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	_ = eng.wal.file.Close()
	rows := []WriteRow{{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}}}
	if err := eng.WriteBatch(rows); err == nil {
		t.Error("expected error when WriteBatch with closed WAL file (sync)")
	}
}

func TestV15_WriteBatch_MemTablePutFail(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	eng.activeMem.Freeze()
	rows := []WriteRow{{Key: "k1", Values: map[string]common.Value{colVal: common.NewInt64(1)}}}
	if err := eng.WriteBatch(rows); err == nil {
		t.Error("expected error when WriteBatch with frozen memtable (Put)")
	}
	_ = eng.Close()
}

func TestV15_WriteBatch_EmptyRows(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if err := eng.WriteBatch(nil); err != nil {
		t.Errorf("WriteBatch(nil) should return nil, got: %v", err)
	}
	if err := eng.WriteBatch([]WriteRow{}); err != nil {
		t.Errorf("WriteBatch([]) should return nil, got: %v", err)
	}
}

// --- NewEngine error paths (88% -> target: data dir create fail, WAL replay fail) ---

func TestV15_NewEngine_DataDirCreateFail(t *testing.T) {
	_, err := NewEngine(EngineConfig{DataDir: "/dev/null/subdir"})
	if err == nil {
		t.Error("expected error for data dir creation failure")
	}
}

func TestV15_NewEngine_WALReplayCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")
	w, err := CreateWAL(walPath)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}
	// Write a record with corrupt payload that cannot be deserialized
	if err := w.AppendWrite([]byte{0x99, 0x88, 0x77}); err != nil {
		t.Fatalf("AppendWrite: %v", err)
	}
	_ = w.Close()

	// NewEngine should succeed; replayWALRecords logs errors but doesn't fail
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine should succeed with corrupt WAL records: %v", err)
	}
	_ = eng.Close()
}

func TestV15_NewEngine_ExistingWALReplay(t *testing.T) {
	dir := t.TempDir()
	eng1, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 1: %v", err)
	}
	if err := eng1.Write("key1", map[string]common.Value{colVal: common.NewInt64(100)}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = eng1.Close()

	eng2, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("NewEngine 2: %v", err)
	}
	defer func() { _ = eng2.Close() }()
	row, ok := eng2.Get("key1")
	if !ok || row.Columns[colVal].Int64 != 100 {
		t.Errorf("expected key1 val=100 after replay, got ok=%v val=%d", ok, row.Columns[colVal].Int64)
	}
}
