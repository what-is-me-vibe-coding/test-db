package query

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// --- fillColumnValues tests ---

func TestFillColumnValues_ColumnNotFound(t *testing.T) {
	col := storage.NewColumnVector(0, common.TypeInt64, 1)
	batch := []storage.ScanEntry{
		{Key: "a", Value: storage.Row{Columns: map[string]common.Value{
			"other_col": common.NewInt64(1),
		}}},
	}
	colDef := ColumnDef{Name: testColNonexistent, Type: common.TypeInt64}

	fillColumnValues(col, batch, colDef)

	if !col.IsNull(0) {
		t.Error("expected NULL for missing column, got non-NULL value")
	}
}

func TestFillColumnValues_TypeMismatchCoerceSucceeds(t *testing.T) {
	col := storage.NewColumnVector(0, common.TypeFloat64, 1)
	batch := []storage.ScanEntry{
		{Key: "a", Value: storage.Row{Columns: map[string]common.Value{
			colNameVal: common.NewInt64(42),
		}}},
	}
	colDef := ColumnDef{Name: colNameVal, Type: common.TypeFloat64}

	fillColumnValues(col, batch, colDef)

	if col.IsNull(0) {
		t.Fatal("expected non-NULL value after successful coercion")
	}
	got := col.GetValue(0)
	if got.Typ != common.TypeFloat64 {
		t.Errorf("expected Float64 type after coercion, got %v", got.Typ)
	}
	if got.Float64 != 42.0 {
		t.Errorf("expected 42.0 after Int64->Float64 coercion, got %g", got.Float64)
	}
}

func TestFillColumnValues_TypeMismatchCoerceFails(t *testing.T) {
	col := storage.NewColumnVector(0, common.TypeInt64, 1)
	batch := []storage.ScanEntry{
		{Key: "a", Value: storage.Row{Columns: map[string]common.Value{
			colNameVal: common.NewString("hello"),
		}}},
	}
	colDef := ColumnDef{Name: colNameVal, Type: common.TypeInt64}

	fillColumnValues(col, batch, colDef)

	if !col.IsNull(0) {
		t.Error("expected NULL for uncoercible type mismatch (String->Int64), got non-NULL")
	}
}

func TestFillColumnValues_SetValueError(t *testing.T) {
	// Create a ColumnVector with a type different from colDef.Type.
	// After coerceValue succeeds (types match colDef), SetValue will fail
	// because the ColumnVector's actual type differs from colDef.Type.
	col := storage.NewColumnVector(0, common.TypeString, 1)
	batch := []storage.ScanEntry{
		{Key: "a", Value: storage.Row{Columns: map[string]common.Value{
			colNameVal: common.NewInt64(42),
		}}},
	}
	// colDef says Int64, but the ColumnVector is String.
	// coerceValue(Int64(42), Int64) = Int64(42) (no-op, types already match)
	// SetValue(Int64(42)) on a String column -> ErrTypeMismatch -> SetNull
	colDef := ColumnDef{Name: colNameVal, Type: common.TypeInt64}

	fillColumnValues(col, batch, colDef)

	if !col.IsNull(0) {
		t.Error("expected NULL when SetValue fails due to type mismatch with ColumnVector, got non-NULL")
	}
}

func TestFillColumnValues_NormalTypeMatch(t *testing.T) {
	col := storage.NewColumnVector(0, common.TypeInt64, 1)
	batch := []storage.ScanEntry{
		{Key: "a", Value: storage.Row{Columns: map[string]common.Value{
			colNameVal: common.NewInt64(99),
		}}},
	}
	colDef := ColumnDef{Name: colNameVal, Type: common.TypeInt64}

	fillColumnValues(col, batch, colDef)

	if col.IsNull(0) {
		t.Fatal("expected non-NULL value for matching types")
	}
	got := col.GetValue(0)
	if got.Int64 != 99 {
		t.Errorf("expected 99, got %d", got.Int64)
	}
}

// --- appendValueSafe tests ---

func TestAppendValueSafe_NormalAppendSucceeds(t *testing.T) {
	col := storage.NewColumnVector(0, common.TypeInt64, 1)
	appendValueSafe(col, common.NewInt64(42), common.TypeInt64)

	if col.Len() != 1 {
		t.Fatalf("expected len 1, got %d", col.Len())
	}
	got := col.GetValue(0)
	if got.Int64 != 42 {
		t.Errorf("expected 42, got %d", got.Int64)
	}
}

func TestAppendValueSafe_AppendFailsCoerceSucceeds(t *testing.T) {
	col := storage.NewColumnVector(0, common.TypeFloat64, 1)
	// Int64 value into Float64 column: Append fails, coerceValue succeeds
	appendValueSafe(col, common.NewInt64(7), common.TypeFloat64)

	if col.Len() != 1 {
		t.Fatalf("expected len 1, got %d", col.Len())
	}
	got := col.GetValue(0)
	if got.Typ != common.TypeFloat64 {
		t.Errorf("expected Float64 type, got %v", got.Typ)
	}
	if got.Float64 != 7.0 {
		t.Errorf("expected 7.0 after coercion, got %g", got.Float64)
	}
}

func TestAppendValueSafe_AppendFailsCoerceFailsNullAppended(t *testing.T) {
	col := storage.NewColumnVector(0, common.TypeInt64, 1)
	// String value into Int64 column: both Append and coerceValue fail, NULL appended
	appendValueSafe(col, common.NewString("hello"), common.TypeInt64)

	if col.Len() != 1 {
		t.Fatalf("expected len 1, got %d", col.Len())
	}
	if !col.IsNull(0) {
		t.Error("expected NULL value when both Append and coerceValue fail")
	}
}

// Note: The path where Append(common.NewNull()) fails is unreachable with the
// current ColumnVector implementation because SetValue for NULL always returns nil.
// The log.Printf in appendValueSafe is a defensive measure for future changes.

// --- buildChunksFromEntries tests ---

func TestBuildChunksFromEntriesStability_EmptyEntries(t *testing.T) {
	schema := []ColumnDef{{Name: "col", Type: common.TypeInt64}}
	chunks, err := buildChunksFromEntries(nil, schema, defaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunks != nil {
		t.Errorf("expected nil chunks for empty entries, got %v", chunks)
	}
}

func TestBuildChunksFromEntriesStability_EmptySchema(t *testing.T) {
	entries := []storage.ScanEntry{
		{Key: "a", Value: storage.Row{Columns: map[string]common.Value{"col": common.NewInt64(1)}}},
	}
	chunks, err := buildChunksFromEntries(entries, nil, defaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunks != nil {
		t.Errorf("expected nil chunks for empty schema, got %v", chunks)
	}
}

func TestBuildChunksFromEntriesStability_MultipleChunks(t *testing.T) {
	schema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
		{Name: "name", Type: common.TypeString},
	}

	const chunkSize = 3
	const totalEntries = 7
	entries := make([]storage.ScanEntry, totalEntries)
	for i := 0; i < totalEntries; i++ {
		entries[i] = storage.ScanEntry{
			Key: fmtKey(i),
			Value: storage.Row{Columns: map[string]common.Value{
				"id":   common.NewInt64(int64(i)),
				"name": common.NewString("name_" + fmtKey(i)),
			}},
		}
	}

	chunks, err := buildChunksFromEntries(entries, schema, chunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedChunks := (totalEntries + chunkSize - 1) / chunkSize
	if len(chunks) != expectedChunks {
		t.Fatalf("expected %d chunks, got %d", expectedChunks, len(chunks))
	}

	totalRows := countRows(chunks)
	if totalRows != totalEntries {
		t.Errorf("expected %d total rows, got %d", totalEntries, totalRows)
	}

	// Verify first chunk has chunkSize rows
	if chunks[0].RowCount() != chunkSize {
		t.Errorf("expected first chunk with %d rows, got %d", chunkSize, chunks[0].RowCount())
	}

	// Verify last chunk has remainder rows
	lastChunkRows := totalEntries - (expectedChunks-1)*chunkSize
	if chunks[len(chunks)-1].RowCount() != uint32(lastChunkRows) {
		t.Errorf("expected last chunk with %d rows, got %d", lastChunkRows, chunks[len(chunks)-1].RowCount())
	}
}

func TestBuildChunksFromEntriesStability_SingleChunk(t *testing.T) {
	schema := []ColumnDef{
		{Name: "id", Type: common.TypeInt64},
	}
	entries := []storage.ScanEntry{
		{Key: "a", Value: storage.Row{Columns: map[string]common.Value{"id": common.NewInt64(1)}}},
		{Key: "b", Value: storage.Row{Columns: map[string]common.Value{"id": common.NewInt64(2)}}},
	}

	chunks, err := buildChunksFromEntries(entries, schema, defaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].RowCount() != 2 {
		t.Errorf("expected 2 rows, got %d", chunks[0].RowCount())
	}
}
