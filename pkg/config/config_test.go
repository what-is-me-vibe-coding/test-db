package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Server.TCPAddr != "0.0.0.0:9000" {
		t.Errorf("TCPAddr = %q, want 0.0.0.0:9000", cfg.Server.TCPAddr)
	}
	if cfg.Server.HTTPAddr != "0.0.0.0:8080" {
		t.Errorf("HTTPAddr = %q, want 0.0.0.0:8080", cfg.Server.HTTPAddr)
	}
	if cfg.Storage.DataDir != "./data" {
		t.Errorf("DataDir = %q, want ./data", cfg.Storage.DataDir)
	}
	if cfg.Storage.MaxMemTableSize != 64<<20 {
		t.Errorf("MaxMemTableSize = %d, want %d", cfg.Storage.MaxMemTableSize, 64<<20)
	}
	if !cfg.Scheduler.Enabled {
		t.Error("Scheduler.Enabled = false, want true")
	}
	if time.Duration(cfg.Scheduler.FlushInterval) != 5*time.Second {
		t.Errorf("FlushInterval = %v, want 5s", cfg.Scheduler.FlushInterval)
	}
	if time.Duration(cfg.Scheduler.CompactInterval) != 10*time.Second {
		t.Errorf("CompactInterval = %v, want 10s", cfg.Scheduler.CompactInterval)
	}
	if time.Duration(cfg.Scheduler.WALCleanInterval) != 30*time.Second {
		t.Errorf("WALCleanInterval = %v, want 30s", cfg.Scheduler.WALCleanInterval)
	}
	if cfg.Scheduler.WALCleanThreshold != 64<<20 {
		t.Errorf("WALCleanThreshold = %d, want %d", cfg.Scheduler.WALCleanThreshold, 64<<20)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "合法默认配置", cfg: Default(), wantErr: false},
		{name: "TCPAddr 为空", cfg: func() Config {
			c := Default()
			c.Server.TCPAddr = ""
			return c
		}(), wantErr: true},
		{name: "HTTPAddr 为空", cfg: func() Config {
			c := Default()
			c.Server.HTTPAddr = ""
			return c
		}(), wantErr: true},
		{name: "DataDir 为空", cfg: func() Config {
			c := Default()
			c.Storage.DataDir = ""
			return c
		}(), wantErr: true},
		{name: "MaxMemTableSize 为零", cfg: func() Config {
			c := Default()
			c.Storage.MaxMemTableSize = 0
			return c
		}(), wantErr: true},
		{name: "MaxMemTableSize 为负", cfg: func() Config {
			c := Default()
			c.Storage.MaxMemTableSize = -1
			return c
		}(), wantErr: true},
		{name: "FlushInterval 为负", cfg: func() Config {
			c := Default()
			c.Scheduler.FlushInterval = Duration(-1)
			return c
		}(), wantErr: true},
		{name: "CompactInterval 为负", cfg: func() Config {
			c := Default()
			c.Scheduler.CompactInterval = Duration(-1)
			return c
		}(), wantErr: true},
		{name: "WALCleanInterval 为负", cfg: func() Config {
			c := Default()
			c.Scheduler.WALCleanInterval = Duration(-1)
			return c
		}(), wantErr: true},
		{name: "WALCleanThreshold 为负", cfg: func() Config {
			c := Default()
			c.Scheduler.WALCleanThreshold = -1
			return c
		}(), wantErr: true},
		{name: "调度器禁用时负值不校验", cfg: func() Config {
			c := Default()
			c.Scheduler.Enabled = false
			c.Scheduler.FlushInterval = Duration(-1)
			return c
		}(), wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "widb.yaml")
	content := `server:
  tcp_addr: "127.0.0.1:7000"
  http_addr: "127.0.0.1:7001"
storage:
  data_dir: "/tmp/widb-data"
  max_memtable_size: 33554432
scheduler:
  enabled: true
  flush_interval: 2s
  compact_interval: 7s
  wal_clean_interval: 1m
  wal_clean_threshold: 10485760
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Server.TCPAddr != "127.0.0.1:7000" {
		t.Errorf("TCPAddr = %q, want 127.0.0.1:7000", cfg.Server.TCPAddr)
	}
	if cfg.Server.HTTPAddr != "127.0.0.1:7001" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:7001", cfg.Server.HTTPAddr)
	}
	if cfg.Storage.DataDir != "/tmp/widb-data" {
		t.Errorf("DataDir = %q, want /tmp/widb-data", cfg.Storage.DataDir)
	}
	if cfg.Storage.MaxMemTableSize != 33554432 {
		t.Errorf("MaxMemTableSize = %d, want 33554432", cfg.Storage.MaxMemTableSize)
	}
	if time.Duration(cfg.Scheduler.FlushInterval) != 2*time.Second {
		t.Errorf("FlushInterval = %v, want 2s", cfg.Scheduler.FlushInterval)
	}
	if time.Duration(cfg.Scheduler.WALCleanInterval) != time.Minute {
		t.Errorf("WALCleanInterval = %v, want 1m", cfg.Scheduler.WALCleanInterval)
	}
	if cfg.Scheduler.WALCleanThreshold != 10485760 {
		t.Errorf("WALCleanThreshold = %d, want 10485760", cfg.Scheduler.WALCleanThreshold)
	}
}

func TestLoadFillsDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "widb.yaml")
	// 仅设置部分字段，其余应使用默认值
	content := `server:
  tcp_addr: "127.0.0.1:7000"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Server.TCPAddr != "127.0.0.1:7000" {
		t.Errorf("TCPAddr = %q, want 127.0.0.1:7000", cfg.Server.TCPAddr)
	}
	// 未设置的字段应保留默认值
	if cfg.Server.HTTPAddr != "0.0.0.0:8080" {
		t.Errorf("HTTPAddr = %q, want 默认 0.0.0.0:8080", cfg.Server.HTTPAddr)
	}
	if cfg.Storage.DataDir != "./data" {
		t.Errorf("DataDir = %q, want 默认 ./data", cfg.Storage.DataDir)
	}
	if !cfg.Scheduler.Enabled {
		t.Error("Scheduler.Enabled = false, want 默认 true")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("预期 Load 缺失文件返回错误，但返回 nil")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("server: [invalid\n  not yaml"), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("预期 Load 非法 YAML 返回错误，但返回 nil")
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	content := `scheduler:
  enabled: true
  flush_interval: not-a-duration
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("预期 Load 非法 duration 返回错误，但返回 nil")
	}
}

func TestLoadInvalidValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	content := `storage:
  max_memtable_size: 0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("预期 Load 非法值返回错误，但返回 nil")
	}
}

func TestGenerateTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "widb.yaml")
	if err := GenerateTemplate(path); err != nil {
		t.Fatalf("GenerateTemplate 失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取生成的模板失败: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("生成的模板为空")
	}
	// 生成的模板应可被 Load 正确加载
	if _, err := Load(path); err != nil {
		t.Errorf("生成的模板无法被 Load 加载: %v", err)
	}
}

func TestGenerateTemplateCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "deep", "widb.yaml")
	if err := GenerateTemplate(path); err != nil {
		t.Fatalf("GenerateTemplate 失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("模板文件未创建: %v", err)
	}
}

func TestGenerateTemplateRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "widb.yaml")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("写入占位文件失败: %v", err)
	}
	err := GenerateTemplate(path)
	if err == nil {
		t.Fatal("预期 GenerateTemplate 拒绝覆盖已存在文件，但返回 nil")
	}
}

func TestResolvePathExplicit(t *testing.T) {
	dir := t.TempDir()
	got := ResolvePath("/custom/path.yaml", dir)
	if got != "/custom/path.yaml" {
		t.Errorf("ResolvePath = %q, want /custom/path.yaml", got)
	}
}

func TestResolvePathEmptyConfigFindsWidbYaml(t *testing.T) {
	dir := t.TempDir()
	widbPath := filepath.Join(dir, "widb.yaml")
	if err := os.WriteFile(widbPath, []byte("server: {}"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	got := ResolvePath("", dir)
	if got != widbPath {
		t.Errorf("ResolvePath = %q, want %q", got, widbPath)
	}
}

func TestResolvePathFallsBackToConfigYaml(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("server: {}"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	got := ResolvePath("", dir)
	if got != configPath {
		t.Errorf("ResolvePath = %q, want %q", got, configPath)
	}
}

func TestResolvePathPrefersWidbOverConfig(t *testing.T) {
	dir := t.TempDir()
	widbPath := filepath.Join(dir, "widb.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(widbPath, []byte("server: {}"), 0o644); err != nil {
		t.Fatalf("写入 widb.yaml 失败: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("server: {}"), 0o644); err != nil {
		t.Fatalf("写入 config.yaml 失败: %v", err)
	}
	got := ResolvePath("", dir)
	if got != widbPath {
		t.Errorf("ResolvePath = %q, want 优先 widb.yaml %q", got, widbPath)
	}
}

func TestResolvePathNoneExist(t *testing.T) {
	dir := t.TempDir()
	got := ResolvePath("", dir)
	if got != "" {
		t.Errorf("ResolvePath = %q, want 空字符串", got)
	}
}

func TestDurationUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    time.Duration
		wantErr bool
	}{
		{name: "秒", yaml: "5s", want: 5 * time.Second, wantErr: false},
		{name: "分钟", yaml: "1m", want: time.Minute, wantErr: false},
		{name: "复合", yaml: "1m30s", want: 90 * time.Second, wantErr: false},
		{name: "毫秒", yaml: "200ms", want: 200 * time.Millisecond, wantErr: false},
		{name: "非法", yaml: "not-a-duration", want: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "scheduler:\n  flush_interval: " + tt.yaml + "\n"
			dir := t.TempDir()
			path := filepath.Join(dir, "widb.yaml")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("写入文件失败: %v", err)
			}
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("预期 Load 返回错误，但返回 nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load 失败: %v", err)
			}
			got := time.Duration(cfg.Scheduler.FlushInterval)
			if got != tt.want {
				t.Errorf("FlushInterval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateTemplateContentHasComments(t *testing.T) {
	if defaultTemplate == "" {
		t.Fatal("嵌入的模板为空")
	}
	// 模板应包含注释与关键配置段
	for _, want := range []string{"server:", "storage:", "scheduler:", "tcp_addr", "data_dir", "flush_interval"} {
		if !strings.Contains(defaultTemplate, want) {
			t.Errorf("模板缺少关键内容 %q", want)
		}
	}
}
