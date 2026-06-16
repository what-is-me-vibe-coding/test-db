package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---------------------------------------------------------------------------
// MemTable.ScanRange: 范围扫描覆盖
// ---------------------------------------------------------------------------

func TestMemTableScanRange_Basic(t *testing.T) {
	mt := NewMemTable()
	mt.Put("a", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("alice")}})
	mt.Put("b", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("bob")}})
	mt.Put("c", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("charlie")}})
	mt.Put("d", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("diana")}})

	entries := mt.ScanRange("b", "c")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Key != "b" {
		t.Errorf("first key = %q, want %q", entries[0].Key, "b")
	}
	if entries[1].Key != "c" {
		t.Errorf("second key = %q, want %q", entries[1].Key, "c")
	}
}

func TestMemTableScanRange_SingleMatch(t *testing.T) {
	mt := NewMemTable()
	mt.Put("a", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("alice")}})
	mt.Put("c", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("charlie")}})

	entries := mt.ScanRange("a", "a")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Key != "a" {
		t.Errorf("key = %q, want %q", entries[0].Key, "a")
	}
}

func TestMemTableScanRange_NoMatch(t *testing.T) {
	mt := NewMemTable()
	mt.Put("a", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("alice")}})
	mt.Put("z", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("zara")}})

	entries := mt.ScanRange("m", "n")
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestMemTableScanRange_FullRange(t *testing.T) {
	mt := NewMemTable()
	for i := 0; i < 20; i++ {
		key := fmtKey(i)
		mt.Put(key, Row{Version: 1, Columns: map[string]common.Value{colID: common.NewInt64(int64(i))}})
	}

	entries := mt.ScanRange("", "\xff\xff\xff\xff")
	if len(entries) != 20 {
		t.Errorf("expected 20 entries, got %d", len(entries))
	}
}

func TestMemTableScanRange_EmptyTable(t *testing.T) {
	mt := NewMemTable()
	entries := mt.ScanRange("a", "z")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on empty table, got %d", len(entries))
	}
}

func TestMemTableScanRange_StartAfterAllKeys(t *testing.T) {
	mt := NewMemTable()
	mt.Put("a", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("alice")}})
	mt.Put("b", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("bob")}})

	entries := mt.ScanRange("z", "\xff\xff\xff\xff")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestMemTableScanRange_EndBeforeAllKeys(t *testing.T) {
	mt := NewMemTable()
	mt.Put("m", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("mary")}})
	mt.Put("z", Row{Version: 1, Columns: map[string]common.Value{colName: common.NewString("zara")}})

	entries := mt.ScanRange("", "a")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// ColumnVector.SetLen: 正常和越界 panic
// ---------------------------------------------------------------------------

func TestColumnVectorSetLen_Normal(t *testing.T) {
	cv := NewColumnVector(1, common.TypeInt64, 16)
	cv.SetInt64(0, 42)
	cv.SetInt64(1, 100)
	cv.SetLen(2)

	if cv.Len() != 2 {
		t.Errorf("Len = %d, want 2", cv.Len())
	}
	if v := cv.GetValue(0); v.Int64 != 42 {
		t.Errorf("GetValue(0) = %d, want 42", v.Int64)
	}
	if v := cv.GetValue(1); v.Int64 != 100 {
		t.Errorf("GetValue(1) = %d, want 100", v.Int64)
	}
}

func TestColumnVectorSetLen_Zero(t *testing.T) {
	cv := NewColumnVector(1, common.TypeInt64, 16)
	cv.SetLen(0)
	if cv.Len() != 0 {
		t.Errorf("Len = %d, want 0", cv.Len())
	}
}

func TestColumnVectorSetLen_ExceedsCapacity(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for SetLen exceeding capacity")
		}
	}()
	cv := NewColumnVector(1, common.TypeInt64, 4)
	cv.SetLen(5) // 超过容量，应 panic
}

func TestColumnVectorSetLen_ExactlyAtCapacity(t *testing.T) {
	cv := NewColumnVector(1, common.TypeInt64, 8)
	cv.SetLen(8)
	if cv.Len() != 8 {
		t.Errorf("Len = %d, want 8", cv.Len())
	}
}
