package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"clash-statistics/database"
	"clash-statistics/utils"
)

//go:embed templates/*
var templateFiles embed.FS

// Server 结构体
type Server struct {
	config *utils.Config
	db     *database.Database
}

// NewServer 创建新的服务器实例
func NewServer(config *utils.Config, db *database.Database) *Server {
	return &Server{
		config: config,
		db:     db,
	}
}

// Start 启动 Web 服务器
func (s *Server) Start(port string) {
	// 设置静态文件路由
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../static/"))))

	// 设置路由
	http.HandleFunc("/", s.showDashboard)
	http.HandleFunc("/api/stats", s.getStats)
	http.HandleFunc("/api/sourceip-stats", s.getSourceIPStats)
	http.HandleFunc("/api/gantt", s.getGanttData)
	http.HandleFunc("/refresh", s.refreshData)

	log := utils.GetLogger()
	log.Startup("服务器启动在端口 %s\n", port)
	
	// 启动完成后，关闭控制台输出，只保留文件输出
	log.SetConsoleOutput(false)
	
	// 在单独的 goroutine 中定期获取数据
	go s.periodicallyRefreshData()
	
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		// 启动失败时，确保错误信息也输出到控制台
		log.SetConsoleOutput(true)
		log.Error("启动服务器失败: %v\n", err)
		return
	}
}

// periodicallyRefreshData 定期刷新数据
func (s *Server) periodicallyRefreshData() {
	log := utils.GetLogger()
	ticker := time.NewTicker(time.Duration(s.config.ClashInterval) * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// 从 Clash API 获取连接信息
		connectionInfo, err := utils.GetConnectionsFromClash(s.config.ClashHost, s.config.ClashSecret, s.config.ClashInterval)
		if err != nil {
			log.Error("获取连接信息失败: %v\n", err)
			continue
		}

		// 获取当前活跃的连接ID
		currentActive := make(map[string]bool)
		for _, conn := range connectionInfo.Connections {
			currentActive[conn.ID] = true
		}

		// 获取数据库中所有未关闭的连接ID
		openConnections, err := s.db.GetOpenConnectionIDs()
		if err != nil {
			log.Error("获取未关闭连接ID失败: %v\n", err)
		} else {
			// 检查哪些连接在数据库中存在但当前不在活跃列表中，这些连接已经关闭
			for _, connID := range openConnections {
				if !currentActive[connID] {
					// 连接已关闭，将最后一次更新的时间作为关闭时间
					closedTime := time.Now().Format("2006-01-02 15:04:05")
					err := s.db.SetConnectionClosedTime(connID, closedTime)
					if err != nil {
						log.Error("设置连接关闭时间失败: %v\n", err)
					} else {
						log.Info("连接 %s 已关闭，关闭时间: %s\n", connID, closedTime)
					}
				}
			}
		}

		// 转换 utils.Connection 到 database.Connection
		dbConnections := make([]database.Connection, len(connectionInfo.Connections))
		for i, conn := range connectionInfo.Connections {
			dbConnections[i] = database.Connection{
				ID:                conn.ID,
				Chain:             conn.Chain,
				Rule:              conn.Rule,
				RulePayload:       conn.RulePayload,
				Download:          conn.Download,
				Upload:            conn.Upload,
				SourceIP:          conn.SourceIP,
				SourcePort:        conn.SourcePort,
				DestPort:          conn.DestPort,
				DestIP:            conn.DestIP,
				StartTime:         conn.StartTime,
				ClosedTime:        conn.ClosedTime,
				Network:           conn.Network,
				ConnectionType:    conn.ConnectionType,
				SourceIPAddr:      conn.SourceIPAddr,
				DestinationIP:     conn.DestinationIP,
				SourcePortNum:     conn.SourcePortNum,
				DestinationPort:   conn.DestinationPort,
				Host:              conn.Host,
				DNSMode:           conn.DNSMode,
				Uid:               conn.Uid,
				Process:           conn.Process,
				ProcessPath:       conn.ProcessPath,
				SpecialProxy:      conn.SpecialProxy,
				SpecialRules:      conn.SpecialRules,
				RemoteDestination: conn.RemoteDestination,
				DSCP:              conn.DSCP,
				SniffHost:         conn.SniffHost,
				InboundIP:         conn.InboundIP,
				InboundPort:       conn.InboundPort,
				InboundName:       conn.InboundName,
				InboundUser:       conn.InboundUser,
				SourceGeoIP:       conn.SourceGeoIP,
				DestinationGeoIP:  conn.DestinationGeoIP,
				SourceIPASN:       conn.SourceIPASN,
				DestinationIPASN:  conn.DestinationIPASN,
			}
		}

		// 保存当前连接到数据库（每次保存时都会更新closed_time为当前时间）
		err = s.db.SaveConnections(dbConnections)
		if err != nil {
			log.Error("保存连接信息到数据库失败: %v\n", err)
		} else {
			log.Info("成功保存 %d 个连接到数据库\n", len(dbConnections))
		}
	}
}

// showDashboard 显示仪表板页面
func (s *Server) showDashboard(w http.ResponseWriter, r *http.Request) {
	// 解析模板文件
	tmpl, err := template.ParseFS(templateFiles, "templates/dashboard.html")
	if err != nil {
		http.Error(w, "解析模板失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		CurrentDate string
	}{
		CurrentDate: time.Now().Format("2006-01-02"),
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// getStats 返回统计信息的 API 端点
func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats, err := s.db.GetStats()
	if err != nil {
		http.Error(w, "获取统计数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(stats)
}

// getSourceIPStats 返回SourceIP统计信息的 API 端点
func (s *Server) getSourceIPStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	log := utils.GetLogger()
	log.Info("正在获取SourceIP统计数据...\n")
	stats, err := s.db.GetSourceIPStats()
	if err != nil {
		log.Error("获取SourceIP统计数据失败: %v\n", err)
		http.Error(w, "获取SourceIP统计数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Info("成功获取 %d 条SourceIP统计数据\n", len(stats))
	json.NewEncoder(w).Encode(stats)
}

// getGanttData 返回甘特图数据的 API 端点
func (s *Server) getGanttData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02") // 使用今天日期
	}

	ganttData, err := s.db.GetGanttData(date)
	if err != nil {
		http.Error(w, "获取甘特图数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(ganttData)
}

// refreshData 刷新数据的 API 端点
func (s *Server) refreshData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 从 Clash API 获取连接信息
	connectionInfo, err := utils.GetConnectionsFromClash(s.config.ClashHost, s.config.ClashSecret, s.config.ClashInterval)
	if err != nil {
		http.Error(w, "获取连接信息失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 转换 utils.Connection 到 database.Connection
	dbConnections := make([]database.Connection, len(connectionInfo.Connections))
	for i, conn := range connectionInfo.Connections {
		dbConnections[i] = database.Connection{
			ID:                conn.ID,
			Chain:             conn.Chain,
			Rule:              conn.Rule,
			RulePayload:       conn.RulePayload,
			Download:          conn.Download,
			Upload:            conn.Upload,
			SourceIP:          conn.SourceIP,
			SourcePort:        conn.SourcePort,
			DestPort:          conn.DestPort,
			DestIP:            conn.DestIP,
			StartTime:         conn.StartTime,
			ClosedTime:        conn.ClosedTime,
			Network:           conn.Network,
			ConnectionType:    conn.ConnectionType,
			SourceIPAddr:      conn.SourceIPAddr,
			DestinationIP:     conn.DestinationIP,
			SourcePortNum:     conn.SourcePortNum,
			DestinationPort:   conn.DestinationPort,
			Host:              conn.Host,
			DNSMode:           conn.DNSMode,
			Uid:               conn.Uid,
			Process:           conn.Process,
			ProcessPath:       conn.ProcessPath,
			SpecialProxy:      conn.SpecialProxy,
			SpecialRules:      conn.SpecialRules,
			RemoteDestination: conn.RemoteDestination,
			DSCP:              conn.DSCP,
			SniffHost:         conn.SniffHost,
			InboundIP:         conn.InboundIP,
			InboundPort:       conn.InboundPort,
			InboundName:       conn.InboundName,
			InboundUser:       conn.InboundUser,
			SourceGeoIP:       conn.SourceGeoIP,
			DestinationGeoIP:  conn.DestinationGeoIP,
			SourceIPASN:       conn.SourceIPASN,
			DestinationIPASN:  conn.DestinationIPASN,
		}
	}

	// 保存到数据库
	err = s.db.SaveConnections(dbConnections)
	if err != nil {
		http.Error(w, "保存连接信息到数据库失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "数据已刷新"})
}
