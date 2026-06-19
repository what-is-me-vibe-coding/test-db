// Package cmdutil 的单元测试。
package cmdutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/config"
)

// TestNewServerFlags_Defaults 验证默认值与历史行为一致：所有「覆盖型」flag 为零值。
func TestNewServerFlags_Defaults(t *testing.T) {
	f := NewServerFlags("test")
	if *f.TCPAddr != "" {
		t.Errorf("TCPAddr 默认值 = %q, want 空", *f.TCPAddr)
	}
	if *f.HTTPAddr != "" {
		t.Errorf("HTTPAddr 默认值 = %q, want 空", *f.HTTPAddr)
	}
	if *f.PGAddr != "" {
		t.Errorf("PGAddr 默认值 = %q, want 空", *f.PGAddr)
	}
	if *f.DataDir != "" {
		t.Errorf("DataDir 默认值 = %q, want 空", *f.DataDir)
	}
	if *f.MaxMemTableSize != 0 {
		t.Errorf("MaxMemTableSize 默认值 = %d, want 0", *f.MaxMemTableSize)
	}
	if *f.EnableScheduler {
		t.Error("EnableScheduler 默认值 = true, want false")
	}
}

// TestServerFlags_ApplyOverrides_Partial 验证仅覆盖传入的 flag，其他保留默认值。
func TestServerFlags_ApplyOverrides_Partial(t *testing.T) {
	f := NewServerFlags("test")
	if err := f.FS.Parse([]string{
		"-tcp", "127.0.0.1:9999",
		"-data", "/custom/data",
		"-max-memtable", "12345",
		"-scheduler.flush-interval", "7s",
	}); err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}

	cfg := config.Default()
	f.ApplyOverrides(&cfg)

	if cfg.Server.TCPAddr != "127.0.0.1:9999" {
		t.Errorf("TCPAddr = %q, want 127.0.0.1:9999", cfg.Server.TCPAddr)
	}
	// 未覆盖的字段保留 config.Default() 中的值
	if cfg.Server.HTTPAddr != "0.0.0.0:8080" {
		t.Errorf("HTTPAddr = %q, want 默认 0.0.0.0:8080", cfg.Server.HTTPAddr)
	}
	if cfg.Storage.DataDir != "/custom/data" {
		t.Errorf("DataDir = %q, want /custom/data", cfg.Storage.DataDir)
	}
	if cfg.Storage.MaxMemTableSize != 12345 {
		t.Errorf("MaxMemTableSize = %d, want 12345", cfg.Storage.MaxMemTableSize)
	}
	if time.Duration(cfg.Scheduler.FlushInterval) != 7*time.Second {
		t.Errorf("FlushInterval = %v, want 7s", cfg.Scheduler.FlushInterval)
	}
	// 未覆盖的调度器字段保留默认 10s
	if time.Duration(cfg.Scheduler.CompactInterval) != 10*time.Second {
		t.Errorf("CompactInterval = %v, want 默认 10s", cfg.Scheduler.CompactInterval)
	}
}

// TestServerFlags_ApplyOverrides_None 验证未传任何参数时不修改 cfg。
func TestServerFlags_ApplyOverrides_None(t *testing.T) {
	f := NewServerFlags("test")
	if err := f.FS.Parse([]string{}); err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}

	cfg := config.Default()
	f.ApplyOverrides(&cfg)

	if cfg.Server.TCPAddr != "0.0.0.0:9000" {
		t.Errorf("TCPAddr = %q, want 默认 0.0.0.0:9000", cfg.Server.TCPAddr)
	}
	if cfg.Storage.DataDir != "./data" {
		t.Errorf("DataDir = %q, want 默认 ./data", cfg.Storage.DataDir)
	}
}

// TestServerFlags_ApplyOverrides_AllFlags 覆盖所有 flag 字段。
func TestServerFlags_ApplyOverrides_AllFlags(t *testing.T) {
	f := NewServerFlags("test")
	if err := f.FS.Parse([]string{
		"-tcp", "127.0.0.1:1",
		"-http", "127.0.0.1:2",
		"-pg", "127.0.0.1:3",
		"-data", "/d",
		"-max-memtable", "1",
		"-scheduler",
		"-scheduler.flush-interval", "1s",
		"-scheduler.compact-interval", "2s",
		"-scheduler.wal-clean-interval", "3s",
		"-scheduler.wal-clean-threshold", "4",
	}); err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	cfg := config.Default()
	f.ApplyOverrides(&cfg)

	if cfg.Server.PGAddr != "127.0.0.1:3" {
		t.Errorf("PGAddr = %q, want 127.0.0.1:3", cfg.Server.PGAddr)
	}
	if !cfg.Scheduler.Enabled {
		t.Error("Scheduler.Enabled = false, want true")
	}
	if cfg.Scheduler.WALCleanThreshold != 4 {
		t.Errorf("WALCleanThreshold = %d, want 4", cfg.Scheduler.WALCleanThreshold)
	}
}

// TestSetFlags 验证 SetFlags 正确报告哪些 flag 被显式设置。
func TestSetFlags(t *testing.T) {
	f := NewServerFlags("test")
	if err := f.FS.Parse([]string{"-tcp", "127.0.0.1:1", "-data", "/x"}); err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	set := f.SetFlags()
	if !set["tcp"] || !set["data"] {
		t.Errorf("SetFlags 缺漏: %v", set)
	}
	if set["http"] {
		t.Error("未传入 -http, SetFlags 不应报告 http")
	}
}

// TestToServerConfig 验证 YAML 配置到服务层配置的转换。
func TestToServerConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Server.TCPAddr = "127.0.0.1:7000"
	cfg.Server.HTTPAddr = "127.0.0.1:7001"
	cfg.Server.PGAddr = "127.0.0.1:7002"
	cfg.Storage.DataDir = "/tmp/data"
	cfg.Storage.MaxMemTableSize = 1024
	cfg.Scheduler.Enabled = false
	cfg.Scheduler.FlushInterval = config.Duration(3 * time.Second)
	cfg.Scheduler.CompactInterval = config.Duration(6 * time.Second)
	cfg.Scheduler.WALCleanInterval = config.Duration(30 * time.Second)
	cfg.Scheduler.WALCleanThreshold = 2048

	got := ToServerConfig(cfg)
	if got.TCPAddr != "127.0.0.1:7000" {
		t.Errorf("TCPAddr = %q", got.TCPAddr)
	}
	if got.HTTPAddr != "127.0.0.1:7001" {
		t.Errorf("HTTPAddr = %q", got.HTTPAddr)
	}
	if got.PGAddr != "127.0.0.1:7002" {
		t.Errorf("PGAddr = %q", got.PGAddr)
	}
	if got.DataDir != "/tmp/data" {
		t.Errorf("DataDir = %q", got.DataDir)
	}
	if got.MaxMemTableSize != 1024 {
		t.Errorf("MaxMemTableSize = %d", got.MaxMemTableSize)
	}
	if got.EnableScheduler {
		t.Error("EnableScheduler = true, want false")
	}
	if got.SchedulerConfig.FlushInterval != 3*time.Second {
		t.Errorf("FlushInterval = %v, want 3s", got.SchedulerConfig.FlushInterval)
	}
	if got.SchedulerConfig.CompactInterval != 6*time.Second {
		t.Errorf("CompactInterval = %v, want 6s", got.SchedulerConfig.CompactInterval)
	}
	if got.SchedulerConfig.WALCleanInterval != 30*time.Second {
		t.Errorf("WALCleanInterval = %v", got.SchedulerConfig.WALCleanInterval)
	}
	if got.SchedulerConfig.WALCleanThreshold != 2048 {
		t.Errorf("WALCleanThreshold = %d", got.SchedulerConfig.WALCleanThreshold)
	}
}

// TestLoadConfig_NotFound 验证指向不存在文件时返回错误。
func TestLoadConfig_NotFound(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Error("预期返回错误")
	}
}

// TestLoadConfig_Default 验证未找到任何配置文件时使用默认值。
func TestLoadConfig_Default(t *testing.T) {
	// 在临时目录中运行，确保默认文件名都不存在
	oldWD, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir 失败: %v", err)
	}

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.Server.TCPAddr != "0.0.0.0:9000" {
		t.Errorf("默认 TCPAddr = %q", cfg.Server.TCPAddr)
	}
}

// TestLoadConfig_FromYAML 验证从 YAML 文件加载配置。
func TestLoadConfig_FromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widb.yaml")
	if err := config.GenerateTemplate(path); err != nil {
		t.Fatalf("生成模板失败: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.Storage.DataDir != "./data" {
		t.Errorf("DataDir = %q, want ./data", cfg.Storage.DataDir)
	}
}
