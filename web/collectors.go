package web

import (
	"encoding/json"
	"net/http"
	"os"

	"clash-statistics/database"
	"clash-statistics/utils"
)

// Collector 是数据源的采集器。各源内部自己决定调度方式：Clash 是轮询 ticker，
// IPFIX 是 UDP 常驻监听，二者没有同构的调度结构，所以这里只抽真正共用的
// 「启停」契约，不强行统一 ticker / interval。
type Collector interface {
	Name() string
	Start()
	Stop()
}

// syncCollectors 按 data_sources 表重建采集器：管理页增删改后调用。
// 全量重建的代价只是 Clash 少采一帧、IPFIX 重收一次模板，换来的是状态不会漂移。
func (s *Server) syncCollectors() {
	log := utils.GetLogger()
	sources, err := s.db.GetEnabledSources()
	if err != nil {
		log.Error("读取数据源失败: %v\n", err)
		return
	}

	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()

	for _, c := range s.collectors {
		c.Stop()
	}
	s.collectors = s.collectors[:0]

	// 健康心跳只对「正在采集」的源有意义：重建时清掉已移除 / 已禁用源的心跳，
	// 新启用的源在首次采集结果出来前按 waiting（启动即探测的源不硬塞成默认，见规格）。
	s.healthMu.Lock()
	active := make(map[string]bool, len(sources))
	for _, src := range sources {
		active[src.ID] = true
		if _, ok := s.health[src.ID]; !ok {
			s.health[src.ID] = &SourceHealth{}
		}
	}
	for id := range s.health {
		if !active[id] {
			delete(s.health, id)
		}
	}
	s.healthMu.Unlock()

	for _, src := range sources {
		var c Collector
		switch src.Type {
		case "clash":
			c = newClashCollector(s, src)
		case "ipfix":
			c = newIPFIXCollector(s, src)
		default:
			log.Warn("未知数据源类型 %s（%s），已跳过\n", src.Type, src.ID)
			continue
		}
		c.Start()
		s.collectors = append(s.collectors, c)
		log.Info("数据源 %s（%s）已启动\n", src.ID, src.Name)
	}
}

// resolveSecret 按数据源定义解析明文凭据，供 Clash 采集器与 /refresh 共用：
//   - credential_ref 非空 → 取它指向的环境变量（env 优先）；
//   - 否则 → 解混淆表内保存的 credential_value。
//
// 凭据真值只在内存里用，绝不进接口 / 日志 / 页面。
func resolveSecret(src database.DataSource) string {
	switch src.CredentialSource() {
	case "env":
		return os.Getenv(src.CredentialRef)
	case "table":
		return utils.DeobfuscateSecret(src.CredentialValue)
	}
	return ""
}

// clashSource 只从 data_sources 表读取启用的 Clash 源配置，供 /refresh 手动刷新。
// 返回该源 ID（写入时按源隔离落盘打标）、地址、凭据、间隔；没有启用的 Clash 源时 ok=false。
// 运行时配置只认表，不再回落到 config.toml / env。
func (s *Server) clashSource() (id, host, secret string, interval int, ok bool) {
	sources, err := s.db.GetSources()
	if err != nil {
		return "", "", "", 0, false
	}
	for _, src := range sources {
		if src.Type != "clash" || !src.Enabled {
			continue
		}
		return src.ID, src.Address, resolveSecret(src), src.Interval, true
	}
	return "", "", "", 0, false
}

// dataSourceOptions 是在线状态页取数所需的最小信息：跟随「在线默认」单选源。
// v3.0.0 取消了页面上的数据源下拉，所以不再返回完整候选列表。
type dataSourceOptions struct {
	Default     string `json:"default"`      // 在线默认源 ID
	DefaultName string `json:"default_name"` // 默认源显示名（页面静态标签用）
	HasSources  bool   `json:"has_sources"`  // 是否存在任意数据源
}

// getSources 返回在线默认源与「是否有源」，供在线状态页按该源取数。
// 所有数据源恒可选、不再按 enabled 过滤：采集关闭的源仍可被设为在线默认、看历史数据。
// 空表时 has_sources=false，由前端显示「暂无数据源」引导。
func (s *Server) getSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sources, err := s.db.GetSources()
	if err != nil {
		http.Error(w, "读取数据源失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// 在线默认严格取 GetDefaultSource 的结果：显式默认优先，否则回落「第一个
	// 启用源」。采集关闭且又没显式默认的源不会被硬塞成默认（见规格 point 6 /
	// detail A）；此时 default 为空，由前端按 has_sources 给出相应引导。
	defaultID, err := s.db.GetDefaultSource()
	if err != nil {
		defaultID = ""
	}
	defaultName := ""
	for _, src := range sources {
		if src.ID == defaultID {
			defaultName = src.Name
			break
		}
	}
	json.NewEncoder(w).Encode(dataSourceOptions{
		Default:     defaultID,
		DefaultName: defaultName,
		HasSources:  len(sources) > 0,
	})
}
