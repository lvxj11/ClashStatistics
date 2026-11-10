package web

import (
	"encoding/json"
	"net/http"
	"os"

	"clash-statistics/database"
	"clash-statistics/utils"
)

func (s *Server) showDataSources(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "数据源", "data-sources", "data-sources.html")
}

// dataSourceInput 是 PUT 的请求体。credential_secret 是只写的明文密钥：
// 混淆后存入 credential_value，绝不回显；留空表示「不修改」已有凭据。
type dataSourceInput struct {
	database.DataSource
	CredentialSecret string `json:"credential_secret"`
}

// dataSourceView 是管理页 GET 拿到的视图：只暴露凭据「来源 / 是否已配置」，
// 既不回显明文，也不回显混淆串（DataSource.CredentialValue 是 json:"-"）。
type dataSourceView struct {
	database.DataSource
	CredentialConfigured bool   `json:"credential_configured"`
	CredentialSource     string `json:"credential_source"` // "env" | "table" | ""
}

func (s *Server) handleDataSources(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getDataSources(w, r)
	case http.MethodPut:
		s.putDataSource(w, r)
	case http.MethodDelete:
		s.deleteDataSource(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getDataSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sources, err := s.db.GetSources()
	if err != nil {
		http.Error(w, "读取数据源失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]dataSourceView, 0, len(sources))
	for _, src := range sources {
		view := dataSourceView{
			DataSource:       src,
			CredentialSource: src.CredentialSource(),
		}
		switch view.CredentialSource {
		case "env":
			view.CredentialConfigured = os.Getenv(src.CredentialRef) != ""
		case "table":
			view.CredentialConfigured = true
		}
		views = append(views, view)
	}
	json.NewEncoder(w).Encode(views)
}

func (s *Server) putDataSource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var input dataSourceInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&input); err != nil {
		http.Error(w, "请求体无效: "+err.Error(), http.StatusBadRequest)
		return
	}
	src := input.DataSource
	src.ID = cleanInput(src.ID, 64)
	src.Type = cleanInput(src.Type, 32)
	src.Name = cleanInput(src.Name, 128)
	src.Address = cleanInput(src.Address, 256)
	src.LANCIDRs = cleanInput(src.LANCIDRs, 512)
	src.ExcludeIPs = cleanInput(src.ExcludeIPs, 512)
	src.CredentialRef = cleanInput(src.CredentialRef, 128)
	src.Thresholds = cleanInput(src.Thresholds, 128)
	if src.CustomThresholds {
		if _, err := database.ParseThresholds(src.Thresholds); err != nil {
			http.Error(w, "自定义阈值无效: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// 凭据：填了明文就混淆后覆盖；没填就保留表里的旧值（编辑时密钥框留空表示不修改）。
	// CredentialValue 是 json:"-"，decode 不会带进来，必须显式保留，否则会被空串覆盖掉。
	if input.CredentialSecret != "" {
		src.CredentialValue = utils.ObfuscateSecret(input.CredentialSecret)
	} else {
		existing, err := s.db.GetSourceCredentialValue(src.ID)
		if err != nil {
			http.Error(w, "读取已有凭据失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		src.CredentialValue = existing
	}

	if err := s.db.UpsertSource(src); err != nil {
		http.Error(w, "保存数据源失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	// 配置变化立即重建采集器，无需重启进程
	s.syncCollectors()
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) deleteDataSource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := cleanInput(r.URL.Query().Get("id"), 64)
	if id == "" {
		http.Error(w, "缺少 id 参数", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteSource(id); err != nil {
		http.Error(w, "删除数据源失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.syncCollectors()
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
