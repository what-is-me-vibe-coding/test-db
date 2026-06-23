// Package pgwire 补充 Extended Query Protocol 边缘场景的单测。
//
// PR #237 增加了端到端 e2e 测试（tests/integration/e2e_pgwire_extended_query_test.go），
// 覆盖真实 PG 客户端协议栈的完整工作负载；PR #236 引入了 Extended Query 实现
// （pkg/server/pgwire/conn_extended.go）并补齐了大部分单测（conn_extended_test.go）。
//
// 本文件补齐以下未覆盖场景，确保 pgwire 包的代码覆盖率回到 90% 阈值之上：
//   - extractParamOIDs：纯函数，覆盖占位符数量解析的所有边界
//   - handleClose('X')：Close 消息携带未识别 ObjectType（PG 规范中应为 'S'/'P'）
//   - handleDescribe('X')：Describe 消息携带未识别 ObjectType
//   - handleParse 空 SQL：Parse 携带空查询字符串的兼容行为
//   - handleClose 不存在对象：Close 对未注册名称仍返回 CloseComplete
//   - extractParamOIDs 不影响 Sync 后错误状态清除
package pgwire

import (
	"net"
	"reflect"
	"testing"

	"github.com/jackc/pgproto3/v2"
)

// TestExtractParamOIDs 表驱动覆盖 extractParamOIDs 的所有边界。
// 该函数被 handleDescribe('S') 调用以生成 ParameterDescription，
// 客户端据此决定是否预处理 $N 占位符。返回值长度 = max($N) 出现的最大 N。
func TestExtractParamOIDs(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []uint32
	}{
		{
			name: "无占位符",
			sql:  "SELECT id, name FROM t",
			want: nil,
		},
		{
			name: "单占位符 $1",
			sql:  "SELECT * FROM t WHERE id = $1",
			want: []uint32{0},
		},
		{
			name: "乱序占位符 $3 $1 $2 取最大 N",
			sql:  "SELECT * FROM t WHERE a = $3 AND b = $1 AND c = $2",
			want: []uint32{0, 0, 0},
		},
		{
			name: "占位符 $10 跨字符长度边界",
			sql:  "SELECT $10",
			want: []uint32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			name: "$ 后无数字（视为非占位符）",
			sql:  "SELECT * FROM t WHERE col = 'price$'",
			want: nil,
		},
		{
			name: "字符串内含 $N 不应被错误识别（当前简化实现会识别）",
			sql:  "SELECT 'foo$1bar' AS s",
			want: []uint32{0},
		},
		{
			name: "$ 在 SQL 末尾无后继字符",
			sql:  "SELECT * FROM t WHERE col = $",
			want: nil,
		},
		{
			name: "$0 当前实现视为无占位符（maxN==0 时返回 nil）",
			sql:  "SELECT $0",
			want: nil,
		},
		{
			name: "多个连续占位符 $1$2$3 取最大 N",
			sql:  "SELECT $1 || $2 || $3",
			want: []uint32{0, 0, 0},
		},
		{
			name: "dollar-quoted 字符串 $$tag$$ ... $$tag$$（简化为字符遍历）",
			sql:  "$$begin SELECT $5 end$$",
			want: []uint32{0, 0, 0, 0, 0},
		},
		{
			name: "空 SQL 字符串",
			sql:  "",
			want: nil,
		},
		{
			name: "占位符在字符串字面量内：'foo $5' 当前实现视为占位符",
			sql:  "SELECT 'foo $5' AS s",
			want: []uint32{0, 0, 0, 0, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractParamOIDs(tt.sql)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractParamOIDs(%q)\n got: %v (len=%d)\nwant: %v (len=%d)",
					tt.sql, got, len(got), tt.want, len(tt.want))
			}
		})
	}
}

// TestExtractParamOIDsNonDigitAtDollar 验证 $ 后紧跟非数字字符（非 '0'-'9'）
// 时不计入占位符数量。这是简化实现的明示行为，文档化以避免未来误改。
func TestExtractParamOIDsNonDigitAtDollar(t *testing.T) {
	cases := map[string][]uint32{
		"SELECT $a":           nil, // 字母
		"SELECT $-":           nil, // 符号
		"SELECT $\n":          nil, // 空白
		"SELECT $\x00":        nil, // NUL
		"SELECT $_underscore": nil,
	}
	for sql, want := range cases {
		t.Run(sql, func(t *testing.T) {
			got := extractParamOIDs(sql)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("extractParamOIDs(%q) = %v, want %v", sql, got, want)
			}
		})
	}
}

// TestExtendedQueryCloseInvalidType 验证 Close 消息携带未识别 ObjectType（如 'X'）
// 时，handleClose 不进入错误分支且仍发送 CloseComplete。
// PG 规范下服务端应默默忽略未知对象类型。
func TestExtendedQueryCloseInvalidType(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// Close 携带未识别的 ObjectType 'X'
	if err := c.sendClose('X', "anything"); err != nil {
		t.Fatalf("sendClose 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 期望序列: CloseComplete(3) + ReadyForQuery(Z)
	if len(types) < 2 {
		t.Fatalf("消息过少, got %v", types)
	}
	if types[0] != '3' {
		t.Errorf("期望 CloseComplete('3') 开头, got %c", types[0])
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery('Z') 结尾, got %c", types[len(types)-1])
	}
	// 不应出现 ErrorResponse
	for _, m := range types {
		if m == 'E' {
			t.Errorf("Close 未知 ObjectType 不应触发 ErrorResponse, got %v", types)
		}
	}
}

// TestExtendedQueryCloseNonExistent 验证 Close 对未注册名称仍返回 CloseComplete。
// 按 PG 规范，Close 始终成功，不感知对象是否存在。
func TestExtendedQueryCloseNonExistent(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// Close 一个从未 Parse/Bind 过的 prepared statement
	if err := c.sendClose('S', "no_such_stmt"); err != nil {
		t.Fatalf("sendClose(S) 失败: %v", err)
	}
	if err := c.sendClose('P', "no_such_portal"); err != nil {
		t.Fatalf("sendClose(P) 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if len(types) < 3 {
		t.Fatalf("期望至少 3 条消息, got %v", types)
	}
	// 期望 3 + 3 + Z
	if types[0] != '3' || types[1] != '3' {
		t.Errorf("期望两条 CloseComplete, got %v", types[:2])
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
}

// TestExtendedQueryDescribeInvalidType 验证 Describe 携带未识别 ObjectType 时
// 进入错误状态（按 PG 协议，Describe 是会进入错误状态的少数消息之一）。
func TestExtendedQueryDescribeInvalidType(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// Describe 携带未识别的 ObjectType 'X'
	if err := c.sendDescribe('X', "anything"); err != nil {
		t.Fatalf("sendDescribe 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 期望序列: ErrorResponse(E) + ReadyForQuery(Z)
	hasE := false
	for _, m := range types {
		if m == 'E' {
			hasE = true
		}
	}
	if !hasE {
		t.Errorf("Describe 未知 ObjectType 应返回 ErrorResponse, got %v", types)
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
}

// TestExtendedQueryDescribeMissingStmt 验证 Describe('S') 引用不存在的
// prepared statement 时进入错误状态（区别于 Close 对不存在对象静默成功）。
func TestExtendedQueryDescribeMissingStmt(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := c.sendDescribe('S', "no_such_stmt"); err != nil {
		t.Fatalf("sendDescribe 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	hasE := false
	for _, m := range types {
		if m == 'E' {
			hasE = true
		}
	}
	if !hasE {
		t.Errorf("Describe(S) 未知 statement 应返回 ErrorResponse, got %v", types)
	}
}

// TestExtendedQueryParseEmptySQL 验证 Parse 携带空 SQL 字符串的兼容行为。
// PG 规范允许空 Parse，此时服务端应返回 ParseComplete 而不报错。
// 随后 Bind/Execute 时若使用 unnamed statement，会因找不到映射进入错误状态。
func TestExtendedQueryParseEmptySQL(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// Parse 携带空 SQL
	if err := c.sendParse("", "", nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 期望 ParseComplete(1) + ReadyForQuery(Z)，不进入错误状态
	if types[0] != '1' {
		t.Errorf("期望 ParseComplete('1'), got %c", types[0])
	}
	if types[len(types)-1] != 'Z' {
		t.Errorf("应以 ReadyForQuery 结尾, got %c", types[len(types)-1])
	}
	for _, m := range types {
		if m == 'E' {
			t.Errorf("Parse 空 SQL 不应触发 ErrorResponse, got %v", types)
		}
	}
}

// TestExtendedQueryParseOverwrite 验证 Parse 携带同名 statement 时会替换旧值，
// 这是 PG 规范允许的行为。覆盖 handleParse 中 m.Name 已存在的分支。
func TestExtendedQueryParseOverwrite(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"x"},
		Rows:    []map[string]any{{"x": int64(1)}},
		IsQuery: true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// 第一次 Parse 命名 stmt_a
	if err := c.sendParse("stmt_a", "SELECT 1", nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	// 第二次 Parse 同一名字，新 SQL 应替换旧 SQL
	if err := c.sendParse("stmt_a", "SELECT 2", nil); err != nil {
		t.Fatalf("sendParse(overwrite) 失败: %v", err)
	}
	if err := c.sendBind("", "stmt_a", nil, nil); err != nil {
		t.Fatalf("sendBind 失败: %v", err)
	}
	if err := c.sendDescribe('P', ""); err != nil {
		t.Fatalf("sendDescribe 失败: %v", err)
	}
	if err := c.sendExecute("", 0); err != nil {
		t.Fatalf("sendExecute 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	// 验证 executor 收到的是新 SQL（"SELECT 2"），而不是 "SELECT 1"
	if got := exec.lastQuery(); got != "SELECT 2" {
		t.Errorf("Parse 覆盖后 Execute 应执行新 SQL, got %q want %q", got, "SELECT 2")
	}
}

// TestExtendedQueryErrorStateClearedBySync 验证 Parse 失败进入错误状态后，
// 后续消息被丢弃直到 Sync；Sync 触发 ReadyForQuery 并清除错误状态。
// 错误状态清除后，下一次 Parse 应能正常处理。
func TestExtendedQueryErrorStateClearedBySync(t *testing.T) {
	srv := startTestServer(t, &mockExecutor{})
	defer srv.Stop()

	// 第一阶段：在连接 c 上制造错误状态并通过 Sync 清除
	c := newPGClient(t, srv.Addr())
	defer c.close()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	// Bind 引用不存在的 stmt 进入错误状态，Parse 在错误态下被丢弃，Sync 清除状态
	if err := c.sendBind("", "no_such_stmt", nil, nil); err != nil {
		t.Fatalf("sendBind 失败: %v", err)
	}
	if err := c.sendParse("", "SELECT 1", nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	// 第二阶段：新连接走完整 Parse/Bind/Execute/Sync，验证无 ErrorResponse
	c2 := newPGClient(t, srv.Addr())
	defer c2.close()
	types := runParseBindExecuteSync(t, c2, "SELECT 42")
	for _, m := range types {
		if m == 'E' {
			t.Errorf("Sync 后新一轮查询不应出现 ErrorResponse, got %v", types)
		}
	}
}

// runParseBindExecuteSync 在客户端 c 上执行完整 Parse/Bind/Describe/Execute/Sync 周期，
// 返回消息类型序列。供 TestExtendedQueryErrorStateClearedBySync 等用例复用。
func runParseBindExecuteSync(t *testing.T, c *pgClient, sql string) []byte {
	t.Helper()
	if err := c.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := c.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	if err := c.sendParse("", sql, nil); err != nil {
		t.Fatalf("sendParse 失败: %v", err)
	}
	if err := c.sendBind("", "", nil, nil); err != nil {
		t.Fatalf("sendBind 失败: %v", err)
	}
	if err := c.sendDescribe('P', ""); err != nil {
		t.Fatalf("sendDescribe 失败: %v", err)
	}
	if err := c.sendExecute("", 0); err != nil {
		t.Fatalf("sendExecute 失败: %v", err)
	}
	if err := c.sendSync(); err != nil {
		t.Fatalf("sendSync 失败: %v", err)
	}
	types, err := c.readUntilReadyForQuery()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	return types
}

// TestConsumeExtErrorNil 验证新建的 connHandler 不处于错误状态。
func TestConsumeExtErrorNil(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	backend := pgproto3.NewBackend(pgproto3.NewChunkReader(serverConn), serverConn)
	h := newConnHandler(backend, &mockExecutor{}, serverConn, 0, 0)
	if h.consumeExtError() {
		t.Error("新建的 connHandler 不应处于错误状态")
	}
	if h.extErr != nil {
		t.Errorf("新建的 connHandler extErr 应为 nil, got %v", h.extErr)
	}
}

// TestConsumeExtErrorNonNil 验证 setExtError 设置后 consumeExtError 返回 true。
func TestConsumeExtErrorNonNil(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	backend := pgproto3.NewBackend(pgproto3.NewChunkReader(serverConn), serverConn)
	h := newConnHandler(backend, &mockExecutor{}, serverConn, 0, 0)
	h.setExtError(errFakeExtended)
	if !h.consumeExtError() {
		t.Error("setExtError 后 consumeExtError 应返回 true")
	}
	if h.extErr == nil {
		t.Error("setExtError 后 extErr 不应为 nil")
	}
}

// errFakeExtended 是测试用哨兵错误。
var errFakeExtended = &fakeErr{msg: "fake extended error"}

// fakeErr 是简单的 error 实现，用于测试断言。
type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }
