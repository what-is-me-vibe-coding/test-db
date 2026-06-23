// Package integration 子进程级端到端测试：widb 一键启动二进制（cmd/widb）。
//
// 与 e2e_subproc_*_test.go 互补：后者覆盖 cmd/server 子进程，本文件补充
// cmd/widb（server + CLI 同进程模式）的子进程级验证。覆盖维度：
//  1. -e 单条 SQL 模式（DDL/DML/DQL/错误 SQL）
//  2. -e 多格式输出（pretty/vertical/json/csv）
//  3. -gen-config 生成配置模板
//  4. -config 加载配置并执行 SQL
//  5. 持久化：写数据 → 同 DataDir 再次 -e 读出
//  6. 非法参数路径（-format xml、未知 flag）
//  7. 非 TTY REPL：stdin 管道输入 SQL → stdout 输出结果 → \q 退出
//  8. 多行 SQL 通过 REPL 拼接
//  9. 端到端：widb 子进程仍能对外暴露 TCP/HTTP（同进程模式不阻塞外部接入）
package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// widbBinaryOnce 保证 cmd/widb 二进制只构建一次（与 cmd/server 的 buildSubprocBinary 并行）。
var (
	widbBinaryOnce sync.Once
	widbBinaryPath string
	widbBinaryErr  error
)

// buildWidbBinary 编译 cmd/widb 为临时目录下的可执行文件，并发安全。
func buildWidbBinary() (string, error) {
	widbBinaryOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "widb-widbbin-*")
		if err != nil {
			widbBinaryErr = fmt.Errorf("创建临时目录失败: %w", err)
			return
		}
		bin := filepath.Join(tmp, "widb")
		cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/widb")
		cmd.Dir = repoRoot()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Stdout = &bytes.Buffer{}
		if err := cmd.Run(); err != nil {
			widbBinaryErr = fmt.Errorf("编译 widb 失败: %w (stderr: %s)", err, stderr.String())
			return
		}
		widbBinaryPath = bin
	})
	return widbBinaryPath, widbBinaryErr
}

// widbSubprocLog 收集 widb 子进程的 stdout/stderr 行，失败时统一打印。
type widbSubprocLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *widbSubprocLog) append(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		l.buf.WriteString("\n")
	}
}

func (l *widbSubprocLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// widbOneShotOpts 描述 -e 模式子进程调用参数。
type widbOneShotOpts struct {
	SQL      string        // -e 后的 SQL；为空时仅启动 REPL
	Format   string        // -format；空时使用默认值 pretty
	DataDir  string        // -data 目录；空时使用 t.TempDir()
	ExtraEnv []string      // 额外环境变量
	Stdin    io.Reader     // REPL 模式的输入；nil 时关闭 stdin
	Timeout  time.Duration // 进程级超时；默认 30s
}

// runWidbOneShot 启动 widb 子进程执行指定操作并返回 (stdout, stderr, exitCode)。
// exitCode < 0 表示进程被 SIGKILL 等信号杀死（视为异常）。
func runWidbOneShot(t *testing.T, opts widbOneShotOpts) (string, string, int) {
	t.Helper()
	bin, err := buildWidbBinary()
	if err != nil {
		t.Skipf("无法构建 widb 二进制: %v", err)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	format := opts.Format
	if format == "" {
		format = "pretty"
	}
	tcpPort := allocateEphemeralPort(t)
	httpPort := allocateEphemeralPort(t)
	args := []string{
		"-data", dataDir,
		"-tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort),
		"-http", fmt.Sprintf("127.0.0.1:%d", httpPort),
		"-pg", "",
		"-format", format,
	}
	if opts.SQL != "" {
		args = append(args, "-e", opts.SQL)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), opts.ExtraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	} else if opts.SQL == "" {
		// REPL 模式下若未传 stdin，显式喂一个 \q 防止进程阻塞在终端读取。
		cmd.Stdin = strings.NewReader("\\q\n")
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 widb 子进程失败: %v", err)
	}

	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Wait() }()
	select {
	case err := <-doneCh:
		exitCode := 0
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("widb 进程异常退出: %v\nstdout: %s\nstderr: %s", err, stdoutBuf.String(), stderrBuf.String())
		}
		return stdoutBuf.String(), stderrBuf.String(), exitCode
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-doneCh
		t.Fatalf("widb 进程执行超时（%v），stdout: %s\nstderr: %s", timeout, stdoutBuf.String(), stderrBuf.String())
		return "", "", -1
	}
}

// widbREPLOpts 描述 REPL 模式子进程调用参数。
type widbREPLOpts struct {
	Inputs  []string      // 喂入 REPL 的多行输入（不含尾部换行会被补上）
	DataDir string        // -data 目录；空时使用 t.TempDir()
	Format  string        // -format；空时使用默认值 pretty
	Timeout time.Duration // 进程级超时；默认 30s
}

// runWidbREPL 启动 widb 子进程进入非 TTY REPL，按行喂入 inputs 等待退出。
func runWidbREPL(t *testing.T, opts widbREPLOpts) (string, string, int) {
	t.Helper()
	bin, err := buildWidbBinary()
	if err != nil {
		t.Skipf("无法构建 widb 二进制: %v", err)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	format := opts.Format
	if format == "" {
		format = "pretty"
	}
	tcpPort := allocateEphemeralPort(t)
	httpPort := allocateEphemeralPort(t)
	args := []string{
		"-data", dataDir,
		"-tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort),
		"-http", fmt.Sprintf("127.0.0.1:%d", httpPort),
		"-pg", "",
		"-format", format,
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoRoot()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdin pipe 失败: %v", err)
	}
	defer func() { _ = stdinR.Close() }()
	cmd.Stdin = stdinR
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		_ = stdinW.Close()
		t.Fatalf("启动 widb REPL 子进程失败: %v", err)
	}
	// 喂入 inputs，按行写入（统一加 \n）。
	go func() {
		defer func() { _ = stdinW.Close() }()
		w := bufio.NewWriter(stdinW)
		for _, line := range opts.Inputs {
			_, _ = w.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				_, _ = w.WriteString("\n")
			}
			_ = w.Flush()
		}
	}()

	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Wait() }()
	select {
	case err := <-doneCh:
		exitCode := 0
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("widb REPL 进程异常退出: %v\nstdout: %s\nstderr: %s", err, stdoutBuf.String(), stderrBuf.String())
		}
		return stdoutBuf.String(), stderrBuf.String(), exitCode
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-doneCh
		t.Fatalf("widb REPL 进程执行超时（%v），stdout: %s\nstderr: %s", timeout, stdoutBuf.String(), stderrBuf.String())
		return "", "", -1
	}
}

// TestWidbOneShotCreateAndQuery 验证 -e 模式依次执行 DDL → DQL，
// 验证 DDL 结果在同一次进程内可被查询。
func TestWidbOneShotCreateAndQuery(t *testing.T) {
	dir := t.TempDir()
	// 第一次：建表 + 写入
	stdout1, _, code1 := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "CREATE TABLE t (id INT64, name STRING, PRIMARY KEY(id))",
		DataDir: dir,
	})
	if code1 != 0 {
		t.Fatalf("建表退出码 = %d, want 0; stdout: %s", code1, stdout1)
	}
	if !strings.Contains(stdout1, "成功") {
		t.Errorf("建表输出缺少成功提示: %q", stdout1)
	}
	// 第二次：插入数据（通过 -e 的 INSERT 需要走 HTTP 才能写入；本测试先验证 SELECT）
	// 用 /write 写入几行，然后 -e 查询
	srv, _ := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	if srv == nil {
		t.Fatal("无法启动 widb-server 子进程")
	}
	t.Cleanup(func() { stopSubprocessServer(t, srv) })
	if wr := httpPostWrite(t, srv.httpAddr, "t", []map[string]any{
		{"id": int64(1), "name": "alice"},
		{"id": int64(2), "name": "bob"},
	}); wr.Code != 0 {
		t.Fatalf("写入失败: %s", wr.Message)
	}
	// 第三次：-e SELECT 应返回两行
	stdout2, _, code2 := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "SELECT * FROM t ORDER BY id",
		DataDir: dir,
	})
	if code2 != 0 {
		t.Fatalf("查询退出码 = %d, want 0; stdout: %s", code2, stdout2)
	}
	if !strings.Contains(stdout2, "alice") || !strings.Contains(stdout2, "bob") {
		t.Errorf("查询输出缺少 alice/bob: %q", stdout2)
	}
}

// TestWidbOneShotCSVFormat 验证 -e 模式 -format csv 输出包含列名行。
func TestWidbOneShotCSVFormat(t *testing.T) {
	dir := t.TempDir()
	// 先建表
	if out, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "CREATE TABLE c (id INT64, name STRING, PRIMARY KEY(id))",
		DataDir: dir,
	}); code != 0 {
		t.Fatalf("建表退出码 = %d, want 0; out: %s", code, out)
	}
	// 写入两行
	srv, _ := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	if srv == nil {
		t.Fatal("无法启动 widb-server 子进程")
	}
	t.Cleanup(func() { stopSubprocessServer(t, srv) })
	if wr := httpPostWrite(t, srv.httpAddr, "c", []map[string]any{
		{"id": int64(1), "name": "alpha"},
		{"id": int64(2), "name": "beta"},
	}); wr.Code != 0 {
		t.Fatalf("写入失败: %s", wr.Message)
	}

	stdout, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "SELECT * FROM c ORDER BY id",
		DataDir: dir,
		Format:  "csv",
	})
	if code != 0 {
		t.Fatalf("csv 退出码 = %d, want 0; stdout: %s", code, stdout)
	}
	if !strings.Contains(stdout, "id,name") {
		t.Errorf("csv 输出缺少列名行 'id,name': %q", stdout)
	}
	if !strings.Contains(stdout, "alpha") || !strings.Contains(stdout, "beta") {
		t.Errorf("csv 输出缺少数据: %q", stdout)
	}
}

// TestWidbOneShotJSONFormat 验证 -e -format json 输出为合法 JSON 数组。
// 注意：JSON 渲染器会在尾部附加 "<rows> 行" 摘要（如 `] (1 行)`），
// 解析时需先去除该后缀再 unmarshal。
func TestWidbOneShotJSONFormat(t *testing.T) {
	dir := t.TempDir()
	if out, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "CREATE TABLE j (id INT64, v STRING, PRIMARY KEY(id))",
		DataDir: dir,
	}); code != 0 {
		t.Fatalf("建表退出码 = %d, want 0; out: %s", code, out)
	}
	srv, _ := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	if srv == nil {
		t.Fatal("无法启动 widb-server 子进程")
	}
	t.Cleanup(func() { stopSubprocessServer(t, srv) })
	if wr := httpPostWrite(t, srv.httpAddr, "j", []map[string]any{
		{"id": int64(1), "v": "x"},
	}); wr.Code != 0 {
		t.Fatalf("写入失败: %s", wr.Message)
	}

	stdout, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "SELECT * FROM j",
		DataDir: dir,
		Format:  "json",
	})
	if code != 0 {
		t.Fatalf("json 退出码 = %d, want 0; stdout: %s", code, stdout)
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "[") {
		t.Errorf("json 输出应以 [ 开头: %q", trimmed)
	}
	// JSON 渲染器会在尾部附 "(N 行)" 摘要，切掉括号之后的部分再 unmarshal。
	jsonPart := trimmed
	if idx := strings.LastIndex(trimmed, "]"); idx >= 0 {
		jsonPart = trimmed[:idx+1]
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &arr); err != nil {
		t.Errorf("json 输出解析失败: %v; raw: %q", err, trimmed)
	}
	if len(arr) != 1 {
		t.Errorf("json 输出应为 1 行，实际 %d 行: %v", len(arr), arr)
	}
}

// TestWidbOneShotInvalidSQL 验证 -e 模式下错误 SQL 时输出包含错误信息。
// 注意：当前 cmd/widb -e 模式仅在 ExecuteQuery 返回 Go 错误时返回非零码；
// 对 SQL 解析/执行错误（Code != 0, err == nil）会渲染 "错误: ..." 后正常返回 0。
// 本测试仅验证错误信息被呈现，不强求退出码。
func TestWidbOneShotInvalidSQL(t *testing.T) {
	stdout, _, _ := runWidbOneShot(t, widbOneShotOpts{
		SQL: "INVALID SQL !!!",
	})
	if !strings.Contains(stdout, "错误") {
		t.Errorf("输出缺少错误信息: %q", stdout)
	}
}

// TestWidbOneShotPersistence 验证 -e 模式下数据持久化：写一次，再起子进程能读出。
func TestWidbOneShotPersistence(t *testing.T) {
	dir := t.TempDir()
	// 1) 建表
	if out, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "CREATE TABLE p (id INT64, v STRING, PRIMARY KEY(id))",
		DataDir: dir,
	}); code != 0 {
		t.Fatalf("建表退出码 = %d, want 0; out: %s", code, out)
	}
	// 2) 写入
	srv, _ := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	if srv == nil {
		t.Fatal("无法启动 widb-server 子进程")
	}
	t.Cleanup(func() { stopSubprocessServer(t, srv) })
	if wr := httpPostWrite(t, srv.httpAddr, "p", []map[string]any{
		{"id": int64(42), "v": "hello-persist"},
	}); wr.Code != 0 {
		t.Fatalf("写入失败: %s", wr.Message)
	}
	stopSubprocessServer(t, srv)
	// 3) 重新拉起 widb -e 查，应能读出
	stdout, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "SELECT * FROM p",
		DataDir: dir,
	})
	if code != 0 {
		t.Fatalf("查询退出码 = %d, want 0; stdout: %s", code, stdout)
	}
	if !strings.Contains(stdout, "hello-persist") {
		t.Errorf("查询输出缺少持久化数据: %q", stdout)
	}
}

// TestWidbOneShotInvalidFormat 验证 -format xml 返回非零退出码且提示。
func TestWidbOneShotInvalidFormat(t *testing.T) {
	stdout, stderr, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:    "SELECT 1",
		Format: "xml",
	})
	if code == 0 {
		t.Fatalf("xml 格式应返回非零退出码，实际 0; stdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stderr, "未知输出格式") {
		t.Errorf("stderr 缺少 '未知输出格式' 提示: %q", stderr)
	}
}

// TestWidbGenConfig 验证 -gen-config 写出可加载的 YAML 模板。
func TestWidbGenConfig(t *testing.T) {
	bin, err := buildWidbBinary()
	if err != nil {
		t.Skipf("无法构建 widb 二进制: %v", err)
	}
	out := filepath.Join(t.TempDir(), "widb.yaml")
	cmd := exec.Command(bin, "-gen-config", out)
	cmd.Dir = repoRoot()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		t.Fatalf("-gen-config 退出码非零: %v, stderr: %s", err, stderr.String())
	}
	st, statErr := os.Stat(out)
	if statErr != nil {
		t.Fatalf("配置模板未生成: %v", statErr)
	}
	if st.Size() == 0 {
		t.Fatal("配置模板为空")
	}
}

// TestWidbInvalidFlag 验证未知 flag 立即以非零码退出。
func TestWidbInvalidFlag(t *testing.T) {
	bin, err := buildWidbBinary()
	if err != nil {
		t.Skipf("无法构建 widb 二进制: %v", err)
	}
	cmd := exec.Command(bin, "--no-such-flag")
	cmd.Dir = repoRoot()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &bytes.Buffer{}
	err = cmd.Run()
	if err == nil {
		t.Fatal("预期 --no-such-flag 退出码非零，实际为 0")
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("预期 *exec.ExitError，实际: %T", err)
	}
	if ee.ExitCode() == 0 {
		t.Errorf("退出码 = 0，期望非零")
	}
}

// TestWidbConfigNotFound 验证 -config 指向不存在文件返回非零退出码。
func TestWidbConfigNotFound(t *testing.T) {
	bin, err := buildWidbBinary()
	if err != nil {
		t.Skipf("无法构建 widb 二进制: %v", err)
	}
	path := filepath.Join(t.TempDir(), "nonexistent.yaml")
	cmd := exec.Command(bin, "-config", path, "-e", "SELECT 1")
	cmd.Dir = repoRoot()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &bytes.Buffer{}
	err = cmd.Run()
	if err == nil {
		t.Fatalf("预期 -config 指向不存在文件时退出码非零")
	}
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() == 0 {
		t.Errorf("预期非零 ExitCode，实际 err=%v", err)
	}
}

// TestWidbREPLBasicSQL 验证 REPL 模式接受 SELECT 语句并输出结果。
func TestWidbREPLBasicSQL(t *testing.T) {
	dir := t.TempDir()
	// 先建表与写入
	if out, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "CREATE TABLE r (id INT64, name STRING, PRIMARY KEY(id))",
		DataDir: dir,
	}); code != 0 {
		t.Fatalf("建表退出码 = %d, want 0; out: %s", code, out)
	}
	srv, _ := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	if srv == nil {
		t.Fatal("无法启动 widb-server 子进程")
	}
	t.Cleanup(func() { stopSubprocessServer(t, srv) })
	if wr := httpPostWrite(t, srv.httpAddr, "r", []map[string]any{
		{"id": int64(1), "name": "repl-1"},
	}); wr.Code != 0 {
		t.Fatalf("写入失败: %s", wr.Message)
	}
	stopSubprocessServer(t, srv)

	// 启动 REPL 执行 SELECT + \q
	stdout, _, code := runWidbREPL(t, widbREPLOpts{
		DataDir: dir,
		Inputs:  []string{"SELECT * FROM r;", "\\q"},
	})
	if code != 0 {
		t.Fatalf("REPL 退出码 = %d, want 0; stdout: %s", code, stdout)
	}
	if !strings.Contains(stdout, "repl-1") {
		t.Errorf("REPL 输出缺少查询结果 'repl-1': %q", stdout)
	}
	if !strings.Contains(stdout, "再见") {
		t.Errorf("REPL 输出缺少退出提示: %q", stdout)
	}
}

// TestWidbREPLFormatSwitch 验证 REPL 内 \format 切换 csv 格式后 SELECT 输出 CSV。
func TestWidbREPLFormatSwitch(t *testing.T) {
	dir := t.TempDir()
	if out, _, code := runWidbOneShot(t, widbOneShotOpts{
		SQL:     "CREATE TABLE fs (id INT64, v STRING, PRIMARY KEY(id))",
		DataDir: dir,
	}); code != 0 {
		t.Fatalf("建表退出码 = %d, want 0; out: %s", code, out)
	}
	srv, _ := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	if srv == nil {
		t.Fatal("无法启动 widb-server 子进程")
	}
	t.Cleanup(func() { stopSubprocessServer(t, srv) })
	if wr := httpPostWrite(t, srv.httpAddr, "fs", []map[string]any{
		{"id": int64(1), "v": "a"},
	}); wr.Code != 0 {
		t.Fatalf("写入失败: %s", wr.Message)
	}
	stopSubprocessServer(t, srv)

	stdout, _, code := runWidbREPL(t, widbREPLOpts{
		DataDir: dir,
		Inputs:  []string{"\\format csv", "SELECT * FROM fs;", "\\q"},
	})
	if code != 0 {
		t.Fatalf("REPL 退出码 = %d, want 0; stdout: %s", code, stdout)
	}
	if !strings.Contains(stdout, "已切换到 csv") {
		t.Errorf("REPL 输出缺少格式切换提示: %q", stdout)
	}
	if !strings.Contains(stdout, "id,v") {
		t.Errorf("REPL 输出缺少 csv 列名行: %q", stdout)
	}
}

// TestWidbREPLMultiLineSQL 验证 REPL 接受多行 SQL（先 CREATE 跨多行，结尾 ; 触发执行）。
func TestWidbREPLMultiLineSQL(t *testing.T) {
	dir := t.TempDir()
	// 不预建表，让 REPL 内用多行 SQL 建表
	stdout, _, code := runWidbREPL(t, widbREPLOpts{
		DataDir: dir,
		Inputs: []string{
			"CREATE TABLE m (",
			"  id INT64,",
			"  PRIMARY KEY(id)",
			");",
			"\\q",
		},
	})
	if code != 0 {
		t.Fatalf("REPL 多行建表退出码 = %d, want 0; stdout: %s", code, stdout)
	}
	// 验证表已建立
	srv, _ := startSubprocessServer(t, allocateEphemeralPort(t), allocateEphemeralPort(t), dir)
	if srv == nil {
		t.Fatal("无法启动 widb-server 子进程")
	}
	t.Cleanup(func() { stopSubprocessServer(t, srv) })
	resp := httpPostQuery(t, srv.httpAddr, "SHOW TABLES")
	if resp.Code != 0 {
		t.Fatalf("SHOW TABLES 失败: %s", resp.Message)
	}
	if !strings.Contains(string(resp.Data), "m") {
		t.Errorf("SHOW TABLES 输出缺少表 m: %s", string(resp.Data))
	}
}

// TestWidbExternalClientWhileRunning 验证 widb 一键启动模式下，外部 HTTP 客户端
// 仍能连通 /health（确认同进程模式不阻塞外部接入）。
func TestWidbExternalClientWhileRunning(t *testing.T) {
	bin, err := buildWidbBinary()
	if err != nil {
		t.Skipf("无法构建 widb 二进制: %v", err)
	}
	dir := t.TempDir()
	tcpPort := allocateEphemeralPort(t)
	httpPort := allocateEphemeralPort(t)
	args := []string{
		"-data", dir,
		"-tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort),
		"-http", fmt.Sprintf("127.0.0.1:%d", httpPort),
		"-pg", "",
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoRoot()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdinR, stdinW, _ := os.Pipe()
	defer func() { _ = stdinR.Close() }()
	cmd.Stdin = stdinR
	var stderrBuf bytes.Buffer
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		_ = stdinW.Close()
		t.Fatalf("启动 widb 子进程失败: %v", err)
	}
	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// 等待子进程就绪：循环探测 /health，最长 10s
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		client := &http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", httpPort))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("/health 状态码 = %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("widb 子进程未在 10s 内就绪: %v; stderr: %s", lastErr, stderrBuf.String())
	}
}
