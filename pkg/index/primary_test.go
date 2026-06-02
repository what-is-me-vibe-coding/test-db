package index

import (
	"testing"
)

func TestNewPrimaryIndex(t *testing.T) {
	pi := NewPrimaryIndex()
	if pi == nil {
		t.Fatal("expected non-nil PrimaryIndex")
	}
	if pi.SegmentCount() != 0 {
		t.Errorf("expected 0 segments, got %d", pi.SegmentCount())
	}
}

func TestRegisterSegment(t *testing.T) {
	pi := NewPrimaryIndex()

	seg := SegmentMeta{
		ID:     1,
		MinKey: "a",
		MaxKey: "z",
		Level:  0,
	}

	if err := pi.RegisterSegment(seg); err != nil {
		t.Fatal(err)
	}
	if pi.SegmentCount() != 1 {
		t.Errorf("expected 1 segment, got %d", pi.SegmentCount())
	}
}

func TestRegisterSegmentInvalidID(t *testing.T) {
	pi := NewPrimaryIndex()

	err := pi.RegisterSegment(SegmentMeta{ID: 0, MinKey: "a", MaxKey: "z"})
	if err == nil {
		t.Error("expected error for zero segment ID")
	}
}

func TestRegisterSegmentInvalidKeyRange(t *testing.T) {
	pi := NewPrimaryIndex()

	err := pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "z", MaxKey: "a"})
	if err == nil {
		t.Error("expected error for invalid key range")
	}
}

func TestUnregisterSegment(t *testing.T) {
	pi := NewPrimaryIndex()

	_ = pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "a", MaxKey: "z"})
	_ = pi.RegisterSegment(SegmentMeta{ID: 2, MinKey: "b", MaxKey: "y"})

	if pi.SegmentCount() != 2 {
		t.Fatalf("expected 2 segments, got %d", pi.SegmentCount())
	}

	if err := pi.UnregisterSegment(1); err != nil {
		t.Fatal(err)
	}
	if pi.SegmentCount() != 1 {
		t.Errorf("expected 1 segment after unregister, got %d", pi.SegmentCount())
	}

	if _, ok := pi.GetSegment(1); ok {
		t.Error("segment 1 should have been removed")
	}
	if _, ok := pi.GetSegment(2); !ok {
		t.Error("segment 2 should still exist")
	}
}

func TestUnregisterSegmentNotFound(t *testing.T) {
	pi := NewPrimaryIndex()

	err := pi.UnregisterSegment(999)
	if err == nil {
		t.Error("expected error for non-existent segment")
	}
}

func TestLookup(t *testing.T) {
	pi := NewPrimaryIndex()

	_ = pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "a", MaxKey: "m"})
	_ = pi.RegisterSegment(SegmentMeta{ID: 2, MinKey: "n", MaxKey: "z"})

	tests := []struct {
		key     string
		wantIDs []uint64
	}{
		{"a", []uint64{1}},
		{"m", []uint64{1}},
		{"n", []uint64{2}},
		{"z", []uint64{2}},
		{"g", []uint64{1}},
		{"s", []uint64{2}},
		{"0", nil},
		{"zz", nil},
	}

	for _, tt := range tests {
		ids := pi.Lookup(tt.key)
		if !sliceEqual(ids, tt.wantIDs) {
			t.Errorf("Lookup(%q) = %v, want %v", tt.key, ids, tt.wantIDs)
		}
	}
}

func TestLookupOverlappingL0(t *testing.T) {
	pi := NewPrimaryIndex()

	_ = pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "a", MaxKey: "m", Level: 0})
	_ = pi.RegisterSegment(SegmentMeta{ID: 2, MinKey: "g", MaxKey: "z", Level: 0})

	ids := pi.Lookup("h")
	if len(ids) != 2 {
		t.Errorf("expected 2 segments for overlapping L0, got %d", len(ids))
	}
}

func TestRange(t *testing.T) {
	pi := NewPrimaryIndex()

	_ = pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "a", MaxKey: "e"})
	_ = pi.RegisterSegment(SegmentMeta{ID: 2, MinKey: "f", MaxKey: "j"})
	_ = pi.RegisterSegment(SegmentMeta{ID: 3, MinKey: "k", MaxKey: "o"})
	_ = pi.RegisterSegment(SegmentMeta{ID: 4, MinKey: "p", MaxKey: "z"})

	tests := []struct {
		start   string
		end     string
		wantIDs []uint64
	}{
		{"a", "e", []uint64{1}},
		{"a", "j", []uint64{1, 2}},
		{"c", "h", []uint64{1, 2}},
		{"f", "o", []uint64{2, 3}},
		{"a", "z", []uint64{1, 2, 3, 4}},
		{"x", "z", []uint64{4}},
		{"0", "0", nil},
		{"{", "}", nil},
	}

	for _, tt := range tests {
		ids := pi.Range(tt.start, tt.end)
		if !sliceEqual(ids, tt.wantIDs) {
			t.Errorf("Range(%q, %q) = %v, want %v", tt.start, tt.end, ids, tt.wantIDs)
		}
	}
}

func TestGetSegment(t *testing.T) {
	pi := NewPrimaryIndex()

	_ = pi.RegisterSegment(SegmentMeta{ID: 42, MinKey: "a", MaxKey: "z", Level: 1})

	seg, ok := pi.GetSegment(42)
	if !ok {
		t.Fatal("expected segment 42 to exist")
	}
	if seg.ID != 42 {
		t.Errorf("expected ID 42, got %d", seg.ID)
	}
	if seg.MinKey != "a" {
		t.Errorf("expected MinKey 'a', got %q", seg.MinKey)
	}
	if seg.MaxKey != "z" {
		t.Errorf("expected MaxKey 'z', got %q", seg.MaxKey)
	}
	if seg.Level != 1 {
		t.Errorf("expected Level 1, got %d", seg.Level)
	}

	_, ok = pi.GetSegment(999)
	if ok {
		t.Error("segment 999 should not exist")
	}
}

func TestClear(t *testing.T) {
	pi := NewPrimaryIndex()

	_ = pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "a", MaxKey: "z"})
	_ = pi.RegisterSegment(SegmentMeta{ID: 2, MinKey: "b", MaxKey: "y"})

	pi.Clear()
	if pi.SegmentCount() != 0 {
		t.Errorf("expected 0 segments after clear, got %d", pi.SegmentCount())
	}
}

func TestConcurrentRegisterAndLookup(t *testing.T) {
	_ = t
	pi := NewPrimaryIndex()

	done := make(chan bool, 10)
	for i := 0; i < 5; i++ {
		go func(id uint64) {
			for j := 0; j < 100; j++ {
				key := string(rune('a' + (j % 26)))
				_ = pi.RegisterSegment(SegmentMeta{
					ID:     id*uint64(100) + uint64(j),
					MinKey: key,
					MaxKey: key,
					Level:  0,
				})
			}
			done <- true
		}(uint64(i))
	}

	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				pi.Lookup("m")
				pi.Range("a", "z")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestEmptyKeyRange(t *testing.T) {
	pi := NewPrimaryIndex()

	_ = pi.RegisterSegment(SegmentMeta{ID: 1, MinKey: "", MaxKey: ""})

	ids := pi.Lookup("anything")
	if len(ids) != 0 {
		t.Errorf("expected no results for empty key range, got %d segments", len(ids))
	}
}

func sliceEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
