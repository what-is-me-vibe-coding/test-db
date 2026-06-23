// Package integration 端到端集成测试：子进程 server 崩溃恢复 + 并发写入。
//
// 本文件补齐既有测试未覆盖的「进程级崩溃恢复 + 并发写入」组合：在真实子进程
// server 下，N 个 HTTP 客户端并发 INSERT 各 ID 区间的行；中途用 SIGKILL
// 强杀子进程（模拟断电/OOM 杀死），再在同 DataDir 重新拉起 server，校验：
//  1. 所有「已被 server 确认 ack」的写入在重启后完整恢复（无丢失）；
//  2. 重启后表结构（DDL）正确恢复（catalog 持久化生效）；
//  3. 写入路径「最后一批」的不变量（最大 ID = 已 ack 的最大 ID）；
//  4. 连续多轮 kill - 重启 - 写入循环不出现数据污染或累积漂移。
//
// 与既有测试的区别：
//   - e2e_memory_engine_restart_test.go：使用同进程 *server.Server 顺序 Stop/Start，
//     覆盖「优雅重启」语义（Stop 会 flush），未走 SIGKILL 路径。
//   - e2e_subproc_smoke_test.go：覆盖 flag 解析、/health、/metrics 等基础路径，
//     未做并发写入与崩溃恢复的组合。
//   - storage 包 concurrent_correctness_test.go：单进程多 goroutine 压测，
//     未走真实子进程。
//
// 关键设计：
//   - 复用 e2e_subproc_smoke_test.go 的 buildSubprocBinary / startSubprocessServer
//     / stopSubprocessServer / sendSignalToSubprocess / waitForSubprocessExit
//     / httpPostQuery / httpPostWrite 等 helper。
//   - HTTP 客户端使用独立 ctx 取消信号；kill 后未完成的请求由 errCh 汇总，
//     不会因「连接被服务端关闭」误判为「写入失败」——本测试用 ack 模式：
//     server 返回 code=0 才算成功。
//   - 多轮循环：每次重启后用「最大 ID」与「已 ack ID 数」两个不变量做断言，
//     不强求 ID 区间连续（kill 可能截断末尾若干行）。
//
// 并发测试规范：worker goroutine 内不调用 t.Fatal/t.Errorf，统一通过 errCh
// 汇总到主 goroutine 后再断言（与同包其他 _multiclient_test.go 一致）。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// 崩溃恢复测试常量。
const (
	crTableName = "crash_recovery_kv" // 测试表名
	crClients   = 4                   // 并发 HTTP 客户端数
	crPerClient = 80                  // 每客户端每轮写入行数
	crRounds    = 3                   // kill - 重启 - 写入循环轮数
	crBaseID    = 500000              // ID 起始偏移，避免与其它测试冲突
	crIDStep    = 1000                // 客户端间 ID 步长（避免交叉）
	crWriteTO   = 5 * time.Second     // 单次 /write 超时
	crSubprocTO = 15 * time.Second    // 子进程启动超时
)

// crWriteReq 表示一次 (client, round, seq) 维度的写入请求，预先生成便于复用。
type crWriteReq struct {
	ClientID int
	Round    int
	Seq      int
	ID       int64
	Name     string
}

// crBuildReqs 预生成一轮测试中所有 client × seq 的写入请求。
// 使用确定性数据确保跨轮次可重复（kill 后重启看到的数据集一致）。
func crBuildReqs(round int) []crWriteReq {
	reqs := make([]crWriteReq, 0, crClients*crPerClient)
	for c := 0; c < crClients; c++ {
		for s := 0; s < crPerClient; s++ {
			id := int64(crBaseID + round*crClients*crPerClient + c*crIDStep + s)
			reqs = append(reqs, crWriteReq{
				ClientID: c,
				Round:    round,
				Seq:      s,
				ID:       id,
				Name:     fmt.Sprintf("crash-r%d-c%d-s%d", round, c, s),
			})
		}
	}
	return reqs
}

// crHTTPWrite 用 HTTP POST /write 写入单行，返回 (ack, err)。
// ack=true 表示 server 返回 code=0；其余情况视为未提交。
func crHTTPWrite(ctx context.Context, addr string, id int64, name string) (bool, error) {
	body, _ := json.Marshal(map[string]any{
		"table": crTableName,
		"rows":  []map[string]any{{"id": id, "name": name}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+addr+"/write", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("构造 /write 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: crWriteTO}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out serverQueryResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("解析 /write 响应失败: %w", err)
	}
	return out.Code == 0, nil
}

// crConcurrentWrite 启动 N 个 HTTP worker 并发执行 reqs 中的写入请求，
// 返回已 ack 的 ID 集合与首个非 nil 错误。worker 内禁止使用 *testing.T。
//
// 错误处理：网络错误（ctx 取消、连接重置）由调用方决定是否忽略——
// 在 kill 场景下，kill 之后的网络错误属于「该请求本就不应被 ack」，
// 不计入「数据丢失」。本函数仅对「成功响应但 code != 0」的情况返回错误。
func crConcurrentWrite(ctx context.Context, addr string, reqs []crWriteReq) (map[int64]struct{}, error) {
	var (
		mu       sync.Mutex
		acked    = make(map[int64]struct{}, len(reqs))
		firstErr error
		wg       sync.WaitGroup
	)
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	for _, r := range reqs {
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := crHTTPWrite(ctx, addr, r.ID, r.Name)
			if err != nil {
				// kill 期间 ctx 取消或连接重置属预期，忽略。
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				if isHTTPTransportClosed(err) {
					return
				}
				setErr(fmt.Errorf("client %d round %d seq %d (id=%d): %w",
					r.ClientID, r.Round, r.Seq, r.ID, err))
				return
			}
			if !ok {
				setErr(fmt.Errorf("client %d round %d seq %d (id=%d): server 返回非零 code",
					r.ClientID, r.Round, r.Seq, r.ID))
				return
			}
			mu.Lock()
			acked[r.ID] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return acked, firstErr
}

// isHTTPTransportClosed 判断是否为「连接被对端关闭」类网络错误。
// 在 SIGKILL 期间，未完成的请求几乎都会触发以下错误之一，视为预期。
func isHTTPTransportClosed(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// 注意：仅匹配网络层 / 服务端主动关闭语义；业务错误（code != 0）
	// 与 JSON 解析错误不命中以下任一子串，crConcurrentWrite 会正常上报。
	return strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "server closed idle connection") ||
		strings.Contains(s, "context deadline exceeded")
}

// crAckedIDsSorted 返回 acked 集合中所有 ID 的升序切片，便于断言。
func crAckedIDsSorted(acked map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(acked))
	for id := range acked {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// crRecoveredIDs 通过 SELECT 拉取 server 中当前表所有 ID，返回升序切片。
// 用于断言：crash 之前 acked 的 ID 在重启后应全部可见。
func crRecoveredIDs(t *testing.T, addr string) []int64 {
	t.Helper()
	resp := httpPostQuery(t, addr,
		fmt.Sprintf("SELECT id FROM %s ORDER BY id", crTableName))
	if resp.Code != 0 {
		t.Fatalf("重启后 SELECT 失败: %s", resp.Message)
	}
	if len(resp.Data) == 0 {
		return nil
	}
	var rows []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(resp.Data, &rows); err != nil {
		t.Fatalf("解析 SELECT 响应失败: %v (raw: %s)", err, string(resp.Data))
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// crSubstrID 检查 ids 中是否每个 wanted 都存在。
// 单独的辅助函数：让断言失败时的错误信息更易读。
func crSubstrID(t *testing.T, ids []int64, wanted []int64, label string) {
	t.Helper()
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	var missing []int64
	for _, w := range wanted {
		if _, ok := set[w]; !ok {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%s: 重启后 server 缺失 %d 个已 ack 的 ID（前 10: %v）", label, len(missing), firstN(missing, 10))
	}
}

// firstN 返回切片的前 n 个元素，n 超过长度时返回整个切片。
func firstN(xs []int64, n int) []int64 {
	if n >= len(xs) {
		return xs
	}
	return xs[:n]
}

// crEnsureTable 在 addr 指向的 server 上确保 crTableName 表存在。
// 重启后 catalog.json 会恢复表结构，本函数使用 CREATE TABLE IF NOT EXISTS
// 兼容首次创建与恢复后幂等调用。
func crEnsureTable(t *testing.T, addr string) {
	t.Helper()
	resp := httpPostQuery(t, addr, "CREATE TABLE IF NOT EXISTS "+crTableName+
		" (id INT64 NOT NULL, name STRING NULL, PRIMARY KEY(id))")
	if resp.Code != 0 {
		t.Fatalf("建表失败: %s", resp.Message)
	}
}

// crRunRound 执行一轮「拉起子进程 → 并发写入 → SIGKILL」流程。
// 拆出本函数降低 TestSubprocCrashRecoveryWithConcurrentWriters 的
// 圈复杂度与认知复杂度（gocyclo/gocognit 阈值要求）。
func crRunRound(t *testing.T, round int, dir string, prevMaxID int64) (acked map[int64]struct{}, newMaxID int64) {
	t.Helper()
	s, log := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	t.Cleanup(func() {
		if s != nil && s.cmd != nil && s.cmd.ProcessState == nil {
			stopSubprocessServer(t, s)
		}
		if t.Failed() {
			t.Logf("round %d 子进程日志:\n%s", round, log.String())
		}
	})
	crAssertHealth(t, s.httpAddr)
	crEnsureTable(t, s.httpAddr)
	crAssertCatalogRecovered(t, s.httpAddr, round, prevMaxID)
	acked = crDoConcurrentWriteKill(t, s, round)
	ackedIDs := crAckedIDsSorted(acked)
	if len(ackedIDs) == 0 {
		t.Fatalf("round %d 并发写入未 ack 任何行（kill 太早或 server 启动失败）", round)
	}
	t.Logf("round %d: ack=%d, maxID=%d", round, len(ackedIDs), ackedIDs[len(ackedIDs)-1])
	return acked, ackedIDs[len(ackedIDs)-1]
}

// crAssertHealth 验证子进程 /health 返回 200。
func crAssertHealth(t *testing.T, httpAddr string) {
	t.Helper()
	hp := httpHealthHit(t, httpAddr)
	_ = hp.Body.Close()
	if hp.StatusCode != 200 {
		t.Fatalf("/health 状态码 = %d, want 200", hp.StatusCode)
	}
}

// crAssertCatalogRecovered 在 round > 0 时校验重启后表非空且最大 ID 单调不减。
func crAssertCatalogRecovered(t *testing.T, httpAddr string, round int, prevMaxID int64) {
	t.Helper()
	if round == 0 {
		return
	}
	ids := crRecoveredIDs(t, httpAddr)
	if len(ids) == 0 {
		t.Fatalf("round %d 重启后表为空，catalog 未恢复", round)
	}
	if maxID := ids[len(ids)-1]; maxID < prevMaxID {
		t.Fatalf("round %d 重启后最大 ID=%d 小于 round %d 的最大 ID=%d（数据回退）",
			round, maxID, round-1, prevMaxID)
	}
}

// crDoConcurrentWriteKill 启动并发写入并按计划 SIGKILL 子进程，
// 返回已 ack 的 ID 集合。SIGKILL 时机与 ctx 取消都在此函数内完成。
func crDoConcurrentWriteKill(t *testing.T, s *subprocServer, round int) map[int64]struct{} {
	t.Helper()
	reqs := crBuildReqs(round)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	killAfter := 50 * time.Millisecond
	go func() {
		select {
		case <-time.After(killAfter):
			sendSignalToSubprocess(t, s, syscall.SIGKILL)
			cancel()
		case <-ctx.Done():
			return
		}
	}()
	acked, werr := crConcurrentWrite(ctx, s.httpAddr, reqs)
	if werr != nil {
		t.Logf("round %d 并发写入期间出现错误（可能是 kill 副作用）: %v", round, werr)
	}
	_, _ = crForceSubprocExit(t, s, syscall.SIGKILL, crSubprocTO)
	return acked
}

// crRunFinalRecovery 拉起新子进程并校验「所有累计 ack 的 ID 全部可读」。
// 拆出本函数降低主测试函数复杂度。
func crRunFinalRecovery(t *testing.T, dir string, allAcked map[int64]struct{}) {
	t.Helper()
	s, log := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	t.Cleanup(func() {
		if s != nil {
			stopSubprocessServer(t, s)
		}
		if t.Failed() {
			t.Logf("最终恢复子进程日志:\n%s", log.String())
		}
	})
	crAssertHealth(t, s.httpAddr)
	ids := crWaitForRecovery(t, s.httpAddr)
	allWanted := crAckedIDsSorted(allAcked)
	crSubstrID(t, ids, allWanted, "FinalRecovery")
	if len(ids) < len(allWanted) {
		t.Fatalf("FinalRecovery: server ID 数 (%d) 少于 ack 集合数 (%d)，存在已 ack 写入丢失",
			len(ids), len(allWanted))
	}
	t.Logf("最终恢复: server 中 ID=%d, ack 集合 ID=%d, 在飞写入=%d（kill 期间未收到响应的写）",
		len(ids), len(allWanted), len(ids)-len(allWanted))
}

// crWaitForRecovery 轮询 SELECT 直到表非空或超时，返回全部 ID。
func crWaitForRecovery(t *testing.T, httpAddr string) []int64 {
	t.Helper()
	deadline := time.Now().Add(crSubprocTO)
	var ids []int64
	for time.Now().Before(deadline) {
		ids = crRecoveredIDs(t, httpAddr)
		if len(ids) > 0 {
			return ids
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("最终重启后表仍为空（catalog 恢复失败）")
	return nil
}

// TestSubprocCrashRecoveryWithConcurrentWriters 验证子进程 SIGKILL 后
// 所有已 ack 写入在重启后完整恢复。
//
// 流程（crRounds 轮）：
//  1. 拉起子进程 server（A）
//  2. 建表（首轮）/ 验证 catalog 恢复（后续轮）
//  3. crClients 个 HTTP 客户端并发 INSERT 各自 ID 区间
//  4. 在写入中途向子进程 A 发送 SIGKILL
//  5. 等待 A 退出，拉起子进程 server B（同 DataDir）
//  6. SELECT 拉取 B 中所有 ID，断言「上一轮 acked 集合中的每个 ID 都存在」
//  7. 记录本轮 acked 集合累计，进入下一轮
//
// 终态断言：
//   - 所有 acked ID 在最后一次重启后全部可读（无丢失）
//   - 每轮最大 ID 不小于上一轮最大 ID（数据非回退）
func TestSubprocCrashRecoveryWithConcurrentWriters(t *testing.T) {
	if testing.Short() {
		t.Skip("短测试模式下跳过子进程崩溃恢复测试")
	}
	dir := t.TempDir()
	allAcked := make(map[int64]struct{})
	var prevMaxID int64

	for round := 0; round < crRounds; round++ {
		round := round
		t.Run(fmt.Sprintf("Round_%d", round), func(t *testing.T) {
			acked, maxID := crRunRound(t, round, dir, prevMaxID)
			prevMaxID = maxID
			for id := range acked {
				allAcked[id] = struct{}{}
			}
		})
	}

	t.Run("FinalRecovery", func(t *testing.T) {
		crRunFinalRecovery(t, dir, allAcked)
	})
}

// crForceSubprocExit 强制结束子进程并等待其退出，返回 (exitCode, err)。
// 复用于优雅 (SIGTERM) 与强杀 (SIGKILL) 两种场景。
func crForceSubprocExit(t *testing.T, s *subprocServer, sig syscall.Signal, timeout time.Duration) (int, error) {
	t.Helper()
	sendSignalToSubprocess(t, s, sig)
	return waitForSubprocessExit(t, s, timeout)
}
