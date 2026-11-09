package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"clash-statistics/database"
)

// showClientNames 独立的客户端名称管理页
func (s *Server) showClientNames(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "客户端名称", "client-names", "client-names.html")
}

// clientNamesResponse 是 GET /api/client-names 的响应。
// names 以 IP 为键，方便前端 O(1) 取展示名；knownIPs 供管理页列出"可命名"的已知客户端。
type clientNamesResponse struct {
	Names    map[string]database.ClientName `json:"names"`
	KnownIPs []string                       `json:"knownIPs"`
}

// handleClientNames 按方法分发到 GET / PUT / DELETE
func (s *Server) handleClientNames(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getClientNames(w, r)
	case http.MethodPut:
		s.putClientNames(w, r)
	case http.MethodDelete:
		s.deleteClientName(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getClientNames(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	records, err := s.db.GetClientNames()
	if err != nil {
		http.Error(w, "读取客户端名称失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	known, err := s.db.GetKnownSourceIPs()
	if err != nil {
		http.Error(w, "读取已知客户端失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	names := make(map[string]database.ClientName, len(records))
	for _, rec := range records {
		names[rec.IP] = rec
	}
	json.NewEncoder(w).Encode(clientNamesResponse{Names: names, KnownIPs: known})
}

type nameInput struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
	Note string `json:"note"`
}

type putRequest struct {
	IP    string      `json:"ip"`
	Name  string      `json:"name"`
	Note  string      `json:"note"`
	Items []nameInput `json:"items"`
}

// putClientNames 支持单条（{ip,name,note}）与批量导入（{items:[...]}）两种形态。
func (s *Server) putClientNames(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req putRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "请求体无效: "+err.Error(), http.StatusBadRequest)
		return
	}

	var items []nameInput
	if len(req.Items) > 0 {
		items = req.Items
	} else {
		items = []nameInput{{IP: req.IP, Name: req.Name, Note: req.Note}}
	}

	count := 0
	for _, item := range items {
		ip := cleanInput(item.IP, 128)
		if ip == "" {
			continue // 跳过空行/无效行，不整批失败
		}
		name := cleanInput(item.Name, 128)
		note := cleanInput(item.Note, 256)
		if err := s.db.UpsertManualName(ip, name, note); err != nil {
			http.Error(w, "保存客户端名称失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		count++
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": count})
}

// deleteClientName 清除某 IP 的手动配置（保留自动名称作为兜底）。
func (s *Server) deleteClientName(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ip := cleanInput(r.URL.Query().Get("ip"), 128)
	if ip == "" {
		http.Error(w, "缺少 ip 参数", http.StatusBadRequest)
		return
	}
	if err := s.db.ClearManualName(ip); err != nil {
		http.Error(w, "清除客户端名称失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// cleanInput 清洗用户输入：去首尾空白、把换行换成空格、按 rune 截断长度。
func cleanInput(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > max {
		s = string(r[:max])
	}
	return s
}
