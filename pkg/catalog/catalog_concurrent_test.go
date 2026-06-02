package catalog

import (
	"fmt"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

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
