// Package main 是 test-db 服务器的入口点。
//
// 命令行参数与配置加载逻辑委托给 pkg/cmdutil.ServerFlags / LoadConfig / ToServerConfig，
// 避免与 cmd/widb 重复维护同一套 flag 定义。本文件保留 cliFlags / newCLIFlags /
// applyOverrides / toServerConfig / loadConfig 等历史符号作为薄包装，
// 现有测试无需迁移。
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/what-is-me-vibe-coding/test-db/pkg/cmdutil"
	"github.com/what-is-me-vibe-coding/test-db/pkg/config"
	"github.com/what-is-me-vibe-coding/test-db/pkg/server"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// run 启动服务器并等待终止信号，用于支持测试。
func run(tcpAddr, httpAddr, dataDir string, maxMemTableSize int64, enableScheduler bool, schedulerCfg storage.SchedulerConfig, opts ...server.Option) error {
	cfg := server.Config{
		TCPAddr:         tcpAddr,
		HTTPAddr:        httpAddr,
		DataDir:         dataDir,
		MaxMemTableSize: maxMemTableSize,
		EnableScheduler: enableScheduler,
		SchedulerConfig: schedulerCfg,
	}

	srv, err := server.NewServer(cfg, opts...)
	if err != nil {
		return err
	}

	if err := srv.Start(); err != nil {
		return err
	}

	// 等待终止信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("收到信号 %v，正在关闭...", sig)

	return srv.Stop()
}

// cliFlags 是 cmd/server 历史测试所依赖的命令行参数封装，内部委托给 pkg/cmdutil.ServerFlags。
// 保留 cliFlags 名称与字段（fs / configPath / ...）以便现有测试代码无需修改。
type cliFlags struct {
	fs                *flag.FlagSet
	cmd               *cmdutil.ServerFlags
	configPath        *string
	genConfigPath     *string
	tcpAddr           *string
	httpAddr          *string
	pgAddr            *string
	dataDir           *string
	maxMemTableSize   *int64
	enableScheduler   *bool
	flushInterval     *time.Duration
	compactInterval   *time.Duration
	walCleanInterval  *time.Duration
	walCleanThreshold *int64
}

// newCLIFlags 构建命令行参数集，实际逻辑由 cmdutil.ServerFlags 提供。
func newCLIFlags() *cliFlags {
	cmd := cmdutil.NewServerFlags("test-db")
	return &cliFlags{
		fs:                cmd.FS,
		cmd:               cmd,
		configPath:        cmd.ConfigPath,
		genConfigPath:     cmd.GenConfigPath,
		tcpAddr:           cmd.TCPAddr,
		httpAddr:          cmd.HTTPAddr,
		pgAddr:            cmd.PGAddr,
		dataDir:           cmd.DataDir,
		maxMemTableSize:   cmd.MaxMemTableSize,
		enableScheduler:   cmd.EnableScheduler,
		flushInterval:     cmd.FlushInterval,
		compactInterval:   cmd.CompactInterval,
		walCleanInterval:  cmd.WALCleanInterval,
		walCleanThreshold: cmd.WALCleanThreshold,
	}
}

// applyOverrides 将显式设置的命令行参数覆盖到配置上。
func (c *cliFlags) applyOverrides(cfg *config.Config) {
	c.cmd.ApplyOverrides(cfg)
}

// toServerConfig 将 YAML 配置转换为服务层配置。
func toServerConfig(cfg config.Config) server.Config {
	return cmdutil.ToServerConfig(cfg)
}

// loadConfig 按分层策略加载配置：默认值 < 配置文件。
// configPath 非空时必须存在；为空时依次查找 ./widb.yaml、./config.yaml，均不存在则使用默认值。
func loadConfig(configPath string) (config.Config, error) {
	return cmdutil.LoadConfig(configPath)
}

// runMainWithArgs 解析命令行参数并启动服务器，返回退出码。
// 使用自定义 FlagSet 以支持在测试中多次调用。
func runMainWithArgs(args []string) int {
	c := newCLIFlags()

	if err := c.fs.Parse(args); err != nil {
		return 2
	}

	if *c.genConfigPath != "" {
		if err := config.GenerateTemplate(*c.genConfigPath); err != nil {
			log.Printf("生成配置模板失败: %v", err)
			return 1
		}
		log.Printf("已生成配置模板: %s", *c.genConfigPath)
		return 0
	}

	cfg, err := loadConfig(*c.configPath)
	if err != nil {
		log.Printf("加载配置失败: %v", err)
		return 1
	}

	c.applyOverrides(&cfg)

	if err := cfg.Validate(); err != nil {
		log.Printf("配置不合法: %v", err)
		return 1
	}

	serverCfg := toServerConfig(cfg)
	srv, err := server.NewServer(serverCfg)
	if err != nil {
		log.Printf("服务器错误: %v", err)
		return 1
	}

	if err := srv.Start(); err != nil {
		log.Printf("服务器错误: %v", err)
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("收到信号 %v，正在关闭...", sig)

	if err := srv.Stop(); err != nil {
		log.Printf("关闭错误: %v", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(runMainWithArgs(os.Args[1:]))
}
