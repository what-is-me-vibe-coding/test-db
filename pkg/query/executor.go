package query

import (
	"fmt"
	"strings"

	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/index"
	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

const defaultChunkSize = 1024

// StorageProvider 提供查询执行所需的存储引擎访问能力。
type StorageProvider interface {
	ScanRange(start, end string) []storage.ScanEntry
	ColumnMeta() []storage.ColumnMeta
	PrimaryIndex() *index.PrimaryIndex
	SparseIndex() *index.SparseIndex
}

// Executor 执行查询计划，返回结果 Chunk 流。
type Executor struct {
	storage StorageProvider
}

// NewExecutor 创建一个新的 Executor。
func NewExecutor(sp StorageProvider) *Executor {
	return &Executor{storage: sp}
}

// Execute 执行查询计划节点，返回结果 Chunk 切片。
func (e *Executor) Execute(node PlanNode) ([]*storage.Chunk, error) {
	result, err := e.executeNode(node)
	if err != nil {
		return nil, err
	}
	return result.chunks, nil
}

// execResult 是执行结果，携带 Chunk 切片和对应的 schema。
type execResult struct {
	chunks []*storage.Chunk
	schema []ColumnDef
}

func (e *Executor) executeNode(node PlanNode) (*execResult, error) {
	switch n := node.(type) {
	case *ScanNode:
		return e.executeScan(n)
	case *FilterNode:
		return e.executeFilter(n)
	case *ProjectNode:
		return e.executeProject(n)
	case *AggregateNode:
		return e.executeAggregate(n)
	case *LimitNode:
		return e.executeLimit(n)
	default:
		return nil, fmt.Errorf("executor: unsupported plan node type %T", node)
	}
}

// executeScan 执行 ScanNode，从存储引擎读取数据并转换为 Chunk。
func (e *Executor) executeScan(scan *ScanNode) (*execResult, error) {
	entries := e.scanWithPredicate(scan)
	schema := scan.Schema()

	if len(entries) == 0 {
		return &execResult{chunks: nil, schema: schema}, nil
	}

	chunks := buildChunksFromEntries(entries, schema, defaultChunkSize)
	return &execResult{chunks: chunks, schema: schema}, nil
}

// scanWithPredicate 根据谓词从存储引擎获取数据。
func (e *Executor) scanWithPredicate(scan *ScanNode) []storage.ScanEntry {
	pred := scan.Predicate
	if pred == nil {
		return e.storage.ScanRange("", "\xff\xff\xff\xff")
	}

	keyRange := e.extractKeyRange(pred)
	entries := e.storage.ScanRange(keyRange.start, keyRange.end)

	return e.filterEntriesByPredicate(entries, pred, scan.Columns)
}

// keyRange 表示主键范围。
type keyRange struct {
	start string
	end   string
}

// extractKeyRange 从谓词中提取主键范围，用于缩小扫描范围。
func (e *Executor) extractKeyRange(pred Expression) keyRange {
	kr := keyRange{start: "", end: "\xff\xff\xff\xff"}

	conjuncts := splitConjuncts(pred)
	for _, c := range conjuncts {
		bin, ok := c.(*BinaryExpr)
		if !ok {
			continue
		}

		col, ok := bin.Left.(*ResolvedColumnExpr)
		if !ok {
			continue
		}

		if col.Idx != 0 {
			continue
		}

		lit, ok := bin.Right.(*LiteralExpr)
		if !ok || !lit.Value.Valid {
			continue
		}

		keyStr := lit.Value.String()
		switch bin.Op {
		case OpEq:
			kr.start = maxStr(kr.start, keyStr)
			kr.end = minStr(kr.end, keyStr)
		case OpGe:
			kr.start = maxStr(kr.start, keyStr)
		case OpGt:
			kr.start = maxStr(kr.start, keyStr)
		case OpLe:
			kr.end = minStr(kr.end, keyStr)
		case OpLt:
			kr.end = minStr(kr.end, keyStr)
		}
	}

	return kr
}

// filterEntriesByPredicate 使用谓词过滤扫描结果。
func (e *Executor) filterEntriesByPredicate(entries []storage.ScanEntry, pred Expression, cols []string) []storage.ScanEntry {
	colIdxMap := buildColIdxMap(cols)

	var result []storage.ScanEntry
	for _, entry := range entries {
		val, err := evalExpr(pred, entry.Value.Columns, colIdxMap)
		if err != nil {
			continue
		}
		if isTruthyValue(val) {
			result = append(result, entry)
		}
	}
	return result
}

// executeFilter 执行 FilterNode，对输入数据进行向量化过滤。
func (e *Executor) executeFilter(filter *FilterNode) (*execResult, error) {
	childResult, err := e.executeNode(filter.Child)
	if err != nil {
		return nil, err
	}

	schema := childResult.schema
	colIdxMap := buildColIdxMapFromSchema(schema)

	var chunks []*storage.Chunk
	for _, input := range childResult.chunks {
		output, err := filterChunk(input, filter.Condition, schema, colIdxMap)
		if err != nil {
			return nil, err
		}
		if output.RowCount() > 0 {
			chunks = append(chunks, output)
		}
	}

	return &execResult{chunks: chunks, schema: schema}, nil
}

// filterChunk 对单个 Chunk 执行向量化过滤。
func filterChunk(input *storage.Chunk, cond Expression, schema []ColumnDef, colIdxMap map[string]int) (*storage.Chunk, error) {
	rowCount := input.RowCount()
	if rowCount == 0 {
		return storage.NewChunk(defaultChunkSize), nil
	}

	selection := make([]uint32, 0, rowCount)
	for row := uint32(0); row < rowCount; row++ {
		rowVals := buildRowValues(input, schema, row)
		val, err := evalExpr(cond, rowVals, colIdxMap)
		if err != nil {
			continue
		}
		if isTruthyValue(val) {
			selection = append(selection, row)
		}
	}

	if len(selection) == 0 {
		return storage.NewChunk(defaultChunkSize), nil
	}

	output := storage.NewChunk(defaultChunkSize)
	for _, col := range input.Columns() {
		newCol := storage.NewColumnVector(col.ColumnID, col.Typ, uint32(len(selection)))
		for _, rowIdx := range selection {
			v := col.GetValue(rowIdx)
			if err := newCol.Append(v); err != nil {
				return nil, fmt.Errorf("executor filter: %w", err)
			}
		}
		if err := output.AddColumn(newCol); err != nil {
			return nil, fmt.Errorf("executor filter: %w", err)
		}
	}

	return output, nil
}

// executeProject 执行 ProjectNode，对输入数据进行投影。
func (e *Executor) executeProject(proj *ProjectNode) (*execResult, error) {
	childResult, err := e.executeNode(proj.Child)
	if err != nil {
		return nil, err
	}

	inputSchema := childResult.schema
	colIdxMap := buildColIdxMapFromSchema(inputSchema)
	outputSchema := proj.Schema()

	var chunks []*storage.Chunk
	for _, input := range childResult.chunks {
		output, err := projectChunk(input, proj.Expressions, inputSchema, outputSchema, colIdxMap)
		if err != nil {
			return nil, err
		}
		if output.RowCount() > 0 {
			chunks = append(chunks, output)
		}
	}

	return &execResult{chunks: chunks, schema: outputSchema}, nil
}

// projectChunk 对单个 Chunk 执行投影。
func projectChunk(input *storage.Chunk, exprs []Expression, inputSchema, outputSchema []ColumnDef, colIdxMap map[string]int) (*storage.Chunk, error) {
	rowCount := input.RowCount()
	output := storage.NewChunk(defaultChunkSize)

	for exprIdx, expr := range exprs {
		colDef := outputSchema[exprIdx]
		newCol := storage.NewColumnVector(uint32(exprIdx), colDef.Type, rowCount)

		for row := uint32(0); row < rowCount; row++ {
			rowVals := buildRowValues(input, inputSchema, row)

			val, err := evalExpr(expr, rowVals, colIdxMap)
			if err != nil {
				return nil, fmt.Errorf("executor project: row %d: %w", row, err)
			}
			if err := newCol.Append(val); err != nil {
				typedVal := coerceValue(val, colDef.Type)
				if err2 := newCol.Append(typedVal); err2 != nil {
					return nil, fmt.Errorf("executor project: row %d: %w", row, err)
				}
			}
		}

		if err := output.AddColumn(newCol); err != nil {
			return nil, fmt.Errorf("executor project: %w", err)
		}
	}

	return output, nil
}

// groupRow 存储分组键对应的原始行数据。
type groupRow struct {
	key    string
	values map[string]common.Value
}

// executeAggregate 执行 AggregateNode。
func (e *Executor) executeAggregate(agg *AggregateNode) (*execResult, error) {
	childResult, err := e.executeNode(agg.Child)
	if err != nil {
		return nil, err
	}

	inputSchema := childResult.schema
	colIdxMap := buildColIdxMapFromSchema(inputSchema)

	groupAccum := make(map[string][]accumulator)
	groupRows := make(map[string]*groupRow)
	groupOrder := make([]string, 0)

	for _, chunk := range childResult.chunks {
		for row := uint32(0); row < chunk.RowCount(); row++ {
			rowVals := buildRowValues(chunk, inputSchema, row)

			groupKey := buildGroupKey(agg.GroupBy, rowVals, colIdxMap)
			if _, ok := groupAccum[groupKey]; !ok {
				groupAccum[groupKey] = newAccumulators(agg.Aggregates)
				groupRows[groupKey] = &groupRow{key: groupKey, values: rowVals}
				groupOrder = append(groupOrder, groupKey)
			}

			for i := range groupAccum[groupKey] {
				var val common.Value
				if agg.Aggregates[i].Arg != nil {
					val, _ = evalExpr(agg.Aggregates[i].Arg, rowVals, colIdxMap)
				}
				groupAccum[groupKey][i].update(val)
			}
		}
	}

	schema := agg.Schema()
	output := storage.NewChunk(defaultChunkSize)

	outputCols := make([]*storage.ColumnVector, len(schema))
	for i, colDef := range schema {
		outputCols[i] = storage.NewColumnVector(uint32(i), colDef.Type, uint32(len(groupOrder)))
	}

	// 当没有输入行时，仍然需要为 COUNT 等聚合生成一行结果
	if len(groupOrder) == 0 {
		groupOrder = append(groupOrder, "")
		groupAccum[""] = newAccumulators(agg.Aggregates)
		groupRows[""] = &groupRow{key: "", values: nil}
	}

	for _, groupKey := range groupOrder {
		accs := groupAccum[groupKey]
		gr := groupRows[groupKey]
		colIdx := 0

		for _, gb := range agg.GroupBy {
			val, _ := evalExpr(gb, gr.values, colIdxMap)
			if err := outputCols[colIdx].Append(coerceValue(val, schema[colIdx].Type)); err != nil {
				return nil, fmt.Errorf("executor aggregate: %w", err)
			}
			colIdx++
		}

		for _, acc := range accs {
			val := acc.result()
			if err := outputCols[colIdx].Append(coerceValue(val, schema[colIdx].Type)); err != nil {
				return nil, fmt.Errorf("executor aggregate: %w", err)
			}
			colIdx++
		}
	}

	for _, col := range outputCols {
		if err := output.AddColumn(col); err != nil {
			return nil, fmt.Errorf("executor aggregate: %w", err)
		}
	}

	return &execResult{chunks: []*storage.Chunk{output}, schema: schema}, nil
}

// executeLimit 执行 LimitNode。
func (e *Executor) executeLimit(limit *LimitNode) (*execResult, error) {
	childResult, err := e.executeNode(limit.Child)
	if err != nil {
		return nil, err
	}

	var chunks []*storage.Chunk
	skipped := uint64(0)
	returned := uint64(0)

	for _, chunk := range childResult.chunks {
		rowCount := uint64(chunk.RowCount())

		if skipped+rowCount <= limit.Offset {
			skipped += rowCount
			continue
		}

		startRow := uint32(0)
		if skipped < limit.Offset {
			startRow = uint32(limit.Offset - skipped)
			skipped = limit.Offset
		}

		remaining := limit.Count - returned
		endRow := uint32(min(uint64(chunk.RowCount()), uint64(startRow)+remaining))

		if startRow >= endRow {
			break
		}

		limited := sliceChunk(chunk, startRow, endRow)
		if limited.RowCount() > 0 {
			chunks = append(chunks, limited)
			returned += uint64(endRow - startRow)
		}

		if returned >= limit.Count {
			break
		}
	}

	return &execResult{chunks: chunks, schema: childResult.schema}, nil
}

// buildRowValues 从 Chunk 构建指定行的列名到值的映射。
func buildRowValues(chunk *storage.Chunk, schema []ColumnDef, row uint32) map[string]common.Value {
	rowVals := make(map[string]common.Value, len(schema))
	for i, col := range chunk.Columns() {
		if i < len(schema) {
			rowVals[schema[i].Name] = col.GetValue(row)
		}
	}
	return rowVals
}

// evalExpr 在给定行数据上求值表达式。
func evalExpr(expr Expression, row map[string]common.Value, colIdxMap map[string]int) (common.Value, error) {
	switch e := expr.(type) {
	case *LiteralExpr:
		return e.Value, nil
	case *ResolvedColumnExpr:
		val, ok := row[e.Name]
		if !ok {
			return common.NewNull(), nil
		}
		return val, nil
	case *ColumnExpr:
		val, ok := row[e.Name]
		if !ok {
			return common.NewNull(), nil
		}
		return val, nil
	case *BinaryExpr:
		return evalBinaryExpr(e, row, colIdxMap)
	case *UnaryExpr:
		return evalUnaryExpr(e, row, colIdxMap)
	case *FuncExpr:
		return evalFuncExpr(e, row, colIdxMap)
	case *StarExpr:
		return common.NewNull(), nil
	default:
		return common.NewNull(), fmt.Errorf("executor: unsupported expression type %T", expr)
	}
}

func evalBinaryExpr(e *BinaryExpr, row map[string]common.Value, colIdxMap map[string]int) (common.Value, error) {
	left, err := evalExpr(e.Left, row, colIdxMap)
	if err != nil {
		return common.NewNull(), err
	}

	switch e.Op {
	case OpAnd:
		if !isTruthyValue(left) {
			return common.NewBool(false), nil
		}
		right, err := evalExpr(e.Right, row, colIdxMap)
		if err != nil {
			return common.NewNull(), err
		}
		return common.NewBool(isTruthyValue(right)), nil
	case OpOr:
		if isTruthyValue(left) {
			return common.NewBool(true), nil
		}
		right, err := evalExpr(e.Right, row, colIdxMap)
		if err != nil {
			return common.NewNull(), err
		}
		return common.NewBool(isTruthyValue(right)), nil
	}

	right, err := evalExpr(e.Right, row, colIdxMap)
	if err != nil {
		return common.NewNull(), err
	}

	if !left.Valid || !right.Valid {
		return common.NewNull(), nil
	}

	switch e.Op {
	case OpEq:
		return common.NewBool(left.Equal(right)), nil
	case OpNe:
		return common.NewBool(!left.Equal(right)), nil
	case OpLt:
		return common.NewBool(left.Less(right)), nil
	case OpGt:
		return common.NewBool(right.Less(left)), nil
	case OpLe:
		return common.NewBool(!right.Less(left)), nil
	case OpGe:
		return common.NewBool(!left.Less(right)), nil
	case OpAdd:
		return evalArithmetic(left, right, opAdd)
	case OpSub:
		return evalArithmetic(left, right, opSub)
	case OpMul:
		return evalArithmetic(left, right, opMul)
	case OpDiv:
		return evalArithmetic(left, right, opDiv)
	}

	return common.NewNull(), fmt.Errorf("executor: unsupported binary op %v", e.Op)
}

func evalUnaryExpr(e *UnaryExpr, row map[string]common.Value, colIdxMap map[string]int) (common.Value, error) {
	val, err := evalExpr(e.Expr, row, colIdxMap)
	if err != nil {
		return common.NewNull(), err
	}

	switch e.Op {
	case OpNot:
		return common.NewBool(!isTruthyValue(val)), nil
	case OpNeg:
		if !val.Valid {
			return common.NewNull(), nil
		}
		switch val.Typ {
		case common.TypeInt64:
			return common.NewInt64(-val.Int64), nil
		case common.TypeFloat64:
			return common.NewFloat64(-val.Float64), nil
		}
	}

	return common.NewNull(), fmt.Errorf("executor: unsupported unary op %v", e.Op)
}

func evalFuncExpr(e *FuncExpr, row map[string]common.Value, colIdxMap map[string]int) (common.Value, error) {
	return common.NewNull(), fmt.Errorf("executor: scalar function %q not supported in row eval", e.Name)
}

type arithOp int

const (
	opAdd arithOp = iota
	opSub
	opMul
	opDiv
)

func evalArithmetic(left, right common.Value, op arithOp) (common.Value, error) {
	if left.Typ == common.TypeFloat64 || right.Typ == common.TypeFloat64 {
		lf := toFloat64(left)
		rf := toFloat64(right)
		switch op {
		case opAdd:
			return common.NewFloat64(lf + rf), nil
		case opSub:
			return common.NewFloat64(lf - rf), nil
		case opMul:
			return common.NewFloat64(lf * rf), nil
		case opDiv:
			if rf == 0 {
				return common.NewNull(), nil
			}
			return common.NewFloat64(lf / rf), nil
		}
	}

	switch op {
	case opAdd:
		return common.NewInt64(left.Int64 + right.Int64), nil
	case opSub:
		return common.NewInt64(left.Int64 - right.Int64), nil
	case opMul:
		return common.NewInt64(left.Int64 * right.Int64), nil
	case opDiv:
		if right.Int64 == 0 {
			return common.NewNull(), nil
		}
		return common.NewInt64(left.Int64 / right.Int64), nil
	}

	return common.NewNull(), nil
}

func toFloat64(v common.Value) float64 {
	switch v.Typ {
	case common.TypeFloat64:
		return v.Float64
	case common.TypeInt64:
		return float64(v.Int64)
	}
	return 0
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
		a.count++
	case AggSum:
		if val.Valid {
			a.count++
			a.sum += toFloat64(val)
		}
	case AggMin:
		if val.Valid {
			if !a.hasValue || val.Less(a.minVal) {
				a.minVal = val
			}
			a.hasValue = true
		}
	case AggMax:
		if val.Valid {
			if !a.hasValue || a.maxVal.Less(val) {
				a.maxVal = val
			}
			a.hasValue = true
		}
	case AggAvg:
		if val.Valid {
			a.count++
			a.sum += toFloat64(val)
		}
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

// buildGroupKey 构建分组键。
func buildGroupKey(groupBy []Expression, row map[string]common.Value, colIdxMap map[string]int) string {
	if len(groupBy) == 0 {
		return ""
	}
	parts := make([]string, len(groupBy))
	for i, gb := range groupBy {
		val, _ := evalExpr(gb, row, colIdxMap)
		parts[i] = val.String()
	}
	return strings.Join(parts, "|")
}

// buildChunksFromEntries 将 ScanEntry 切片转换为 Chunk 切片。
func buildChunksFromEntries(entries []storage.ScanEntry, schema []ColumnDef, chunkSize int) []*storage.Chunk {
	if len(entries) == 0 || len(schema) == 0 {
		return nil
	}

	var chunks []*storage.Chunk
	for start := 0; start < len(entries); start += chunkSize {
		end := start + chunkSize
		if end > len(entries) {
			end = len(entries)
		}

		batch := entries[start:end]
		chunk := storage.NewChunk(uint32(chunkSize))

		for colIdx, colDef := range schema {
			col := storage.NewColumnVector(uint32(colIdx), colDef.Type, uint32(len(batch)))
			for _, entry := range batch {
				val, ok := entry.Value.Columns[colDef.Name]
				if !ok {
					val = common.NewNull()
				}
				if err := col.Append(val); err != nil {
					val = coerceValue(val, colDef.Type)
					_ = col.Append(val)
				}
			}
			_ = chunk.AddColumn(col)
		}

		chunks = append(chunks, chunk)
	}

	return chunks
}

// buildColIdxMap 构建列名到索引的映射。
func buildColIdxMap(cols []string) map[string]int {
	m := make(map[string]int, len(cols))
	for i, col := range cols {
		m[col] = i
	}
	return m
}

// buildColIdxMapFromSchema 从 ColumnDef 列表构建列名到索引的映射。
func buildColIdxMapFromSchema(schema []ColumnDef) map[string]int {
	m := make(map[string]int, len(schema))
	for i, col := range schema {
		m[col.Name] = i
	}
	return m
}

// isTruthyValue 判断值是否为真。
func isTruthyValue(v common.Value) bool {
	if !v.Valid {
		return false
	}
	switch v.Typ {
	case common.TypeBool:
		return v.Int64 != 0
	case common.TypeInt64:
		return v.Int64 != 0
	case common.TypeFloat64:
		return v.Float64 != 0
	case common.TypeString:
		return v.Str != ""
	}
	return true
}

// coerceValue 将值强制转换为指定类型。
func coerceValue(val common.Value, target common.DataType) common.Value {
	if !val.Valid {
		return common.NewNull()
	}
	if val.Typ == target {
		return val
	}
	switch target {
	case common.TypeInt64:
		switch val.Typ {
		case common.TypeFloat64:
			return common.NewInt64(int64(val.Float64))
		case common.TypeBool:
			return common.NewInt64(val.Int64)
		}
	case common.TypeFloat64:
		switch val.Typ {
		case common.TypeInt64:
			return common.NewFloat64(float64(val.Int64))
		case common.TypeBool:
			return common.NewFloat64(float64(val.Int64))
		}
	case common.TypeBool:
		return common.NewBool(isTruthyValue(val))
	}
	return val
}

// sliceChunk 从 Chunk 中截取指定行范围。
func sliceChunk(chunk *storage.Chunk, startRow, endRow uint32) *storage.Chunk {
	result := storage.NewChunk(defaultChunkSize)
	for _, col := range chunk.Columns() {
		newCol := storage.NewColumnVector(col.ColumnID, col.Typ, endRow-startRow)
		for row := startRow; row < endRow; row++ {
			_ = newCol.Append(col.GetValue(row))
		}
		_ = result.AddColumn(newCol)
	}
	return result
}

func maxStr(a, b string) string {
	if a > b {
		return a
	}
	return b
}

func minStr(a, b string) string {
	if a < b {
		return a
	}
	return b
}
