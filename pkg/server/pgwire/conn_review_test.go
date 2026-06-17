package pgwire

import (
	"strings"
	"testing"
	"time"
)

// readCommandTag 读取消息直到 CommandComplete('C')，返回其命令标签。
func (c *pgClient) readCommandTag() (string, error) {
	for {
		mt, body, err := c.readMessage()
		if err != nil {
			return "", err
		}
		if mt == 'C' { // CommandComplete
			return strings.TrimRight(string(body), "\x00"), nil
		}
		if mt == 'Z' { // ReadyForQuery（未遇到 CommandComplete）
			return "", nil
		}
	}
}

// TestConnCommandTagForReturning 验证带结果集的写操作（如 INSERT...RETURNING）
// 返回正确的 CommandTag，而非被覆盖为 "SELECT N"（修复 review #1）。
func TestConnCommandTagForReturning(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns: []string{"id"},
		Rows: []map[string]any{
			{"id": int64(1)},
			{"id": int64(2)},
			{"id": int64(3)},
		},
		RowsAffected: 3,
		CommandTag:   "INSERT 0 3",
		IsQuery:      true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("INSERT INTO t VALUES (1) RETURNING id"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	tag, err := client.readCommandTag()
	if err != nil {
		t.Fatalf("读取 CommandComplete 失败: %v", err)
	}
	if tag != "INSERT 0 3" {
		t.Errorf("CommandTag 期望 INSERT 0 3，得到 %q（不应被覆盖为 SELECT 3）", tag)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("读取 ReadyForQuery 失败: %v", err)
	}
}

// TestConnCommandTagForSelect 验证普通 SELECT 仍返回 "SELECT N" 标签。
func TestConnCommandTagForSelect(t *testing.T) {
	exec := &mockExecutor{result: &SQLResult{
		Columns:    []string{"v"},
		Rows:       []map[string]any{{"v": int64(1)}, {"v": int64(2)}},
		CommandTag: "SELECT 2",
		IsQuery:    true,
	}}
	srv := startTestServer(t, exec)
	defer srv.Stop()

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	if err := client.sendQuery("SELECT * FROM t"); err != nil {
		t.Fatalf("sendQuery 失败: %v", err)
	}
	tag, err := client.readCommandTag()
	if err != nil {
		t.Fatalf("读取 CommandComplete 失败: %v", err)
	}
	if tag != "SELECT 2" {
		t.Errorf("CommandTag 期望 SELECT 2，得到 %q", tag)
	}
}

// TestServerMaxConnsLimit 验证达到最大连接数时拒绝新连接（修复 review #2）。
func TestServerMaxConnsLimit(t *testing.T) {
	exec := &mockExecutor{}
	srv := NewServer("127.0.0.1:0", exec, WithMaxConns(2))
	if err := srv.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer srv.Stop()
	time.Sleep(20 * time.Millisecond)

	// 占用 2 个连接槽（完成握手后进入 queryLoop 等待，持续占用槽位）
	holders := make([]*pgClient, 0, 2)
	for i := 0; i < 2; i++ {
		c := newPGClient(t, srv.Addr())
		if err := c.sendStartupMessage(); err != nil {
			t.Fatalf("holder %d startup: %v", i, err)
		}
		if _, err := c.readUntilReadyForQuery(); err != nil {
			t.Fatalf("holder %d handshake: %v", i, err)
		}
		holders = append(holders, c)
	}
	defer func() {
		for _, c := range holders {
			c.close()
		}
	}()

	// 第 3 个连接应被拒绝：服务端立即关闭，读取返回 EOF/错误
	rejected := newPGClient(t, srv.Addr())
	defer rejected.close()
	rejected.conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := rejected.readMessage(); err == nil {
		t.Error("达到最大连接数时新连接应被拒绝（读取应失败）")
	}
}

// TestServerIdleTimeout 验证空闲连接在超时后被关闭（修复 review #2）。
func TestServerIdleTimeout(t *testing.T) {
	exec := &mockExecutor{}
	srv := NewServer("127.0.0.1:0", exec, WithIdleTimeout(150*time.Millisecond))
	if err := srv.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer srv.Stop()
	time.Sleep(20 * time.Millisecond)

	client := newPGClient(t, srv.Addr())
	defer client.close()
	if err := client.sendStartupMessage(); err != nil {
		t.Fatalf("sendStartupMessage 失败: %v", err)
	}
	if _, err := client.readUntilReadyForQuery(); err != nil {
		t.Fatalf("握手失败: %v", err)
	}

	// 不发送任何消息，等待空闲超时后服务端关闭连接
	client.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := client.readMessage(); err == nil {
		t.Error("空闲超时后服务端应关闭连接（读取应失败）")
	}
}

// TestServerOptionsDefaults 验证 NewServer 默认启用连接保护参数。
func TestServerOptionsDefaults(t *testing.T) {
	srv := NewServer("127.0.0.1:0", &mockExecutor{})
	if srv.maxConns != defaultMaxConns {
		t.Errorf("默认 maxConns 期望 %d，得到 %d", defaultMaxConns, srv.maxConns)
	}
	if srv.idleTimeout != defaultIdleTimeout {
		t.Errorf("默认 idleTimeout 期望 %v，得到 %v", defaultIdleTimeout, srv.idleTimeout)
	}
	if srv.writeTimeout != defaultWriteTimeout {
		t.Errorf("默认 writeTimeout 期望 %v，得到 %v", defaultWriteTimeout, srv.writeTimeout)
	}
	if srv.sem == nil || cap(srv.sem) != defaultMaxConns {
		t.Errorf("sem 应为容量 %d 的信号量", defaultMaxConns)
	}
}

// TestServerOptionsOverride 验证 Option 可覆盖默认参数。
func TestServerOptionsOverride(t *testing.T) {
	srv := NewServer("127.0.0.1:0", &mockExecutor{},
		WithMaxConns(8), WithIdleTimeout(time.Second), WithWriteTimeout(2*time.Second))
	if srv.maxConns != 8 || srv.idleTimeout != time.Second || srv.writeTimeout != 2*time.Second {
		t.Errorf("覆盖后参数错误: maxConns=%d idle=%v write=%v",
			srv.maxConns, srv.idleTimeout, srv.writeTimeout)
	}
	if srv.sem == nil || cap(srv.sem) != 8 {
		t.Errorf("sem 应为容量 8 的信号量")
	}
}

// TestServerMaxConnsUnlimited 验证 WithMaxConns(0) 表示不限制（sem 为 nil）。
func TestServerMaxConnsUnlimited(t *testing.T) {
	srv := NewServer("127.0.0.1:0", &mockExecutor{}, WithMaxConns(0))
	if srv.sem != nil {
		t.Errorf("maxConns=0 时 sem 应为 nil，得到 cap=%d", cap(srv.sem))
	}
	if !srv.acquireConnSlot() {
		t.Error("不限制连接数时 acquireConnSlot 应返回 true")
	}
}
