package catalog

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

const tableUsers = "users"
const colName = "name"

func TestNewDatabase(t *testing.T) {
	db := NewDatabase()
	if db == nil {
		t.Fatal("NewDatabase() returned nil")
	}
	if db.Version != 1 {
		t.Errorf("Version = %d, want 1", db.Version)
	}
	if db.Tables == nil {
		t.Error("Tables map is nil")
	}
	if len(db.Tables) != 0 {
		t.Errorf("len(Tables) = %d, want 0", len(db.Tables))
	}
}

func TestDatabaseGetTable(t *testing.T) {
	db := NewDatabase()
	db.Tables[tableUsers] = &Table{Name: tableUsers}

	_, err := db.GetTable(tableUsers)
	if err != nil {
		t.Errorf("GetTable(users) error = %v", err)
	}

	_, err = db.GetTable("notexist")
	if err != common.ErrTableNotExist {
		t.Errorf("GetTable(notexist) error = %v, want ErrTableNotExist", err)
	}
}

func TestTableColumnIndex(t *testing.T) {
	tbl := &Table{
		Name: tableUsers,
		Columns: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: colName, Type: common.TypeString},
			{Name: "age", Type: common.TypeInt64},
		},
	}

	idx, err := tbl.ColumnIndex(colName)
	if err != nil || idx != 1 {
		t.Errorf("ColumnIndex(name) = %d, %v, want 1, nil", idx, err)
	}

	_, err = tbl.ColumnIndex("notexist")
	if err != common.ErrColumnNotExist {
		t.Errorf("ColumnIndex(notexist) error = %v, want ErrColumnNotExist", err)
	}
}

func TestTableGetColumn(t *testing.T) {
	tbl := &Table{
		Name: tableUsers,
		Columns: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: colName, Type: common.TypeString},
		},
	}

	col, err := tbl.GetColumn(colName)
	if err != nil || col.Name != colName || col.Type != common.TypeString {
		t.Errorf("GetColumn(name) = %+v, %v", col, err)
	}

	_, err = tbl.GetColumn("notexist")
	if err != common.ErrColumnNotExist {
		t.Errorf("GetColumn(notexist) error = %v, want ErrColumnNotExist", err)
	}
}

func TestTableHasColumn(t *testing.T) {
	tbl := &Table{
		Name: tableUsers,
		Columns: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
		},
	}

	if !tbl.HasColumn("id") {
		t.Error("HasColumn(id) = false, want true")
	}
	if tbl.HasColumn("notexist") {
		t.Error("HasColumn(notexist) = true, want false")
	}
}

func TestTableColTypeMap(t *testing.T) {
	tbl := &Table{
		Name: tableUsers,
		Columns: []ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: colName, Type: common.TypeString},
			{Name: "score", Type: common.TypeFloat64},
		},
	}

	// 首次调用应创建映射
	m := tbl.ColTypeMap()
	if len(m) != 3 {
		t.Errorf("ColTypeMap length = %d, want 3", len(m))
	}
	if m["id"] != common.TypeInt64 {
		t.Errorf("ColTypeMap[id] = %v, want TypeInt64", m["id"])
	}
	if m[colName] != common.TypeString {
		t.Errorf("ColTypeMap[name] = %v, want TypeString", m[colName])
	}
	if m["score"] != common.TypeFloat64 {
		t.Errorf("ColTypeMap[score] = %v, want TypeFloat64", m["score"])
	}

	// 再次调用应返回缓存的同一映射
	m2 := tbl.ColTypeMap()
	if len(m2) != 3 {
		t.Errorf("ColTypeMap second call length = %d, want 3", len(m2))
	}
}
