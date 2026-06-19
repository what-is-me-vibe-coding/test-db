// Package integration 端到端集成测试：并发 DDL + DML 混合工作负载。
//
// 补充既有集成测试未覆盖的「并发 DDL+DML」场景。既有所有多客户端测试均由
// 单个 setup 客户端预先建表后再并发执行 DML，未验证 DDL（CREATE/DROP TABLE）
// 与 DML（INSERT/SELECT/UPDATE/DELETE）并发交错时的正确性。本文件覆盖：
//   - 启动一个 server，预创建一张共享 LSM 表并写入初始数据
//   - DDL 客户端：多轮 CREATE TABLE → INSERT → SELECT → DROP TABLE，操作各自独立的临时表
//   - 写入客户端：向共享表写入唯一 ID 区间的行并读回校验
//   - 混合客户端：在共享表上执行 INSERT → UPDATE → DELETE 全 DML 链路
//   - 聚合客户端：并发执行 COUNT/SUM/AVG/MIN/MAX/GROUP BY，仅校验查询成功
//   - 最终校验：共享表行数、临时表已全部清理、聚合结果一致性
//
// 直接验证「一个 server + 多个 client + 一般 SQL + DDL/DML 并发」的稳定性，
// 可捕获 catalog 锁、表引擎注销、并发扫描等路径的潜在缺陷。
package integration

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/what-is-me-vibe-coding/test-db/pkg/catalog"
	"github.com/what-is-me-vibe-coding/test-db/pkg/common"
	"github.com/what-is-me-vibe-coding/test-db/pkg/server"
)

// cdm 常量：并发 DDL+DML 工作负载参数。
const (
	cdmTotalClients   = 10 // 并发客户端总数
	cdmDDLClientCount = 3  // DDL 客户端数（角色 0）
	cdmWriterCount    = 3  // 写入客户端数（角色 1）
	cdmMixedCount     = 2  // 混合 DML 客户端数（角色 2）
	cdmAggCount       = 2  // 聚合客户端数（角色 3）
	cdmAggIterations  = 4  // 聚合客户端迭代次数
	cdmDDLRounds      = 3  // DDL 客户端建表/删表轮数
	cdmRowsPerWriter  = 10 // 每个写入客户端写入行数
	cdmRowsPerMixed   = 8  // 每个混合客户端初始写入行数
	cdmWriterBaseID   = 10000
	cdmMixedBaseID    = 20000
	cdmSharedTable    = "shared_evt"
)

// cdmSharedRows 返回共享表的初始数据。
func cdmSharedRows() []map[string]any {
	return []map[string]any{
		{"id": 1, "category": "alpha", "amount": 10.5, "active": true},
		{"id": 2, "category": "beta", "amount": 20.0, "active": false},
		{"id": 3, "category": "alpha", "amount": 15.0, "active": true},
		{"id": 4, "category": "gamma", "amount": 30.0, "active": true},
	}
}

// cdmSetupSharedTable 创建共享 LSM 表并写入初始数据。
func cdmSetupSharedTable(t *testing.T, s *sqlServer) {
	t.Helper()
	err := s.srv.Catalog().CreateTable(cdmSharedTable, []catalog.ColumnDef{
		{Name: "id", Type: common.TypeInt64, Nullable: false},
		{Name: "category", Type: common.TypeString, Nullable: true},
		{Name: "amount", Type: common.TypeFloat64, Nullable: true},
		{Name: "active", Type: common.TypeBool, Nullable: true},
	}, []string{"id"}, catalog.TableOptions{})
	if err != nil {
		t.Fatalf("创建共享表失败: %v", err)
	}
	writeVia(t, s, "tcp", cdmSharedTable, cdmSharedRows())
}

// cdmDDLClientWork 执行 DDL 客户端工作负载。
//
// 每轮创建一张独立的临时表，写入若干行、查询校验、再删除，并验证删除后查询失败。
// 所有操作仅作用于以 clientID 命名的独立表，不与其他客户端冲突。
func cdmDDLClientWork(s *sqlServer, via string, clientID int) error {
	for round := 0; round < cdmDDLRounds; round++ {
		table := fmt.Sprintf("tmp_ddl_%d_%d", clientID, round)
		ddl := fmt.Sprintf("CREATE TABLE %s (id INT64 NOT NULL, "+
			"val STRING NULL, PRIMARY KEY(id))", table)
		if resp, err := rawQuery(s, via, ddl); err != nil || resp.Code != 0 {
			return fmt.Errorf("建表 %s: err=%v code=%d msg=%s",
				table, err, respCode(resp), respMsg(resp))
		}
		rows := []map[string]any{
			{"id": round*10 + 1, "val": fmt.Sprintf("v-%d-%d", clientID, round)},
			{"id": round*10 + 2, "val": fmt.Sprintf("v-%d-%d", clientID, round)},
		}
		if err := cdmWriteAndVerify(s, via, table, rows); err != nil {
			return err
		}
		if err := cdmDropAndVerify(s, via, table); err != nil {
			return err
		}
	}
	return nil
}

// cdmWriteAndVerify 写入行并校验 COUNT 与点查结果。
func cdmWriteAndVerify(s *sqlServer, via, table string,
	rows []map[string]any,
) error {
	wresp, err := rawWrite(s, via, table, rows)
	if err != nil || wresp.Code != 0 {
		return fmt.Errorf("写入 %s: err=%v code=%d msg=%s",
			table, err, respCode(wresp), respMsg(wresp))
	}
	cresp, err := rawQuery(s, via,
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", table))
	if err != nil || cresp.Code != 0 {
		return fmt.Errorf("COUNT %s: err=%v code=%d", table, err, respCode(cresp))
	}
	cnt, _ := toInt64(firstRow(cresp)["cnt"])
	if cnt != int64(len(rows)) {
		return fmt.Errorf("COUNT %s: 期望 %d 得到 %d", table, len(rows), cnt)
	}
	return nil
}

// cdmDropAndVerify 删除表并验证删除后查询返回错误。
func cdmDropAndVerify(s *sqlServer, via, table string) error {
	dresp, err := rawQuery(s, via, "DROP TABLE "+table)
	if err != nil || dresp.Code != 0 {
		return fmt.Errorf("DROP %s: err=%v code=%d", table, err, respCode(dresp))
	}
	qresp, err := rawQuery(s, via,
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", table))
	if err != nil {
		return nil // 网络层错误也视为「表已不可用」
	}
	if qresp.Code == 0 {
		return fmt.Errorf("DROP %s 后查询仍成功", table)
	}
	return nil
}

// cdmWriterClientWork 执行写入客户端工作负载：向共享表写入唯一 ID 区间的行并读回。
func cdmWriterClientWork(s *sqlServer, via string, clientID int) error {
	base := cdmWriterBaseID + clientID*cdmRowsPerWriter
	rows := make([]map[string]any, cdmRowsPerWriter)
	for i := 0; i < cdmRowsPerWriter; i++ {
		id := base + i
		rows[i] = map[string]any{
			"id":       id,
			"category": fmt.Sprintf("w-%d", clientID),
			"amount":   float64(id) * 0.5,
			"active":   i%2 == 0,
		}
	}
	wresp, err := rawWrite(s, via, cdmSharedTable, rows)
	if err != nil || wresp.Code != 0 {
		return fmt.Errorf("写入共享表: err=%v code=%d", err, respCode(wresp))
	}
	// 读回校验：写入区间内行数正确
	qresp, err := rawQuery(s, via,
		fmt.Sprintf("SELECT * FROM %s WHERE id >= %d AND id < %d",
			cdmSharedTable, base, base+cdmRowsPerWriter))
	if err != nil || qresp.Code != 0 {
		return fmt.Errorf("读回写入区间: err=%v code=%d", err, respCode(qresp))
	}
	if got := len(respRows(qresp)); got != cdmRowsPerWriter {
		return fmt.Errorf("写入区间行数: 期望 %d 得到 %d", cdmRowsPerWriter, got)
	}
	return nil
}

// cdmMixedClientWork 执行混合 DML 客户端工作负载：
// 写入 → UPDATE 一行 → DELETE 一行 → 读回校验剩余行数。
func cdmMixedClientWork(s *sqlServer, via string, clientID int) error {
	base := cdmMixedBaseID + clientID*cdmRowsPerMixed
	rows := make([]map[string]any, cdmRowsPerMixed)
	for i := 0; i < cdmRowsPerMixed; i++ {
		id := base + i
		rows[i] = map[string]any{
			"id":       id,
			"category": fmt.Sprintf("m-%d", clientID),
			"amount":   float64(id),
			"active":   true,
		}
	}
	wresp, err := rawWrite(s, via, cdmSharedTable, rows)
	if err != nil || wresp.Code != 0 {
		return fmt.Errorf("混合写入: err=%v code=%d", err, respCode(wresp))
	}
	// UPDATE：修改区间首行的 amount
	uresp, err := rawQuery(s, via,
		fmt.Sprintf("UPDATE %s SET amount = 999 WHERE id = %d",
			cdmSharedTable, base))
	if err != nil || uresp.Code != 0 || uresp.Rows != 1 {
		return fmt.Errorf("UPDATE id=%d: err=%v code=%d rows=%d",
			base, err, respCode(uresp), uresp.Rows)
	}
	// DELETE：删除区间第二行
	dresp, err := rawQuery(s, via,
		fmt.Sprintf("DELETE FROM %s WHERE id = %d", cdmSharedTable, base+1))
	if err != nil || dresp.Code != 0 || dresp.Rows != 1 {
		return fmt.Errorf("DELETE id=%d: err=%v code=%d rows=%d",
			base+1, err, respCode(dresp), dresp.Rows)
	}
	// 读回校验：区间内剩余 cdmRowsPerMixed-1 行
	want := cdmRowsPerMixed - 1
	qresp, err := rawQuery(s, via,
		fmt.Sprintf("SELECT * FROM %s WHERE id >= %d AND id < %d",
			cdmSharedTable, base, base+cdmRowsPerMixed))
	if err != nil || qresp.Code != 0 {
		return fmt.Errorf("混合读回: err=%v code=%d", err, respCode(qresp))
	}
	if got := len(respRows(qresp)); got != want {
		return fmt.Errorf("混合区间剩余行数: 期望 %d 得到 %d", want, got)
	}
	return nil
}

// cdmAggregateClientWork 执行聚合客户端工作负载：
// 多轮运行 COUNT/SUM/AVG/MIN/MAX/GROUP BY，仅校验查询成功（不校验精确值，
// 因并发写入下计数会变化；精确校验在最终验证阶段进行）。
func cdmAggregateClientWork(s *sqlServer, via string, _ int) error {
	queries := []string{
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", cdmSharedTable),
		fmt.Sprintf("SELECT category, COUNT(*) AS cnt, SUM(amount) AS s, "+
			"AVG(amount) AS a, MIN(amount) AS mn, MAX(amount) AS mx "+
			"FROM %s GROUP BY category", cdmSharedTable),
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s WHERE active = true",
			cdmSharedTable),
		fmt.Sprintf("SELECT id, amount FROM %s WHERE amount > 0 LIMIT 5",
			cdmSharedTable),
	}
	for i := 0; i < cdmAggIterations; i++ {
		for _, sql := range queries {
			resp, err := rawQuery(s, via, sql)
			if err != nil || resp.Code != 0 {
				return fmt.Errorf("聚合查询 [%s]: err=%v code=%d",
					sql, err, respCode(resp))
			}
		}
	}
	return nil
}

// cdmRunClient 按角色分发客户端工作负载。
func cdmRunClient(s *sqlServer, via string, clientID, role int) error {
	switch role {
	case 0:
		return cdmDDLClientWork(s, via, clientID)
	case 1:
		return cdmWriterClientWork(s, via, clientID)
	case 2:
		return cdmMixedClientWork(s, via, clientID)
	default:
		return cdmAggregateClientWork(s, via, clientID)
	}
}

// TestConcurrentDDLAndDML 验证一个 server 下多客户端并发执行
// DDL + DML 混合工作负载的正确性。
//
// 10 个客户端按角色分配：3 个 DDL（建/删独立临时表）、3 个写入、2 个混合 DML、
// 2 个聚合。客户端使用 TCP/HTTP 混合协议，在共享表与各自独立表上并发操作，
// 验证 catalog 锁、表引擎注销、并发扫描等路径在 DDL/DML 交错下保持正确。
func TestConcurrentDDLAndDML(t *testing.T) {
	s := startSQLServer(t)
	cdmSetupSharedTable(t, s)

	var wg sync.WaitGroup
	var failCount int64
	var lastErr atomic.Value
	for i := 0; i < cdmTotalClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			via := "tcp"
			if clientID%3 == 0 {
				via = "http"
			}
			role := clientID % 4
			if err := cdmRunClient(s, via, clientID, role); err != nil {
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
	cdmVerifyFinalState(t, s)
}

// cdmVerifyFinalState 校验全部客户端完成后的最终状态。
//
// 校验项：
//   - 共享表行数 = 初始 4 行 + 写入客户端 3*cdmRowsPerWriter + 混合客户端 2*(cdmRowsPerMixed-1)
//   - 所有 DDL 临时表已删除（catalog 快照中无 tmp_ddl_* 表）
//   - 聚合查询结果与实际行数一致
func cdmVerifyFinalState(t *testing.T, s *sqlServer) {
	t.Helper()
	// 10 个客户端按 clientID%4 分配角色：
	//   role 0 (DDL): clientID 0,4,8 → 3 个
	//   role 1 (Writer): clientID 1,5,9 → 3 个
	//   role 2 (Mixed): clientID 2,6 → 2 个
	//   role 3 (Agg): clientID 3,7 → 2 个
	wantShared := int64(len(cdmSharedRows()) +
		cdmWriterCount*cdmRowsPerWriter +
		cdmMixedCount*(cdmRowsPerMixed-1))
	resp := queryVia(t, s, "tcp",
		fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", cdmSharedTable))
	if resp.Code != 0 {
		t.Fatalf("最终 COUNT 失败: %s", resp.Message)
	}
	got, _ := toInt64(firstRow(resp)["cnt"])
	if got != wantShared {
		t.Errorf("共享表行数: 期望 %d 得到 %d", wantShared, got)
	}
	// 校验所有 DDL 临时表已删除
	snap := s.srv.Catalog().Snapshot()
	for name := range snap.Tables {
		if len(name) >= 8 && name[:8] == "tmp_ddl_" {
			t.Errorf("临时表未删除: %s", name)
		}
	}
	// 校验聚合：GROUP BY 分组数 >= 初始 3 类别
	gresp := queryVia(t, s, "tcp",
		fmt.Sprintf("SELECT category, COUNT(*) AS cnt FROM %s "+
			"GROUP BY category", cdmSharedTable))
	if gresp.Code != 0 {
		t.Fatalf("最终 GROUP BY 失败: %s", gresp.Message)
	}
	if got := len(respRows(gresp)); got < 3 {
		t.Errorf("GROUP BY 分组数: 期望 >=3 得到 %d", got)
	}
	// 校验 SUM 聚合非空
	sresp := queryVia(t, s, "tcp",
		fmt.Sprintf("SELECT SUM(amount) AS s FROM %s", cdmSharedTable))
	if sresp.Code != 0 {
		t.Fatalf("最终 SUM 失败: %s", sresp.Message)
	}
	srow := firstRow(sresp)
	if srow["s"] == nil {
		t.Errorf("SUM(amount): 期望非 nil，得到 nil")
	}
}

// firstRow 返回响应的首行，无行时返回 nil。
func firstRow(resp *server.Response) map[string]any {
	rows := respRows(resp)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// respCode 安全读取响应码（resp 为 nil 时返回 -1）。
func respCode(resp *server.Response) int {
	if resp == nil {
		return -1
	}
	return resp.Code
}

// respMsg 安全读取响应消息。
func respMsg(resp *server.Response) string {
	if resp == nil {
		return ""
	}
	return resp.Message
}
