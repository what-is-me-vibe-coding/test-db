package query

import (
	"fmt"
	"strings"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// groupRow 存储分组键对应的原始行数据。
type groupRow struct {
	key    string
	values map[string]common.Value
}

// accumulator 聚合函数累加器。
type accumulator struct {
	funcType AggregateFunc
	count    int64
	sum      float64
	minVal   common.Value
	maxVal   common.Value
	hasValue bool
}

func newAccumulators(aggs []AggregateExpr) []accumulator {
	accs := make([]accumulator, len(aggs))
	for i, agg := range aggs {
		accs[i].funcType = agg.Func
	}
	return accs
}

func (a *accumulator) update(val common.Value) {
	switch a.funcType {
	case AggCount:
		a.updateCount()
	case AggSum:
		a.updateSum(val)
	case AggMin:
		a.updateMin(val)
	case AggMax:
		a.updateMax(val)
	case AggAvg:
		a.updateAvg(val)
	}
}

func (a *accumulator) updateCount() {
	a.count++
}

func (a *accumulator) updateSum(val common.Value) {
	if val.Valid {
		a.count++
		a.sum += toFloat64(val)
	}
}

func (a *accumulator) updateMin(val common.Value) {
	if val.Valid {
		if !a.hasValue || val.Less(a.minVal) {
			a.minVal = val
		}
		a.hasValue = true
	}
}

func (a *accumulator) updateMax(val common.Value) {
	if val.Valid {
		if !a.hasValue || a.maxVal.Less(val) {
			a.maxVal = val
		}
		a.hasValue = true
	}
}

func (a *accumulator) updateAvg(val common.Value) {
	if val.Valid {
		a.count++
		a.sum += toFloat64(val)
	}
}

func (a *accumulator) result() common.Value {
	switch a.funcType {
	case AggCount:
		return common.NewInt64(a.count)
	case AggSum:
		if a.count == 0 {
			return common.NewNull()
		}
		return common.NewFloat64(a.sum)
	case AggMin:
		if !a.hasValue {
			return common.NewNull()
		}
		return a.minVal
	case AggMax:
		if !a.hasValue {
			return common.NewNull()
		}
		return a.maxVal
	case AggAvg:
		if a.count == 0 {
			return common.NewNull()
		}
		return common.NewFloat64(a.sum / float64(a.count))
	}
	return common.NewNull()
}

// executeAggregate 执行 AggregateNode。
func (e *Executor) executeAggregate(agg *AggregateNode) (*execResult, error) {
	childResult, err := e.executeNode(agg.Child)
	if err != nil {
		return nil, err
	}

	inputSchema := childResult.schema
	colIdxMap := buildColIdxMapFromSchema(inputSchema)

	groupAccum, groupRows, groupOrder := e.aggregateRows(agg, childResult, inputSchema, colIdxMap)

	schema := agg.Schema()
	outputCols, err := e.buildAggregateOutput(agg, schema, groupAccum, groupRows, groupOrder, colIdxMap)
	if err != nil {
		return nil, err
	}

	output := storage.NewChunk(defaultChunkSize)
	for _, col := range outputCols {
		if err := output.AddColumn(col); err != nil {
			return nil, fmt.Errorf("executor aggregate: %w", err)
		}
	}

	return &execResult{chunks: []*storage.Chunk{output}, schema: schema}, nil
}

func (e *Executor) aggregateRows(agg *AggregateNode, childResult *execResult, inputSchema []ColumnDef, colIdxMap map[string]int) (map[string][]accumulator, map[string]*groupRow, []string) {
	// 快速路径：当所有 GROUP BY 与聚合参数均为列引用（或 COUNT(*)）时，
	// 直接按列索引读取列向量，跳过逐行 map 构建与全列读取，对宽表显著降低每行开销。
	if plan, ok := trySimpleAggregatePlan(agg, colIdxMap); ok {
		return e.aggregateRowsFast(agg, childResult, plan)
	}

	groupAccum := make(map[string][]accumulator)
	groupRows := make(map[string]*groupRow)
	groupOrder := make([]string, 0)

	// 复用 rowVals map，减少聚合路径的堆分配
	var rowValsBuf map[string]common.Value
	for _, chunk := range childResult.chunks {
		for row := uint32(0); row < chunk.RowCount(); row++ {
			rowVals := buildRowValues(chunk, inputSchema, row, rowValsBuf)
			rowValsBuf = rowVals // 后续行复用同一 map
			groupKey := buildGroupKey(agg.GroupBy, rowVals, colIdxMap)

			if _, ok := groupAccum[groupKey]; !ok {
				groupAccum[groupKey] = newAccumulators(agg.Aggregates)
				groupRows[groupKey] = newGroupRow(groupKey, rowVals)
				groupOrder = append(groupOrder, groupKey)
			}

			e.updateAccumulators(groupAccum[groupKey], agg.Aggregates, rowVals, colIdxMap)
		}
	}

	if len(groupOrder) == 0 {
		groupOrder = append(groupOrder, "")
		groupAccum[""] = newAccumulators(agg.Aggregates)
		groupRows[""] = &groupRow{key: "", values: nil}
	}

	return groupAccum, groupRows, groupOrder
}

// simpleAggPlan 描述可走快速路径的聚合计划。
// gbIdx 为 GROUP BY 列在 inputSchema 中的索引；argIdx 为聚合参数列索引，
// COUNT(*) 的参数为 nil，对应位置存 -1（实际更新时按 Arg==nil 判定，不访问该索引）。
type simpleAggPlan struct {
	gbIdx  []int
	argIdx []int
}

// trySimpleAggregatePlan 检查聚合是否可走快速路径。
// 条件：所有 GROUP BY 表达式与聚合参数均为 *ResolvedColumnExpr（COUNT(*) 参数为 nil）。
// 列索引按名称从 colIdxMap 解析，兼容列裁剪后的子集 schema（不依赖 ResolvedColumnExpr.Idx）。
func trySimpleAggregatePlan(agg *AggregateNode, colIdxMap map[string]int) (simpleAggPlan, bool) {
	plan := simpleAggPlan{
		gbIdx:  make([]int, len(agg.GroupBy)),
		argIdx: make([]int, len(agg.Aggregates)),
	}
	for i, gb := range agg.GroupBy {
		rc, ok := gb.(*ResolvedColumnExpr)
		if !ok {
			return simpleAggPlan{}, false
		}
		idx, ok := colIdxMap[rc.Name]
		if !ok {
			return simpleAggPlan{}, false
		}
		plan.gbIdx[i] = idx
	}
	for i, a := range agg.Aggregates {
		if a.Arg == nil {
			plan.argIdx[i] = -1
			continue
		}
		rc, ok := a.Arg.(*ResolvedColumnExpr)
		if !ok {
			return simpleAggPlan{}, false
		}
		idx, ok := colIdxMap[rc.Name]
		if !ok {
			return simpleAggPlan{}, false
		}
		plan.argIdx[i] = idx
	}
	return plan, true
}

// aggregateRowsFast 聚合快速路径：直接按列索引读取列向量，跳过逐行 map 构建。
// 仅读取 GROUP BY 与聚合参数引用的列，对宽表（列多但聚合引用少）显著减少每行工作量。
// 语义与慢速路径一致：分组键格式、首行分组值、累加器更新规则均保持相同。
func (e *Executor) aggregateRowsFast(agg *AggregateNode, childResult *execResult, plan simpleAggPlan) (map[string][]accumulator, map[string]*groupRow, []string) {
	groupAccum := make(map[string][]accumulator)
	groupRows := make(map[string]*groupRow)
	groupOrder := make([]string, 0)
	gbNames := simpleAggGroupByNames(agg)

	for _, chunk := range childResult.chunks {
		cols := chunk.Columns()
		rowCount := chunk.RowCount()
		for row := uint32(0); row < rowCount; row++ {
			groupKey := buildSimpleGroupKey(cols, plan.gbIdx, row)
			if _, ok := groupAccum[groupKey]; !ok {
				groupAccum[groupKey] = newAccumulators(agg.Aggregates)
				groupRows[groupKey] = newSimpleGroupRow(groupKey, cols, plan.gbIdx, gbNames, row)
				groupOrder = append(groupOrder, groupKey)
			}
			updateAccumulatorsFromCols(groupAccum[groupKey], agg.Aggregates, cols, plan.argIdx, row)
		}
	}

	if len(groupOrder) == 0 {
		groupOrder = append(groupOrder, "")
		groupAccum[""] = newAccumulators(agg.Aggregates)
		groupRows[""] = &groupRow{key: "", values: nil}
	}

	return groupAccum, groupRows, groupOrder
}

// simpleAggGroupByNames 提取 GROUP BY 列引用的名称，用于构建分组行的值映射。
// 调用前已由 trySimpleAggregatePlan 保证所有 GROUP BY 均为 *ResolvedColumnExpr。
func simpleAggGroupByNames(agg *AggregateNode) []string {
	names := make([]string, len(agg.GroupBy))
	for i, gb := range agg.GroupBy {
		names[i] = gb.(*ResolvedColumnExpr).Name
	}
	return names
}

// buildSimpleGroupKey 直接从列向量构建分组键，格式与 buildGroupKey 完全一致：
// 各分组列值的 String() 以 '\x00' 分隔，无分组列时返回空串。
func buildSimpleGroupKey(cols []*storage.ColumnVector, gbIdx []int, row uint32) string {
	if len(gbIdx) == 0 {
		return ""
	}
	var b strings.Builder
	for i, idx := range gbIdx {
		if i > 0 {
			b.WriteByte('\x00')
		}
		b.WriteString(cols[idx].GetValue(row).String())
	}
	return b.String()
}

// newSimpleGroupRow 创建分组行，仅保存 GROUP BY 列的值（按名称映射），
// 供 buildAggregateOutput 重新求值分组表达式。语义等价于慢速路径的 newGroupRow
// （后者保存全部列，但输出阶段仅读取 GROUP BY 列）。
func newSimpleGroupRow(key string, cols []*storage.ColumnVector, gbIdx []int, gbNames []string, row uint32) *groupRow {
	values := make(map[string]common.Value, len(gbIdx))
	for i, idx := range gbIdx {
		values[gbNames[i]] = cols[idx].GetValue(row)
	}
	return &groupRow{key: key, values: values}
}

// updateAccumulatorsFromCols 直接从列向量更新累加器，跳过 evalExpr 的 map 查找。
// COUNT(*)（Arg==nil）以 NewNull 更新（与慢速路径一致，updateCount 忽略入参）。
func updateAccumulatorsFromCols(accs []accumulator, aggs []AggregateExpr, cols []*storage.ColumnVector, argIdx []int, row uint32) {
	for i := range accs {
		if aggs[i].Arg == nil {
			accs[i].update(common.NewNull())
			continue
		}
		accs[i].update(cols[argIdx[i]].GetValue(row))
	}
}

// newGroupRow 创建分组行，复制当前行值避免后续行覆盖。
func newGroupRow(key string, rowVals map[string]common.Value) *groupRow {
	copied := make(map[string]common.Value, len(rowVals))
	for k, v := range rowVals {
		copied[k] = v
	}
	return &groupRow{key: key, values: copied}
}

// updateAccumulators 更新一组累加器。
func (e *Executor) updateAccumulators(accs []accumulator, aggs []AggregateExpr, rowVals map[string]common.Value, colIdxMap map[string]int) {
	for i := range accs {
		if aggs[i].Arg != nil {
			val, evalErr := evalExpr(aggs[i].Arg, rowVals, colIdxMap)
			if evalErr != nil {
				// 表达式求值失败时跳过该聚合列的更新，避免零值污染累加器
				continue
			}
			accs[i].update(val)
		} else {
			// COUNT(*) 等无参数聚合函数，直接更新
			accs[i].update(common.NewNull())
		}
	}
}

func (e *Executor) buildAggregateOutput(agg *AggregateNode, schema []ColumnDef, groupAccum map[string][]accumulator, groupRows map[string]*groupRow, groupOrder []string, colIdxMap map[string]int) ([]*storage.ColumnVector, error) {
	outputCols := make([]*storage.ColumnVector, len(schema))
	for i, colDef := range schema {
		outputCols[i] = storage.NewColumnVector(uint32(i), colDef.Type, uint32(len(groupOrder)))
	}

	for _, groupKey := range groupOrder {
		accs := groupAccum[groupKey]
		gr := groupRows[groupKey]
		colIdx := 0

		for _, gb := range agg.GroupBy {
			val, evalErr := evalExpr(gb, gr.values, colIdxMap)
			if evalErr != nil {
				val = common.NewNull()
			}
			if err := outputCols[colIdx].Append(coerceValue(val, schema[colIdx].Type)); err != nil {
				return nil, fmt.Errorf("aggregate output: group-by append: %w", err)
			}
			colIdx++
		}

		for _, acc := range accs {
			val := acc.result()
			if err := outputCols[colIdx].Append(coerceValue(val, schema[colIdx].Type)); err != nil {
				return nil, fmt.Errorf("aggregate output: aggregate append: %w", err)
			}
			colIdx++
		}
	}

	return outputCols, nil
}

// buildGroupKey 构建分组键。
// 使用 strings.Builder 避免创建临时字符串切片，减少内存分配。
// 使用 '\x00' 作为分隔符，避免列值中包含可打印字符时产生碰撞。
// 表达式求值失败时使用空字符串占位，确保分组键仍可正常构建。
func buildGroupKey(groupBy []Expression, row map[string]common.Value, colIdxMap map[string]int) string {
	if len(groupBy) == 0 {
		return ""
	}
	var b strings.Builder
	for i, gb := range groupBy {
		if i > 0 {
			b.WriteByte('\x00')
		}
		val, evalErr := evalExpr(gb, row, colIdxMap)
		if evalErr != nil {
			b.WriteString("<error>")
		} else {
			b.WriteString(val.String())
		}
	}
	return b.String()
}
