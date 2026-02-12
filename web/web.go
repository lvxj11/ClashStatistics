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

// NewServer 创建一个新的服务器实例
func NewServer(config *utils.Config, db *database.Database) *Server {
	return &Server{
		config: config,
		db:     db,
	}
}

// startDatabaseMaintenance 启动数据库维护任务
func (s *Server) startDatabaseMaintenance() {
	log := utils.GetLogger()

	// 初始化定时器
	cleanupTicker := time.NewTicker(24 * time.Hour)
	vacuumTicker := time.NewTicker(time.Duration(s.config.DBVacuumInterval) * time.Hour)

	// 立即执行一次初始化维护
	s.performDatabaseMaintenance()

	for {
		select {
		case <-cleanupTicker.C:
			// 每天执行一次数据清理
			if err := s.db.CleanupOldData(s.config.DBCleanupDays); err != nil {
				log.Error("清理过期数据失败: %v\n", err)
			} else {
				log.Info("成功清理过期数据\n")
			}
		case <-vacuumTicker.C:
			// 定期执行数据库VACUUM
			if err := s.db.Vacuum(); err != nil {
				log.Error("数据库VACUUM失败: %v\n", err)
			} else {
				log.Info("数据库VACUUM成功\n")
			}
		}
	}
}

// performDatabaseMaintenance 执行数据库维护任务
func (s *Server) performDatabaseMaintenance() {
	log := utils.GetLogger()

	// 检查数据库大小
	dbSize, err := s.db.GetDatabaseSize()
	if err != nil {
		log.Error("获取数据库大小失败: %v\n", err)
	} else {
		log.Info("当前数据库大小: %d bytes (%.2f MB)\n", dbSize, float64(dbSize)/1024/1024)
	}

	// 执行一次数据清理
	if err := s.db.CleanupOldData(s.config.DBCleanupDays); err != nil {
		log.Error("初始化数据清理失败: %v\n", err)
	} else {
		log.Info("初始化数据清理完成\n")
	}

	// 执行一次数据库VACUUM
	if err := s.db.Vacuum(); err != nil {
		log.Error("初始化数据库VACUUM失败: %v\n", err)
	} else {
		log.Info("初始化数据库VACUUM完成\n")
	}

	// 再次检查数据库大小，比较清理前后的变化
	newDbSize, err := s.db.GetDatabaseSize()
	if err != nil {
		log.Error("获取清理后数据库大小失败: %v\n", err)
	} else {
		log.Info("清理后数据库大小: %d bytes (%.2f MB)\n", newDbSize, float64(newDbSize)/1024/1024)
		if dbSize > newDbSize {
			log.Info("数据库大小减少: %d bytes (%.2f MB)\n", dbSize-newDbSize, float64(dbSize-newDbSize)/1024/1024)
		}
	}
}

// Start 启动 Web 服务器
func (s *Server) Start(port string) {
	// 设置静态文件路由
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../static/"))))

	// 设置路由
	http.HandleFunc("/", s.showDashboard)
	http.HandleFunc("/host-stats", s.showHostStats)
	http.HandleFunc("/sourceip-stats", s.showSourceIPStats)
	http.HandleFunc("/online-time", s.showOnlineTime)
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

	// 在单独的 goroutine 中定期执行数据库维护任务
	go s.startDatabaseMaintenance()

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
					// 连接已关闭，更新 end_timestamp
					err := s.db.SetConnectionClosedTime(connID)
					if err != nil {
						log.Error("设置连接关闭时间失败: %v\n", err)
					} else {
						log.Info("连接 %s 已关闭\n", connID)
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

// showDashboard 显示仪表板页面（重定向到Host/IP统计页面）
func (s *Server) showDashboard(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/host-stats", http.StatusMovedPermanently)
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

// showHostStats 显示Host/IP统计页面
func (s *Server) showHostStats(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "Host/IP 统计", "host-stats", "host-stats.html")
}

// showSourceIPStats 显示SourceIP统计页面
func (s *Server) showSourceIPStats(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "SourceIP 统计", "sourceip-stats", "sourceip-stats.html")
}

// showOnlineTime 显示SourceIP在线时间页面
func (s *Server) showOnlineTime(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "SourceIP 在线时间", "online-time", "online-time.html")
}

// renderPage 渲染页面的通用函数
func (s *Server) renderPage(w http.ResponseWriter, title, currentPage, contentTemplate string) {
	tmpl, err := template.ParseFS(templateFiles, "templates/base.html", "templates/"+contentTemplate)
	if err != nil {
		http.Error(w, "解析模板失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Title       string
		CurrentPage string
		CurrentDate string
	}{
		Title:       title,
		CurrentPage: currentPage,
		CurrentDate: time.Now().Format("2006-01-02"),
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
