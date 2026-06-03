package storage

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestConcurrent_WriteReadConsistency 验证并发写入和读取的数据一致性。
// 多个 goroutine 同时写入不同的 key，同时读取已写入的 key，确保读到正确的值。
func TestConcurrent_WriteReadConsistency(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	const writers = 8
	const writesPerWriter = 100
	var wg sync.WaitGroup
	var writeErr atomic.Int32

	// Concurrent writes
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("g%d_key_%04d", gid, j)
				val := int64(gid*10000 + j)
				if err := eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(val),
				}); err != nil {
					writeErr.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()

	if writeErr.Load() > 0 {
		t.Fatalf("write errors: %d", writeErr.Load())
	}

	// Verify all data is readable and correct
	for g := 0; g < writers; g++ {
		for j := 0; j < writesPerWriter; j++ {
			key := fmt.Sprintf("g%d_key_%04d", g, j)
			expected := int64(g*10000 + j)
			row, ok := eng.Get(key)
			if !ok {
				t.Errorf("key %s not found", key)
				continue
			}
			if row.Columns[colVal].Int64 != expected {
				t.Errorf("key %s: expected %d, got %d", key, expected, row.Columns[colVal].Int64)
			}
		}
	}
}

// TestConcurrent_ReadWhileWriting 验证写入过程中并发读取不会崩溃或返回不一致数据。
// 读取线程可能读到旧值或新值，但不应读到脏数据。
func TestConcurrent_ReadWhileWriting(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Pre-write some data
	const preWriteCount = 50
	for i := 0; i < preWriteCount; i++ {
		key := fmt.Sprintf("pre_key_%04d", i)
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}

	const writers = 4
	const writesPerWriter = 100
	const readers = 4
	const readsPerReader = 200

	var wg sync.WaitGroup
	var readErr atomic.Int32

	// Start writers
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("w%d_key_%04d", gid, j)
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid*10000 + j)),
				})
			}
		}(g)
	}

	// Start readers
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				// Read pre-written keys (should always succeed)
				key := fmt.Sprintf("pre_key_%04d", j%preWriteCount)
				row, ok := eng.Get(key)
				if ok {
					// If found, value must be correct
					val := row.Columns[colVal].Int64
					if val < 0 || val >= preWriteCount {
						readErr.Add(1)
					}
				}
				// Also read keys being written (may or may not exist)
				_, _ = eng.Get("w0_key_0001")
			}
		}()
	}

	wg.Wait()

	if readErr.Load() > 0 {
		t.Errorf("read consistency errors: %d", readErr.Load())
	}
}

// TestConcurrent_WriteWithFlush 验证并发写入与 Flush 交替执行的数据一致性。
func TestConcurrent_WriteWithFlush(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	const writers = 4
	const writesPerWriter = 100
	const flushCount = 5
	var wg sync.WaitGroup

	// Concurrent writes
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("g%d_key_%04d", gid, j)
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid*10000 + j)),
				})
			}
		}(g)
	}

	// Periodic flushes
	var flushWg sync.WaitGroup
	for f := 0; f < flushCount; f++ {
		flushWg.Add(1)
		go func() {
			defer flushWg.Done()
			time.Sleep(time.Duration(f+1) * 10 * time.Millisecond)
			_ = eng.Flush(cols)
		}()
	}

	wg.Wait()
	flushWg.Wait()

	// Verify all data is accessible
	for g := 0; g < writers; g++ {
		for j := 0; j < writesPerWriter; j++ {
			key := fmt.Sprintf("g%d_key_%04d", g, j)
			expected := int64(g*10000 + j)
			row, ok := eng.Get(key)
			if !ok {
				t.Errorf("key %s not found after flush", key)
				continue
			}
			if row.Columns[colVal].Int64 != expected {
				t.Errorf("key %s: expected %d, got %d", key, expected, row.Columns[colVal].Int64)
			}
		}
	}
}

// TestConcurrent_WriteFlushCompact 验证并发写入、Flush 和 Compact 的数据一致性。
func TestConcurrent_WriteFlushCompact(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// Write initial data and flush to create L0 segments
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("init_%04d", i)
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("initial flush: %v", err)
	}

	const writers = 4
	const writesPerWriter = 50
	var wg sync.WaitGroup

	// Concurrent writes
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("cw%d_key_%04d", gid, j)
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid*10000 + j)),
				})
			}
		}(g)
	}

	// Concurrent flush and compact
	var bgWg sync.WaitGroup
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		for i := 0; i < 3; i++ {
			time.Sleep(20 * time.Millisecond)
			_ = eng.Flush(cols)
		}
	}()
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		time.Sleep(50 * time.Millisecond)
		if eng.ShouldCompact() {
			_ = eng.Compact(cols)
		}
	}()

	wg.Wait()
	bgWg.Wait()

	// Verify all concurrent write data is accessible
	for g := 0; g < writers; g++ {
		for j := 0; j < writesPerWriter; j++ {
			key := fmt.Sprintf("cw%d_key_%04d", g, j)
			expected := int64(g*10000 + j)
			row, ok := eng.Get(key)
			if !ok {
				t.Errorf("key %s not found", key)
				continue
			}
			if row.Columns[colVal].Int64 != expected {
				t.Errorf("key %s: expected %d, got %d", key, expected, row.Columns[colVal].Int64)
			}
		}
	}
}

// TestConcurrent_OverwriteConsistency 验证并发覆盖写入后，最终读取到的是最后一次写入的值之一。
func TestConcurrent_OverwriteConsistency(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	const key = "shared_key"
	const overwriters = 10
	var wg sync.WaitGroup

	// Each goroutine overwrites the same key with its own ID
	for g := 0; g < overwriters; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid)),
				})
			}
		}(g)
	}
	wg.Wait()

	// The final value must be one of the goroutine IDs
	row, ok := eng.Get(key)
	if !ok {
		t.Fatal("shared_key not found")
	}
	val := row.Columns[colVal].Int64
	if val < 0 || val >= overwriters {
		t.Errorf("unexpected value %d for shared_key, expected one of [0, %d]", val, overwriters-1)
	}
}

// TestConcurrent_ScanWhileWriting 验证写入过程中进行 Scan 不会崩溃。
func TestConcurrent_ScanWhileWriting(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Pre-write data
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("scan_key_%04d", i)
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}

	const writers = 4
	const scanners = 4
	const opsPerWorker = 50
	var wg sync.WaitGroup
	var scanErr atomic.Int32

	// Concurrent writers
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				key := fmt.Sprintf("sw%d_key_%04d", gid, j)
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid*1000 + j)),
				})
			}
		}(g)
	}

	// Concurrent scanners
	for s := 0; s < scanners; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				results := eng.Scan("scan_key_0000", "scan_key_0099")
				// Results should be sorted
				for i := 1; i < len(results); i++ {
					if results[i].Key < results[i-1].Key {
						scanErr.Add(1)
						break
					}
				}
			}
		}()
	}

	wg.Wait()

	if scanErr.Load() > 0 {
		t.Errorf("scan consistency errors: %d", scanErr.Load())
	}
}

// TestConcurrent_WithScheduler 验证后台调度器运行时的并发读写一致性。
func TestConcurrent_WithScheduler(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	sched := NewScheduler(eng, SchedulerConfig{
		FlushInterval:    50 * time.Millisecond,
		CompactInterval:  100 * time.Millisecond,
		WALCleanInterval: 200 * time.Millisecond,
	})
	sched.Start()
	defer sched.Stop()

	const writers = 4
	const writesPerWriter = 100
	var wg sync.WaitGroup

	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("sched%d_key_%04d", gid, j)
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid*10000 + j)),
				})
			}
		}(g)
	}

	wg.Wait()

	// Give scheduler time to process
	time.Sleep(300 * time.Millisecond)

	// Verify all data
	for g := 0; g < writers; g++ {
		for j := 0; j < writesPerWriter; j++ {
			key := fmt.Sprintf("sched%d_key_%04d", g, j)
			expected := int64(g*10000 + j)
			row, ok := eng.Get(key)
			if !ok {
				t.Errorf("key %s not found", key)
				continue
			}
			if row.Columns[colVal].Int64 != expected {
				t.Errorf("key %s: expected %d, got %d", key, expected, row.Columns[colVal].Int64)
			}
		}
	}
}

// TestConcurrent_MixedOperations 验证混合并发操作（Write/Get/Scan/Flush）的数据一致性。
func TestConcurrent_MixedOperations(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// Pre-write data
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("mix_pre_%04d", i)
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}

	const duration = 500 * time.Millisecond
	var ops atomic.Int64
	var errors atomic.Int32

	done := make(chan struct{})
	time.AfterFunc(duration, func() { close(done) })

	// Writers
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-done:
					return
				default:
					key := fmt.Sprintf("mix_w%d_%04d", gid, i)
					_ = eng.Write(key, map[string]common.Value{
						colVal: common.NewInt64(int64(gid*10000 + i)),
					})
					ops.Add(1)
					i++
				}
			}
		}(g)
	}

	// Readers
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_, _ = eng.Get("mix_pre_0010")
					ops.Add(1)
				}
			}
		}()
	}

	// Scanners
	for s := 0; s < 2; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					results := eng.Scan("mix_pre_0000", "mix_pre_0049")
					for i := 1; i < len(results); i++ {
						if results[i].Key < results[i-1].Key {
							errors.Add(1)
							break
						}
					}
					ops.Add(1)
				}
			}
		}()
	}

	// Flushers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = eng.Flush(cols)
				ops.Add(1)
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	wg.Wait()

	t.Logf("Completed %d operations, %d errors", ops.Load(), errors.Load())
	if errors.Load() > 0 {
		t.Errorf("found %d consistency errors during mixed operations", errors.Load())
	}
}

// TestConcurrent_WriteAfterFlushRecovery 验证并发写入后 Flush，数据在崩溃恢复后仍然一致。
func TestConcurrent_WriteAfterFlushRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := EngineConfig{DataDir: dir}

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	const writers = 4
	const writesPerWriter = 50
	var wg sync.WaitGroup

	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("rcv%d_key_%04d", gid, j)
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid*10000 + j)),
				})
			}
		}(g)
	}
	wg.Wait()

	// Flush to persist data
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Write more data (unflushed)
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("rcv_extra_%04d", i)
		_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i + 1000))})
	}

	// Simulate crash
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	// Reopen and verify
	eng2, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	defer func() { _ = eng2.Close() }()

	// Verify flushed data
	for g := 0; g < writers; g++ {
		for j := 0; j < writesPerWriter; j++ {
			key := fmt.Sprintf("rcv%d_key_%04d", g, j)
			expected := int64(g*10000 + j)
			row, ok := eng2.Get(key)
			if !ok {
				t.Errorf("key %s not recovered", key)
				continue
			}
			if row.Columns[colVal].Int64 != expected {
				t.Errorf("key %s: expected %d, got %d", key, expected, row.Columns[colVal].Int64)
			}
		}
	}

	// Verify unflushed data (from WAL)
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("rcv_extra_%04d", i)
		expected := int64(i + 1000)
		row, ok := eng2.Get(key)
		if !ok {
			t.Errorf("extra key %s not recovered from WAL", key)
			continue
		}
		if row.Columns[colVal].Int64 != expected {
			t.Errorf("extra key %s: expected %d, got %d", key, expected, row.Columns[colVal].Int64)
		}
	}
}

// TestConcurrent_StressWrite 验证高并发写入压力下引擎的稳定性。
func TestConcurrent_StressWrite(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	const goroutines = 16
	const writesPerGoroutine = 200
	var wg sync.WaitGroup
	var errCount atomic.Int32

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				key := fmt.Sprintf("stress_%d_%04d", gid, j)
				if err := eng.Write(key, map[string]common.Value{
					colVal: common.NewInt64(int64(gid*100000 + j)),
				}); err != nil {
					errCount.Add(1)
				}
			}
		}(g)
	}

	wg.Wait()

	if errCount.Load() > 0 {
		t.Errorf("stress write errors: %d", errCount.Load())
	}

	// Verify total key count by reading each key
	verifiedCount := 0
	for g := 0; g < goroutines; g++ {
		for j := 0; j < writesPerGoroutine; j++ {
			key := fmt.Sprintf("stress_%d_%04d", g, j)
			if _, ok := eng.Get(key); ok {
				verifiedCount++
			}
		}
	}
	expectedCount := goroutines * writesPerGoroutine
	if verifiedCount != expectedCount {
		t.Errorf("expected %d keys, verified %d", expectedCount, verifiedCount)
	}
}

// TestConcurrent_MemTableRotationUnderLoad 验证高负载下 MemTable 自动轮转的正确性。
func TestConcurrent_MemTableRotationUnderLoad(t *testing.T) {
	eng, err := NewEngine(EngineConfig{
		DataDir:         t.TempDir(),
		MaxMemTableSize: 512, // Small size to trigger frequent rotations
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	const writers = 4
	const writesPerWriter = 100
	var wg sync.WaitGroup

	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				key := fmt.Sprintf("rot%d_%04d", gid, j)
				_ = eng.Write(key, map[string]common.Value{
					colVal: common.NewString(fmt.Sprintf("value_%d_%d", gid, j)),
				})
			}
		}(g)
	}

	wg.Wait()

	// Verify all data
	for g := 0; g < writers; g++ {
		for j := 0; j < writesPerWriter; j++ {
			key := fmt.Sprintf("rot%d_%04d", g, j)
			expected := fmt.Sprintf("value_%d_%d", g, j)
			row, ok := eng.Get(key)
			if !ok {
				t.Errorf("key %s not found", key)
				continue
			}
			if row.Columns[colVal].Str != expected {
				t.Errorf("key %s: expected %q, got %q", key, expected, row.Columns[colVal].Str)
			}
		}
	}
}

// TestConcurrent_MultipleDataTypeWriteRead 验证并发写入不同数据类型的一致性。
func TestConcurrent_MultipleDataTypeWriteRead(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	var wg sync.WaitGroup
	const count = 50

	// Concurrent int writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			_ = eng.Write(fmt.Sprintf("int_%04d", i), map[string]common.Value{
				colVal: common.NewInt64(int64(i)),
			})
		}
	}()

	// Concurrent float writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			_ = eng.Write(fmt.Sprintf("float_%04d", i), map[string]common.Value{
				colVal: common.NewFloat64(float64(i) * 1.1),
			})
		}
	}()

	// Concurrent string writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			_ = eng.Write(fmt.Sprintf("str_%04d", i), map[string]common.Value{
				colVal: common.NewString(fmt.Sprintf("hello_%d", i)),
			})
		}
	}()

	// Concurrent bool writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			_ = eng.Write(fmt.Sprintf("bool_%04d", i), map[string]common.Value{
				colVal: common.NewBool(i%2 == 0),
			})
		}
	}()

	wg.Wait()

	// Verify int data
	for i := 0; i < count; i++ {
		row, ok := eng.Get(fmt.Sprintf("int_%04d", i))
		if !ok || row.Columns[colVal].Int64 != int64(i) {
			t.Errorf("int_%04d not correct", i)
		}
	}

	// Verify float data
	for i := 0; i < count; i++ {
		row, ok := eng.Get(fmt.Sprintf("float_%04d", i))
		if !ok {
			t.Errorf("float_%04d not found", i)
			continue
		}
		expected := float64(i) * 1.1
		if row.Columns[colVal].Float64 != expected {
			t.Errorf("float_%04d: expected %f, got %f", i, expected, row.Columns[colVal].Float64)
		}
	}

	// Verify string data
	for i := 0; i < count; i++ {
		row, ok := eng.Get(fmt.Sprintf("str_%04d", i))
		if !ok || row.Columns[colVal].Str != fmt.Sprintf("hello_%d", i) {
			t.Errorf("str_%04d not correct", i)
		}
	}

	// Verify bool data
	for i := 0; i < count; i++ {
		row, ok := eng.Get(fmt.Sprintf("bool_%04d", i))
		if !ok {
			t.Errorf("bool_%04d not found", i)
			continue
		}
		expected := i%2 == 0
		got := row.Columns[colVal].Int64 != 0
		if got != expected {
			t.Errorf("bool_%04d: expected %v, got %v", i, expected, got)
		}
	}
}

// TestConcurrent_SnapshotIsolation 验证读取操作看到的是一致的数据快照。
// 写入新值不应影响正在进行的读取。
func TestConcurrent_SnapshotIsolation(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	const key = "snap_key"
	_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(1)})

	var wg sync.WaitGroup
	var readViolations atomic.Int32

	// Reader reads the key many times, should always get a valid value
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			row, ok := eng.Get(key)
			if !ok {
				readViolations.Add(1)
				continue
			}
			val := row.Columns[colVal].Int64
			// Value should be a positive integer (each write increments by 1)
			if val < 1 {
				readViolations.Add(1)
			}
		}
	}()

	// Writer continuously overwrites the key
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 2; i <= 1002; i++ {
			_ = eng.Write(key, map[string]common.Value{colVal: common.NewInt64(int64(i))})
		}
	}()

	wg.Wait()

	if readViolations.Load() > 0 {
		t.Errorf("snapshot isolation violations: %d", readViolations.Load())
	}
}

// TestConcurrent_FlushAndReadSegments 验证 Flush 产生的新 Segment 不影响并发读取。
func TestConcurrent_FlushAndReadSegments(t *testing.T) {
	eng, err := NewEngine(EngineConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}

	// Pre-write and flush
	for i := 0; i < 50; i++ {
		_ = eng.Write(fmt.Sprintf("seg_%04d", i), map[string]common.Value{colVal: common.NewInt64(int64(i))})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("initial flush: %v", err)
	}

	var wg sync.WaitGroup
	var readErr atomic.Int32

	// Reader continuously reads from segments
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			row, ok := eng.Get("seg_0025")
			if !ok {
				readErr.Add(1)
			} else if row.Columns[colVal].Int64 != 25 {
				readErr.Add(1)
			}
		}
	}()

	// Writer adds more data and flushes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 50; i < 100; i++ {
			_ = eng.Write(fmt.Sprintf("seg_%04d", i), map[string]common.Value{colVal: common.NewInt64(int64(i))})
		}
		_ = eng.Flush(cols)
	}()

	wg.Wait()

	if readErr.Load() > 0 {
		t.Errorf("segment read errors during flush: %d", readErr.Load())
	}
}
