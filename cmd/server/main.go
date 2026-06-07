// Package main 是 test-db 服务器的入口点。
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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

// runMainWithArgs 解析命令行参数并启动服务器，返回退出码。
// 使用自定义 FlagSet 以支持在测试中多次调用。
func runMainWithArgs(args []string) int {
	fs := flag.NewFlagSet("test-db", flag.ContinueOnError)
	tcpAddr := fs.String("tcp", "0.0.0.0:9000", "TCP 监听地址")
	httpAddr := fs.String("http", "0.0.0.0:8080", "HTTP 监听地址")
	dataDir := fs.String("data", "./data", "数据目录")
	maxMemTableSize := fs.Int64("max-memtable", 64*1024*1024, "MemTable 最大字节数")
	enableScheduler := fs.Bool("scheduler", true, "启用后台调度器（自动刷盘、Compaction、WAL 清理）")
	flushInterval := fs.Duration("scheduler.flush-interval", 5*time.Second, "自动刷盘检查间隔")
	compactInterval := fs.Duration("scheduler.compact-interval", 10*time.Second, "自动 Compaction 检查间隔")
	walCleanInterval := fs.Duration("scheduler.wal-clean-interval", 30*time.Second, "WAL 清理检查间隔")
	walCleanThreshold := fs.Int64("scheduler.wal-clean-threshold", 64<<20, "WAL 文件大小超过此阈值时清理旧文件（字节）")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	schedulerCfg := storage.SchedulerConfig{
		FlushInterval:     *flushInterval,
		CompactInterval:   *compactInterval,
		WALCleanInterval:  *walCleanInterval,
		WALCleanThreshold: *walCleanThreshold,
	}

	if err := run(*tcpAddr, *httpAddr, *dataDir, *maxMemTableSize, *enableScheduler, schedulerCfg); err != nil {
		log.Printf("服务器错误: %v", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(runMainWithArgs(os.Args[1:]))
}
