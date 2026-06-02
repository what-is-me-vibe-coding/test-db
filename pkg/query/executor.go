package query

import (
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

const defaultChunkSize = 1024

// Executor 执行查询计划，按批次（Chunk）返回结果。
type Executor interface {
	// NextChunk 返回下一个结果批次。
	// 返回 nil 表示没有更多数据（EOF）。
	NextChunk() (*storage.Chunk, error)
	// Close 释放执行器持有的资源。
	Close()
	// Schema 返回输出结果的列定义。
	Schema() []ColumnDef
}

// buildColIndexMap 根据 schema 构建列名到 Chunk 列索引的映射。
func buildColIndexMap(schema []ColumnDef) map[string]int {
	m := make(map[string]int, len(schema))
	for i, col := range schema {
		m[col.Name] = i
	}
	return m
}
