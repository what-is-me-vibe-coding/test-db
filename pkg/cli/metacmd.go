package cli

import (
	"strconv"
	"strings"
)

// MetaCmd 标识 REPL 反斜杠元命令的语义分类。
//
// 所有 REPL（cmd/widb、cmd/cli 及其 TTY 版本）共享同一份命令语法，但允许
// 在不同入口启用不同子集（例如独立 CLI 支持 \use TCP/HTTP，一体化模式
// 仅支持 \addrs）。parseMetaCmd 不区分上下文，统一返回识别到的语义；
// 调用方按需决定是否处理。
type MetaCmd int

const (
	// MetaCmdNotCommand 表示输入不以反斜杠开头，应按 SQL 处理。
	MetaCmdNotCommand MetaCmd = iota
	// MetaCmdUnknown 表示输入以反斜杠开头但不匹配任何已知命令。
	MetaCmdUnknown
	// MetaCmdQuit 退出 REPL。
	MetaCmdQuit
	// MetaCmdHelp 显示帮助。
	MetaCmdHelp
	// MetaCmdStatus 显示服务器状态。
	MetaCmdStatus
	// MetaCmdAddrs 显示监听地址（一键启动模式 widb 专用）。
	MetaCmdAddrs
	// MetaCmdUseTCP 切换到 TCP 模式（独立 CLI 专用）。
	MetaCmdUseTCP
	// MetaCmdUseHTTP 切换到 HTTP 模式（独立 CLI 专用）。
	MetaCmdUseHTTP
	// MetaCmdFormat 切换输出格式（带可选子参数，例如 "\format csv"）。
	MetaCmdFormat
)

// String 返回 MetaCmd 的可读名称，便于测试断言与日志输出。
// MetaCmdNotCommand 单独标注，避免误读。
func (m MetaCmd) String() string {
	switch m {
	case MetaCmdNotCommand:
		return "NotCommand"
	case MetaCmdUnknown:
		return "Unknown"
	case MetaCmdQuit:
		return "Quit"
	case MetaCmdHelp:
		return "Help"
	case MetaCmdStatus:
		return "Status"
	case MetaCmdAddrs:
		return "Addrs"
	case MetaCmdUseTCP:
		return "UseTCP"
	case MetaCmdUseHTTP:
		return "UseHTTP"
	case MetaCmdFormat:
		return "Format"
	default:
		return "MetaCmd(" + strconv.Itoa(int(m)) + ")"
	}
}

// ParseMetaCmd 解析用户输入并返回对应的 MetaCmd。
//
// 规则：
//   - 不以反斜杠开头的输入返回 MetaCmdNotCommand，应走 SQL 路径。
//   - 以反斜杠开头但不匹配任何已知命令的输入返回 MetaCmdUnknown。
//   - "\format" 及其子参数（如 "\format csv"）均归为 MetaCmdFormat；
//     具体的格式校验与切换由 FormatState 处理。
//
// 该函数为纯函数（无 IO、无副作用、无锁），便于在单元测试中穷举所有分支。
func ParseMetaCmd(line string) MetaCmd {
	switch line {
	case "\\q", "\\quit":
		return MetaCmdQuit
	case "\\h", "\\help":
		return MetaCmdHelp
	case "\\status":
		return MetaCmdStatus
	case "\\addrs":
		return MetaCmdAddrs
	case "\\use TCP":
		return MetaCmdUseTCP
	case "\\use HTTP":
		return MetaCmdUseHTTP
	}
	if strings.HasPrefix(line, "\\format") {
		return MetaCmdFormat
	}
	if strings.HasPrefix(line, "\\") {
		return MetaCmdUnknown
	}
	return MetaCmdNotCommand
}
