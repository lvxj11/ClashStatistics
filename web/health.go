package web

import (
	"strconv"
	"time"

	"clash-statistics/database"
)

// 数据源健康状态：从「采集动作本身」推导，不做任何额外的探测。
//   - Clash 采集器每次成功 GET /connections 就是一次真实连通证据，失败则累计；
//   - IPFIX 收到 UDP 包就是到达证据，包时间戳即心跳。
//
// 状态只放内存（不落库）：它是运行时健康，不是配置。重启后内存清零、显示
// 「等待」才是对的——此刻确实还没有任何证据。进程活着时状态始终最新，无需刷新。
type HealthState string

const (
	HealthWaiting  HealthState = "waiting"  // 采集器已起但还没拿到第一次结果
	HealthOK       HealthState = "ok"       // 正常：Clash 连败 < 3；IPFIX 距最近一次 < 5 分钟
	HealthFail     HealthState = "fail"     // 警告：Clash 连败 ≥ 3；IPFIX 距最近成功 ≥ 5 分钟
	HealthDisabled HealthState = "disabled" // 采集关闭：采集器未起、无证据，前端显示灰
)

// Clash 连续失败多少次才判为警告，抗单次网络抖动。
const clashFailThreshold = 3

// IPFIX 距最近一次收到包超过该时长判为警告（用户定的 5 分钟）。
const ipfixStaleAfter = 5 * time.Minute

// SourceHealth 是某个数据源在内存里的运行时心跳。
// Clash 维护 FailCount；IPFIX 只维护 LastOkAt，状态在读取时按时间推导。
type SourceHealth struct {
	LastOkAt  int64  `json:"last_ok_at"`           // 最近一次成功的 Unix 秒，0 = 尚未成功
	LastErrAt int64  `json:"last_err_at"`          // 最近一次失败的 Unix 秒
	FailCount int    `json:"fail_count"`           // Clash 连续失败计数（成功清零）
	LastError string `json:"last_error,omitempty"` // 最近一次失败的错误信息（tooltip 用）
}

// ipfixHealth 在读取时按「距最近成功的时间」推导，避免给 IPFIX 维护一个需要
// 定时器翻转的状态——直接读就是对的，无需后台定时器。
func ipfixHealth(h SourceHealth, now time.Time) HealthState {
	if h.LastOkAt == 0 {
		return HealthWaiting
	}
	if now.Unix()-h.LastOkAt >= int64(ipfixStaleAfter/time.Second) {
		return HealthFail
	}
	return HealthOK
}

// clashHealth 在读取时按「连败计数是否达阈值」推导。成功过的源即使此刻没有新
// 结果也保持 OK（连败计数由采集循环维护，不靠读取翻转）。
func clashHealth(h SourceHealth) HealthState {
	if h.FailCount >= clashFailThreshold {
		return HealthFail
	}
	if h.LastOkAt == 0 {
		return HealthWaiting
	}
	return HealthOK
}

// SourceHealthView 是接口返回给前端的一行健康状态。
type SourceHealthView struct {
	State     string `json:"state"`      // waiting | ok | fail | disabled
	LastOkAt  int64  `json:"last_ok_at"` // Unix 秒，0 = 尚未成功
	FailCount int    `json:"fail_count,omitempty"`
	Message   string `json:"message,omitempty"` // tooltip：失败原因或等待说明
}

// recordClashOK / recordClashFail / recordIPFIXOk 由各采集器在采集循环里调用，
// 只改内存心跳，不碰磁盘。
func (s *Server) recordClashOK(id string) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	h := s.ensureHealthLocked(id)
	h.LastOkAt = time.Now().Unix()
	h.FailCount = 0
	h.LastError = ""
}

func (s *Server) recordClashFail(id string, errMsg string) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	h := s.ensureHealthLocked(id)
	h.FailCount++
	h.LastErrAt = time.Now().Unix()
	h.LastError = errMsg
}

func (s *Server) recordIPFIXOk(id string) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	h := s.ensureHealthLocked(id)
	h.LastOkAt = time.Now().Unix()
}

// ensureHealthLocked 在 map 里取出（或惰性创建）该源的心跳。调用方须已持写锁。
func (s *Server) ensureHealthLocked(id string) *SourceHealth {
	h, ok := s.health[id]
	if !ok {
		h = &SourceHealth{}
		s.health[id] = h
	}
	return h
}

// dropHealth 删除一个源的内存心跳（源被删除或采集关闭时）。
func (s *Server) dropHealth(id string) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	delete(s.health, id)
}

// getHealthView 汇总一个源对外的健康视图。
//   - 采集关闭（src.Enabled == false）→ disabled（灰，前端不显示标记）；
//   - 否则按类型推导：Clash 用连败计数，IPFIX 用距最近成功的时间。
func (s *Server) getHealthView(src database.DataSource, now time.Time) SourceHealthView {
	if !src.Enabled {
		return SourceHealthView{State: string(HealthDisabled)}
	}
	s.healthMu.RLock()
	h := s.health[src.ID]
	s.healthMu.RUnlock()

	if h == nil {
		return SourceHealthView{State: string(HealthWaiting), Message: "采集器已启动，等待首次探测结果"}
	}
	switch src.Type {
	case "clash":
		st := clashHealth(*h)
		return SourceHealthView{
			State:     string(st),
			LastOkAt:  h.LastOkAt,
			FailCount: h.FailCount,
			Message:   clashHealthMessage(st, h),
		}
	case "ipfix":
		st := ipfixHealth(*h, now)
		return SourceHealthView{
			State:    string(st),
			LastOkAt: h.LastOkAt,
			Message:  ipfixHealthMessage(st, now, h.LastOkAt),
		}
	default:
		return SourceHealthView{State: string(HealthWaiting)}
	}
}

func clashHealthMessage(st HealthState, h *SourceHealth) string {
	switch st {
	case HealthOK:
		if h.LastOkAt > 0 {
			return "正常 · 最近成功 " + time.Unix(h.LastOkAt, 0).Format("15:04:05")
		}
		return "正常"
	case HealthFail:
		msg := "警告 · 连续 " + strconv.Itoa(h.FailCount) + " 次获取失败"
		if h.LastError != "" {
			msg += "：" + h.LastError
		}
		return msg
	default:
		return "等待 · 采集器已启动，尚未完成首次探测"
	}
}

func ipfixHealthMessage(st HealthState, now time.Time, lastOk int64) string {
	switch st {
	case HealthOK:
		return "正常 · 最近收到流 " + time.Unix(lastOk, 0).Format("15:04:05")
	case HealthFail:
		return "警告 · 超过 " + strconv.Itoa(int(ipfixStaleAfter/time.Minute)) + " 分钟未收到任何流数据"
	default:
		return "等待 · 采集器已启动，尚未收到流数据"
	}
}
