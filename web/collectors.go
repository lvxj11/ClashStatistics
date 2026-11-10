package web

import (
	"encoding/json"
	"net/http"
	"os"

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

// clashSource 供 /refresh 手动刷新使用：优先用数据源表里的 Clash 配置，
// 没有时回落到 config.toml/env，保证老部署零配置可用。
func (s *Server) clashSource() (host, secret string, interval int) {
	if sources, err := s.db.GetSources(); err == nil {
		for _, src := range sources {
			if src.ID != "clash" {
				continue
			}
			if src.Address != "" {
				host = src.Address
			}
			if src.Interval > 0 {
				interval = src.Interval
			}
			if src.CredentialRef != "" {
				secret = os.Getenv(src.CredentialRef)
			}
		}
	}
	if host == "" {
		host = s.config.ClashHost
	}
	if interval <= 0 {
		interval = s.config.ClashInterval
	}
	if secret == "" {
		secret = s.config.ClashSecret
	}
	return host, secret, interval
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
func (s *Server) getSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sources, err := s.db.GetEnabledSources()
	if err != nil {
		http.Error(w, "读取数据源失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defaultID, err := s.db.GetDefaultSource()
	if err != nil {
		defaultID = "clash"
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
