package storage

import (
	"log"
	"os"
)

// recoverOpen 尝试重新打开 WAL 文件用于错误恢复，失败时记录日志。
func (w *WAL) recoverOpen() {
	f, err := os.OpenFile(w.path, os.O_RDWR|os.O_CREATE, 0644)
	if err == nil {
		w.file = f
	} else {
		log.Printf("wal: recovery open failed: %v", err)
	}
}

// logClose 记录文件关闭错误。
func logClose(f *os.File) {
	if err := f.Close(); err != nil {
		log.Printf("wal: close file in error path: %v", err)
	}
}

// logRemove 记录文件删除错误。
func logRemove(path string) {
	if err := os.Remove(path); err != nil {
		log.Printf("wal: remove file in error path: %v", err)
	}
}
