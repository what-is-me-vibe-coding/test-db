// Package cli 的 TTY 增强 REPL 公共循环。
//
// RunWithLiner 与配套类型（TTYHandler / TTYOptions）从 repl.go 抽出：
//   - repl.go 保留多行 SQL 收集、FormatState、LinerSession 等基础原语；
//   - 本文件提供「TTY 模式下的 REPL 主循环」单一职责抽象，被 cmd/widb
//     与 cmd/cli 复用。
//
// 抽出动机：pkg/cli/repl.go 在 2026-06-23 的 RunWithLiner 落地后达到
// 523 行，超过 CI 规定的单文件 ≤ 500 行阈值；将其拆出后两文件均在
// 阈值内，且模块边界更清晰。
package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// TTYHandler 描述 RunWithLiner 对每行用户输入的回调。
//
// 入参 line 的语义：
//   - 当输入以反斜杠开头（原始 line，未拼接）时，line 等于用户键入的命令（含 `\`
//     前缀），例如 `\q`、`\status`、`\format csv`。
//   - 当输入为 SQL（非反斜杠开头）时，line 是经过多行拼接与 TrimSpace/去分号
//     处理后的最终 SQL 字符串。
//
// 返回值：
//   - shouldExit=true：终止 REPL 循环，RunWithLiner 立即返回 nil。
//   - shouldExit=false：继续等待下一行输入。
//
// 回调不应在内部再次读取 stdin，否则会与 liner 抢输入导致混乱。
type TTYHandler func(line string) (shouldExit bool)

// TTYOptions 配置 RunWithLiner 的一次性运行参数。
//
// Prompt 字段允许包含 ANSI 转义码（由 ColorizePrompt 等产生），RunWithLiner
// 会通过 LinerSession.PromptWithWriter 写出以规避 liner 拒绝控制字符的
// ErrInvalidPrompt。ContPrompt 仅写入 liner（含 ASCII 时），若含 ANSI 也
// 走 PromptWithWriter 等价路径。
type TTYOptions struct {
	// Session 提供 TTY 输入与历史管理；不可为 nil。
	Session *LinerSession
	// Writer 接收提示符与回调输出；不可为 nil。
	Writer io.Writer
	// Prompt 是首行提示符（可含 ANSI）。空字符串时使用默认 "widb> "。
	Prompt string
	// ContPrompt 是多行 SQL 续行提示符（可含 ANSI）。空字符串时使用默认 "  ...> "。
	ContPrompt string
	// OnLine 处理每一条非空输入行；不可为 nil。
	OnLine TTYHandler
}

// RunWithLiner 在 TTY 模式下运行增强 REPL：行编辑、历史、Tab 补全、颜色高亮
// 提示符与多行 SQL 拼接均由本函数统一处理，回调方只需关心单条最终输入的
// 命令/SQL 分发与执行。
//
// 行为约定：
//   - 首行提示符用 opts.Prompt（可含 ANSI），通过 LinerSession.PromptWithWriter
//     写出，规避 liner 拒绝控制字符的 ErrInvalidPrompt。
//   - 空行（TrimSpace 后为空）跳过且不进入历史。
//   - 非空行：去掉首尾空白，写入历史。
//   - 以 `\` 开头的行直接以「原始命令（含前缀）」调用 OnLine；
//   - 其他行通过 LinerSession 续行提示拼接多行 SQL，最终以「TrimSpace +
//     去末尾分号」的结果调用 OnLine。续行时仍允许以 `\` 开头并被视作
//     普通 SQL 一部分，避免误判。
//   - 回调返回 shouldExit=true 时 REPL 正常退出并返回 nil。
//   - 收到 io.EOF（Ctrl-D）或非 EOF 错误时按情况返回 nil 或 error。
//
// 本函数不负责 SQL 解析、命令分发、结果输出与格式切换；这些由 OnLine 闭包
// 注入，使 cmd/widb 与 cmd/cli 共享同一套 TTY 基础设施。
func RunWithLiner(opts TTYOptions) error {
	prompt, contPrompt, err := resolveTTYOptions(opts)
	if err != nil {
		return err
	}
	for {
		err := runTTYIteration(opts, prompt, contPrompt)
		if err == nil {
			continue
		}
		if errors.Is(err, errTTYExit) {
			return nil
		}
		return err
	}
}

// resolveTTYOptions 校验 TTYOptions 的必填字段并填充默认 prompt。
// 抽出来降低 RunWithLiner 的认知复杂度。
func resolveTTYOptions(opts TTYOptions) (prompt, contPrompt string, err error) {
	switch {
	case opts.Session == nil:
		return "", "", fmt.Errorf("RunWithLiner: Session 不能为空")
	case opts.Writer == nil:
		return "", "", fmt.Errorf("RunWithLiner: Writer 不能为空")
	case opts.OnLine == nil:
		return "", "", fmt.Errorf("RunWithLiner: OnLine 不能为空")
	}
	if opts.Prompt == "" {
		opts.Prompt = "widb> "
	}
	if opts.ContPrompt == "" {
		opts.ContPrompt = continuationPrompt
	}
	return opts.Prompt, opts.ContPrompt, nil
}

// runTTYIteration 完成 REPL 的一次迭代：读取首行 → 处理空行/历史 → 分发
// 命令或 SQL → 调用 OnLine。返回 nil 表示"继续"；返回非 nil 错误
// 表示应终止循环：errTTYExit 由 RunWithLiner 翻译为正常退出，其他
// 错误原样透传给调用方。
func runTTYIteration(opts TTYOptions, prompt, contPrompt string) error {
	line, err := opts.Session.PromptWithWriter(opts.Writer, prompt)
	if err != nil {
		return handlePromptError(err)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	opts.Session.AppendHistory(trimmed)
	if strings.HasPrefix(trimmed, "\\") {
		return dispatchTTYLine(opts, trimmed)
	}
	sql, err := ReadMultiLineSQLWithLiner(opts.Session, contPrompt, trimmed)
	if err != nil && err != io.EOF {
		return err
	}
	return dispatchTTYLine(opts, sql)
}

// handlePromptError 把 PromptWithWriter 的错误归一化：io.EOF 视为正常
// 退出（返回 nil），其他错误透传。
func handlePromptError(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}

// dispatchTTYLine 调用 OnLine 并把其布尔返回值翻译为 error：
//   - shouldExit=true → 返回 errTTYExit（RunWithLiner 识别为终止）
//   - shouldExit=false → 返回 nil（继续下一轮迭代）
func dispatchTTYLine(opts TTYOptions, line string) error {
	if opts.OnLine(line) {
		return errTTYExit
	}
	return nil
}

// errTTYExit 是 RunWithLiner 内部的退出哨兵错误，不导出、不暴露给调用方。
var errTTYExit = &ttyExitError{}

type ttyExitError struct{}

func (e *ttyExitError) Error() string { return "tty REPL exit" }

// Is 支持 errors.Is(err, io.EOF) 风格的检测，调用方可识别 REPL 主动退出。
func (e *ttyExitError) Is(target error) bool { return target == errTTYExit }
