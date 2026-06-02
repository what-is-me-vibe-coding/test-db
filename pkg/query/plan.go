package query

import (
	"fmt"
	"strings"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// PlanNode 表示查询计划中的一个算子节点。
type PlanNode interface {
	planNode()
	// Schema 返回该算子输出的列定义。
	Schema() []ColumnDef
	// Children 返回子节点列表。
	Children() []PlanNode
	// String 返回计划节点的可读表示。
	String() string
}

// ScanNode 表示全表扫描算子。
type ScanNode struct {
	Table     string
	Columns   []string
	Predicate Expression
	schema    []ColumnDef
}

func (n *ScanNode) planNode() {}

// Schema 返回 ScanNode 的输出列定义。
func (n *ScanNode) Schema() []ColumnDef { return n.schema }

// Children 返回 ScanNode 的子节点（无子节点）。
func (n *ScanNode) Children() []PlanNode { return nil }

// String 返回 ScanNode 的可读表示。
func (n *ScanNode) String() string {
	cols := strings.Join(n.Columns, ", ")
	s := fmt.Sprintf("Scan(%s, [%s])", n.Table, cols)
	if n.Predicate != nil {
		s += fmt.Sprintf(" WHERE %s", n.Predicate.String())
	}
	return s
}

// FilterNode 表示过滤算子。
type FilterNode struct {
	Child     PlanNode
	Condition Expression
}

func (n *FilterNode) planNode() {}

// Schema 返回 FilterNode 的输出列定义（与子节点相同）。
func (n *FilterNode) Schema() []ColumnDef { return n.Child.Schema() }

// Children 返回 FilterNode 的子节点。
func (n *FilterNode) Children() []PlanNode { return []PlanNode{n.Child} }

// String 返回 FilterNode 的可读表示。
func (n *FilterNode) String() string {
	return fmt.Sprintf("Filter(%s)", n.Condition.String())
}

// ProjectNode 表示投影算子。
type ProjectNode struct {
	Child       PlanNode
	Expressions []Expression
	Aliases     []string
	schema      []ColumnDef
}

func (n *ProjectNode) planNode() {}

// Schema 返回 ProjectNode 的输出列定义。
func (n *ProjectNode) Schema() []ColumnDef { return n.schema }

// Children 返回 ProjectNode 的子节点。
func (n *ProjectNode) Children() []PlanNode { return []PlanNode{n.Child} }

// String 返回 ProjectNode 的可读表示。
func (n *ProjectNode) String() string {
	exprs := make([]string, len(n.Expressions))
	for i, e := range n.Expressions {
		if i < len(n.Aliases) && n.Aliases[i] != "" {
			exprs[i] = fmt.Sprintf("%s AS %s", e.String(), n.Aliases[i])
		} else {
			exprs[i] = e.String()
		}
	}
	return fmt.Sprintf("Project([%s])", strings.Join(exprs, ", "))
}

// AggregateExpr 表示聚合函数表达式。
type AggregateExpr struct {
	Func string
	Arg  Expression
}

// String 返回 AggregateExpr 的可读表示。
func (a AggregateExpr) String() string {
	if a.Arg != nil {
		return fmt.Sprintf("%s(%s)", a.Func, a.Arg.String())
	}
	return fmt.Sprintf("%s(*)", a.Func)
}

// AggregateNode 表示聚合算子。
type AggregateNode struct {
	Child      PlanNode
	GroupBy    []Expression
	Aggregates []AggregateExpr
	schema     []ColumnDef
}

func (n *AggregateNode) planNode() {}

// Schema 返回 AggregateNode 的输出列定义。
func (n *AggregateNode) Schema() []ColumnDef { return n.schema }

// Children 返回 AggregateNode 的子节点。
func (n *AggregateNode) Children() []PlanNode { return []PlanNode{n.Child} }

// String 返回 AggregateNode 的可读表示。
func (n *AggregateNode) String() string {
	groups := make([]string, len(n.GroupBy))
	for i, g := range n.GroupBy {
		groups[i] = g.String()
	}
	aggs := make([]string, len(n.Aggregates))
	for i, a := range n.Aggregates {
		aggs[i] = a.String()
	}
	return fmt.Sprintf("Aggregate(groupBy=[%s], aggs=[%s])",
		strings.Join(groups, ", "), strings.Join(aggs, ", "))
}

// LimitNode 表示 LIMIT/OFFSET 算子。
type LimitNode struct {
	Child  PlanNode
	Offset uint64
	Count  uint64
}

func (n *LimitNode) planNode() {}

// Schema 返回 LimitNode 的输出列定义（与子节点相同）。
func (n *LimitNode) Schema() []ColumnDef { return n.Child.Schema() }

// Children 返回 LimitNode 的子节点。
func (n *LimitNode) Children() []PlanNode { return []PlanNode{n.Child} }

// String 返回 LimitNode 的可读表示。
func (n *LimitNode) String() string {
	if n.Offset > 0 {
		return fmt.Sprintf("Limit(offset=%d, count=%d)", n.Offset, n.Count)
	}
	return fmt.Sprintf("Limit(count=%d)", n.Count)
}

// ResolvedColumnExpr 表示已解析的列引用，包含列在 Schema 中的索引。
type ResolvedColumnExpr struct {
	Name string
	Idx  int
	typ  common.DataType
}

func (e *ResolvedColumnExpr) exprNode() {}
func (e *ResolvedColumnExpr) String() string {
	return fmt.Sprintf("$%d(%s)", e.Idx, e.Name)
}
