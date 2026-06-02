package query

import (
	"fmt"

	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// BuildExecutor 根据查询计划树构建执行器树。
// iter 为 ScanIterator 工厂函数，根据表名返回对应的扫描迭代器。
func BuildExecutor(plan PlanNode, iterFn func(table string) storage.ScanIterator) (Executor, error) {
	return buildExecutor(plan, iterFn)
}

// buildExecutor 递归构建执行器树。
func buildExecutor(plan PlanNode, iterFn func(table string) storage.ScanIterator) (Executor, error) {
	switch n := plan.(type) {
	case *ScanNode:
		return buildScanExecutor(n, iterFn)
	case *FilterNode:
		return buildFilterExecutor(n, iterFn)
	case *ProjectNode:
		return buildProjectExecutor(n, iterFn)
	case *AggregateNode:
		return nil, fmt.Errorf("build executor: AggregateNode not yet supported")
	case *LimitNode:
		return buildLimitExecutor(n, iterFn)
	default:
		return nil, fmt.Errorf("build executor: unsupported plan node type %T", plan)
	}
}

// buildScanExecutor 构建 ScanExecutor。
func buildScanExecutor(n *ScanNode, iterFn func(table string) storage.ScanIterator) (Executor, error) {
	iter := iterFn(n.Table)
	return NewScanExecutor(iter, n.Schema()), nil
}

// buildFilterExecutor 构建 FilterExecutor。
func buildFilterExecutor(n *FilterNode, iterFn func(table string) storage.ScanIterator) (Executor, error) {
	child, err := buildExecutor(n.Child, iterFn)
	if err != nil {
		return nil, fmt.Errorf("build filter: %w", err)
	}
	return NewFilterExecutor(child, n.Condition), nil
}

// buildProjectExecutor 构建 ProjectExecutor。
func buildProjectExecutor(n *ProjectNode, iterFn func(table string) storage.ScanIterator) (Executor, error) {
	child, err := buildExecutor(n.Child, iterFn)
	if err != nil {
		return nil, fmt.Errorf("build project: %w", err)
	}
	return NewProjectExecutor(child, n.Expressions, n.Aliases, n.Schema()), nil
}

// buildLimitExecutor 构建 LimitExecutor。
func buildLimitExecutor(n *LimitNode, iterFn func(table string) storage.ScanIterator) (Executor, error) {
	child, err := buildExecutor(n.Child, iterFn)
	if err != nil {
		return nil, fmt.Errorf("build limit: %w", err)
	}
	return NewLimitExecutor(child, n.Offset, n.Count), nil
}
