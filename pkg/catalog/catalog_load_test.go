package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestLoadCatalogEmptyPath tests that LoadCatalog with an empty path
// returns a new Catalog without attempting to load from file.
func TestLoadCatalogEmptyPath(t *testing.T) {
	c, err := LoadCatalog("")
	if err != nil {
		t.Fatalf("LoadCatalog('') error = %v", err)
	}
	if c == nil {
		t.Fatal("LoadCatalog('') returned nil catalog")
	}
	if c.Version() != 1 {
		t.Errorf("version = %d, want 1 for new catalog", c.Version())
	}
}

// TestLoadCatalogValidFile tests that LoadCatalog correctly loads
// catalog data from a valid persisted file.
func TestLoadCatalogValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	// Create and persist a catalog with a table.
	c1 := NewCatalog(path)
	err := c1.CreateTable("test_table", []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
		{Name: "name", Type: common.TypeString},
	}, []string{"id"}, TableOptions{})
	if err != nil {
		t.Fatalf("CreateTable error = %v", err)
	}

	// Load from the same file.
	c2, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog error = %v", err)
	}
	if c2 == nil {
		t.Fatal("LoadCatalog returned nil catalog")
	}

	tbl, err := c2.GetTable("test_table")
	if err != nil {
		t.Fatalf("GetTable error = %v", err)
	}
	if tbl.Name != "test_table" {
		t.Errorf("table name = %q, want %q", tbl.Name, "test_table")
	}
	if len(tbl.Columns) != 2 {
		t.Errorf("columns count = %d, want 2", len(tbl.Columns))
	}
}

// TestLoadCatalogCorruptedJSON tests that LoadCatalog returns an error
// when the catalog file contains corrupted JSON.
func TestLoadCatalogCorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	// Write corrupted JSON to the catalog file.
	if err := os.WriteFile(path, []byte("{invalid json!!!"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := LoadCatalog(path)
	if err == nil {
		t.Error("LoadCatalog should return error for corrupted JSON file")
	}
}
