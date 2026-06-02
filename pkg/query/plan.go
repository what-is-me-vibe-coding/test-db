package query

import (
	"fmt"
	"strings"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

type PlanNode interface {
	planNode()
	Schema() []ColumnDef
	Children() []PlanNode
	String() string
}

type ScanNode struct {
	Table     string
	Columns   []string
	Predicate Expression
	schema    []ColumnDef
}

func (n *ScanNode) planNode() {}

func (n *ScanNode) Schema() []ColumnDef {
	return n.schema
}

func (n *ScanNode) Children() []PlanNode {
	return nil
}

func (n *ScanNode) String() string {
	pred := ""
	if n.Predicate != nil {
		pred = fmt.Sprintf(", Predicate: %s", n.Predicate.String())
	}
	return fmt.Sprintf("Scan(Table: %s, Columns: %v%s)", n.Table, n.Columns, pred)
}

type FilterNode struct {
	Child     PlanNode
	Condition Expression
}

func (n *FilterNode) planNode() {}

func (n *FilterNode) Schema() []ColumnDef {
	return n.Child.Schema()
}

func (n *FilterNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *FilterNode) String() string {
	return fmt.Sprintf("Filter(Condition: %s, %s)", n.Condition.String(), n.Child.String())
}

type ProjectNode struct {
	Child       PlanNode
	Expressions []Expression
	Aliases     []string
	schema      []ColumnDef
}

func (n *ProjectNode) planNode() {}

func (n *ProjectNode) Schema() []ColumnDef {
	return n.schema
}

func (n *ProjectNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *ProjectNode) String() string {
	exprs := make([]string, len(n.Expressions))
	for i, e := range n.Expressions {
		if n.Aliases[i] != "" {
			exprs[i] = fmt.Sprintf("%s AS %s", e.String(), n.Aliases[i])
		} else {
			exprs[i] = e.String()
		}
	}
	return fmt.Sprintf("Project(%s, %s)", strings.Join(exprs, ", "), n.Child.String())
}

type AggregateNode struct {
	Child      PlanNode
	GroupBy    []Expression
	Aggregates []AggregateExpr
	schema     []ColumnDef
}

func (n *AggregateNode) planNode() {}

func (n *AggregateNode) Schema() []ColumnDef {
	return n.schema
}

func (n *AggregateNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *AggregateNode) String() string {
	var b strings.Builder
	b.WriteString("Aggregate(")
	if len(n.GroupBy) > 0 {
		b.WriteString("GroupBy: [")
		for i, g := range n.GroupBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(g.String())
		}
		b.WriteString("], ")
	}
	b.WriteString("Aggs: [")
	for i, a := range n.Aggregates {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.String())
	}
	b.WriteString("], ")
	b.WriteString(n.Child.String())
	b.WriteString(")")
	return b.String()
}

type LimitNode struct {
	Child  PlanNode
	Offset uint64
	Count  uint64
}

func (n *LimitNode) planNode() {}

func (n *LimitNode) Schema() []ColumnDef {
	return n.Child.Schema()
}

func (n *LimitNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *LimitNode) String() string {
	return fmt.Sprintf("Limit(Offset: %d, Count: %d, %s)", n.Offset, n.Count, n.Child.String())
}

type AggregateFunc int

const (
	AggCount AggregateFunc = iota
	AggSum
	AggMin
	AggMax
	AggAvg
)

func (f AggregateFunc) String() string {
	switch f {
	case AggCount:
		return "COUNT"
	case AggSum:
		return "SUM"
	case AggMin:
		return "MIN"
	case AggMax:
		return "MAX"
	case AggAvg:
		return "AVG"
	default:
		return "UNKNOWN"
	}
}

type AggregateExpr struct {
	Func AggregateFunc
	Arg  Expression
}

func (e AggregateExpr) String() string {
	if e.Arg == nil {
		return fmt.Sprintf("%s(*)", e.Func)
	}
	return fmt.Sprintf("%s(%s)", e.Func, e.Arg.String())
}

func inferAggReturnType(agg AggregateExpr) common.DataType {
	if agg.Func == AggCount {
		return common.TypeInt64
	}
	if lit, ok := agg.Arg.(*LiteralExpr); ok {
		return lit.Value.Typ
	}
	if col, ok := agg.Arg.(*ColumnExpr); ok {
		return col.typ
	}
	return common.TypeNull
}

type ResolvedColumnExpr struct {
	Name string
	Idx  int
	typ  common.DataType
}

func (e *ResolvedColumnExpr) exprNode()      {}
func (e *ResolvedColumnExpr) String() string { return e.Name }

func exprReturnType(e Expression) common.DataType {
	switch v := e.(type) {
	case *LiteralExpr:
		return v.Value.Typ
	case *ResolvedColumnExpr:
		return v.typ
	case *ColumnExpr:
		return v.typ
	case *BinaryExpr:
		return inferBinaryReturnType(v)
	case *UnaryExpr:
		if v.Op == OpNot {
			return common.TypeBool
		}
		return exprReturnType(v.Expr)
	case *FuncExpr:
		return inferFuncReturnType(v)
	case *StarExpr:
		return common.TypeNull
	}
	return common.TypeNull
}

func inferBinaryReturnType(e *BinaryExpr) common.DataType {
	switch e.Op {
	case OpAnd, OpOr:
		return common.TypeBool
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpLike:
		return common.TypeBool
	case OpAdd, OpSub, OpMul, OpDiv:
		lt := exprReturnType(e.Left)
		rt := exprReturnType(e.Right)
		if lt == common.TypeFloat64 || rt == common.TypeFloat64 {
			return common.TypeFloat64
		}
		return common.TypeInt64
	}
	return common.TypeNull
}

func inferFuncReturnType(e *FuncExpr) common.DataType {
	switch strings.ToLower(e.Name) {
	case "count":
		return common.TypeInt64
	case "sum":
		if len(e.Args) > 0 {
			return exprReturnType(e.Args[0])
		}
	case "avg":
		return common.TypeFloat64
	case "min", "max":
		if len(e.Args) > 0 {
			return exprReturnType(e.Args[0])
		}
	}
	return common.TypeNull
}
