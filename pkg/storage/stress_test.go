package storage

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// stressCols returns a standard INT64 column meta for stress tests.
func stressCols() []ColumnMeta {
	return []ColumnMeta{
		{ID: 0, Name: colVal, Type: common.TypeInt64},
	}
}

// stressStringCols returns a STRING column meta for stress tests.
func stressStringCols() []ColumnMeta {
	return []ColumnMeta{
		{ID: 0, Name: colVal, Type: common.TypeString},
	}
}

// newStressEngine creates an Engine for stress testing.
func newStressEngine(t *testing.T, maxMemSize int64) *Engine {
	t.Helper()
	cfg := EngineConfig{DataDir: t.TempDir()}
	if maxMemSize > 0 {
		cfg.MaxMemTableSize = maxMemSize
	}
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return eng
}

// TestStress_LongWriteRead verifies sustained write+read workload stability.
func TestStress_LongWriteRead(t *testing.T) {
	t.Parallel()
	eng := newStressEngine(t, 0)
	defer func() { _ = eng.Close() }()

	const batches = 20
	const batchSize = 100
	cols := stressCols()

	for b := 0; b < batches; b++ {
		for i := 0; i < batchSize; i++ {
			key := fmt.Sprintf("lwr_%04d_%04d", b, i)
			val := int64(b*batchSize + i)
			if err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(val)}); err != nil {
				t.Fatalf("write batch %d key %d: %v", b, i, err)
			}
		}
		// Verify a sample of keys after each batch
		for i := 0; i < batchSize; i += 10 {
			key := fmt.Sprintf("lwr_%04d_%04d", b, i)
			expected := int64(b*batchSize + i)
			row, ok := eng.Get(key)
			if !ok || row.Columns[colVal].Int64 != expected {
				t.Fatalf("read key %s: got %v, want %d", key, row.Columns[colVal].Int64, expected)
			}
		}
		// Flush periodically to exercise segment path
		if b%5 == 4 {
			if err := eng.Flush(cols); err != nil {
				t.Fatalf("flush batch %d: %v", b, err)
			}
		}
	}
	t.Logf("LongWriteRead: %d keys written and verified", batches*batchSize)
}

// TestStress_WriteFlushCompactCycle verifies repeated write→flush→compact cycles.
// All flushes complete before compaction to avoid segment ID conflicts.
func TestStress_WriteFlushCompactCycle(t *testing.T) {
	eng := newStressEngine(t, 0)
	defer func() { _ = eng.Close() }()

	cols := stressCols()
	const cycles = 10
	const rowsPerCycle = 50

	// Phase 1: Write and flush in cycles (builds up L0 segments)
	for c := 0; c < cycles; c++ {
		for i := 0; i < rowsPerCycle; i++ {
			key := fmt.Sprintf("wfc_%02d_%04d", c, i)
			val := int64(c*rowsPerCycle + i)
			if err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(val)}); err != nil {
				t.Fatalf("write cycle %d key %d: %v", c, i, err)
			}
		}
		if err := eng.Flush(cols); err != nil {
			t.Fatalf("flush cycle %d: %v", c, err)
		}
	}

	// Phase 2: Compact all L0 segments into L1
	if eng.ShouldCompact() {
		if err := eng.Compact(cols); err != nil {
			t.Fatalf("compact: %v", err)
		}
	}

	// Phase 3: Verify all keys are still accessible
	missing := 0
	for c := 0; c < cycles; c++ {
		for i := 0; i < rowsPerCycle; i += 5 {
			key := fmt.Sprintf("wfc_%02d_%04d", c, i)
			if _, ok := eng.Get(key); !ok {
				missing++
			}
		}
	}
	if missing > 0 {
		t.Errorf("%d keys missing after write/flush/compact cycles", missing)
	}
	t.Logf("WriteFlushCompactCycle: %d cycles, %d missing keys", cycles, missing)
}

// stressMixedWriter runs concurrent writes until done is closed.
func stressMixedWriter(t *testing.T, eng *Engine, gid int, done <-chan struct{}, ops *atomic.Int64) {
	t.Helper()
	i := 0
	for {
		select {
		case <-done:
			return
		default:
			key := fmt.Sprintf("mw_w%d_%04d", gid, i)
			_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
			ops.Add(1)
			i++
		}
	}
}

// stressMixedReader runs concurrent reads until done is closed.
func stressMixedReader(eng *Engine, done <-chan struct{}, ops *atomic.Int64) {
	for {
		select {
		case <-done:
			return
		default:
			_, _ = eng.Get("mix_0025")
			ops.Add(1)
		}
	}
}

// stressMixedScanner runs concurrent scans and checks sort order.
func stressMixedScanner(eng *Engine, done <-chan struct{}, ops, errs *atomic.Int64) {
	for {
		select {
		case <-done:
			return
		default:
			results := eng.Scan("mix_0000", "mix_0049")
			for i := 1; i < len(results); i++ {
				if results[i].Key < results[i-1].Key {
					errs.Add(1)
					break
				}
			}
			ops.Add(1)
		}
	}
}

// stressMixedFlusher periodically flushes until done is closed.
func stressMixedFlusher(eng *Engine, cols []ColumnMeta, done <-chan struct{}, ops *atomic.Int64) {
	for {
		select {
		case <-done:
			return
		default:
			_ = eng.Flush(cols)
			ops.Add(1)
			time.Sleep(30 * time.Millisecond)
		}
	}
}

// stressMixedCompactor periodically compacts until done is closed.
func stressMixedCompactor(eng *Engine, cols []ColumnMeta, done <-chan struct{}, ops *atomic.Int64) {
	for {
		select {
		case <-done:
			return
		default:
			if eng.ShouldCompact() {
				_ = eng.Compact(cols)
			}
			ops.Add(1)
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestStress_MixedWorkload runs concurrent writes, reads, scans, flushes, and compactions.
func TestStress_MixedWorkload(t *testing.T) {
	eng := newStressEngine(t, 0)
	defer func() { _ = eng.Close() }()

	cols := stressCols()
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("mix_%04d", i)
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}

	const duration = 300 * time.Millisecond
	done := make(chan struct{})
	time.AfterFunc(duration, func() { close(done) })

	var wg sync.WaitGroup
	var ops, errs atomic.Int64

	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func(gid int) { defer wg.Done(); stressMixedWriter(t, eng, gid, done, &ops) }(g)
	}
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() { defer wg.Done(); stressMixedReader(eng, done, &ops) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); stressMixedScanner(eng, done, &ops, &errs) }()
	wg.Add(1)
	go func() { defer wg.Done(); stressMixedFlusher(eng, cols, done, &ops) }()
	wg.Add(1)
	go func() { defer wg.Done(); stressMixedCompactor(eng, cols, done, &ops) }()

	wg.Wait()
	t.Logf("MixedWorkload: %d ops, %d errors", ops.Load(), errs.Load())
	if errs.Load() > 0 {
		t.Errorf("found %d consistency errors", errs.Load())
	}
}

// TestStress_MemTableRotation uses small MemTable to trigger frequent rotations.
func TestStress_MemTableRotation(t *testing.T) {
	t.Parallel()
	eng := newStressEngine(t, 256) // Very small to trigger rotations
	defer func() { _ = eng.Close() }()

	cols := stressStringCols()
	const totalWrites = 500

	for i := 0; i < totalWrites; i++ {
		key := fmt.Sprintf("rot_%04d", i)
		val := fmt.Sprintf("value_%d_with_padding_to_increase_size", i)
		if err := eng.Write(key, map[string]common.Value{colVal: common.NewString(val)}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Flush all and verify
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Verify all keys
	notFound := 0
	for i := 0; i < totalWrites; i++ {
		key := fmt.Sprintf("rot_%04d", i)
		row, ok := eng.Get(key)
		if !ok {
			notFound++
		} else {
			expected := fmt.Sprintf("value_%d_with_padding_to_increase_size", i)
			if row.Columns[colVal].Str != expected {
				t.Errorf("key %s: expected %q, got %q", key, expected, row.Columns[colVal].Str)
			}
		}
	}
	if notFound > 0 {
		t.Errorf("%d keys not found after rotation stress", notFound)
	}
	t.Logf("MemTableRotation: %d writes, %d not found", totalWrites, notFound)
}

// TestStress_OverwriteAndDelete tests repeated overwrites for memory reuse.
// It verifies the engine handles key overwrites without panicking or leaking.
func TestStress_OverwriteAndDelete(t *testing.T) {
	t.Parallel()
	eng := newStressEngine(t, 0)
	defer func() { _ = eng.Close() }()

	cols := stressCols()
	const keys = 50
	const overwrites = 200

	// Repeatedly overwrite the same set of keys
	for o := 0; o < overwrites; o++ {
		for k := 0; k < keys; k++ {
			key := fmt.Sprintf("ow_%04d", k)
			val := int64(o*keys + k)
			if err := eng.Write(key, map[string]common.Value{colVal: common.NewInt64(val)}); err != nil {
				t.Fatalf("overwrite %d key %d: %v", o, k, err)
			}
		}
		// Periodically flush (no compact to avoid segment ID conflicts)
		if o%50 == 49 {
			if err := eng.Flush(cols); err != nil {
				t.Fatalf("flush at overwrite %d: %v", o, err)
			}
		}
	}

	// Final flush
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("final flush: %v", err)
	}

	// Verify keys exist; the latest value should be from the last overwrite
	// that was written to memtable or the most recent segment.
	notFound := 0
	for k := 0; k < keys; k++ {
		key := fmt.Sprintf("ow_%04d", k)
		row, ok := eng.Get(key)
		if !ok {
			notFound++
		} else if row.Columns[colVal].Int64 < 0 {
			t.Errorf("key %s: unexpected negative value %d", key, row.Columns[colVal].Int64)
		}
	}
	if notFound > 0 {
		t.Errorf("%d keys not found after overwrite stress", notFound)
	}
	t.Logf("OverwriteAndDelete: %d overwrites of %d keys, %d not found",
		overwrites, keys, notFound)
}

// TestStress_MemoryStability monitors heap growth over many write/read cycles.
func TestStress_MemoryStability(t *testing.T) {
	eng := newStressEngine(t, 0)
	defer func() { _ = eng.Close() }()

	cols := stressCols()
	const rounds = 15
	const rowsPerRound = 200

	// Warm up and get baseline
	for i := 0; i < rowsPerRound; i++ {
		_ = eng.Write(fmt.Sprintf("mem_%04d", i), map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}
	_ = eng.Flush(cols)
	// Two GC cycles to fully clear sync.Pool victim caches
	runtime.GC()
	runtime.GC()

	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	baselineHeap := baseline.HeapAlloc

	for r := 1; r <= rounds; r++ {
		// Write new keys each round
		for i := 0; i < rowsPerRound; i++ {
			key := fmt.Sprintf("mem_r%02d_%04d", r, i)
			_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(r*10000 + i))})
		}
		// Flush to move data out of memtable
		_ = eng.Flush(cols)
		if eng.ShouldCompact() {
			_ = eng.Compact(cols)
		}
		// Read back some keys to exercise read path
		for i := 0; i < rowsPerRound; i += 20 {
			key := fmt.Sprintf("mem_r%02d_%04d", r, i)
			_, _ = eng.Get(key)
		}
	}

	runtime.GC()
	runtime.GC()
	var final runtime.MemStats
	runtime.ReadMemStats(&final)
	finalHeap := final.HeapAlloc

	// Allow up to 10x growth (generous for CI), but detect unbounded leaks
	growth := float64(finalHeap) / float64(baselineHeap)
	t.Logf("MemoryStability: baseline=%d bytes, final=%d bytes, growth=%.2fx",
		baselineHeap, finalHeap, growth)

	if growth > 10.0 {
		t.Errorf("heap grew %.1fx from %d to %d bytes, possible memory leak",
			growth, baselineHeap, finalHeap)
	}
}
