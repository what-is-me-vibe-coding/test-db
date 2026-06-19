// Package cli 的单元测试。
package cli

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/render"
)

// TestReadMultiLineSQL_SingleLineWithSemicolon 验证单行带分号直接返回。
func TestReadMultiLineSQL_SingleLineWithSemicolon(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	var out bytes.Buffer
	got := ReadMultiLineSQL(scanner, &out, "SELECT 1;")
	if got != "SELECT 1" {
		t.Errorf("结果 = %q, want %q", got, "SELECT 1")
	}
	if out.Len() != 0 {
		t.Errorf("单行 SQL 不应输出续行提示: %q", out.String())
	}
}

// TestReadMultiLineSQL_MultiLine 验证多行 SQL 被空格拼接。
func TestReadMultiLineSQL_MultiLine(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("FROM users\nWHERE id = 1;\n"))
	var out bytes.Buffer
	got := ReadMultiLineSQL(scanner, &out, "SELECT *")
	want := "SELECT * FROM users WHERE id = 1"
	if got != want {
		t.Errorf("结果 = %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), continuationPrompt) {
		t.Errorf("多行输入应输出续行提示: %q", out.String())
	}
}

// TestReadMultiLineSQL_NoSemicolonEOF 验证无分号遇 EOF 立即结束。
func TestReadMultiLineSQL_NoSemicolonEOF(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("FROM users\n"))
	var out bytes.Buffer
	got := ReadMultiLineSQL(scanner, &out, "SELECT *")
	want := "SELECT * FROM users"
	if got != want {
		t.Errorf("结果 = %q, want %q", got, want)
	}
}

// TestReadMultiLineSQL_EmptySemicolon 验证只有分号的输入返回空字符串。
func TestReadMultiLineSQL_EmptySemicolon(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	var out bytes.Buffer
	got := ReadMultiLineSQL(scanner, &out, ";")
	if got != "" {
		t.Errorf("结果 = %q, want 空字符串", got)
	}
}

// TestReadMultiLineSQL_TrimsWhitespace 验证首尾空白被去除。
func TestReadMultiLineSQL_TrimsWhitespace(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	var out bytes.Buffer
	got := ReadMultiLineSQL(scanner, &out, "  SELECT 1;  ")
	if got != "SELECT 1" {
		t.Errorf("结果 = %q, want %q", got, "SELECT 1")
	}
}

// TestFormatState_InitialPretty 验证 NewFormatState 初始为 pretty。
func TestFormatState_InitialPretty(t *testing.T) {
	s := NewFormatState()
	if s.Current() != render.FormatPretty {
		t.Errorf("初始格式 = %q, want %q", s.Current(), render.FormatPretty)
	}
}

// TestFormatState_HandleCommand_Show 验证无参 \format 显示当前格式。
func TestFormatState_HandleCommand_Show(t *testing.T) {
	s := NewFormatState()
	var out bytes.Buffer
	s.HandleCommand(&out, "\\format")
	if !strings.Contains(out.String(), "当前格式") {
		t.Errorf("应显示当前格式: %q", out.String())
	}
	if !strings.Contains(out.String(), render.FormatPretty) {
		t.Errorf("应包含 pretty: %q", out.String())
	}
}

// TestFormatState_HandleCommand_Switch 验证合法参数切换格式。
func TestFormatState_HandleCommand_Switch(t *testing.T) {
	s := NewFormatState()
	var out bytes.Buffer
	s.HandleCommand(&out, "\\format csv")
	if s.Current() != render.FormatCSV {
		t.Errorf("切换后 = %q, want %q", s.Current(), render.FormatCSV)
	}
	if !strings.Contains(out.String(), "已切换到 csv 格式") {
		t.Errorf("应显示切换成功: %q", out.String())
	}
}

// TestFormatState_HandleCommand_Invalid 验证非法参数给出错误提示且保持原格式。
func TestFormatState_HandleCommand_Invalid(t *testing.T) {
	s := NewFormatState()
	prev := s.Current()
	var out bytes.Buffer
	s.HandleCommand(&out, "\\format xml")
	if s.Current() != prev {
		t.Errorf("非法参数不应修改格式: %q", s.Current())
	}
	if !strings.Contains(out.String(), "未知格式") {
		t.Errorf("应显示未知格式提示: %q", out.String())
	}
}

// TestFormatState_Set 验证 Set 直接更新格式。
func TestFormatState_Set(t *testing.T) {
	s := NewFormatState()
	s.Set(render.FormatJSON)
	if s.Current() != render.FormatJSON {
		t.Errorf("Set 后 = %q, want %q", s.Current(), render.FormatJSON)
	}
}
