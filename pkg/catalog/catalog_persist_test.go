package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestCatalogPersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	c := NewCatalog(path)
	err := c.CreateTable(tableUsers, []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
		{Name: colName, Type: common.TypeString},
	}, []string{"id"}, TableOptions{})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	err = c.RegisterSegment(tableUsers, SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z", Size: 1024, RowCount: 50})
	if err != nil {
		t.Fatalf("RegisterSegment() error = %v", err)
	}

	// 从文件加载
	c2, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	tbl, err := c2.GetTable(tableUsers)
	if err != nil {
		t.Fatalf("GetTable() after load error = %v", err)
	}
	if tbl.Name != tableUsers {
		t.Errorf("table name = %q, want %q", tbl.Name, tableUsers)
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
	err := c.CreateTable(tableUsers, []ColumnDef{
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
	err := c.CreateTable(tableUsers, []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}

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
	err := c.CreateTable(tableUsers, []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}, []string{"id"}, TableOptions{})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	err = c.AddColumn(tableUsers, colEmail, ColumnDef{Name: colEmail, Type: common.TypeString})
	if err != nil {
		t.Fatalf("AddColumn() error = %v", err)
	}
	err = c.RegisterSegment(tableUsers, SegmentRef{ID: 1, Level: 0, MinKey: "a", MaxKey: "z"})
	if err != nil {
		t.Fatalf("RegisterSegment(1) error = %v", err)
	}
	err = c.RegisterSegment(tableUsers, SegmentRef{ID: 2, Level: 1, MinKey: "b", MaxKey: "y"})
	if err != nil {
		t.Fatalf("RegisterSegment(2) error = %v", err)
	}
	err = c.UnregisterSegment(tableUsers, 1)
	if err != nil {
		t.Fatalf("UnregisterSegment() error = %v", err)
	}

	// 重新加载验证所有操作
	c2, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	tbl, err := c2.GetTable(tableUsers)
	if err != nil {
		t.Fatalf("GetTable() error = %v", err)
	}
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
