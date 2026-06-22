package server

import (
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
	"github.com/what-is-me-vibe-coding/test-db/pkg/query"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// --- 主键等值快路径的端到端测试 ---

// TestSQLDeleteByPKFastPath 验证 DELETE WHERE pk = literal 命中点查快路径：
//   - 命中行被删除，影响行数为 1
//   - 未命中的行不受影响
//   - 删除后 SELECT 反映正确结果
func TestSQLDeleteByPKFastPath(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	// 点查快路径：DELETE WHERE id = 2
	resp := runSQL(t, srv, "DELETE FROM t WHERE id = 2")
	if resp.Rows != 1 {
		t.Errorf("DELETE 影响行数 = %d, 期望 1", resp.Rows)
	}

	// 验证只剩 id=1, 3
	resp = runSQL(t, srv, "SELECT id, v FROM t ORDER BY id")
	if resp.Rows != 2 {
		t.Fatalf("DELETE 后剩余 %d 行, 期望 2", resp.Rows)
	}
	rows := resp.Data.([]map[string]any)
	if rows[0]["id"].(int64) != 1 || rows[0]["v"].(string) != "a" {
		t.Errorf("id=1 的 v = %v, 期望 'a'", rows[0]["v"])
	}
	if rows[1]["id"].(int64) != 3 || rows[1]["v"].(string) != "c" {
		t.Errorf("id=3 的 v = %v, 期望 'c'", rows[1]["v"])
	}
}

// TestSQLDeleteByPKFastPathReverse 验证反向写法 (literal = pk_col) 同样命中快路径。
func TestSQLDeleteByPKFastPathReverse(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a'), (2, 'b')")

	// 反向等值：2 = id 同样应命中快路径
	resp := runSQL(t, srv, "DELETE FROM t WHERE 2 = id")
	if resp.Rows != 1 {
		t.Errorf("DELETE 影响行数 = %d, 期望 1", resp.Rows)
	}
}

// TestSQLDeleteByPKFastPathMiss 验证主键等值但目标行不存在时返回 0 行，
// 与历史「删除不存在行 = 影响 0 行」语义一致。
func TestSQLDeleteByPKFastPathMiss(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a')")

	resp := runSQL(t, srv, "DELETE FROM t WHERE id = 999")
	if resp.Rows != 0 {
		t.Errorf("DELETE 不存在主键的影响行数 = %d, 期望 0", resp.Rows)
	}

	// 验证原数据未受影响
	resp = runSQL(t, srv, "SELECT COUNT(*) FROM t")
	if resp.Rows != 1 {
		t.Errorf("未命中删除后行数 = %v, 期望 1", resp.Data)
	}
}

// TestSQLDeleteByCompositePKFastPath 验证复合主键等值 AND 命中快路径。
func TestSQLDeleteByCompositePKFastPath(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (a INT64, b INT64, v STRING, PRIMARY KEY (a, b))")
	runSQL(t, srv, "INSERT INTO t (a, b, v) VALUES (1, 1, 'x'), (1, 2, 'y'), (2, 1, 'z'), (2, 2, 'w')")

	// 复合主键 (a, b) = (1, 2)
	resp := runSQL(t, srv, "DELETE FROM t WHERE a = 1 AND b = 2")
	if resp.Rows != 1 {
		t.Errorf("DELETE 影响行数 = %d, 期望 1", resp.Rows)
	}

	// 反向 (b, a) 顺序书写也应命中（按表定义顺序构造 key）
	resp = runSQL(t, srv, "DELETE FROM t WHERE b = 1 AND a = 2")
	if resp.Rows != 1 {
		t.Errorf("反向顺序 DELETE 影响行数 = %d, 期望 1", resp.Rows)
	}

	resp = runSQL(t, srv, "SELECT COUNT(*) FROM t")
	if resp.Rows != 1 {
		t.Errorf("剩余行数 = %v, 期望 1", resp.Data)
	}
}

// TestSQLUpdateByPKFastPath 验证 UPDATE WHERE pk = literal 命中点查快路径。
func TestSQLUpdateByPKFastPath(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	resp := runSQL(t, srv, "UPDATE t SET v = 'updated' WHERE id = 2")
	if resp.Rows != 1 {
		t.Errorf("UPDATE 影响行数 = %d, 期望 1", resp.Rows)
	}

	if got := queryString(t, srv, "SELECT v FROM t WHERE id = 2", "v"); got != "updated" {
		t.Errorf("id=2 的 v = %q, 期望 'updated'", got)
	}
	if got := queryString(t, srv, "SELECT v FROM t WHERE id = 1", "v"); got != "a" {
		t.Errorf("id=1 的 v = %q, 期望 'a'（不应被更新）", got)
	}
	if got := queryString(t, srv, "SELECT v FROM t WHERE id = 3", "v"); got != "c" {
		t.Errorf("id=3 的 v = %q, 期望 'c'（不应被更新）", got)
	}
}

// TestSQLUpdateByPKFastPathMiss 验证主键等值 UPDATE 未命中时返回 0 行。
func TestSQLUpdateByPKFastPathMiss(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a')")

	resp := runSQL(t, srv, "UPDATE t SET v = 'new' WHERE id = 999")
	if resp.Rows != 0 {
		t.Errorf("UPDATE 不存在主键的影响行数 = %d, 期望 0", resp.Rows)
	}

	if got := queryString(t, srv, "SELECT v FROM t WHERE id = 1", "v"); got != "a" {
		t.Errorf("未命中后 id=1 的 v = %q, 期望 'a'", got)
	}
}

// TestSQLUpdateByPKFastPathWithPKChange 验证主键等值 UPDATE 变更主键时仍能正确执行：
//   - 旧行被删除、新行被写入
//   - 影响行数为 1
//   - 新主键可被查到且新值正确
func TestSQLUpdateByPKFastPathWithPKChange(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a'), (2, 'b')")

	resp := runSQL(t, srv, "UPDATE t SET id = 10, v = 'x' WHERE id = 1")
	if resp.Rows != 1 {
		t.Errorf("UPDATE 影响行数 = %d, 期望 1", resp.Rows)
	}

	// 旧主键 1 不应再存在
	if got := queryStringOrEmpty(t, srv, "SELECT v FROM t WHERE id = 1", "v"); got != "" {
		t.Errorf("id=1 仍存在 v=%q, 期望已被迁移", got)
	}
	// 新主键 10 应存在且 v='x'
	if got := queryString(t, srv, "SELECT v FROM t WHERE id = 10", "v"); got != "x" {
		t.Errorf("id=10 的 v = %q, 期望 'x'", got)
	}
}

// TestSQLDeleteByPKFastPathMemoryEngine 验证内存引擎表同样命中快路径。
func TestSQLDeleteByPKFastPathMemoryEngine(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE cache (id INT64, v STRING, PRIMARY KEY (id)) ENGINE=memory")
	runSQL(t, srv, "INSERT INTO cache (id, v) VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	resp := runSQL(t, srv, "DELETE FROM cache WHERE id = 2")
	if resp.Rows != 1 {
		t.Errorf("DELETE 影响行数 = %d, 期望 1", resp.Rows)
	}

	if got := queryString(t, srv, "SELECT v FROM cache WHERE id = 1", "v"); got != "a" {
		t.Errorf("id=1 的 v = %q, 期望 'a'", got)
	}
	if got := queryStringOrEmpty(t, srv, "SELECT v FROM cache WHERE id = 2", "v"); got != "" {
		t.Errorf("id=2 仍存在 v=%q", got)
	}
}

// TestSQLDeleteByPKFastPathNotTriggered 验证含非主键列的 WHERE 不命中快路径，
// 仍走段裁剪或全表扫描路径，结果与历史一致。
func TestSQLDeleteByPKFastPathNotTriggered(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, name STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, name) VALUES (1, 'alice'), (2, 'bob'), (3, 'alice')")

	// id=2 AND name='bob' → 含非主键列，回退到扫描路径
	resp := runSQL(t, srv, "DELETE FROM t WHERE id = 2 AND name = 'bob'")
	if resp.Rows != 1 {
		t.Errorf("DELETE 影响行数 = %d, 期望 1", resp.Rows)
	}

	if got := queryString(t, srv, "SELECT name FROM t WHERE id = 1", "name"); got != "alice" {
		t.Errorf("id=1 的 name = %q, 期望 'alice'", got)
	}
}

// TestSQLDeleteByPKFastPathPartialPKNotTriggered 验证部分主键覆盖的 WHERE 不命中快路径。
func TestSQLDeleteByPKFastPathPartialPKNotTriggered(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (a INT64, b INT64, v STRING, PRIMARY KEY (a, b))")
	runSQL(t, srv, "INSERT INTO t (a, b, v) VALUES (1, 1, 'x'), (1, 2, 'y')")

	// 仅 a = 1，未覆盖 b → 不能化简为点查，走段裁剪路径
	resp := runSQL(t, srv, "DELETE FROM t WHERE a = 1")
	if resp.Rows != 2 {
		t.Errorf("DELETE 影响行数 = %d, 期望 2", resp.Rows)
	}
}

// TestSQLDeleteByPKFastPathORNotTriggered 验证 OR 谓词不命中快路径。
func TestSQLDeleteByPKFastPathORNotTriggered(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	// OR 不参与 PK 等值化简，走 EvalRowPredicate 完整求值
	resp := runSQL(t, srv, "DELETE FROM t WHERE id = 1 OR id = 3")
	if resp.Rows != 2 {
		t.Errorf("DELETE 影响行数 = %d, 期望 2", resp.Rows)
	}
}

// TestSQLDeleteByPKFastPathRangeNotTriggered 验证范围谓词不命中快路径。
func TestSQLDeleteByPKFastPathRangeNotTriggered(t *testing.T) {
	srv := newTestServer(t)
	defer func() { _ = srv.Stop() }()

	runSQL(t, srv, "CREATE TABLE t (id INT64, v STRING, PRIMARY KEY (id))")
	runSQL(t, srv, "INSERT INTO t (id, v) VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	// id > 1 是范围谓词，走段裁剪路径
	resp := runSQL(t, srv, "DELETE FROM t WHERE id > 1")
	if resp.Rows != 2 {
		t.Errorf("DELETE 影响行数 = %d, 期望 2", resp.Rows)
	}
}

// --- 单元测试：tryBuildKeyFromPKEquality 与 helper ---

// TestTryBuildKeyFromPKEquality 覆盖 tryBuildKeyFromPKEquality 的判定路径。
func TestTryBuildKeyFromPKEquality(t *testing.T) {
	tbl := &catalog.Table{
		Name: "t",
		Columns: []catalog.ColumnDef{
			{Name: "id", Type: common.TypeInt64},
			{Name: "uid", Type: common.TypeInt64},
		},
		PrimaryKey: []string{"id"},
	}

	tests := []struct {
		name    string
		where   query.Expression
		wantKey string
		wantOK  bool
	}{
		{
			name:    "single PK equality",
			where:   eqExpr(colExpr("id"), litInt(42)),
			wantKey: "42",
			wantOK:  true,
		},
		{
			name:    "reverse literal = column",
			where:   eqExpr(litInt(7), colExpr("id")),
			wantKey: "7",
			wantOK:  true,
		},
		{
			name:    "non-PK column → false",
			where:   eqExpr(colExpr("uid"), litInt(1)),
			wantKey: "",
			wantOK:  false,
		},
		{
			name:    "range predicate → false",
			where:   &query.BinaryExpr{Op: query.OpGt, Left: colExpr("id"), Right: litInt(5)},
			wantKey: "",
			wantOK:  false,
		},
		{
			name:    "OR predicate → false",
			where:   &query.BinaryExpr{Op: query.OpOr, Left: eqExpr(colExpr("id"), litInt(1)), Right: eqExpr(colExpr("id"), litInt(2))},
			wantKey: "",
			wantOK:  false,
		},
		{
			name:    "AND with non-PK column → false",
			where:   &query.BinaryExpr{Op: query.OpAnd, Left: eqExpr(colExpr("id"), litInt(1)), Right: eqExpr(colExpr("uid"), litInt(2))},
			wantKey: "",
			wantOK:  false,
		},
		{
			name:    "nil WHERE → false",
			where:   nil,
			wantKey: "",
			wantOK:  false,
		},
		{
			name:    "duplicate PK column → false",
			where:   &query.BinaryExpr{Op: query.OpAnd, Left: eqExpr(colExpr("id"), litInt(1)), Right: eqExpr(colExpr("id"), litInt(1))},
			wantKey: "",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotOK := tryBuildKeyFromPKEquality(tt.where, tbl)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotKey != tt.wantKey {
				t.Errorf("key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

// TestTryBuildKeyFromPKEqualityComposite 验证复合主键的多列等值 AND 命中快路径。
func TestTryBuildKeyFromPKEqualityComposite(t *testing.T) {
	tbl := &catalog.Table{
		Name: "t",
		Columns: []catalog.ColumnDef{
			{Name: "a", Type: common.TypeInt64},
			{Name: "b", Type: common.TypeInt64},
			{Name: "c", Type: common.TypeInt64},
		},
		PrimaryKey: []string{"a", "b"},
	}

	// a=1 AND b=2 顺序与 PK 定义一致
	where := &query.BinaryExpr{Op: query.OpAnd, Left: eqExpr(colExpr("a"), litInt(1)), Right: eqExpr(colExpr("b"), litInt(2))}
	gotKey, ok := tryBuildKeyFromPKEquality(where, tbl)
	if !ok {
		t.Fatalf("期望命中快路径，但 ok=false")
	}
	if gotKey != "1\x002" {
		t.Errorf("key = %q, want %q", gotKey, "1\\x002")
	}

	// 反向书写 b=2 AND a=1，应按 PK 顺序构造 key
	where = &query.BinaryExpr{Op: query.OpAnd, Left: eqExpr(colExpr("b"), litInt(2)), Right: eqExpr(colExpr("a"), litInt(1))}
	gotKey, ok = tryBuildKeyFromPKEquality(where, tbl)
	if !ok {
		t.Fatalf("反向书写期望命中快路径")
	}
	if gotKey != "1\x002" {
		t.Errorf("反向 key = %q, want %q", gotKey, "1\\x002")
	}
}

// TestTryBuildKeyFromPKEqualityNoPrimaryKey 验证无主键表返回 false。
func TestTryBuildKeyFromPKEqualityNoPrimaryKey(t *testing.T) {
	tbl := &catalog.Table{Name: "t"}
	_, ok := tryBuildKeyFromPKEquality(eqExpr(colExpr("id"), litInt(1)), tbl)
	if ok {
		t.Errorf("无主键表应返回 ok=false")
	}
}

// TestExtractEqColumnLiteral 验证 extractEqColumnLiteral 在两种形式与无效输入下的行为。
func TestExtractEqColumnLiteral(t *testing.T) {
	tests := []struct {
		name      string
		bin       *query.BinaryExpr
		wantName  string
		wantValid bool
	}{
		{
			name:      "column = literal",
			bin:       eqExpr(colExpr("id"), litInt(1)),
			wantName:  "id",
			wantValid: true,
		},
		{
			name:      "literal = column",
			bin:       eqExpr(litInt(2), colExpr("id")),
			wantName:  "id",
			wantValid: true,
		},
		{
			name:      "column = column → false",
			bin:       eqExpr(colExpr("a"), colExpr("b")),
			wantName:  "",
			wantValid: false,
		},
		{
			name:      "NULL literal → false",
			bin:       eqExpr(colExpr("id"), &query.LiteralExpr{Value: common.NewNull()}),
			wantName:  "",
			wantValid: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, _, gotOK := extractEqColumnLiteral(tt.bin)
			if gotOK != tt.wantValid {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantValid)
			}
			if gotName != tt.wantName {
				t.Errorf("name = %q, want %q", gotName, tt.wantName)
			}
		})
	}
}

// fakeEngineNoGet 实现 TableEngine 但未实现 Get，验证 tryEngineGetter
// 在引擎缺失 Get 能力时返回 nil，让调用方安全回退到扫描路径。
type fakeEngineNoGet struct{}

func (fakeEngineNoGet) Write(_ string, _ map[string]common.Value) error { return nil }
func (fakeEngineNoGet) WriteBatch(_ []storage.WriteRow) error           { return nil }
func (fakeEngineNoGet) Delete(_ string) error                           { return nil }
func (fakeEngineNoGet) ScanRange(_, _ string) []storage.ScanEntry       { return nil }
func (fakeEngineNoGet) ScanRangeWithPruning(_ string, _ string, _ []storage.ColumnPredicate) []storage.ScanEntry {
	return nil
}
func (fakeEngineNoGet) ColumnMeta() []storage.ColumnMeta  { return nil }
func (fakeEngineNoGet) PrimaryIndex() *index.PrimaryIndex { return nil }
func (fakeEngineNoGet) SparseIndex() *index.SparseIndex   { return nil }
func (fakeEngineNoGet) Close() error                      { return nil }

// TestTryEngineGetterNotSupported 验证 tryEngineGetter 对不实现 Get 的
// TableEngine 返回 nil，调用方应回退到 ScanRange 路径。
func TestTryEngineGetterNotSupported(t *testing.T) {
	if got := tryEngineGetter(fakeEngineNoGet{}); got != nil {
		t.Errorf("tryEngineGetter(fakeEngineNoGet) = %v, want nil", got)
	}
}

// TestCheckPKConflictNoGet 验证 checkPKConflict 在引擎无 Get 能力时直接返回 nil。
func TestCheckPKConflictNoGet(t *testing.T) {
	if err := checkPKConflict(fakeEngineNoGet{}, "any-key"); err != nil {
		t.Errorf("checkPKConflict(fakeEngineNoGet) = %v, want nil", err)
	}
}

// --- 测试辅助 ---

// eqExpr 构造等值比较表达式。
func eqExpr(left, right query.Expression) *query.BinaryExpr {
	return &query.BinaryExpr{Op: query.OpEq, Left: left, Right: right}
}

// colExpr 构造未分析的列引用。
func colExpr(name string) *query.ColumnExpr {
	return &query.ColumnExpr{Name: name}
}

// litInt 构造 INT64 字面量。
func litInt(v int64) *query.LiteralExpr {
	return &query.LiteralExpr{Value: common.NewInt64(v)}
}

// queryStringOrEmpty 与 queryString 类似，但允许结果集为空（用于验证行已被删除）。
func queryStringOrEmpty(t *testing.T, srv *Server, sql, col string) string {
	t.Helper()
	resp, err := srv.handleQuery(&QueryRequest{SQL: sql})
	if err != nil {
		t.Fatalf("执行 SQL %q 出错: %v", sql, err)
	}
	if resp.Code != 0 {
		t.Fatalf("执行 SQL %q 失败: %s", sql, resp.Message)
	}
	rows, ok := resp.Data.([]map[string]any)
	if !ok || len(rows) == 0 {
		return ""
	}
	v, ok := rows[0][col].(string)
	if !ok {
		t.Fatalf("SQL %q 列 %s = %v（类型 %T），期望 string", sql, col, rows[0][col], rows[0][col])
	}
	return v
}
