package storage

import (
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
)

// TestScanRangeWithPruning_SkipsSegments 验证段裁剪能正确跳过不满足列谓词的段。
func TestScanRangeWithPruning_SkipsSegments(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer eng.Close()

	cols := []ColumnMeta{
		{ID: 0, Name: "id", Type: common.TypeInt64},
		{ID: 1, Name: "temp", Type: common.TypeFloat64},
	}

	// 写入低温数据并刷盘
	for i := 0; i < 50; i++ {
		key := padKey("cold", i)
		eng.Write(key, map[string]common.Value{
			"id":   common.NewInt64(int64(i)),
			"temp": common.NewFloat64(10.0 + float64(i)*0.1), // 10.0~14.9
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush cold: %v", err)
	}

	// 写入高温数据并刷盘
	for i := 0; i < 50; i++ {
		key := padKey("hot", i)
		eng.Write(key, map[string]common.Value{
			"id":   common.NewInt64(int64(50 + i)),
			"temp": common.NewFloat64(80.0 + float64(i)*0.1), // 80.0~84.9
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush hot: %v", err)
	}

	// 不使用段裁剪：应返回所有行
	allEntries := eng.ScanRange("", "\xff\xff\xff\xff")
	if len(allEntries) != 100 {
		t.Fatalf("expected 100 entries without pruning, got %d", len(allEntries))
	}

	// 使用段裁剪：temp > 50 应只返回高温段的数据
	predicates := []ColumnPredicate{
		{ColumnName: "temp", Op: index.OpGreater, Value: common.NewFloat64(50.0)},
	}
	prunedEntries := eng.ScanRangeWithPruning("", "\xff\xff\xff\xff", predicates)
	if len(prunedEntries) == 0 {
		t.Fatal("expected some entries with pruning, got 0")
	}
	if len(prunedEntries) >= len(allEntries) {
		t.Fatalf("pruning should reduce result count: pruned=%d, all=%d", len(prunedEntries), len(allEntries))
	}

	// 验证裁剪后的结果都满足谓词
	for _, entry := range prunedEntries {
		temp, ok := entry.Value.Columns["temp"]
		if !ok {
			t.Fatal("missing temp column")
		}
		if temp.Float64 <= 50.0 {
			t.Fatalf("pruned result should have temp > 50, got %f", temp.Float64)
		}
	}
}

// TestScanRangeWithPruning_NoPredicates 验证无谓词时段裁剪等同于普通扫描。
func TestScanRangeWithPruning_NoPredicates(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer eng.Close()

	cols := []ColumnMeta{
		{ID: 0, Name: "id", Type: common.TypeInt64},
	}

	for i := 0; i < 10; i++ {
		eng.Write(padKey("key", i), map[string]common.Value{
			"id": common.NewInt64(int64(i)),
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	normal := eng.ScanRange("", "\xff\xff\xff\xff")
	pruned := eng.ScanRangeWithPruning("", "\xff\xff\xff\xff", nil)

	if len(normal) != len(pruned) {
		t.Fatalf("no predicates: expected same count, normal=%d, pruned=%d", len(normal), len(pruned))
	}
}

// TestScanRangeWithPruning_EqualPredicate 验证等值谓词的段裁剪。
func TestScanRangeWithPruning_EqualPredicate(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer eng.Close()

	cols := []ColumnMeta{
		{ID: 0, Name: "id", Type: common.TypeInt64},
		{ID: 1, Name: "status", Type: common.TypeInt64},
	}

	// 段1: status = 1
	for i := 0; i < 10; i++ {
		eng.Write(padKey("s1", i), map[string]common.Value{
			"id":     common.NewInt64(int64(i)),
			"status": common.NewInt64(1),
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush s1: %v", err)
	}

	// 段2: status = 2
	for i := 0; i < 10; i++ {
		eng.Write(padKey("s2", i), map[string]common.Value{
			"id":     common.NewInt64(int64(10 + i)),
			"status": common.NewInt64(2),
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush s2: %v", err)
	}

	// 段3: status = 3
	for i := 0; i < 10; i++ {
		eng.Write(padKey("s3", i), map[string]common.Value{
			"id":     common.NewInt64(int64(20 + i)),
			"status": common.NewInt64(3),
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush s3: %v", err)
	}

	// status = 2 应只返回段2的数据
	predicates := []ColumnPredicate{
		{ColumnName: "status", Op: index.OpEqual, Value: common.NewInt64(2)},
	}
	pruned := eng.ScanRangeWithPruning("", "\xff\xff\xff\xff", predicates)

	if len(pruned) != 10 {
		t.Fatalf("expected 10 entries for status=2, got %d", len(pruned))
	}
	for _, entry := range pruned {
		status := entry.Value.Columns["status"]
		if status.Int64 != 2 {
			t.Fatalf("expected status=2, got %d", status.Int64)
		}
	}
}

// TestScanRangeWithPruning_MultiplePredicates 验证多谓词段裁剪（AND 语义）。
func TestScanRangeWithPruning_MultiplePredicates(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer eng.Close()

	cols := []ColumnMeta{
		{ID: 0, Name: "id", Type: common.TypeInt64},
		{ID: 1, Name: "age", Type: common.TypeInt64},
		{ID: 2, Name: "score", Type: common.TypeFloat64},
	}

	// 段1: age 20-30, score 50-60
	for i := 0; i < 10; i++ {
		eng.Write(padKey("seg1", i), map[string]common.Value{
			"id":    common.NewInt64(int64(i)),
			"age":   common.NewInt64(int64(20 + i)),
			"score": common.NewFloat64(50.0 + float64(i)),
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush seg1: %v", err)
	}

	// 段2: age 40-50, score 80-90
	for i := 0; i < 10; i++ {
		eng.Write(padKey("seg2", i), map[string]common.Value{
			"id":    common.NewInt64(int64(10 + i)),
			"age":   common.NewInt64(int64(40 + i)),
			"score": common.NewFloat64(80.0 + float64(i)),
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush seg2: %v", err)
	}

	// age > 35 AND score > 70 应只返回段2
	predicates := []ColumnPredicate{
		{ColumnName: "age", Op: index.OpGreater, Value: common.NewInt64(35)},
		{ColumnName: "score", Op: index.OpGreater, Value: common.NewFloat64(70.0)},
	}
	pruned := eng.ScanRangeWithPruning("", "\xff\xff\xff\xff", predicates)

	if len(pruned) != 10 {
		t.Fatalf("expected 10 entries for multi-predicate pruning, got %d", len(pruned))
	}
}

// TestScanRangeWithPruning_CannotSkipAll 验证当所有段都可能包含匹配数据时不裁剪。
func TestScanRangeWithPruning_CannotSkipAll(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer eng.Close()

	cols := []ColumnMeta{
		{ID: 0, Name: "id", Type: common.TypeInt64},
		{ID: 1, Name: "value", Type: common.TypeInt64},
	}

	// 所有段的 value 范围都覆盖 0-100
	for seg := 0; seg < 3; seg++ {
		for i := 0; i < 10; i++ {
			eng.Write(padKey("seg", seg*10+i), map[string]common.Value{
				"id":    common.NewInt64(int64(seg*10 + i)),
				"value": common.NewInt64(int64(i * 10)),
			})
		}
		if err := eng.Flush(cols); err != nil {
			t.Fatalf("flush seg %d: %v", seg, err)
		}
	}

	// value > 5: 所有段都可能包含匹配数据，不应裁剪任何段
	predicates := []ColumnPredicate{
		{ColumnName: "value", Op: index.OpGreater, Value: common.NewInt64(5)},
	}
	pruned := eng.ScanRangeWithPruning("", "\xff\xff\xff\xff", predicates)
	all := eng.ScanRange("", "\xff\xff\xff\xff")

	if len(pruned) != len(all) {
		t.Fatalf("no segments should be pruned when all may contain matches: pruned=%d, all=%d", len(pruned), len(all))
	}
}

// TestCanSkipSegment 验证 canSkipSegment 方法的正确性。
func TestCanSkipSegment(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(EngineConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer eng.Close()

	cols := []ColumnMeta{
		{ID: 0, Name: "id", Type: common.TypeInt64},
		{ID: 1, Name: "value", Type: common.TypeInt64},
	}

	// 写入数据并刷盘以注册稀疏索引
	for i := 0; i < 10; i++ {
		eng.Write(padKey("key", i), map[string]common.Value{
			"id":    common.NewInt64(int64(i)),
			"value": common.NewInt64(int64(i * 10)), // 0, 10, 20, ..., 90
		})
	}
	if err := eng.Flush(cols); err != nil {
		t.Fatalf("flush: %v", err)
	}

	colNameToID := map[string]uint32{"id": 0, "value": 1}

	// 获取段 ID
	segID := eng.segments[0].ID

	tests := []struct {
		name      string
		predicate ColumnPredicate
		wantSkip  bool
	}{
		{"value > 100 (超出范围)", ColumnPredicate{ColumnName: "value", Op: index.OpGreater, Value: common.NewInt64(100)}, true},
		{"value < 0 (低于范围)", ColumnPredicate{ColumnName: "value", Op: index.OpLess, Value: common.NewInt64(0)}, true},
		{"value > 50 (在范围内)", ColumnPredicate{ColumnName: "value", Op: index.OpGreater, Value: common.NewInt64(50)}, false},
		{"value = 50 (在范围内)", ColumnPredicate{ColumnName: "value", Op: index.OpEqual, Value: common.NewInt64(50)}, false},
		{"value = 200 (超出范围)", ColumnPredicate{ColumnName: "value", Op: index.OpEqual, Value: common.NewInt64(200)}, true},
		{"unknown column (不裁剪)", ColumnPredicate{ColumnName: "unknown", Op: index.OpGreater, Value: common.NewInt64(0)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eng.canSkipSegment(segID, []ColumnPredicate{tt.predicate}, colNameToID)
			if got != tt.wantSkip {
				t.Errorf("canSkipSegment() = %v, want %v", got, tt.wantSkip)
			}
		})
	}
}

// padKey 生成固定长度的键，确保键排序正确。
func padKey(prefix string, i int) string {
	return fmt.Sprintf("%s_%04d", prefix, i)
}
