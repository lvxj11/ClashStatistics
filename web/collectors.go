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
// 运行时配置只认表，不再回落到 config.toml / env；没有启用的 Clash 源时 ok=false。
func (s *Server) clashSource() (host, secret string, interval int, ok bool) {
	sources, err := s.db.GetSources()
	if err != nil {
		return "", "", 0, false
	}
	for _, src := range sources {
		if src.Type != "clash" || !src.Enabled {
			continue
		}
		return src.Address, resolveSecret(src), src.Interval, true
	}
	return "", "", 0, false
}

type sourceOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

type dataSourceOptions struct {
	Sources []sourceOption `json:"sources"`
	Default string         `json:"default"`
}

// getSources 返回启用的数据源与默认源，供在线状态页的下拉使用。
// 空表时返回空列表与空 default，由前端提示「暂无数据源」，不再硬编码 clash。
func (s *Server) getSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sources, err := s.db.GetEnabledSources()
	if err != nil {
		http.Error(w, "读取数据源失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defaultID, err := s.db.GetDefaultSource()
	if err != nil {
		defaultID = ""
	}

	out := dataSourceOptions{
		Sources: make([]sourceOption, 0, len(sources)),
		Default: defaultID,
	}
	for _, src := range sources {
		out.Sources = append(out.Sources, sourceOption{
			ID: src.ID, Name: src.Name, IsDefault: src.IsDefault,
		})
	}
	json.NewEncoder(w).Encode(out)
}
