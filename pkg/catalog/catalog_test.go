package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// ---- Catalog CRUD 测试 ----

func TestCatalogCreateTable(t *testing.T) {
	c := NewCatalog("")
	err := c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
		{Name: "name", Type: common.TypeString},
	}, []string{"id"}, TableOptions{})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	tbl, err := c.GetTable("users")
	if err != nil {
		t.Fatalf("GetTable() error = %v", err)
	}
	if tbl.Name != "users" {
		t.Errorf("table name = %q, want %q", tbl.Name, "users")
	}
	if len(tbl.Columns) != 2 {
		t.Errorf("columns count = %d, want 2", len(tbl.Columns))
	}
	if len(tbl.PrimaryKey) != 1 || tbl.PrimaryKey[0] != "id" {
		t.Errorf("primary key = %v, want [id]", tbl.PrimaryKey)
	}
}

func TestCatalogCreateTableDuplicate(t *testing.T) {
	c := NewCatalog("")
	err := c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})
	if err != nil {
		t.Fatalf("first CreateTable() error = %v", err)
	}
	err = c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})
	if err == nil {
		t.Error("duplicate CreateTable() should return error")
	}
}

func TestCatalogCreateTableNoColumns(t *testing.T) {
	c := NewCatalog("")
	err := c.CreateTable("t", []ColumnDef{}, []string{"id"}, TableOptions{})
	if err == nil {
		t.Error("CreateTable with no columns should return error")
	}
}

func TestCatalogCreateTableNoPrimaryKey(t *testing.T) {
	c := NewCatalog("")
	err := c.CreateTable("t", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, nil, TableOptions{})
	if err == nil {
		t.Error("CreateTable with no primary key should return error")
	}
}

func TestCatalogCreateTableInvalidPrimaryKey(t *testing.T) {
	c := NewCatalog("")
	err := c.CreateTable("t", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"missing_col"}, TableOptions{})
	if err == nil {
		t.Error("CreateTable with invalid primary key column should return error")
	}
}

func TestCatalogDropTable(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	err := c.DropTable("users")
	if err != nil {
		t.Fatalf("DropTable() error = %v", err)
	}
	_, err = c.GetTable("users")
	if err != common.ErrTableNotExist {
		t.Errorf("GetTable after drop = %v, want ErrTableNotExist", err)
	}
}

func TestCatalogDropTableNotExist(t *testing.T) {
	c := NewCatalog("")
	err := c.DropTable("notexist")
	if err != common.ErrTableNotExist {
		t.Errorf("DropTable(notexist) = %v, want ErrTableNotExist", err)
	}
}

func TestCatalogAddColumn(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	err := c.AddColumn("users", "email", ColumnDef{
		Name: "email", Type: common.TypeString,
	})
	if err != nil {
		t.Fatalf("AddColumn() error = %v", err)
	}
	tbl, _ := c.GetTable("users")
	if !tbl.HasColumn("email") {
		t.Error("table should have email column after AddColumn")
	}
	// 新列默认 Nullable
	col, _ := tbl.GetColumn("email")
	if !col.Nullable {
		t.Error("new column should be nullable by default")
	}
}

func TestCatalogAddColumnTableNotExist(t *testing.T) {
	c := NewCatalog("")
	err := c.AddColumn("notexist", "col", ColumnDef{Name: "col", Type: common.TypeInt64})
	if err != common.ErrTableNotExist {
		t.Errorf("AddColumn on non-existent table = %v, want ErrTableNotExist", err)
	}
}

func TestCatalogAddColumnDuplicate(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	err := c.AddColumn("users", "id", ColumnDef{Name: "id", Type: common.TypeInt64})
	if err == nil {
		t.Error("AddColumn duplicate should return error")
	}
}

func TestCatalogDropColumn(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
		{Name: "name", Type: common.TypeString},
	}, []string{"id"}, TableOptions{})

	err := c.DropColumn("users", "name")
	if err != nil {
		t.Fatalf("DropColumn() error = %v", err)
	}
	tbl, _ := c.GetTable("users")
	if tbl.HasColumn("name") {
		t.Error("table should not have name column after DropColumn")
	}
}

func TestCatalogDropColumnPrimaryKey(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	err := c.DropColumn("users", "id")
	if err == nil {
		t.Error("DropColumn on primary key should return error")
	}
}

func TestCatalogDropColumnNotExist(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	err := c.DropColumn("users", "notexist")
	if err != common.ErrColumnNotExist {
		t.Errorf("DropColumn(notexist) = %v, want ErrColumnNotExist", err)
	}
}

// ---- Segment 管理测试 ----

func TestCatalogRegisterSegment(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	seg := SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z", Size: 1024, RowCount: 100}
	err := c.RegisterSegment("users", seg)
	if err != nil {
		t.Fatalf("RegisterSegment() error = %v", err)
	}
	tbl, _ := c.GetTable("users")
	if len(tbl.SegmentList) != 1 {
		t.Errorf("segment count = %d, want 1", len(tbl.SegmentList))
	}
	if tbl.SegmentList[0].ID != 1 {
		t.Errorf("segment ID = %d, want 1", tbl.SegmentList[0].ID)
	}
}

func TestCatalogRegisterSegmentDuplicate(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	seg := SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z"}
	c.RegisterSegment("users", seg)
	err := c.RegisterSegment("users", seg)
	if err == nil {
		t.Error("RegisterSegment duplicate should return error")
	}
}

func TestCatalogUnregisterSegment(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	c.RegisterSegment("users", SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z"})
	c.RegisterSegment("users", SegmentRef{ID: 2, Level: 0, MinKey: "b", MaxKey: "y"})

	err := c.UnregisterSegment("users", 1)
	if err != nil {
		t.Fatalf("UnregisterSegment() error = %v", err)
	}
	tbl, _ := c.GetTable("users")
	if len(tbl.SegmentList) != 1 {
		t.Errorf("segment count = %d, want 1", len(tbl.SegmentList))
	}
	if tbl.SegmentList[0].ID != 2 {
		t.Errorf("remaining segment ID = %d, want 2", tbl.SegmentList[0].ID)
	}
}

func TestCatalogUnregisterSegmentNotFound(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	err := c.UnregisterSegment("users", 999)
	if err == nil {
		t.Error("UnregisterSegment with non-existent ID should return error")
	}
}

// ---- Snapshot 测试 ----

func TestCatalogSnapshot(t *testing.T) {
	c := NewCatalog("")
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
		{Name: "name", Type: common.TypeString},
	}, []string{"id"}, TableOptions{})

	snap := c.Snapshot()
	if snap.Version != c.Version() {
		t.Errorf("snapshot version = %d, want %d", snap.Version, c.Version())
	}
	if len(snap.Tables) != 1 {
		t.Errorf("snapshot tables count = %d, want 1", len(snap.Tables))
	}
	// 修改快照不应影响原始 Catalog
	delete(snap.Tables, "users")
	_, err := c.GetTable("users")
	if err != nil {
		t.Error("modifying snapshot should not affect original catalog")
	}
}

// ---- 版本号递增测试 ----

func TestCatalogVersionIncrement(t *testing.T) {
	c := NewCatalog("")
	initial := c.Version()

	c.CreateTable("t1", []ColumnDef{{Name: "id", Type: common.TypeInt64}}, []string{"id"}, TableOptions{})
	if c.Version() <= initial {
		t.Error("version should increment after CreateTable")
	}

	v := c.Version()
	c.AddColumn("t1", "col1", ColumnDef{Name: "col1", Type: common.TypeString})
	if c.Version() <= v {
		t.Error("version should increment after AddColumn")
	}

	v = c.Version()
	c.DropColumn("t1", "col1")
	if c.Version() <= v {
		t.Error("version should increment after DropColumn")
	}

	v = c.Version()
	c.RegisterSegment("t1", SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z"})
	if c.Version() <= v {
		t.Error("version should increment after RegisterSegment")
	}

	v = c.Version()
	c.UnregisterSegment("t1", 1)
	if c.Version() <= v {
		t.Error("version should increment after UnregisterSegment")
	}

	v = c.Version()
	c.DropTable("t1")
	if c.Version() <= v {
		t.Error("version should increment after DropTable")
	}
}

// ---- 持久化测试 ----

func TestCatalogPersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	c := NewCatalog(path)
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
		{Name: "name", Type: common.TypeString},
	}, []string{"id"}, TableOptions{})
	c.RegisterSegment("users", SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z", Size: 1024, RowCount: 50})

	// 从文件加载
	c2, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	tbl, err := c2.GetTable("users")
	if err != nil {
		t.Fatalf("GetTable() after load error = %v", err)
	}
	if tbl.Name != "users" {
		t.Errorf("table name = %q, want %q", tbl.Name, "users")
	}
	if len(tbl.Columns) != 2 {
		t.Errorf("columns count = %d, want 2", len(tbl.Columns))
	}
	if len(tbl.SegmentList) != 1 {
		t.Errorf("segment count = %d, want 1", len(tbl.SegmentList))
	}
	if tbl.SegmentList[0].ID != 1 {
		t.Errorf("segment ID = %d, want 1", tbl.SegmentList[0].ID)
	}
}

func TestCatalogLoadNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	c, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog() on non-existent file error = %v", err)
	}
	if c.Version() != 1 {
		t.Errorf("version = %d, want 1 for new catalog", c.Version())
	}
}

func TestCatalogPersistEmptyPath(t *testing.T) {
	c := NewCatalog("")
	err := c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})
	if err != nil {
		t.Fatalf("CreateTable with empty path should not error: %v", err)
	}
}

func TestCatalogPersistAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	c := NewCatalog(path)
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})

	// 不应有残留的临时文件
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after persist")
	}
}

func TestCatalogPersistMultipleOperations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	c := NewCatalog(path)
	c.CreateTable("users", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})
	c.AddColumn("users", "email", ColumnDef{Name: "email", Type: common.TypeString})
	c.RegisterSegment("users", SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z"})
	c.RegisterSegment("users", SegmentRef{ID: 2, Level: 1, MinKey: "b", MaxKey: "y"})
	c.UnregisterSegment("users", 1)

	// 重新加载验证所有操作
	c2, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	tbl, _ := c2.GetTable("users")
	if len(tbl.Columns) != 2 {
		t.Errorf("columns count = %d, want 2", len(tbl.Columns))
	}
	if len(tbl.SegmentList) != 1 {
		t.Errorf("segment count = %d, want 1", len(tbl.SegmentList))
	}
	if tbl.SegmentList[0].ID != 2 {
		t.Errorf("segment ID = %d, want 2", tbl.SegmentList[0].ID)
	}
}

// ---- 并发安全测试 ----

func TestCatalogConcurrentCreateTable(t *testing.T) {
	c := NewCatalog("")
	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			name := fmt.Sprintf("table_%d", idx)
			err := c.CreateTable(name, []ColumnDef{
				{Name: "id", Type: common.TypeInt64},
			}, []string{"id"}, TableOptions{})
			done <- err
		}(i)
	}

	successCount := 0
	for i := 0; i < 10; i++ {
		err := <-done
		if err == nil {
			successCount++
		}
	}
	if successCount != 10 {
		t.Errorf("concurrent CreateTable success count = %d, want 10", successCount)
	}
	snap := c.Snapshot()
	if len(snap.Tables) != 10 {
		t.Errorf("tables count = %d, want 10", len(snap.Tables))
	}
}
