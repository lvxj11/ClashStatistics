package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Config 存储配置信息
type Config struct {
	ClashHost     string
	ClashSecret   string
	ClashInterval int
}

// Metadata 包含连接元数据
type Metadata struct {
	Network string `json:"network"`
	Type    string `json:"type"`
	SourceIP string `json:"sourceIP"`
	DestIP   string `json:"destinationIP"`
	SourcePort string `json:"sourcePort"`
	DestPort string `json:"destinationPort"`
	Host    string `json:"host"`
	DNSMode string `json:"dnsMode"`
	Uid     int    `json:"uid"`
	Process string `json:"process"`
	ProcessPath string `json:"processPath"`
	SpecialProxy string `json:"specialProxy"`
	SpecialRules string `json:"specialRules"`
	RemoteDestination string `json:"remoteDestination"`
	DSCP      int    `json:"dscp"`
	SniffHost string `json:"sniffHost"`
	InboundIP string `json:"inboundIP"`
	InboundPort string `json:"inboundPort"`
	InboundName string `json:"inboundName"`
	InboundUser string `json:"inboundUser"`
	SourceGeoIP interface{} `json:"sourceGeoIP"` // 可能为 null
	DestinationGeoIP interface{} `json:"destinationGeoIP"` // 可能为 null
	SourceIPASN string `json:"sourceIPASN"`
	DestinationIPASN string `json:"destinationIPASN"`
}

// ConnectionInfo 表示 Clash 连接信息
type ConnectionInfo struct {
	DownloadSpeed float64 `json:"downloadSpeed"`
	UploadSpeed   float64 `json:"uploadSpeed"`
	Connections   []Connection `json:"connections"`
	DownloadTotal float64 `json:"downloadTotal"`
	UploadTotal   float64 `json:"uploadTotal"`
}

// Connection 表示单个连接
type Connection struct {
	ID                  string `json:"id"`
	Chain               []string `json:"chain"`
	Rule                string `json:"rule"`
	RulePayload         string `json:"rulePayload"`
	Download            int64 `json:"download"`
	Upload              int64 `json:"upload"`
	SourceIP            string `json:"srcIP"`
	SourcePort          string `json:"srcPort"`
	DestPort            string `json:"dstPort"`
	DestIP              string `json:"dstIP"`
	StartTime           string `json:"start"`
	ClosedTime          string `json:"closed,omitempty"` // 连接关闭时间
	Metadata            Metadata `json:"metadata"` // 保留原始结构用于解析
	// 从 Metadata 展开的字段（用于数据库存储）
	Network             string `json:"-"` // 不从 JSON 解析，而是从 Metadata 映射
	ConnectionType      string `json:"-"` // 避免与 Go 关键字冲突
	SourceIPAddr        string `json:"-"`
	DestinationIP       string `json:"-"`
	SourcePortNum       string `json:"-"`
	DestinationPort     string `json:"-"`
	Host                string `json:"-"`
	DNSMode             string `json:"-"`
	Uid                 int    `json:"-"`
	Process             string `json:"-"`
	ProcessPath         string `json:"-"`
	SpecialProxy        string `json:"-"`
	SpecialRules        string `json:"-"`
	RemoteDestination   string `json:"-"`
	DSCP                int    `json:"-"`
	SniffHost           string `json:"-"`
	InboundIP           string `json:"-"`
	InboundPort         string `json:"-"`
	InboundName         string `json:"-"`
	InboundUser         string `json:"-"`
	SourceGeoIP         interface{} `json:"-"` // 可能为 null
	DestinationGeoIP    interface{} `json:"-"` // 可能为 null
	SourceIPASN         string `json:"-"`
	DestinationIPASN    string `json:"-"`
}

// LoadConfig 从 config.ini 加载配置
func LoadConfig() (*Config, error) {
	config := &Config{
		ClashHost:    "127.0.0.1:9090",
		ClashSecret:  "",
		ClashInterval: 1000,
	}

	file, err := os.Open("config.ini")
	if err != nil {
		// 如果配置文件不存在，返回默认配置
		return config, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentSection string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		
		// 处理节名 [clash]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		
		// 处理键值对
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			
			// 移除引号（如果存在）
			value = strings.Trim(value, "\"'")
			
			if currentSection == "clash" {
				switch strings.ToLower(key) {
				case "host":
					config.ClashHost = value
				case "secret":
					config.ClashSecret = value
				case "interval":
					intVal, err := strconv.Atoi(value)
					if err == nil {
						config.ClashInterval = intVal
					}
				}
			}
		}
	}
	
	return config, scanner.Err()
}

// GetConnectionsFromClash 从 Clash API 获取连接信息
func GetConnectionsFromClash(config *Config) (*ConnectionInfo, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	url := fmt.Sprintf("http://%s/connections?interval=%d", config.ClashHost, config.ClashInterval)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	// 添加认证头
	if config.ClashSecret != "" {
		req.Header.Set("Authorization", "Bearer "+config.ClashSecret)
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	var connectionInfo ConnectionInfo
	err = json.Unmarshal(body, &connectionInfo)
	if err != nil {
		return nil, err
	}
	
	// 处理每个连接，将 metadata 字段映射到 Connection 结构体的顶层字段
	for i := range connectionInfo.Connections {
		conn := &connectionInfo.Connections[i]
		// 将 metadata 字段映射到顶层字段
		conn.Network = conn.Metadata.Network
		conn.ConnectionType = conn.Metadata.Type
		conn.SourceIPAddr = conn.Metadata.SourceIP
		conn.DestinationIP = conn.Metadata.DestIP
		conn.SourcePortNum = conn.Metadata.SourcePort
		conn.DestinationPort = conn.Metadata.DestPort
		conn.Host = conn.Metadata.Host
		conn.DNSMode = conn.Metadata.DNSMode
		conn.Uid = conn.Metadata.Uid
		conn.Process = conn.Metadata.Process
		conn.ProcessPath = conn.Metadata.ProcessPath
		conn.SpecialProxy = conn.Metadata.SpecialProxy
		conn.SpecialRules = conn.Metadata.SpecialRules
		conn.RemoteDestination = conn.Metadata.RemoteDestination
		conn.DSCP = conn.Metadata.DSCP
		conn.SniffHost = conn.Metadata.SniffHost
		conn.InboundIP = conn.Metadata.InboundIP
		conn.InboundPort = conn.Metadata.InboundPort
		conn.InboundName = conn.Metadata.InboundName
		conn.InboundUser = conn.Metadata.InboundUser
		conn.SourceGeoIP = conn.Metadata.SourceGeoIP
		conn.DestinationGeoIP = conn.Metadata.DestinationGeoIP
		conn.SourceIPASN = conn.Metadata.SourceIPASN
		conn.DestinationIPASN = conn.Metadata.DestinationIPASN
		
		// 调试输出（仅在需要时启用）
		// fmt.Printf("Debug: Processing connection ID: %s, Host: %s, DestinationIP: %s\n", conn.ID, conn.Host, conn.DestinationIP)
	}
	
	return &connectionInfo, nil
}

// Database 结构体
type Database struct {
	db *sql.DB
}

// 初始化数据库
func InitDB() (*Database, error) {
	db, err := sql.Open("sqlite3", "./clash_statistics.db")
	if err != nil {
		return nil, err
	}

	// 创建表
	sqlStmt := `
	CREATE TABLE IF NOT EXISTS connections (
		id TEXT PRIMARY KEY,
		network TEXT,
		type TEXT,
		source_ip TEXT,
		destination_ip TEXT,
		source_port TEXT,
		destination_port TEXT,
		host TEXT,
		dns_mode TEXT,
		uid INTEGER,
		process TEXT,
		process_path TEXT,
		special_proxy TEXT,
		special_rules TEXT,
		remote_destination TEXT,
		dscp INTEGER,
		sniff_host TEXT,
		inbound_ip TEXT,
		inbound_port TEXT,
		inbound_name TEXT,
		inbound_user TEXT,
		source_geoip TEXT,
		destination_geoip TEXT,
		source_ipasn TEXT,
		destination_ipasn TEXT,
		upload INTEGER,
		download INTEGER,
		start_time TEXT,
		closed_time TEXT NOT NULL DEFAULT '',
		chain TEXT,
		rule TEXT,
		rule_payload TEXT,
		source_real_ip TEXT,
		source_real_port TEXT,
		dest_real_ip TEXT,
		dest_real_port TEXT,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err = db.Exec(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("创建表失败: %v", err)
	}

	return &Database{db: db}, nil
}

// 保存连接到数据库
func (d *Database) SaveConnection(conn Connection) error {
	chainStr := ""
	if len(conn.Chain) > 0 {
		chainStr = strings.Join(conn.Chain, ",")
	}

	// 每次保存时都更新 closed_time 为当前时间
	currentTime := time.Now().Format("2006-01-02 15:04:05")

	stmt, err := d.db.Prepare(`INSERT OR REPLACE INTO connections (
		id, network, type, source_ip, destination_ip, source_port, destination_port, 
		host, dns_mode, uid, process, process_path, special_proxy, special_rules, 
		remote_destination, dscp, sniff_host, inbound_ip, inbound_port, inbound_name, 
		inbound_user, source_geoip, destination_geoip, source_ipasn, destination_ipasn, 
		upload, download, start_time, closed_time, chain, rule, rule_payload, source_real_ip, 
		source_real_port, dest_real_ip, dest_real_port, last_seen
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// 处理可能为 null 的字段
	sourceGeoIPStr := ""
	if conn.SourceGeoIP != nil {
		sourceGeoIPStr = fmt.Sprintf("%v", conn.SourceGeoIP)
	}
	destinationGeoIPStr := ""
	if conn.DestinationGeoIP != nil {
		destinationGeoIPStr = fmt.Sprintf("%v", conn.DestinationGeoIP)
	}

	_, err = stmt.Exec(
		conn.ID, conn.Network, conn.ConnectionType, conn.SourceIPAddr, conn.DestinationIP,
		conn.SourcePortNum, conn.DestinationPort, conn.Host, conn.DNSMode, conn.Uid,
		conn.Process, conn.ProcessPath, conn.SpecialProxy, conn.SpecialRules,
		conn.RemoteDestination, conn.DSCP, conn.SniffHost, conn.InboundIP, conn.InboundPort,
		conn.InboundName, conn.InboundUser, sourceGeoIPStr, destinationGeoIPStr,
		conn.SourceIPASN, conn.DestinationIPASN, conn.Upload, conn.Download,
		conn.StartTime, currentTime, chainStr, conn.Rule, conn.RulePayload, conn.SourceIP, conn.SourcePort,
		conn.DestIP, conn.DestPort, currentTime,
	)
	return err
}

// 保存多个连接到数据库
func (d *Database) SaveConnections(connections []Connection) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO connections (
		id, network, type, source_ip, destination_ip, source_port, destination_port, 
		host, dns_mode, uid, process, process_path, special_proxy, special_rules, 
		remote_destination, dscp, sniff_host, inbound_ip, inbound_port, inbound_name, 
		inbound_user, source_geoip, destination_geoip, source_ipasn, destination_ipasn, 
		upload, download, start_time, closed_time, chain, rule, rule_payload, source_real_ip, 
		source_real_port, dest_real_ip, dest_real_port, last_seen
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, conn := range connections {
		chainStr := ""
		if len(conn.Chain) > 0 {
			chainStr = strings.Join(conn.Chain, ",")
		}

		// 每次保存时都更新 closed_time 为当前时间
		currentTime := time.Now().Format("2006-01-02 15:04:05")

		// 处理可能为 null 的字段
		sourceGeoIPStr := ""
		if conn.SourceGeoIP != nil {
			sourceGeoIPStr = fmt.Sprintf("%v", conn.SourceGeoIP)
		}
		destinationGeoIPStr := ""
		if conn.DestinationGeoIP != nil {
			destinationGeoIPStr = fmt.Sprintf("%v", conn.DestinationGeoIP)
		}

		_, err = stmt.Exec(
			conn.ID, conn.Network, conn.ConnectionType, conn.SourceIPAddr, conn.DestinationIP,
			conn.SourcePortNum, conn.DestinationPort, conn.Host, conn.DNSMode, conn.Uid,
			conn.Process, conn.ProcessPath, conn.SpecialProxy, conn.SpecialRules,
			conn.RemoteDestination, conn.DSCP, conn.SniffHost, conn.InboundIP, conn.InboundPort,
			conn.InboundName, conn.InboundUser, sourceGeoIPStr, destinationGeoIPStr,
			conn.SourceIPASN, conn.DestinationIPASN, conn.Upload, conn.Download,
			conn.StartTime, currentTime, chainStr, conn.Rule, conn.RulePayload, conn.SourceIP, conn.SourcePort,
			conn.DestIP, conn.DestPort, currentTime,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// 获取统计信息
type StatsEntry struct {
	Target      string // host 或 destinationIP
	Download    int64
	Upload      int64
	Total       int64
}

func (d *Database) GetStats() ([]StatsEntry, error) {
	query := `
		SELECT 
			CASE 
				WHEN host IS NOT NULL AND host != '' THEN host
				WHEN destination_ip IS NOT NULL AND destination_ip != '' THEN destination_ip
				ELSE 'Unknown'
			END AS target,
			SUM(download) AS total_download,
			SUM(upload) AS total_upload,
			SUM(download + upload) AS total
		FROM connections
		WHERE (host IS NOT NULL AND host != '') OR (destination_ip IS NOT NULL AND destination_ip != '')
		GROUP BY 
			CASE 
				WHEN host IS NOT NULL AND host != '' THEN host
				WHEN destination_ip IS NOT NULL AND destination_ip != '' THEN destination_ip
				ELSE 'Unknown'
			END
		ORDER BY total DESC
		LIMIT 100
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []StatsEntry
	count := 0
	for rows.Next() {
		var entry StatsEntry
		err := rows.Scan(&entry.Target, &entry.Download, &entry.Upload, &entry.Total)
		if err != nil {
			return nil, err
		}
		stats = append(stats, entry)
		count++
		// 调试输出（仅在需要时启用）
		// fmt.Printf("Debug: Stats entry %d - Target: %s, Download: %d, Upload: %d, Total: %d\n", count, entry.Target, entry.Download, entry.Upload, entry.Total)
	}

	// 调试输出（仅在需要时启用）
	// fmt.Printf("Debug: Total stats entries retrieved: %d\n", count)

	return stats, nil
}

// 获取SourceIP统计信息
func (d *Database) GetSourceIPStats() ([]StatsEntry, error) {
	query := `
		SELECT 
			source_ip AS target,
			SUM(download) AS total_download,
			SUM(upload) AS total_upload,
			SUM(download + upload) AS total
		FROM connections
		WHERE source_ip IS NOT NULL AND source_ip != ''
		GROUP BY source_ip
		ORDER BY total DESC
		LIMIT 100
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []StatsEntry
	for rows.Next() {
		var entry StatsEntry
		err := rows.Scan(&entry.Target, &entry.Download, &entry.Upload, &entry.Total)
		if err != nil {
			return nil, err
		}
		stats = append(stats, entry)
	}

	return stats, nil
}

// GanttEntry 表示甘特图条目
type GanttEntry struct {
	SourceIP    string        `json:"sourceIP"`
	ConnectionType string     `json:"type"`       // 连接类型
	TimeSlots   [144]bool     `json:"timeSlots"`  // 每天144个10分钟时间段，true表示该时间段有活动
}

// GetGanttData 获取甘特图数据
func (d *Database) GetGanttData(date string) ([]GanttEntry, error) {
	// 构建查询，获取指定日期的连接数据
	query := `
		SELECT 
			source_ip, type, start_time, closed_time
		FROM connections
		WHERE DATE(start_time) = ? OR (closed_time != '' AND DATE(closed_time) = ?)
		ORDER BY source_ip, start_time
	`

	rows, err := d.db.Query(query, date, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 按sourceIP和type组织数据
	ganttData := make(map[string]*GanttEntry)
	
	for rows.Next() {
		var sourceIP, connType, startTime, closedTime string
		
		err := rows.Scan(&sourceIP, &connType, &startTime, &closedTime)
		if err != nil {
			return nil, err
		}
		
		// 解析开始和结束时间
		startTimeParsed, err := time.Parse("2006-01-02T15:04:05Z", startTime)
		if err != nil {
			// 尝试另一种时间格式
			startTimeParsed, err = time.Parse("2006-01-02T15:04:05.000000000Z", startTime)
			if err != nil {
				continue // 跳过无法解析的时间
			}
		}
		
		var endTimeParsed time.Time
		if closedTime != "" {
			endTimeParsed, err = time.Parse("2006-01-02 15:04:05", closedTime)
			if err != nil {
				continue // 跳过无法解析的时间
			}
		} else {
			// 如果没有关闭时间，使用当前时间
			endTimeParsed = time.Now()
		}
		
		// 获取或创建GanttEntry
		key := sourceIP
		if sourceIP == "" {
			if connType == "Inner" {
				key = "Inner"
			} else {
				key = "未知IP"
			}
		}
		
		entry, exists := ganttData[key]
		if !exists {
			entry = &GanttEntry{
				SourceIP:       key,
				ConnectionType: connType,
				TimeSlots:      [144]bool{},
			}
			ganttData[key] = entry
		}
		
		// 计算从开始时间到结束时间之间的所有10分钟时间段
		slotStart := time.Date(startTimeParsed.Year(), startTimeParsed.Month(), startTimeParsed.Day(), 0, 0, 0, 0, startTimeParsed.Location())
		for i := 0; i < 144; i++ {
			slotBegin := slotStart.Add(time.Duration(i*10) * time.Minute)
			slotEnd := slotBegin.Add(10 * time.Minute)
			
			// 检查连接是否在这个时间段内活跃
			if (startTimeParsed.Before(slotEnd) || startTimeParsed.Equal(slotEnd)) && 
			   (endTimeParsed.After(slotBegin) || endTimeParsed.Equal(slotBegin)) {
				entry.TimeSlots[i] = true
			}
		}
	}

	// 转换为切片
	var result []GanttEntry
	for _, entry := range ganttData {
		result = append(result, *entry)
	}

	// 按SourceIP进行自然排序（数字部分按数值排序而非字符串排序）
	sort.Slice(result, func(i, j int) bool {
		return naturalIPCompare(result[i].SourceIP, result[j].SourceIP)
	})

	return result, nil
}

// naturalIPCompare 实现IP地址的自然排序
func naturalIPCompare(ip1, ip2 string) bool {
	// 特殊值处理
	if ip1 == "Inner" && ip2 != "Inner" {
		return true
	}
	if ip2 == "Inner" && ip1 != "Inner" {
		return false
	}
	if ip1 == "未知IP" && ip2 != "未知IP" {
		return true
	}
	if ip2 == "未知IP" && ip1 != "未知IP" {
		return false
	}
	
	// 如果两个都不是IP地址，按字符串排序
	if !isIPAddress(ip1) || !isIPAddress(ip2) {
		return ip1 < ip2
	}
	
	// 将IP地址分解为数字部分进行比较
	parts1 := strings.Split(ip1, ".")
	parts2 := strings.Split(ip2, ".")
	
	// 比较每一部分
	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		num1, err1 := strconv.Atoi(parts1[i])
		num2, err2 := strconv.Atoi(parts2[i])
		
		// 如果都能转换为数字，则按数值比较
		if err1 == nil && err2 == nil {
			if num1 != num2 {
				return num1 < num2
			}
		} else {
			// 如果不能转换为数字，则按字符串比较
			if parts1[i] != parts2[i] {
				return parts1[i] < parts2[i]
			}
		}
	}
	
	// 如果前面部分都相等，比较长度（较长的IP地址通常数值更大）
	return len(parts1) < len(parts2)
}

// isIPAddress 检查字符串是否为IP地址格式
func isIPAddress(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	
	for _, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil || num < 0 || num > 255 {
			return false
		}
	}
	
	return true
}

// UpdateClosedTime 更新连接的关闭时间
func (d *Database) UpdateClosedTime(id, closedTime string) error {
	stmt, err := d.db.Prepare("UPDATE connections SET closed_time=? WHERE id=?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	result, err := stmt.Exec(closedTime, id)
	if err != nil {
		return err
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		fmt.Printf("获取受影响行数失败: %v\n", err)
		return err
	}
	
	if rowsAffected == 0 {
		fmt.Printf("警告: 没有找到ID为 %s 的记录\n", id)
	} else {
		fmt.Printf("成功更新 %d 条记录的closed_time为 %s\n", rowsAffected, closedTime)
	}
	
	return nil
}

// Global variables
var (
	latestConnectionInfo *ConnectionInfo
	db *Database
	activeConnections = make(map[string]bool) // 跟踪活跃连接
)

// StartWebServer 启动 Web 服务器
func StartWebServer(port string, config *Config) {
	// 初始化数据库
	var err error
	db, err = InitDB()
	if err != nil {
		fmt.Printf("初始化数据库失败: %v\n", err)
		return
	}
	defer db.db.Close()
	
	// 设置静态文件路由
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static/"))))
	
	// 设置路由
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		showDashboard(w, r)
	})
	
	http.HandleFunc("/api/connections", func(w http.ResponseWriter, r *http.Request) {
		getConnections(w, r, config)
	})
	
	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		getStats(w, r)
	})
	
	http.HandleFunc("/api/sourceip-stats", func(w http.ResponseWriter, r *http.Request) {
		getSourceIPStats(w, r)
	})
	
	http.HandleFunc("/api/gantt", func(w http.ResponseWriter, r *http.Request) {
		getGanttData(w, r)
	})
	
	http.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		refreshData(w, r, config)
	})
	
	fmt.Printf("服务器启动在端口 %s\n", port)
	
	// 在单独的 goroutine 中定期获取数据
	go periodicallyRefreshData(config)
	
	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Printf("启动服务器失败: %v\n", err)
	}
}

// periodicallyRefreshData 定期刷新数据
func periodicallyRefreshData(config *Config) {
	ticker := time.NewTicker(time.Duration(config.ClashInterval) * time.Millisecond)
	defer ticker.Stop()
	
	for range ticker.C {
		connectionInfo, err := GetConnectionsFromClash(config)
		if err != nil {
			fmt.Printf("获取连接信息失败: %v\n", err)
			// 如果无法连接到 Clash 服务，生成模拟数据用于测试
			connectionInfo = generateMockData()
		}
		
		latestConnectionInfo = connectionInfo
		
		// 获取当前活跃的连接ID集合
		currentActive := make(map[string]bool)
		for _, conn := range connectionInfo.Connections {
			currentActive[conn.ID] = true
		}
		
		// 获取数据库中所有未关闭的连接ID
		openConnections, err := db.GetOpenConnectionIDs()
		if err != nil {
			fmt.Printf("获取未关闭连接ID失败: %v\n", err)
		} else {
			// 检查哪些连接在数据库中存在但当前不在活跃列表中，这些连接已经关闭
			for _, connID := range openConnections {
				if !currentActive[connID] {
					// 连接已关闭，将最后一次更新的时间作为关闭时间
					closedTime := time.Now().Format("2006-01-02 15:04:05")
					err := db.SetConnectionClosedTime(connID, closedTime)
					if err != nil {
						// 可选：记录错误但不输出详细信息
						// fmt.Printf("设置连接关闭时间失败: %v\n", err)
					} else {
						// 可选：记录关闭事件但减少输出频率
						// fmt.Printf("连接 %s 已关闭，关闭时间: %s\n", connID, closedTime)
					}
				}
			}
		}
		
		// 保存当前连接到数据库（每次保存时都会更新closed_time为当前时间）
		err = db.SaveConnections(connectionInfo.Connections)
		if err != nil {
			fmt.Printf("保存连接信息到数据库失败: %v\n", err)
		} else {
			fmt.Printf("成功保存 %d 个连接到数据库\n", len(connectionInfo.Connections))
		}
	}
}

// GetOpenConnectionIDs 获取数据库中所有未关闭的连接ID
func (d *Database) GetOpenConnectionIDs() ([]string, error) {
	rows, err := d.db.Query("SELECT id FROM connections WHERE closed_time = '' OR closed_time IS NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var ids []string
	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	
	return ids, nil
}

// SetConnectionClosedTime 设置连接的关闭时间
func (d *Database) SetConnectionClosedTime(id, closedTime string) error {
	stmt, err := d.db.Prepare("UPDATE connections SET closed_time=?, last_seen=? WHERE id=? AND (closed_time = '' OR closed_time IS NULL)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	
	_, err = stmt.Exec(closedTime, closedTime, id)
	return err
}

// generateMockData 生成模拟数据用于测试
func generateMockData() *ConnectionInfo {
	now := time.Now()
	mockConnections := []Connection{
		{
			ID:           fmt.Sprintf("mock-%d", now.Unix()),
			Network:      "tcp",
			ConnectionType: "HTTP",
			SourceIPAddr: "192.168.1.100",
			DestinationIP: "8.8.8.8",
			SourcePortNum: "54321",
			DestinationPort: "443",
			Host:         "google.com",
			Upload:       102400,
			Download:     204800,
			StartTime:    now.Format("2006-01-02T15:04:05Z"),
			Chain:        []string{"proxy", "direct"},
			Rule:         "DOMAIN-SUFFIX",
			RulePayload:  "google.com",
		},
		{
			ID:           fmt.Sprintf("mock-%d-2", now.Unix()),
			Network:      "udp",
			ConnectionType: "DNS",
			SourceIPAddr: "192.168.1.101",
			DestinationIP: "1.1.1.1",
			SourcePortNum: "12345",
			DestinationPort: "53",
			Host:         "cloudflare-dns.com",
			Upload:       512,
			Download:     1024,
			StartTime:    now.Add(-time.Minute * 5).Format("2006-01-02T15:04:05Z"),
			Chain:        []string{"direct"},
			Rule:         "MATCH",
			RulePayload:  "",
		},
	}
	
	return &ConnectionInfo{
		DownloadSpeed: 1000000,
		UploadSpeed:   500000,
		Connections:   mockConnections,
		DownloadTotal: 1048576,
		UploadTotal:   524288,
	}
}

// showDashboard 显示仪表板页面
func showDashboard(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if page == "" {
		page = "1"
	}
	
	var tmpl string
	var data interface{}
	var err error
	
	switch page {
	case "1":
		tmpl = `
<!DOCTYPE html>
<html>
<head>
    <title>Clash Statistics Dashboard</title>
    <meta charset="utf-8">
    <style>
        :root {
            /* 暗色主题 */
            --bg-color: #1e1e1e;
            --text-color: #e0e0e0;
            --header-bg: #2d2d2d;
            --tab-bg: #3c3c3c;
            --tab-active-bg: #455a64;
            --tab-active-text: white;
            --table-border: #444;
            --table-header-bg: #3c3c3c;
            --table-row-even-bg: #2d2d2d;
            --button-bg: #455a64;
            --button-hover-bg: #546e7a;
            --button-text: white;
            --gantt-grid-color: #444;
            --gantt-active-color: #4CAF50;
            --gantt-active-border: #45a049;
        }
        
        body {
            font-family: Arial, sans-serif;
            margin: 20px;
            background-color: var(--bg-color);
            color: var(--text-color);
            transition: background-color 0.3s, color 0.3s;
        }
        
        .header {
            background-color: var(--header-bg);
            padding: 10px;
            border-radius: 5px;
            margin-bottom: 20px;
        }
        
        .tabs {
            margin-bottom: 20px;
        }
        
        .tab {
            display: inline-block;
            padding: 10px 20px;
            margin-right: 5px;
            background-color: var(--tab-bg);
            cursor: pointer;
            border-radius: 5px 5px 0 0;
            transition: background-color 0.3s;
        }
        
        .tab.active {
            background-color: var(--tab-active-bg);
            color: var(--tab-active-text);
        }
        
        .tab-content {
            display: none;
        }
        
        .tab-content.active {
            display: block;
        }
        
        .stats-table {
            border-collapse: collapse;
            width: 100%;
        }
        
        .stats-table th, .stats-table td {
            border: 1px solid var(--table-border);
            padding: 12px;
            text-align: left;
        }
        
        .stats-table th {
            background-color: var(--table-header-bg);
        }
        
        .stats-table tr:nth-child(even) {
            background-color: var(--table-row-even-bg);
        }
        
        button {
            padding: 10px 15px;
            background-color: var(--button-bg);
            color: var(--button-text);
            border: none;
            border-radius: 4px;
            cursor: pointer;
            margin-right: 10px;
            transition: background-color 0.3s;
        }
        
        button:hover {
            background-color: var(--button-hover-bg);
        }
        
        input[type="date"] {
            background-color: var(--header-bg);
            color: var(--text-color);
            border: 1px solid var(--table-border);
            padding: 5px;
            border-radius: 4px;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>Clash Statistics Dashboard</h1>
        <button onclick="location.reload()">刷新数据</button>
        <button onclick="refreshAPI()">API 刷新</button>
    </div>
    
    <div class="tabs">
        <div class="tab active" onclick="showTab(1)">Host/IP 统计</div>
        <div class="tab" onclick="showTab(2)">SourceIP 统计</div>
        <div class="tab" onclick="showTab(3)">SourceIP 在线时间</div>
    </div>
    
    <div id="tab1" class="tab-content active">
        <h2>流量统计 (按 Host/IP 排序)</h2>
        <table class="stats-table">
            <thead>
                <tr>
                    <th>排名</th>
                    <th>Host/IP</th>
                    <th>下载量</th>
                    <th>上传量</th>
                    <th>总计</th>
                </tr>
            </thead>
            <tbody>
                {{range $index, $item := .Stats}}
                <tr>
                    <td>{{add $index 1}}</td>
                    <td>{{$item.Target}}</td>
                    <td>{{formatBytes $item.Download}}</td>
                    <td>{{formatBytes $item.Upload}}</td>
                    <td>{{formatBytes $item.Total}}</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    
    <div id="tab2" class="tab-content">
        <h2>流量统计 (按 SourceIP 排序)</h2>
        <table class="stats-table">
            <thead>
                <tr>
                    <th>排名</th>
                    <th>SourceIP</th>
                    <th>下载量</th>
                    <th>上传量</th>
                    <th>总计</th>
                </tr>
            </thead>
            <tbody>
                {{range $index, $item := .SourceIPStats}}
                <tr>
                    <td>{{add $index 1}}</td>
                    <td>{{$item.Target}}</td>
                    <td>{{formatBytes $item.Download}}</td>
                    <td>{{formatBytes $item.Upload}}</td>
                    <td>{{formatBytes $item.Total}}</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    
    <div id="tab3" class="tab-content">
        <h2>SourceIP 在线时间</h2>
        <label for="date">选择日期: </label>
        <input type="date" id="date" value="{{.CurrentDate}}" onchange="loadGanttChart(this.value)">
        <div id="gantt-container" style="margin-top: 20px;">
            <!-- 甘特图将在这里显示 -->
            <div id="gantt-chart"></div>
        </div>
    </div>
    
    <script>
        function refreshAPI() {
            fetch('/refresh')
                .then(response => response.json())
                .then(data => {
                    alert('数据已刷新');
                    location.reload();
                })
                .catch(error => {
                    console.error('错误:', error);
                    alert('刷新失败');
                });
        }
        
        function showTab(tabNumber) {
            // 隐藏所有标签内容
            var tabContents = document.getElementsByClassName('tab-content');
            for (var i = 0; i < tabContents.length; i++) {
                tabContents[i].classList.remove('active');
            }
            
            // 移除所有标签的活动状态
            var tabs = document.getElementsByClassName('tab');
            for (var i = 0; i < tabs.length; i++) {
                tabs[i].classList.remove('active');
            }
            
            // 显示选中的标签内容
            document.getElementById('tab' + tabNumber).classList.add('active');
            tabs[tabNumber-1].classList.add('active');
            
            // 如果切换到甘特图标签，加载图表
            if(tabNumber === 3) {
                loadGanttChart(document.getElementById('date').value);
            }
        }
        
        function loadGanttChart(date) {
            fetch('/api/gantt?date=' + date)
                .then(response => {
                    if (!response.ok) {
                        throw new Error('HTTP error! status: ' + response.status);
                    }
                    return response.json();
                })
                .then(data => {
                    console.log('Gantt chart data:', data); // 调试信息
                    displayGanttChart(data);
                })
                .catch(error => {
                    console.error('加载甘特图失败:', error);
                    document.getElementById('gantt-chart').innerHTML = '<p>加载甘特图失败: ' + error.message + '</p>';
                });
        }
        
        function displayGanttChart(data) {
            let html = '<div style="font-family: monospace; line-height: 1.8;">';
            html += '<div style="display: flex; align-items: center; margin-bottom: 5px;"><div style="width: 200px;"></div>';
            
            // 创建144个10分钟段（24小时 * 6段/小时）
            for (let segment = 0; segment < 144; segment++) {
                if (segment % 6 === 0) { // 每6段（1小时）显示一次小时数
                    const hour = Math.floor(segment / 6);
                    html += '<div style="width: 15px; text-align: center; font-size: 10px;">' + hour + '</div>';
                } else if (segment % 3 === 0) { // 每3段（30分钟）显示一个竖线
                    html += '<div style="width: 15px; text-align: center; font-size: 8px; color: #999;">|</div>';
                } else {
                    html += '<div style="width: 15px;"></div>';
                }
            }
            html += '</div>';
            
            if (!Array.isArray(data)) {
                document.getElementById('gantt-chart').innerHTML = '<p>无效的数据格式</p>';
                return;
            }
            
            data.forEach(item => {
                html += '<div style="display: flex; align-items: center; margin-bottom: 5px;">';
                
                // 根据sourceIP和type决定显示内容
                let displayIP = item.sourceIP || 'Unknown';
                if (displayIP === 'Unknown' && item.type === 'Inner') {
                    displayIP = 'Inner';
                } else if (displayIP === 'Unknown' && item.type !== 'Inner') {
                    displayIP = '未知IP';
                }
                
                html += '<div style="width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="' + displayIP + '">' + displayIP + '</div>';
                
                // 创建144个10分钟段的时间轴，直接使用返回的timeSlots数组
                for (let i = 0; i < item.timeSlots.length && i < 144; i++) {
                    if (item.timeSlots[i]) {
                        html += '<div style="width: 15px; height: 20px; background-color: var(--gantt-active-color); border: 1px solid var(--gantt-active-border);"></div>';
                    } else {
                        html += '<div style="width: 15px; height: 20px; border: 1px solid var(--gantt-grid-color);"></div>';
                    }
                }

                // 如果timeSlots数组长度不足144，补充空白
                for (let i = item.timeSlots.length; i < 144; i++) {
                    html += '<div style="width: 15px; height: 20px; border: 1px solid var(--gantt-grid-color);"></div>';
                }
                
                html += '</div>';
            });
            
            html += '</div>';
            document.getElementById('gantt-chart').innerHTML = html;
        }
        
        // 切换暗黑模式
        // 页面加载时，如果是甘特图标签，则加载图表
        window.onload = function() {
            const params = new URLSearchParams(window.location.search);
            const page = params.get('page') || '1';
            if(page === '3') {
                loadGanttChart(document.getElementById('date').value);
            }
        };
    </script>
</body>
</html>
`
		// 从数据库获取统计数据
		stats, err := db.GetStats()
		if err != nil {
			http.Error(w, "获取统计数据失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		
		// 从数据库获取SourceIP统计数据
		sourceIPStats, err := db.GetSourceIPStats()
		if err != nil {
			http.Error(w, "获取SourceIP统计数据失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		
		// 创建数据结构传递给模板
		data = struct {
			Stats         []StatsEntry
			SourceIPStats []StatsEntry
			CurrentDate   string
		}{
			Stats:         stats,
			SourceIPStats: sourceIPStats,
			CurrentDate:   time.Now().Format("2006-01-02"),
		}
	case "2":
		// 重定向到主页，显示第二个标签
		http.Redirect(w, r, "/?page=1", http.StatusTemporaryRedirect)
		return
	case "3":
		// 重定向到主页，显示第三个标签
		http.Redirect(w, r, "/?page=1", http.StatusTemporaryRedirect)
		return
	default:
		// 默认显示第一页
		http.Redirect(w, r, "/?page=1", http.StatusTemporaryRedirect)
		return
	}
	
	// 注册模板函数
	funcMap := template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"formatBytes": func(bytes int64) string {
			const unit = 1024
			if bytes < unit {
				return fmt.Sprintf("%d B", bytes)
			}
			div, exp := int64(unit), 0
			for n := bytes / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
		},
	}
	
	t, err := template.New("dashboard").Funcs(funcMap).Parse(tmpl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// getStats 返回统计信息的 API 端点
func getStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	stats, err := db.GetStats()
	if err != nil {
		http.Error(w, "获取统计数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(stats)
}

// getSourceIPStats 返回SourceIP统计信息的 API 端点
func getSourceIPStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	stats, err := db.GetSourceIPStats()
	if err != nil {
		http.Error(w, "获取SourceIP统计数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(stats)
}

// getGanttData 返回甘特图数据的 API 端点
func getGanttData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02") // 使用今天日期
	}
	
	ganttData, err := db.GetGanttData(date)
	if err != nil {
		http.Error(w, "获取甘特图数据失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(ganttData)
}

// getConnections 返回连接信息的 API 端点
func getConnections(w http.ResponseWriter, r *http.Request, config *Config) {
	w.Header().Set("Content-Type", "application/json")
	
	var data ConnectionInfo
	if latestConnectionInfo != nil {
		data = *latestConnectionInfo
	} else {
		// 从数据库获取最新连接信息
		// 由于我们不再保存完整连接信息到内存，这里返回空结构
		// 如果需要获取连接详情，可以添加新的数据库查询方法
		data = ConnectionInfo{}
	}
	
	json.NewEncoder(w).Encode(data)
}

// refreshData 刷新数据的 API 端点
func refreshData(w http.ResponseWriter, r *http.Request, config *Config) {
	w.Header().Set("Content-Type", "application/json")
	
	connectionInfo, err := GetConnectionsFromClash(config)
	if err != nil {
		http.Error(w, "获取连接信息失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	latestConnectionInfo = connectionInfo
	
	// 保存到数据库
	err = db.SaveConnections(connectionInfo.Connections)
	if err != nil {
		http.Error(w, "保存连接信息到数据库失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "数据已刷新"})
}

// loadConnectionInfoFromFile 从文件加载连接信息
func loadConnectionInfoFromFile(filename string) (*ConnectionInfo, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	
	var connectionInfo ConnectionInfo
	err = json.Unmarshal(data, &connectionInfo)
	if err != nil {
		return nil, err
	}
	
	return &connectionInfo, nil
}

func main() {
	config, err := LoadConfig()
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		return
	}
	
	fmt.Printf("Clash Host: %s\n", config.ClashHost)
	fmt.Printf("Clash Secret: %s\n", config.ClashSecret)
	fmt.Printf("Clash Interval: %d\n", config.ClashInterval)
	
	// 启动 Web 服务器
	StartWebServer("8081", config)
}