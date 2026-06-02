package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestEngineScanRangeMemTableOnly(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("c", map[string]common.Value{colVal: common.NewInt64(3)})
	_ = eng.Write("e", map[string]common.Value{colVal: common.NewInt64(5)})

	results := eng.Scan("b", "d")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Key != "c" {
		t.Errorf("expected key c, got %q", results[0].Key)
	}
}

func TestEngineScanRangeWithSegments(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("c", map[string]common.Value{colVal: common.NewInt64(3)})
	_ = eng.Write("e", map[string]common.Value{colVal: common.NewInt64(5)})

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	results := eng.Scan("a", "c")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Key != "a" {
		t.Errorf("expected first key a, got %q", results[0].Key)
	}
	if results[1].Key != "c" {
		t.Errorf("expected second key c, got %q", results[1].Key)
	}
}

func TestEngineScanRangeMemTableOverridesSegment(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("b", map[string]common.Value{colVal: common.NewInt64(2)})

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	_ = eng.Write("b", map[string]common.Value{colVal: common.NewInt64(20)})

	results := eng.Scan("a", "b")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Key != "a" || results[0].Value.Columns[colVal].Int64 != 1 {
		t.Errorf("key a: expected val=1, got val=%d", results[0].Value.Columns[colVal].Int64)
	}
	if results[1].Key != "b" || results[1].Value.Columns[colVal].Int64 != 20 {
		t.Errorf("key b: expected val=20 (memtable override), got val=%d", results[1].Value.Columns[colVal].Int64)
	}
}

func TestEngineScanRangeMultipleSegments(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("b", map[string]common.Value{colVal: common.NewInt64(2)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 1: %v", err)
	}

	_ = eng.Write("c", map[string]common.Value{colVal: common.NewInt64(3)})
	_ = eng.Write("d", map[string]common.Value{colVal: common.NewInt64(4)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 2: %v", err)
	}

	results := eng.Scan("a", "d")
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	expectedKeys := []string{"a", "b", "c", "d"}
	for i, r := range results {
		if r.Key != expectedKeys[i] {
			t.Errorf("result[%d]: expected key %q, got %q", i, expectedKeys[i], r.Key)
		}
	}
}

func TestEngineScanRangeWithCompaction(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("c", map[string]common.Value{colVal: common.NewInt64(3)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 1: %v", err)
	}

	_ = eng.Write("b", map[string]common.Value{colVal: common.NewInt64(2)})
	_ = eng.Write("d", map[string]common.Value{colVal: common.NewInt64(4)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 2: %v", err)
	}

	if err := eng.Compact(cols); err != nil {
		t.Fatalf("compact: %v", err)
	}

	results := eng.Scan("a", "d")
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	expectedKeys := []string{"a", "b", "c", "d"}
	for i, r := range results {
		if r.Key != expectedKeys[i] {
			t.Errorf("result[%d]: expected key %q, got %q", i, expectedKeys[i], r.Key)
		}
	}
}

func TestEngineScanRangeEmpty(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	results := eng.Scan("a", "z")
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty engine, got %d", len(results))
	}
}

func TestEngineScanRangeSorted(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	keys := []string{"e", "a", "c", "b", "d"}
	for i, k := range keys {
		_ = eng.Write(k, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}

	results := eng.Scan("a", "e")
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	for i := 1; i < len(results); i++ {
		if results[i].Key < results[i-1].Key {
			t.Errorf("results not sorted: %q > %q", results[i-1].Key, results[i].Key)
		}
	}
}

func TestEngineScanRangeMultiColumn(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Write("a", map[string]common.Value{
		colName: common.NewString("alice"),
		colAge:  common.NewInt64(30),
	})
	_ = eng.Write("b", map[string]common.Value{
		colName: common.NewString("bob"),
		colAge:  common.NewInt64(25),
	})

	cols := []ColumnMeta{
		{ID: 0, Name: colName, Type: common.TypeString},
		{ID: 1, Name: colAge, Type: common.TypeInt64},
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	results := eng.Scan("a", "b")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Value.Columns[colName].Str != "alice" {
		t.Errorf("expected name=alice, got %v", results[0].Value.Columns[colName])
	}
	if results[1].Value.Columns[colName].Str != "bob" {
		t.Errorf("expected name=bob, got %v", results[1].Value.Columns[colName])
	}
}

func TestEngineScanRangeAfterOverwrite(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("b", map[string]common.Value{colVal: common.NewInt64(2)})

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(100)})

	results := eng.Scan("a", "b")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Key != "a" || results[0].Value.Columns[colVal].Int64 != 100 {
		t.Errorf("key a: expected val=100 (overwritten), got val=%d", results[0].Value.Columns[colVal].Int64)
	}
	if results[1].Key != "b" || results[1].Value.Columns[colVal].Int64 != 2 {
		t.Errorf("key b: expected val=2, got val=%d", results[1].Value.Columns[colVal].Int64)
	}
}

func TestEngineScanRangeIndexPruning(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	_ = eng.Write("a", map[string]common.Value{colVal: common.NewInt64(1)})
	_ = eng.Write("b", map[string]common.Value{colVal: common.NewInt64(2)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 1: %v", err)
	}

	_ = eng.Write("x", map[string]common.Value{colVal: common.NewInt64(24)})
	_ = eng.Write("y", map[string]common.Value{colVal: common.NewInt64(25)})
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush 2: %v", err)
	}

	results := eng.Scan("a", "b")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Key != "a" {
		t.Errorf("expected key a, got %q", results[0].Key)
	}
	if results[1].Key != "b" {
		t.Errorf("expected key b, got %q", results[1].Key)
	}
}
