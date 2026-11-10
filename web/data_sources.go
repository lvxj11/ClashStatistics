package web

import (
	"encoding/json"
	"net/http"
	"os"

	"clash-statistics/database"
)

func (s *Server) showDataSources(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "数据源", "data-sources", "data-sources.html")
}

// dataSourceView 是管理页拿到的视图：只暴露凭据「是否已配置」，绝不回显值。
type dataSourceView struct {
	database.DataSource
	CredentialConfigured bool `json:"credential_configured"`
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
		views = append(views, dataSourceView{
			DataSource:           src,
			CredentialConfigured: src.CredentialRef != "" && os.Getenv(src.CredentialRef) != "",
		})
	}
	json.NewEncoder(w).Encode(views)
}

func (s *Server) putDataSource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var src database.DataSource
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&src); err != nil {
		http.Error(w, "请求体无效: "+err.Error(), http.StatusBadRequest)
		return
	}
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
