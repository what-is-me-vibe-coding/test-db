// Package cli 提供命令行客户端与 REPL 共享的原语，被 cmd/widb 与 cmd/cli 共用。
//
// 设计要点：
//   - ReadMultiLineSQL 收集多行 SQL（以分号结尾），与 cmd/widb、cmd/cli 历史行为一致。
//   - FormatState 封装当前输出格式与 \format 命令处理逻辑，避免在两处 REPL 中重复。
//   - 本包仅依赖 pkg/render，不引入额外第三方依赖，符合 AGENTS.md 的依赖约束。
package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/what-is-me-vibe-coding/test-db/pkg/render"
)

// continuationPrompt 是多行 SQL 输入时展示的续行提示符。
const continuationPrompt = "  ...> "

// readMultiLinePrefix 是 \format 命令的前缀，HandleFormatCommand 据此分流。
const formatCommandPrefix = "\\format"

// ReadMultiLineSQL 从 scanner 读取多行 SQL 直到遇到分号或 EOF。
// firstLine 是已经读到的第一行内容，函数会将其与后续行用空格连接。
// 末尾分号会被去除，结果去除首尾空白；输入为空白时返回空字符串。
//
// 行为与原 cmd/widb.readMultiLineSQL、cmd/cli.readMultiLineSQL 完全一致，
// 用于在重构中替换两份重复实现，保证现有测试不失效。
func ReadMultiLineSQL(scanner *bufio.Scanner, writer io.Writer, firstLine string) string {
	sql := firstLine
	for !strings.HasSuffix(sql, ";") {
		_, _ = fmt.Fprint(writer, continuationPrompt)
		if !scanner.Scan() {
			break
		}
		sql += " " + scanner.Text()
	}
	return strings.TrimSuffix(strings.TrimSpace(sql), ";")
}

// FormatState 持有 REPL 当前输出格式，对外暴露 Current/Set 与 HandleCommand。
// 原 cmd/widb 与 cmd/cli 各自使用一个 string 与 handleFormatCommand 函数，
// 重构后将状态与命令处理统一收敛到本类型。
type FormatState struct {
	current string
}

// NewFormatState 构造 FormatState，初始格式为 render.FormatPretty。
func NewFormatState() *FormatState {
	return &FormatState{current: render.FormatPretty}
}

// Current 返回当前输出格式。
func (s *FormatState) Current() string {
	return s.current
}

// Set 直接设置当前输出格式（不进行合法性校验，调用方需自行保证）。
// 适用于初始化或测试场景；REPL 中应使用 HandleCommand 以获得友好提示。
func (s *FormatState) Set(format string) {
	s.current = format
}

// HandleCommand 处理 \format 命令的输出与格式切换。
// 参数 cmd 形如 "\format" 或 "\format csv"。
//   - 无参数：打印当前格式与支持列表。
//   - 合法参数：切换格式并打印确认信息。
//   - 非法参数：打印错误信息，格式保持不变。
//
// 返回值无意义，仅为兼容未来扩展（例如 \format <fmt> 之外的子命令）。
func (s *FormatState) HandleCommand(writer io.Writer, cmd string) {
	arg := strings.TrimSpace(strings.TrimPrefix(cmd, formatCommandPrefix))
	if arg == "" {
		_, _ = fmt.Fprintf(writer, "当前格式: %s（支持: %s）\n", s.current, strings.Join(render.SupportedFormats, ", "))
		return
	}
	if !render.IsValidFormat(arg) {
		_, _ = fmt.Fprintf(writer, "未知格式: %s，支持: %s\n", arg, strings.Join(render.SupportedFormats, ", "))
		return
	}
	s.current = arg
	_, _ = fmt.Fprintf(writer, "已切换到 %s 格式\n", arg)
}
