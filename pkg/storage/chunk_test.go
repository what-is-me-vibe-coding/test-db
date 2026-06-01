package storage

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

func TestNewChunk(t *testing.T) {
	c := NewChunk(0)
	if c.Capacity() != defaultColumnCapacity {
		t.Errorf("default capacity = %d, want %d", c.Capacity(), defaultColumnCapacity)
	}
	if c.RowCount() != 0 {
		t.Errorf("RowCount = %d, want 0", c.RowCount())
	}
	if c.ColumnCount() != 0 {
		t.Errorf("ColumnCount = %d, want 0", c.ColumnCount())
	}

	c2 := NewChunk(512)
	if c2.Capacity() != 512 {
		t.Errorf("capacity = %d, want 512", c2.Capacity())
	}
}

func TestChunkAddColumn(t *testing.T) {
	c := NewChunk(8)
	col1 := NewColumnVector(0, common.TypeInt64, 8)
	col2 := NewColumnVector(1, common.TypeString, 8)

	if err := c.AddColumn(col1); err != nil {
		t.Fatalf("AddColumn failed: %v", err)
	}
	if c.ColumnCount() != 1 {
		t.Errorf("ColumnCount = %d, want 1", c.ColumnCount())
	}

	if err := c.AddColumn(col2); err != nil {
		t.Fatalf("AddColumn failed: %v", err)
	}
	if c.ColumnCount() != 2 {
		t.Errorf("ColumnCount = %d, want 2", c.ColumnCount())
	}
}

func TestChunkAddColumnLengthMismatch(t *testing.T) {
	c := NewChunk(8)
	col1 := NewColumnVector(0, common.TypeInt64, 8)
	_ = col1.Append(common.NewInt64(1))
	_ = col1.Append(common.NewInt64(2))

	if err := c.AddColumn(col1); err != nil {
		t.Fatalf("AddColumn failed: %v", err)
	}

	col2 := NewColumnVector(1, common.TypeInt64, 8)
	_ = col2.Append(common.NewInt64(3))

	if err := c.AddColumn(col2); err == nil {
		t.Fatal("expected error for column length mismatch")
	}
}

func TestChunkAppendRow(t *testing.T) {
	c := NewChunk(8)
	col1 := NewColumnVector(0, common.TypeInt64, 8)
	col2 := NewColumnVector(1, common.TypeString, 8)
	_ = c.AddColumn(col1)
	_ = c.AddColumn(col2)

	for i := 0; i < 5; i++ {
		err := c.AppendRow([]common.Value{
			common.NewInt64(int64(i)),
			common.NewString("row"),
		})
		if err != nil {
			t.Fatalf("AppendRow %d failed: %v", i, err)
		}
	}

	if c.RowCount() != 5 {
		t.Errorf("RowCount = %d, want 5", c.RowCount())
	}

	for i := uint32(0); i < 5; i++ {
		row, err := c.GetRow(i)
		if err != nil {
			t.Fatalf("GetRow %d failed: %v", i, err)
		}
		if row[0].Int64 != int64(i) {
			t.Errorf("row %d col0 = %d, want %d", i, row[0].Int64, int64(i))
		}
	}
}

func TestChunkAppendRowMismatch(t *testing.T) {
	c := NewChunk(8)
	_ = c.AddColumn(NewColumnVector(0, common.TypeInt64, 8))
	_ = c.AddColumn(NewColumnVector(1, common.TypeInt64, 8))

	err := c.AppendRow([]common.Value{common.NewInt64(1)})
	if err == nil {
		t.Fatal("expected error for value count mismatch")
	}
}

func TestChunkAppendRowTypeMismatch(t *testing.T) {
	c := NewChunk(8)
	_ = c.AddColumn(NewColumnVector(0, common.TypeInt64, 8))

	err := c.AppendRow([]common.Value{common.NewString("not int")})
	if err == nil {
		t.Fatal("expected error for type mismatch in AppendRow")
	}
}

func TestChunkGetColumn(t *testing.T) {
	c := NewChunk(8)
	col := NewColumnVector(0, common.TypeInt64, 8)
	_ = c.AddColumn(col)

	got, err := c.GetColumn(0)
	if err != nil {
		t.Fatalf("GetColumn failed: %v", err)
	}
	if got.ColumnID != 0 {
		t.Errorf("ColumnID = %d, want 0", got.ColumnID)
	}

	_, err = c.GetColumn(1)
	if err == nil {
		t.Fatal("expected error for out-of-range column index")
	}
}

func TestChunkReset(t *testing.T) {
	c := NewChunk(8)
	col := NewColumnVector(0, common.TypeInt64, 8)
	_ = c.AddColumn(col)

	for i := 0; i < 3; i++ {
		_ = c.AppendRow([]common.Value{common.NewInt64(int64(i))})
	}

	c.Reset()
	if c.RowCount() != 0 {
		t.Errorf("RowCount after Reset = %d, want 0", c.RowCount())
	}
}

func TestChunkClear(t *testing.T) {
	c := NewChunk(8)
	col := NewColumnVector(0, common.TypeInt64, 8)
	_ = c.AddColumn(col)
	_ = c.AppendRow([]common.Value{common.NewInt64(1)})

	c.Clear()
	if c.ColumnCount() != 0 {
		t.Errorf("ColumnCount after Clear = %d, want 0", c.ColumnCount())
	}
	if c.RowCount() != 0 {
		t.Errorf("RowCount after Clear = %d, want 0", c.RowCount())
	}
}

func TestChunkGetRowOutOfRange(t *testing.T) {
	c := NewChunk(8)
	_ = c.AddColumn(NewColumnVector(0, common.TypeInt64, 8))

	_, err := c.GetRow(0)
	if err == nil {
		t.Fatal("expected error for out-of-range row")
	}
}

func TestChunkColumns(t *testing.T) {
	c := NewChunk(8)
	col1 := NewColumnVector(0, common.TypeInt64, 8)
	col2 := NewColumnVector(1, common.TypeString, 8)
	_ = c.AddColumn(col1)
	_ = c.AddColumn(col2)

	cols := c.Columns()
	if len(cols) != 2 {
		t.Errorf("Columns count = %d, want 2", len(cols))
	}
}
