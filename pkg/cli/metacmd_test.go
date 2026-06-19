package cli

import "testing"

// TestParseMetaCmdTable 穷举 ParseMetaCmd 的所有分支。
//
// 该表覆盖：
//   - 空字符串、纯 SQL 输入（MetaCmdNotCommand）
//   - 6 类已知命令（Quit/Help/Status/Addrs/UseTCP/UseHTTP）的全部别名
//   - \format 及其子参数（MetaCmdFormat）
//   - 未知反斜杠命令（MetaCmdUnknown）
func TestParseMetaCmdTable(t *testing.T) {
	tests := []struct {
		name string
		line string
		want MetaCmd
	}{
		// 非命令输入
		{"empty", "", MetaCmdNotCommand},
		{"whitespace", "   ", MetaCmdNotCommand},
		{"select SQL", "SELECT * FROM t", MetaCmdNotCommand},
		{"insert SQL lowercase", "insert into t values (1)", MetaCmdNotCommand},
		{"hyphen prefix not command", "-x", MetaCmdNotCommand},

		// Quit
		{"quit short", "\\q", MetaCmdQuit},
		{"quit long", "\\quit", MetaCmdQuit},
		{"quit with trailing space treated as unknown", "\\q ", MetaCmdUnknown},

		// Help
		{"help short", "\\h", MetaCmdHelp},
		{"help long", "\\help", MetaCmdHelp},

		// Status
		{"status", "\\status", MetaCmdStatus},

		// Addrs (widb 专用)
		{"addrs", "\\addrs", MetaCmdAddrs},

		// Use TCP / Use HTTP (cli 专用)
		{"use TCP", "\\use TCP", MetaCmdUseTCP},
		{"use HTTP", "\\use HTTP", MetaCmdUseHTTP},
		{"use lowercase tcp", "\\use tcp", MetaCmdUnknown},

		// Format
		{"format bare", "\\format", MetaCmdFormat},
		{"format with arg", "\\format csv", MetaCmdFormat},
		{"format with extra spaces", "\\format  json ", MetaCmdFormat},
		{"formattypo is unknown", "\\forma", MetaCmdUnknown},

		// Unknown
		{"backslash alone", "\\", MetaCmdUnknown},
		{"unknown command", "\\foo", MetaCmdUnknown},
		{"unknown with arg", "\\foo bar", MetaCmdUnknown},
		{"prefix of quit", "\\qu", MetaCmdUnknown},
		{"prefix of help", "\\he", MetaCmdUnknown},
		{"prefix of status", "\\sta", MetaCmdUnknown},
		{"prefix of addrs", "\\add", MetaCmdUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMetaCmd(tt.line)
			if got != tt.want {
				t.Errorf("ParseMetaCmd(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

// TestMetaCmdString 验证 String 输出可读且覆盖所有枚举值。
func TestMetaCmdString(t *testing.T) {
	all := []MetaCmd{
		MetaCmdNotCommand, MetaCmdUnknown, MetaCmdQuit, MetaCmdHelp,
		MetaCmdStatus, MetaCmdAddrs, MetaCmdUseTCP, MetaCmdUseHTTP,
		MetaCmdFormat,
	}
	seen := make(map[string]bool)
	for _, m := range all {
		s := m.String()
		if s == "" {
			t.Errorf("MetaCmd(%d).String() 返回空字符串", int(m))
		}
		if seen[s] {
			t.Errorf("String 重复: %q", s)
		}
		seen[s] = true
	}
}

// TestMetaCmdStringUnknown 验证越界值仍能返回可读输出。
func TestMetaCmdStringUnknown(t *testing.T) {
	got := MetaCmd(9999).String()
	if got == "" || got == "MetaCmd()" {
		t.Errorf("越界 MetaCmd 字符串不可读: %q", got)
	}
}
