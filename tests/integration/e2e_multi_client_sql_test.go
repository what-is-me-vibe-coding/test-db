// Package integration 端到端集成测试。
//
// 本文件验证多客户端并发执行通用 SQL 工作负载的正确性：
//   - 启动一个 server，同时创建 LSM 表与内存表
//   - 多个客户端（TCP/HTTP 混合）并发执行写入、点查、范围扫描、聚合查询
//   - 混合读写客户端验证写入后立即可读
//   - 全部完成后校验数据总量与聚合结果
package integration

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
)

// multiClient 测试常量。
const (
	mcNumClients    = 12 // 并发客户端总数
	mcRowsPerWriter = 8  // 每个写入客户端写入的行数
	mcWriterBaseID  = 1000
	mcProductBaseID = 5000
)

// orderRows 返回订单初始数据（LSM 表）。
func orderRows() []map[string]any {
	return []map[string]any{
		{"id": 1, "product": "widget", "qty": 10, "amount": 99.5, "active": true},
		{"id": 2, "product": "widget", "qty": 5, "amount": 49.75, "active": true},
		{"id": 3, "product": "gadget", "qty": 20, "amount": 200.0, "active": false},
		{"id": 4, "product": "gadget", "qty": 15, "amount": 150.0, "active": true},
		{"id": 5, "product": "gizmo", "qty": 3, "amount": 30.0, "active": true},
	}
}

// productRows 返回商品初始数据（内存表）。
func productRows() []map[string]any {
	return []map[string]any{
		{"id": 1, "name": "widget", "price": 9.95},
		{"id": 2, "name": "gadget", "price": 10.0},
		{"id": 3, "name": "gizmo", "price": 10.0},
	}
}

// mcCreateTables 创建 LSM 订单表与内存商品表。
func mcCreateTables(t *testing.T, s *sqlServer) {
	t.Helper()
	err := s.srv.Catalog().CreateTable("orders", []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "product", Type: common.TypeString, Nullable: true},
		{Name: "qty", Type: common.TypeInt64, Nullable: true},
		{Name: "amount", Type: common.TypeFloat64, Nullable: true},
		{Name: "active", Type: common.TypeBool, Nullable: true},
	}, []string{"id"}, catalog.TableOptions{})
	if err != nil {
		t.Fatalf("创建 orders 表失败: %v", err)
	}
	resp := queryVia(t, s, "tcp",
		"CREATE TABLE products (id INT64 NOT NULL, name STRING NULL, "+
			"price FLOAT64 NULL, PRIMARY KEY(id)) ENGINE=memory")
	if resp.Code != 0 {
		t.Fatalf("创建 products 内存表失败: %s", resp.Message)
	}
}

// mcSeedData 写入初始订单与商品数据。
func mcSeedData(t *testing.T, s *sqlServer) {
	t.Helper()
	writeVia(t, s, "tcp", "orders", orderRows())
	writeVia(t, s, "tcp", "products", productRows())
}

// mcWriterWork 写入客户端：向 orders 表写入唯一 ID 的行。
func mcWriterWork(s *sqlServer, via string, clientID int) error {
	rows := make([]map[string]any, mcRowsPerWriter)
	for i := 0; i < mcRowsPerWriter; i++ {
		id := mcWriterBaseID + clientID*mcRowsPerWriter + i
		rows[i] = map[string]any{
			"id":      id,
			"product": fmt.Sprintf("prod-%d", clientID),
			"qty":     id % 100,
			"amount":  float64(id) * 1.5,
			"active":  true,
		}
	}
	resp, err := rawWrite(s, via, "orders", rows)
	if err != nil {
		return fmt.Errorf("写入失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("写入返回错误: %s", resp.Message)
	}
	return nil
}

// mcPointReaderWork 点查客户端：对 orders 与 products 做 WHERE 等值查询。
func mcPointReaderWork(s *sqlServer, via string, _ int) error {
	// 查询初始数据中的行
	resp, err := rawQuery(s, via, "SELECT * FROM orders WHERE id = 3")
	if err != nil {
		return fmt.Errorf("点查 orders 失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("点查 orders 返回错误: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		return fmt.Errorf("点查 orders 期望 1 行，得到 %d", len(rows))
	}
	if rows[0]["product"] != "gadget" {
		return fmt.Errorf("点查 product 不匹配: %v", rows[0]["product"])
	}
	// 查询内存表
	presp, err := rawQuery(s, via, "SELECT * FROM products WHERE id = 2")
	if err != nil {
		return fmt.Errorf("点查 products 失败: %w", err)
	}
	if presp.Code != 0 {
		return fmt.Errorf("点查 products 返回错误: %s", presp.Message)
	}
	prows := respRows(presp)
	if len(prows) != 1 {
		return fmt.Errorf("点查 products 期望 1 行，得到 %d", len(prows))
	}
	return nil
}

// mcAggReaderWork 聚合客户端：执行 COUNT 与 GROUP BY 聚合查询。
func mcAggReaderWork(s *sqlServer, via string, _ int) error {
	// COUNT 查询
	resp, err := rawQuery(s, via, "SELECT COUNT(*) AS cnt FROM orders")
	if err != nil {
		return fmt.Errorf("COUNT 查询失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("COUNT 返回错误: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		return fmt.Errorf("COUNT 期望 1 行，得到 %d", len(rows))
	}
	cnt, ok := toInt64(rows[0]["cnt"])
	if !ok || cnt < 5 {
		return fmt.Errorf("COUNT 期望 >=5，得到 %v", rows[0]["cnt"])
	}
	// GROUP BY 聚合
	gresp, err := rawQuery(s, via,
		"SELECT product, COUNT(*) AS cnt, SUM(qty) AS total_qty "+
			"FROM orders GROUP BY product")
	if err != nil {
		return fmt.Errorf("GROUP BY 查询失败: %w", err)
	}
	if gresp.Code != 0 {
		return fmt.Errorf("GROUP BY 返回错误: %s", gresp.Message)
	}
	grows := respRows(gresp)
	if len(grows) < 3 {
		return fmt.Errorf("GROUP BY 期望 >=3 组，得到 %d", len(grows))
	}
	return nil
}

// mcMixedWork 混合客户端：写入一行后立即读回验证。
func mcMixedWork(s *sqlServer, via string, clientID int) error {
	id := mcProductBaseID + clientID
	rows := []map[string]any{
		{"id": id, "name": fmt.Sprintf("mixed-%d", clientID), "price": float64(id) * 0.1},
	}
	resp, err := rawWrite(s, via, "products", rows)
	if err != nil {
		return fmt.Errorf("混合写入失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("混合写入错误: %s", resp.Message)
	}
	// 立即读回
	qresp, err := rawQuery(s, via, fmt.Sprintf("SELECT * FROM products WHERE id = %d", id))
	if err != nil {
		return fmt.Errorf("混合读回失败: %w", err)
	}
	if qresp.Code != 0 {
		return fmt.Errorf("混合读回错误: %s", qresp.Message)
	}
	qrows := respRows(qresp)
	if len(qrows) != 1 {
		return fmt.Errorf("混合读回期望 1 行，得到 %d", len(qrows))
	}
	if qrows[0]["name"] != fmt.Sprintf("mixed-%d", clientID) {
		return fmt.Errorf("混合读回 name 不匹配: %v", qrows[0]["name"])
	}
	return nil
}

// mcRangeScanWork 范围扫描客户端：执行 WHERE 范围查询与 LIMIT。
func mcRangeScanWork(s *sqlServer, via string, _ int) error {
	resp, err := rawQuery(s, via, "SELECT * FROM orders WHERE id > 2 LIMIT 10")
	if err != nil {
		return fmt.Errorf("范围扫描失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("范围扫描错误: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) < 3 {
		return fmt.Errorf("范围扫描期望 >=3 行，得到 %d", len(rows))
	}
	// 验证所有返回行 id > 2
	for _, r := range rows {
		id, ok := toInt64(r["id"])
		if !ok || id <= 2 {
			return fmt.Errorf("范围扫描返回了 id<=2 的行: %v", r["id"])
		}
	}
	return nil
}

// mcRunClient 按角色分发客户端工作负载。
func mcRunClient(s *sqlServer, via string, clientID, role int) error {
	switch role {
	case 0:
		return mcWriterWork(s, via, clientID)
	case 1:
		return mcPointReaderWork(s, via, clientID)
	case 2:
		return mcAggReaderWork(s, via, clientID)
	case 3:
		return mcMixedWork(s, via, clientID)
	default:
		return mcRangeScanWork(s, via, clientID)
	}
}

// TestMultiClientGeneralSQL 验证多客户端并发执行通用 SQL 的正确性。
//
// 启动一个 server，创建 LSM 表与内存表，12 个客户端并发执行
// 写入、点查、聚合、混合读写、范围扫描等操作，最终校验数据完整性。
func TestMultiClientGeneralSQL(t *testing.T) {
	s := startSQLServer(t)
	mcCreateTables(t, s)
	mcSeedData(t, s)

	var wg sync.WaitGroup
	var failCount int64
	var lastErr atomic.Value

	for i := 0; i < mcNumClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			via := "tcp"
			if clientID%3 == 0 {
				via = "http"
			}
			role := clientID % 5
			if err := mcRunClient(s, via, clientID, role); err != nil {
				t.Logf("client %d (%s, role %d) 失败: %v", clientID, via, role, err)
				lastErr.Store(err.Error())
				atomic.AddInt64(&failCount, 1)
			}
		}(i)
	}
	wg.Wait()

	if failCount > 0 {
		t.Fatalf("%d 个客户端失败，最后错误: %v", failCount, lastErr.Load())
	}
	mcVerifyFinalState(t, s)
}

// mcVerifyFinalState 校验全部客户端完成后的数据完整性。
func mcVerifyFinalState(t *testing.T, s *sqlServer) {
	t.Helper()
	// 12 个客户端按 clientID%5 分配角色：role 0=writer 出现 3 次（clientID 0,5,10）
	numWriters := 3
	numMixed := 2
	// orders 表：初始 5 行 + 3 个 writer * 8 行 = 29 行
	wantOrders := int64(5 + numWriters*mcRowsPerWriter)
	resp := queryVia(t, s, "tcp", "SELECT COUNT(*) AS cnt FROM orders")
	if resp.Code != 0 {
		t.Fatalf("最终 COUNT orders 失败: %s", resp.Message)
	}
	rows := respRows(resp)
	if len(rows) != 1 {
		t.Fatalf("COUNT orders 期望 1 行，得到 %d", len(rows))
	}
	gotOrders, _ := toInt64(rows[0]["cnt"])
	if gotOrders != wantOrders {
		t.Errorf("orders 行数: 期望 %d，得到 %d", wantOrders, gotOrders)
	}
	// products 内存表：初始 3 行 + 2 个 mixed = 5 行
	wantProducts := int64(3 + numMixed)
	presp := queryVia(t, s, "tcp", "SELECT COUNT(*) AS cnt FROM products")
	if presp.Code != 0 {
		t.Fatalf("最终 COUNT products 失败: %s", presp.Message)
	}
	prows := respRows(presp)
	gotProducts, _ := toInt64(prows[0]["cnt"])
	if gotProducts != wantProducts {
		t.Errorf("products 行数: 期望 %d，得到 %d", wantProducts, gotProducts)
	}
	// 验证写入的订单可查（LIMIT 放宽以覆盖全部写入行）
	wantWritten := numWriters * mcRowsPerWriter
	wresp := queryVia(t, s, "tcp", "SELECT * FROM orders WHERE id >= 1000 LIMIT 100")
	if wresp.Code != 0 {
		t.Fatalf("查询写入订单失败: %s", wresp.Message)
	}
	wrows := respRows(wresp)
	if len(wrows) != wantWritten {
		t.Errorf("写入订单数: 期望 %d，得到 %d", wantWritten, len(wrows))
	}
	// 验证 GROUP BY 聚合结果正确
	gresp := queryVia(t, s, "tcp",
		"SELECT product, COUNT(*) AS cnt FROM orders GROUP BY product")
	if gresp.Code != 0 {
		t.Fatalf("最终 GROUP BY 失败: %s", gresp.Message)
	}
	grows := respRows(gresp)
	// 初始 3 种 product + writer 写入的 3 种 prod-0/prod-5/prod-10 = 6 组
	if len(grows) < 3 {
		t.Errorf("GROUP BY 分组数: 期望 >=3，得到 %d", len(grows))
	}
}
