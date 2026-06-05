package catalog

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// TestSaveToFile_MarshalError tests that saveToFile returns an error when
// json.MarshalIndent fails. NaN float64 values cannot be represented in JSON,
// so a Database containing one should trigger the marshal error path.
func TestSaveToFile_MarshalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, testCatalogFile)

	db := NewDatabase()
	db.Tables["marshal_fail"] = &Table{
		Name: "marshal_fail",
		Columns: []ColumnDef{
			{
				Name: "val",
				Type: common.TypeFloat64,
				Default: common.Value{
					Typ:     common.TypeFloat64,
					Valid:   true,
					Float64: math.NaN(),
				},
			},
		},
		PrimaryKey: []string{"val"},
		Version:    1,
	}

	err := saveToFile(path, db)
	if err == nil {
		t.Error("saveToFile should return error when json.MarshalIndent fails due to NaN value")
	}
}

// TestSaveToFile_WriteTempFileError tests that saveToFile returns an error when
// writing the temporary file fails. By pre-creating the target directory and then
// making it read-only, MkdirAll succeeds (directory already exists) but
// os.WriteFile fails due to insufficient permissions.
func TestSaveToFile_WriteTempFileError(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("permission-based test not reliable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root user bypasses file permission checks")
	}

	dir := t.TempDir()
	targetDir := filepath.Join(dir, "noperm")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(targetDir, 0555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer func() { _ = os.Chmod(targetDir, 0755) }()

	path := filepath.Join(targetDir, testCatalogFile)
	db := NewDatabase()
	err := saveToFile(path, db)
	if err == nil {
		t.Error("saveToFile should return error when writing temp file fails")
	}
}
