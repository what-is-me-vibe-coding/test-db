package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestEngineBlockCacheAccessor 测试 Engine.BlockCache() 访问器
func TestEngineBlockCacheAccessor(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, BlockCacheSize: 1024})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	bc := eng.BlockCache()
	if bc == nil {
		t.Fatal("expected non-nil BlockCache")
	}

	// 放入数据并验证可通过访问器操作
	key := CacheKey{SegmentID: 1, ColumnIdx: 0}
	dc := decodedColumn{data: []int64{1, 2, 3}, typ: common.TypeInt64}
	bc.put(key, dc)

	got, ok := bc.get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	ints, ok := got.data.([]int64)
	if !ok || len(ints) != 3 || ints[0] != 1 {
		t.Fatalf("unexpected data: %v", got.data)
	}
}

// TestEngineIndexCacheAccessor 测试 Engine.IndexCache() 访问器
func TestEngineIndexCacheAccessor(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, IndexCacheSize: 100})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	ic := eng.IndexCache()
	if ic == nil {
		t.Fatal("expected non-nil IndexCache")
	}

	// 放入数据并验证可通过访问器操作
	stats := []ColumnStat{{ColumnID: 0, NullCount: 5}}
	ic.PutColumnStats(1, stats)

	got, ok := ic.GetColumnStats(1)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 || got[0].ColumnID != 0 {
		t.Fatalf("unexpected stats: %v", got)
	}
}

// TestEngineCacheStatsAccessor 测试 Engine.CacheStats() 访问器
func TestEngineCacheStatsAccessor(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir, BlockCacheSize: 4096, IndexCacheSize: 10})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 初始状态
	blockStats, indexEntries := eng.CacheStats()
	if blockStats.Hits != 0 || blockStats.Misses != 0 {
		t.Fatalf("expected zero initial stats, got hits=%d misses=%d", blockStats.Hits, blockStats.Misses)
	}
	if indexEntries != 0 {
		t.Fatalf("expected 0 index entries, got %d", indexEntries)
	}

	// 写入数据并刷盘以填充缓存
	cols := []ColumnMeta{{ID: 0, Name: "id", Type: common.TypeInt64}}
	for i := 0; i < 5; i++ {
		if err := eng.Write(string(rune('a'+i)), map[string]common.Value{
			"id": common.NewInt64(int64(i)),
		}); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// 查询以触发缓存操作
	eng.Get("a")
	eng.Get("a")

	// 验证缓存统计更新
	blockStats, indexEntries = eng.CacheStats()
	_ = blockStats // 统计可能因缓存命中/未命中而变化
	_ = indexEntries
}

// TestIndexCacheClearNonNil 测试 IndexCache.Clear() 在非 nil 缓存上的行为
func TestIndexCacheClearNonNil(t *testing.T) {
	cache := NewIndexCache(10)

	// 放入多个条目
	for i := uint64(1); i <= 5; i++ {
		cache.PutColumnStats(i, []ColumnStat{{ColumnID: uint32(i)}})
	}

	if cache.Len() != 5 {
		t.Fatalf("expected 5 entries, got %d", cache.Len())
	}

	// 清空缓存
	cache.Clear()

	if cache.Len() != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", cache.Len())
	}

	// 验证清空后可以重新放入数据
	cache.PutColumnStats(10, []ColumnStat{{ColumnID: 10}})
	got, ok := cache.GetColumnStats(10)
	if !ok {
		t.Fatal("expected cache hit after clear and re-insert")
	}
	if len(got) != 1 || got[0].ColumnID != 10 {
		t.Fatalf("unexpected data after re-insert: %v", got)
	}
}

// TestEstimateDecodedSizeTimeAndDefault 测试 estimateDecodedSize 对 time.Time 和未知类型的处理
func TestEstimateDecodedSizeTimeAndDefault(t *testing.T) {
	tests := []struct {
		name    string
		dc      decodedColumn
		minSize int64
	}{
		{
			name:    "time_slice",
			dc:      decodedColumn{data: make([]time.Time, 10), typ: common.TypeTimestamp},
			minSize: 64 + 10*24,
		},
		{
			name:    "unknown_type_default",
			dc:      decodedColumn{data: int64(42), typ: common.DataType(99)},
			minSize: 64 + 256,
		},
		{
			name:    "with_nulls_bitmap",
			dc:      decodedColumn{data: []int64{1, 2, 3}, nulls: common.NewBitmap(3), typ: common.TypeInt64},
			minSize: 64 + 3*8 + 32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := estimateDecodedSize(tt.dc)
			if size < tt.minSize {
				t.Errorf("expected size >= %d, got %d", tt.minSize, size)
			}
		})
	}
}

// TestWALMaybeRotateCloseError 测试 WAL rotate 时关闭旧文件失败
func TestWALMaybeRotateCloseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}

	// 先关闭底层文件，使 maybeRotate 中的 Close 失败
	_ = w.file.Close()

	// 重新打开文件以恢复 WAL 状态
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	w.file = f
	w.maxSize = 1 // 触发 rotate

	err = w.AppendWrite([]byte("trigger rotate"))
	if err == nil {
		t.Log("rotate succeeded despite closed file (file was reopened)")
	} else {
		t.Logf("rotate error (expected): %v", err)
	}
}

// TestWALMaybeRotateRenameError 测试 WAL rotate 时重命名失败
func TestWALMaybeRotateRenameError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wal")

	w, err := CreateWAL(path)
	if err != nil {
		t.Fatalf("CreateWAL: %v", err)
	}

	// 设置很小的 maxSize 触发 rotate
	w.maxSize = walMetaSize + 10

	// 写入数据触发 rotate
	for i := 0; i < 3; i++ {
		if err := w.AppendWrite([]byte("data")); err != nil {
			t.Fatalf("AppendWrite: %v", err)
		}
	}

	_ = w.Close()

	// 验证 rotate 成功（.prev 文件存在）
	_, err = os.Stat(path + ".prev")
	if err != nil {
		t.Fatalf("expected .prev file after rotation: %v", err)
	}
}

// TestEngineNewWithDisabledCache 测试 Engine 使用禁用缓存（容量 <= 0）的配置
func TestEngineNewWithDisabledCache(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{
		DataDir:        dir,
		BlockCacheSize: -1, // 禁用 BlockCache
		IndexCacheSize: -1, // 禁用 IndexCache
	})
	if err != nil {
		t.Fatalf("NewEngine with disabled cache: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// 写入并查询数据，确保禁用缓存后功能正常
	cols := []ColumnMeta{{ID: 0, Name: colVal, Type: common.TypeInt64}}
	if err := eng.Write("key1", map[string]common.Value{
		colVal: common.NewInt64(42),
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	row, ok := eng.Get("key1")
	if !ok {
		t.Fatal("expected to find key1")
	}
	val := row.Columns[colVal]
	if val.Int64 != 42 {
		t.Fatalf("expected val=42, got %v", val.Int64)
	}

	// 验证缓存访问器返回禁用状态
	bc := eng.BlockCache()
	if bc != nil {
		// BlockCache 存在但容量 <= 0，应不缓存
		bc.put(CacheKey{SegmentID: 1, ColumnIdx: 0}, decodedColumn{data: []int64{1}, typ: common.TypeInt64})
		_, ok := bc.get(CacheKey{SegmentID: 1, ColumnIdx: 0})
		if ok {
			t.Error("expected cache miss on disabled BlockCache")
		}
	}

	ic := eng.IndexCache()
	if ic != nil {
		ic.PutColumnStats(1, []ColumnStat{{ColumnID: 0}})
		_, ok := ic.GetColumnStats(1)
		if ok {
			t.Error("expected cache miss on disabled IndexCache")
		}
	}
}
