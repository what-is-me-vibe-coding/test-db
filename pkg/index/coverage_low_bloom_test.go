package index

import (
	"testing"
)

// --- BuildAndRegister: valid keys with custom fpRate ---

func TestBuildAndRegister_CustomFPRate(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"custom1", "custom2", "custom3"}
	err := bi.BuildAndRegister(1, keys, 0.001)
	if err != nil {
		t.Fatalf("BuildAndRegister with custom fpRate: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true with custom fpRate", k)
		}
	}
}

// --- BuildAndRegister: valid keys with fpRate=0 (should use default) ---

func TestBuildAndRegister_ZeroFPRate(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"zero1", "zero2"}
	err := bi.BuildAndRegister(1, keys, 0)
	if err != nil {
		t.Fatalf("BuildAndRegister with fpRate=0: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (fpRate=0 should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: valid keys with negative fpRate (should use default) ---

func TestBuildAndRegister_NegativeFPRate(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"neg1", "neg2"}
	err := bi.BuildAndRegister(1, keys, -0.5)
	if err != nil {
		t.Fatalf("BuildAndRegister with negative fpRate: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (negative fpRate should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: valid keys with fpRate=1.0 (should use default) ---

func TestBuildAndRegister_FPRateOne(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"one1", "one2"}
	err := bi.BuildAndRegister(1, keys, 1.0)
	if err != nil {
		t.Fatalf("BuildAndRegister with fpRate=1.0: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (fpRate=1.0 should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: valid keys with fpRate>1 (should use default) ---

func TestBuildAndRegister_FPRateGreaterThanOne(t *testing.T) {
	bi := NewBloomIndex()

	keys := []string{"gt1_a", "gt1_b"}
	err := bi.BuildAndRegister(1, keys, 2.0)
	if err != nil {
		t.Fatalf("BuildAndRegister with fpRate>1: %v", err)
	}

	if bi.Len() != 1 {
		t.Errorf("Len: got %d, want 1", bi.Len())
	}

	for _, k := range keys {
		if !bi.MayContain(1, []byte(k)) {
			t.Errorf("MayContain(%q): expected true (fpRate>1 should fall back to default)", k)
		}
	}
}

// --- BuildAndRegister: empty keys should not register (nil keys variant) ---

func TestBuildAndRegister_NilKeys(t *testing.T) {
	bi := NewBloomIndex()

	err := bi.BuildAndRegister(1, nil, DefaultBloomFPRate)
	if err != nil {
		t.Fatalf("BuildAndRegister with nil keys: %v", err)
	}

	if bi.Len() != 0 {
		t.Errorf("Len: got %d, want 0 after nil keys", bi.Len())
	}
}

// --- BuildFromKeys: fpRate exactly at boundary (0 and 1) ---

func TestBuildFromKeys_FPRateBoundaryZero(t *testing.T) {
	keys := []string{"b1", "b2"}
	data, err := BuildFromKeys(keys, 0)
	if err != nil {
		t.Fatalf("BuildFromKeys with fpRate=0: %v", err)
	}
	if data == nil {
		t.Fatal("BuildFromKeys with fpRate=0 should return non-nil data (falls back to default)")
	}
}

func TestBuildFromKeys_FPRateBoundaryOne(t *testing.T) {
	keys := []string{"b1", "b2"}
	data, err := BuildFromKeys(keys, 1.0)
	if err != nil {
		t.Fatalf("BuildFromKeys with fpRate=1.0: %v", err)
	}
	if data == nil {
		t.Fatal("BuildFromKeys with fpRate=1.0 should return non-nil data (falls back to default)")
	}
}

func TestBuildFromKeys_NegativeFPRate(t *testing.T) {
	keys := []string{"b1", "b2"}
	data, err := BuildFromKeys(keys, -1.0)
	if err != nil {
		t.Fatalf("BuildFromKeys with negative fpRate: %v", err)
	}
	if data == nil {
		t.Fatal("BuildFromKeys with negative fpRate should return non-nil data (falls back to default)")
	}
}
