package storage

import (
	"io"
	"log"
	"os"
	"testing"
)

// TestMain 重定向标准 logger 输出，避免错误路径测试（`wal: close file`、
// `engine: warning: ... segment files failed to load` 等）累积的日志让 Go
// 测试框架的 testlog.txt 在 CI 环境下触达 2GB 文件大小上限，触发
// `testing: can't write ... file too large` 框架级失败。
//
// 行为：
//   - 单元测试运行（`go test`）期间标准 log 全部写入 io.Discard
//   - 保留 testing.T 与 testing.B 的常规 log 接口（不受影响）
//   - 不影响 coverage 测量与 race detector
//
// 关闭方式：设置 `STORAGE_TEST_VERBOSE_LOG=1` 环境变量时还原为 stderr，
// 便于本地调试。
func TestMain(m *testing.M) {
	if os.Getenv("STORAGE_TEST_VERBOSE_LOG") != "1" {
		log.SetOutput(io.Discard)
	}
	os.Exit(m.Run())
}
