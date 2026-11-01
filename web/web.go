package web

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clash-statistics/database"
	"clash-statistics/utils"
)

//go:embed templates/*
var templateFiles embed.FS

//go:embed static/*
var staticFiles embed.FS

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
// performDatabaseMaintenance 启动时的维护：只清理过期数据
// VACUUM 留给定时器去做 —— 每次启动都全库 VACUUM 一次，库稍大就会明显拖慢启动
func (s *Server) performDatabaseMaintenance() {
	log := utils.GetLogger()

	if err := s.db.CleanupOldData(s.config.DBCleanupDays); err != nil {
		log.Error("启动数据清理失败: %v\n", err)
		return
	}

	log.Info("启动数据清理完成\n")
}

// Handler 返回 HTTP 路由入口
// 后续若要加鉴权，只需要在返回值上再包一层中间件
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.showDashboard)
	mux.HandleFunc("/host-stats", s.showHostStats)
	mux.HandleFunc("/sourceip-stats", s.showSourceIPStats)
	mux.HandleFunc("/online-time", s.showOnlineTime)
	mux.HandleFunc("/api/stats", s.getStats)
	mux.HandleFunc("/api/sourceip-stats", s.getSourceIPStats)
	mux.HandleFunc("/api/target-detail", s.getTargetDetail)
	mux.HandleFunc("/api/sourceip-detail", s.getSourceIPDetail)
	mux.HandleFunc("/api/gantt", s.getGanttData)
	mux.HandleFunc("/refresh", s.refreshData)
	mux.HandleFunc("/favicon.svg", s.serveFavicon)
	// 静态资源（CSS / JS / 图标），全部编译进二进制，随程序分发
	mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	return gzipMiddleware(mux)
}

// gzipResponseWriter 把响应体接到 gzip.Writer 上
type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (g gzipResponseWriter) Write(b []byte) (int, error) {
	return g.writer.Write(b)
}

// gzipMiddleware 压缩响应体
// 甘特图这类接口里有大量重复的 0，压缩后体积能差一个数量级；
// 预编译的 CSS/JS 同样受益，它们本来就要 embed 进二进制发给浏览器
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		zw := gzip.NewWriter(w)
		defer zw.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		// 压缩后长度变了，原有的 Content-Length 不能留
		w.Header().Del("Content-Length")

		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, writer: zw}, r)
	})
}

// serveFavicon 返回站点图标
func (s *Server) serveFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/favicon.svg")
	if err != nil {
		http.Error(w, "图标不存在", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// Start 启动 Web 服务器
func (s *Server) Start(port string) {
	log := utils.GetLogger()
	log.Startup("服务器启动在端口 %s\n", port)

	// 启动完成后，关闭控制台输出，只保留文件输出
	log.SetConsoleOutput(false)

	// 在单独的 goroutine 中定期获取数据
	go s.periodicallyRefreshData()

	// 在单独的 goroutine 中定期执行数据库维护任务
	go s.startDatabaseMaintenance()

	err := http.ListenAndServe(":"+port, s.Handler())
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

		// 转换 utils.Connection 到 database.Connection
		dbConnections := make([]database.Connection, len(connectionInfo.Connections))
		for i, conn := range connectionInfo.Connections {
			dbConnections[i] = database.FromClashConnection(conn)
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

// resolveTimeZoneName 取一个能看懂的时区名
//
// time.Local.String() 在没有设置 TZ 环境变量时返回 "Local"，展示出来毫无意义；
// 而 time.Zone() 给的缩写（如 CST）是有歧义的（中国/古巴/美国中部都叫 CST）。
// 所以按优先级取：TZ 环境变量 → /etc/localtime 指向的 IANA 名称 → 缩写。
// 容器里已经设了 TZ=Asia/Shanghai，这里主要改善裸机/开发环境下的显示
func resolveTimeZoneName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}

	// EvalSymlinks 而不是 Readlink：/etc/localtime 指向的名称本身也可能是个别名
	// （例如 Asia/Chongqing 是 Asia/Shanghai 的链接），解析到底才拿到规范名
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if idx := strings.LastIndex(target, "zoneinfo/"); idx >= 0 {
			return target[idx+len("zoneinfo/"):]
		}
	}

	if name, _ := time.Now().Zone(); name != "" && name != "Local" {
		return name
	}

	return time.Now().Location().String()
}

// parseTimeRange 解析请求里的起始/结束时间（datetime-local 格式）
func parseTimeRange(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	var startTimestamp, endTimestamp int64

	if startTime := r.URL.Query().Get("startTime"); startTime != "" {
		ts, err := parseDateTimeToTimestamp(startTime)
		if err != nil {
			http.Error(w, "无效的起始时间格式: "+err.Error(), http.StatusBadRequest)
			return 0, 0, false
		}
		startTimestamp = ts
	}

	if endTime := r.URL.Query().Get("endTime"); endTime != "" {
		ts, err := parseDateTimeToTimestamp(endTime)
		if err != nil {
			http.Error(w, "无效的结束时间格式: "+err.Error(), http.StatusBadRequest)
			return 0, 0, false
		}
		endTimestamp = ts
	}

	return startTimestamp, endTimestamp, true
}

// getStats 返回按目标聚合的统计信息
func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	startTimestamp, endTimestamp, ok := parseTimeRange(w, r)
	if !ok {
		return
	}

	search, sortBy, order := parseTableParams(r)

	stats, err := s.db.GetTargetStats(startTimestamp, endTimestamp, search, sortBy, order)
	if err != nil {
		http.Error(w, "获取统计数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(stats)
}

// getSourceIPStats 返回按客户端（源IP）聚合的统计信息
func (s *Server) getSourceIPStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	startTimestamp, endTimestamp, ok := parseTimeRange(w, r)
	if !ok {
		return
	}

	search, sortBy, order := parseTableParams(r)

	stats, err := s.db.GetSourceIPStatsWithRange(startTimestamp, endTimestamp, search, sortBy, order)
	if err != nil {
		http.Error(w, "获取客户端统计数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(stats)
}

// parseTableParams 解析表格通用的搜索与排序参数
// 排序字段只接受白名单里的键，方向只接受 asc / desc，其余一律回落默认值，
// 绝不能把用户输入拼进 SQL
func parseTableParams(r *http.Request) (string, string, string) {
	query := r.URL.Query()

	order := query.Get("order")
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	return query.Get("q"), query.Get("sort"), order
}

// parseSplitParams 解析明细的拆分开关：splitChain=1 / splitRule=1
// 默认都不打开，明细只按单一维度（客户端或目标）聚合
func parseSplitParams(r *http.Request) (bool, bool) {
	query := r.URL.Query()
	return query.Get("splitChain") == "1", query.Get("splitRule") == "1"
}

// getTargetDetail 返回某个目标（域名/IP）的明细
func (s *Server) getTargetDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "缺少 target 参数", http.StatusBadRequest)
		return
	}

	startTimestamp, endTimestamp, ok := parseTimeRange(w, r)
	if !ok {
		return
	}

	splitChain, splitRule := parseSplitParams(r)
	search, sortBy, order := parseTableParams(r)

	detail, err := s.db.GetTargetDetail(target, startTimestamp, endTimestamp,
		splitChain, splitRule, search, sortBy, order)
	if err != nil {
		http.Error(w, "获取目标明细失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(detail)
}

// getSourceIPDetail 返回某个客户端（源IP）的明细
func (s *Server) getSourceIPDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sourceIP := r.URL.Query().Get("ip")
	if sourceIP == "" {
		http.Error(w, "缺少 ip 参数", http.StatusBadRequest)
		return
	}

	startTimestamp, endTimestamp, ok := parseTimeRange(w, r)
	if !ok {
		return
	}

	splitChain, splitRule := parseSplitParams(r)
	search, sortBy, order := parseTableParams(r)

	detail, err := s.db.GetSourceIPDetail(sourceIP, startTimestamp, endTimestamp,
		splitChain, splitRule, search, sortBy, order)
	if err != nil {
		http.Error(w, "获取客户端明细失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(detail)
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
		dbConnections[i] = database.FromClashConnection(conn)
	}

	// 保存到数据库
	err = s.db.SaveConnections(dbConnections)
	if err != nil {
		http.Error(w, "保存连接信息到数据库失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "数据已刷新"})
}

// showHostStats 显示目标统计页面
func (s *Server) showHostStats(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "目标统计", "host-stats", "host-stats.html")
}

// showSourceIPStats 显示客户端统计页面
func (s *Server) showSourceIPStats(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "客户端统计", "sourceip-stats", "sourceip-stats.html")
}

// showOnlineTime 显示在线状态页面
func (s *Server) showOnlineTime(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "在线状态", "online-time", "online-time.html")
}

// templateCache 缓存已解析的模板
// 模板内容是编译进二进制的常量，没必要每个请求重新解析一遍
var (
	templateCache   = map[string]*template.Template{}
	templateCacheMu sync.RWMutex
)

// parseTemplate 解析（并缓存）某个页面的模板
func parseTemplate(contentTemplate string) (*template.Template, error) {
	templateCacheMu.RLock()
	tmpl, ok := templateCache[contentTemplate]
	templateCacheMu.RUnlock()
	if ok {
		return tmpl, nil
	}

	parsed, err := template.ParseFS(templateFiles,
		"templates/base.html", "templates/table.html", "templates/detail-modal.html",
		"templates/"+contentTemplate)
	if err != nil {
		return nil, err
	}

	templateCacheMu.Lock()
	templateCache[contentTemplate] = parsed
	templateCacheMu.Unlock()

	return parsed, nil
}

// renderPage 渲染页面的通用函数
func (s *Server) renderPage(w http.ResponseWriter, title, currentPage, contentTemplate string) {
	tmpl, err := parseTemplate(contentTemplate)
	if err != nil {
		http.Error(w, "解析模板失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 获取本地时区信息
	timeZoneName := resolveTimeZoneName()

	// 计算时区偏移
	_, offset := time.Now().Zone()
	offsetHours := offset / 3600
	offsetMinutes := (offset % 3600) / 60
	timeZoneOffset := fmt.Sprintf("GMT%+d", offsetHours)
	if offsetMinutes != 0 {
		timeZoneOffset = fmt.Sprintf("GMT%+d:%02d", offsetHours, offsetMinutes)
	}

	data := struct {
		Title          string
		CurrentPage    string
		CurrentDate    string
		TimeZoneName   string
		TimeZoneOffset string
		Version        string
		CurrentYear    string
	}{
		Title:          title,
		CurrentPage:    currentPage,
		CurrentDate:    time.Now().Format("2006-01-02"),
		TimeZoneName:   timeZoneName,
		TimeZoneOffset: timeZoneOffset,
		Version:        utils.Version,
		CurrentYear:    time.Now().Format("2006"),
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// parseDateTimeToTimestamp 将 datetime-local 格式的时间字符串转换为 Unix 时间戳
// 输入格式: "2006-01-02T15:04" (浏览器 datetime-local 输入格式)
// 输出: Unix 时间戳（秒）
func parseDateTimeToTimestamp(dateTimeStr string) (int64, error) {
	layout := "2006-01-02T15:04"
	// 浏览器 datetime-local 输入里没有时区信息，必须按本地时区解析。
	// 用 time.Parse 会被当成 UTC，在与 Asia/Shanghai 相差 8 小时的情况下筛选会完全失真
	t, err := time.ParseInLocation(layout, dateTimeStr, time.Now().Location())
	if err != nil {
		return 0, fmt.Errorf("解析时间失败: %w", err)
	}
	return t.Unix(), nil
}
