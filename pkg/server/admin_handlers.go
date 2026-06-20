// Package server 管理员运维 HTTP 端点。
//
// 提供 /admin/flush 与 /admin/compact 两个强制运维入口，弥补后台调度器在
// 负载较轻时刷盘/压缩触发不频繁的不足，允许运维人员通过 HTTP 显式触发：
//   - POST /admin/flush：把每张 LSM 表的活跃/不可变 MemTable 立即刷写为 Segment
//   - POST /admin/compact：立即尝试把每张 LSM 表的多层 Segment 合并到下一层
//
// 二者均遍历 routingAdapter 注册的全部 LSM 引擎并顺序执行；任一表失败时
// 整体返回 -1 + 详细错误，便于运维定位失败引擎。内存引擎表（ENGINE=memory）
// 不参与强制运维，因其数据驻内存、关闭即丢。
package server

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/what-is-me-vibe-coding/test-db/pkg/storage"
)

// admin handler 的错误码 / 响应消息常量。
const (
	// adminErrBadMethod 表示非 POST 请求。
	adminErrBadMethod = "仅支持 POST 方法"
	// adminErrFlushFailed 是 admin/flush 失败时的统一前缀。
	adminErrFlushFailed = "admin flush 失败"
	// adminErrCompactFailed 是 admin/compact 失败时的统一前缀。
	adminErrCompactFailed = "admin compact 失败"
	// adminMsgFlushOK 是 admin/flush 成功时的标准响应消息。
	adminMsgFlushOK = "强制 flush 成功"
	// adminMsgCompactOK 是 admin/compact 成功时的标准响应消息。
	adminMsgCompactOK = "强制 compact 成功"
)

// adminResponse 是 /admin/flush 与 /admin/compact 的统一 JSON 响应。
// 字段使用 omitempty 以便 Affected=0 时（无 LSM 表）不输出无意义字段。
type adminResponse struct {
	Code     int    `json:"code"`
	Message  string `json:"message,omitempty"`
	Affected int    `json:"affected,omitempty"`
}

// adminCounter 以原子方式统计被 force 处理过的 LSM 引擎数。
type adminCounter struct{ n atomic.Int64 }

// add 把计数 +1。
func (c *adminCounter) add() { c.n.Add(1) }

// value 返回当前累计值。
func (c *adminCounter) value() int { return int(c.n.Load()) }

// handleAdminFlush 处理 POST /admin/flush 请求：强制把每张 LSM 表的活跃
// MemTable 与不可变 MemTable 刷写为 Segment。无活跃/不可变 MemTable 的
// 表视为无操作跳过；任一表失败返回 500 + 错误信息，全部成功返回 200。
func (s *Server) handleAdminFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, &adminResponse{
			Code:    -1,
			Message: adminErrBadMethod,
		})
		return
	}

	affected, err := s.runOnLSMEngines(func(eng *storage.Engine) error {
		return eng.Flush(eng.ColumnMeta())
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, &adminResponse{
			Code:    -1,
			Message: fmt.Sprintf("%s: %v", adminErrFlushFailed, err),
		})
		return
	}

	writeJSON(w, http.StatusOK, &adminResponse{
		Code:     0,
		Message:  adminMsgFlushOK,
		Affected: affected,
	})
}

// handleAdminCompact 处理 POST /admin/compact 请求：立即尝试对每张 LSM 表
// 触发一次压缩（合并 L0 多段到 L1、必要时进一步合并）。ShouldCompact 返回
// false 的表视为无操作跳过；任一表失败返回 500 + 错误信息。
func (s *Server) handleAdminCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, &adminResponse{
			Code:    -1,
			Message: adminErrBadMethod,
		})
		return
	}

	affected, err := s.runOnLSMEngines(func(eng *storage.Engine) error {
		if !eng.ShouldCompact() {
			// 无需压缩仍计入「已检查」统计，避免 Affected=0 误读为「没找到 LSM 引擎」。
			return errAdminChecked
		}
		return eng.Compact(eng.ColumnMeta())
	})
	if err != nil && err != errAdminChecked {
		writeJSON(w, http.StatusInternalServerError, &adminResponse{
			Code:    -1,
			Message: fmt.Sprintf("%s: %v", adminErrCompactFailed, err),
		})
		return
	}

	writeJSON(w, http.StatusOK, &adminResponse{
		Code:     0,
		Message:  adminMsgCompactOK,
		Affected: affected,
	})
}

// errAdminChecked 是 runOnLSMEngines 内部哨兵错误：表示引擎被检查过但
// 因 ShouldCompact=false 跳过实际压缩。统计上仍计入 Affected。
var errAdminChecked = fmt.Errorf("admin: checked but skipped")

// runOnLSMEngines 遍历所有 LSM 引擎，逐一执行 fn；返回成功处理的引擎数。
// fn 内部应避免阻塞；任一引擎返回非 nil / 非 errAdminChecked 错误时，
// 整体立即返回该错误（其余引擎不再处理）。引擎无法转换为 *storage.Engine
// （例如内存引擎）会被安全跳过。
func (s *Server) runOnLSMEngines(fn func(*storage.Engine) error) (int, error) {
	if s == nil || s.adapter == nil {
		return 0, nil
	}
	var counter adminCounter
	var firstErr error
	s.adapter.forEachLSMEngine(func(eng TableEngine) {
		lsm, ok := eng.(*storage.Engine)
		if !ok {
			return
		}
		if err := fn(lsm); err != nil {
			if err == errAdminChecked {
				counter.add()
				return
			}
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		counter.add()
	})
	if firstErr != nil {
		return counter.value(), firstErr
	}
	return counter.value(), nil
}
