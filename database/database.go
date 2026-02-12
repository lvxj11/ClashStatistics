package database

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"clash-statistics/utils"

	_ "github.com/mattn/go-sqlite3"
)

// Database 结构体
type Database struct {
	db *sql.DB
}

// Connection 表示单个连接
type Connection struct {
	ID             string   `json:"id"`
	Chain          []string `json:"chain"`
	Rule           string   `json:"rule"`
	RulePayload    string   `json:"rulePayload"`
	Download       int64    `json:"download"`
	Upload         int64    `json:"upload"`
	SourceIP       string   `json:"srcIP"`
	SourcePort     string   `json:"srcPort"`
	DestPort       string   `json:"dstPort"`
	DestIP         string   `json:"dstIP"`
	StartTime      string   `json:"start"`
	StartTimestamp int64    `json:"startTimestamp"` // 开始时间戳
	EndTimestamp   int64    `json:"endTimestamp"`   // 结束时间戳
	Metadata       Metadata `json:"metadata"`       // 保留原始结构用于解析
	// 从 Metadata 展开的字段（用于数据库存储）
	Network           string      `json:"-"` // 不从 JSON 解析，而是从 Metadata 映射
	ConnectionType    string      `json:"-"` // 避免与 Go 关键字冲突
	SourceIPAddr      string      `json:"-"`
	DestinationIP     string      `json:"-"`
	SourcePortNum     string      `json:"-"`
	DestinationPort   string      `json:"-"`
	Host              string      `json:"-"`
	DNSMode           string      `json:"-"`
	Uid               int         `json:"-"`
	Process           string      `json:"-"`
	ProcessPath       string      `json:"-"`
	SpecialProxy      string      `json:"-"`
	SpecialRules      string      `json:"-"`
	RemoteDestination string      `json:"-"`
	DSCP              int         `json:"-"`
	SniffHost         string      `json:"-"`
	InboundIP         string      `json:"-"`
	InboundPort       string      `json:"-"`
	InboundName       string      `json:"-"`
	InboundUser       string      `json:"-"`
	SourceGeoIP       interface{} `json:"-"` // 可能为 null
	DestinationGeoIP  interface{} `json:"-"` // 可能为 null
	SourceIPASN       string      `json:"-"`
	DestinationIPASN  string      `json:"-"`
}

// Metadata 包含连接元数据
type Metadata struct {
	Network           string      `json:"network"`
	Type              string      `json:"type"`
	SourceIP          string      `json:"sourceIP"`
	DestIP            string      `json:"destinationIP"`
	SourcePort        string      `json:"sourcePort"`
	DestPort          string      `json:"destinationPort"`
	Host              string      `json:"host"`
	DNSMode           string      `json:"dnsMode"`
	Uid               int         `json:"uid"`
	Process           string      `json:"process"`
	ProcessPath       string      `json:"processPath"`
	SpecialProxy      string      `json:"specialProxy"`
	SpecialRules      string      `json:"specialRules"`
	RemoteDestination string      `json:"remoteDestination"`
	DSCP              int         `json:"dscp"`
	SniffHost         string      `json:"sniffHost"`
	InboundIP         string      `json:"inboundIP"`
	InboundPort       string      `json:"inboundPort"`
	InboundName       string      `json:"inboundName"`
	InboundUser       string      `json:"inboundUser"`
	SourceGeoIP       interface{} `json:"sourceGeoIP"`      // 可能为 null
	DestinationGeoIP  interface{} `json:"destinationGeoIP"` // 可能为 null
	SourceIPASN       string      `json:"sourceIPASN"`
	DestinationIPASN  string      `json:"destinationIPASN"`
}

// StatsEntry 表示统计条目
type StatsEntry struct {
	Target   string // host 或 destinationIP
	SourceIP string // 源IP地址
	Download int64
	Upload   int64
	Total    int64
}

// GanttEntry 表示甘特图条目
type GanttEntry struct {
	SourceIP  string
	TimeSlots [144]bool // 每天144个10分钟时间段，true表示该时间段有活动
}

// parseToTimestamp 将时间字符串转换为 Unix 时间戳
func parseToTimestamp(timeStr string) (int64, error) {
	if timeStr == "" {
		return 0, nil
	}

	// 尝试 ISO 8601 格式 (RFC3339)
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return t.Unix(), nil
	}

	// 尝试带纳秒的 ISO 8601 格式
	if t, err := time.Parse("2006-01-02T15:04:05.000000000Z", timeStr); err == nil {
		return t.Unix(), nil
	}

	// 尝试旧格式（向后兼容）
	if t, err := time.Parse("2006-01-02 15:04:05", timeStr); err == nil {
		return t.Unix(), nil
	}

	return 0, fmt.Errorf("无法解析时间格式: %s", timeStr)
}

// InitDB 初始化数据库
func InitDB() (*Database, error) {
	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %v", err)
	}

	db, err := sql.Open("sqlite3", "./data/clash_statistics.db")
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
		start_timestamp INTEGER DEFAULT 0,
		end_timestamp INTEGER DEFAULT 0,
		chain TEXT,
		rule TEXT,
		rule_payload TEXT,
		source_real_ip TEXT,
		source_real_port TEXT,
		dest_real_ip TEXT,
		dest_real_port TEXT,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	-- 创建索引
	CREATE INDEX IF NOT EXISTS idx_connections_host ON connections(host);
	CREATE INDEX IF NOT EXISTS idx_connections_destination_ip ON connections(destination_ip);
	CREATE INDEX IF NOT EXISTS idx_connections_source_ip ON connections(source_ip);
	CREATE INDEX IF NOT EXISTS idx_connections_last_seen ON connections(last_seen);
	CREATE INDEX IF NOT EXISTS idx_connections_start_time ON connections(start_time);
	`

	_, err = db.Exec(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("创建表失败: %v", err)
	}

	// 检查是否需要添加新字段（数据库迁移）
	database := &Database{db: db}
	database.Migrate()

	return database, nil
}

// Migrate 执行数据库迁移
func (d *Database) Migrate() {
	log := utils.GetLogger()

	// 检查 start_timestamp 字段是否存在
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('connections') WHERE name='start_timestamp'").Scan(&count)
	if err == nil && count == 0 {
		log.Info("开始数据库迁移：添加 start_timestamp 和 end_timestamp 字段\n")

		// 添加新字段
		_, err := d.db.Exec("ALTER TABLE connections ADD COLUMN start_timestamp INTEGER DEFAULT 0")
		if err != nil {
			log.Error("添加 start_timestamp 字段失败: %v\n", err)
			return
		}

		_, err = d.db.Exec("ALTER TABLE connections ADD COLUMN end_timestamp INTEGER DEFAULT 0")
		if err != nil {
			log.Error("添加 end_timestamp 字段失败: %v\n", err)
			return
		}

		// 迁移现有数据：将 start_time 转换为 start_timestamp
		_, err = d.db.Exec(`
			UPDATE connections 
			SET start_timestamp = CAST(strftime('%s', REPLACE(REPLACE(start_time, 'T', ' '), 'Z', '')) AS INTEGER)
			WHERE start_time != '' AND start_timestamp = 0
		`)
		if err != nil {
			log.Warn("迁移 start_timestamp 失败，将在应用层处理: %v\n", err)
		} else {
			log.Info("成功迁移 start_timestamp 数据\n")
		}

		// 迁移现有数据：将 closed_time 转换为 end_timestamp（如果存在）
		var closedTimeCount int
		d.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('connections') WHERE name='closed_time'").Scan(&closedTimeCount)
		if closedTimeCount > 0 {
			_, err = d.db.Exec(`
				UPDATE connections 
				SET end_timestamp = CAST(strftime('%s', closed_time) AS INTEGER)
				WHERE closed_time != '' AND closed_time IS NOT NULL AND end_timestamp = 0
			`)
			if err != nil {
				log.Warn("迁移 end_timestamp 失败，将在应用层处理: %v\n", err)
			} else {
				log.Info("成功迁移 end_timestamp 数据\n")
			}
		}

		// 创建新索引
		_, err = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_connections_end_timestamp ON connections(end_timestamp)")
		if err != nil {
			log.Error("创建 end_timestamp 索引失败: %v\n", err)
		}

		log.Info("数据库迁移完成\n")
	}

	// 检查是否需要删除 closed_time 字段
	var closedTimeCount int
	d.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('connections') WHERE name='closed_time'").Scan(&closedTimeCount)
	if closedTimeCount > 0 {
		log.Info("开始数据库迁移：删除 closed_time 字段\n")

		// 对于 SQLite，删除字段需要重建表
		// 1. 创建临时表
		_, err := d.db.Exec(`
			CREATE TABLE IF NOT EXISTS connections_new (
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
				start_timestamp INTEGER DEFAULT 0,
				end_timestamp INTEGER DEFAULT 0,
				chain TEXT,
				rule TEXT,
				rule_payload TEXT,
				source_real_ip TEXT,
				source_real_port TEXT,
				dest_real_ip TEXT,
				dest_real_port TEXT,
				last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`)
		if err != nil {
			log.Error("创建临时表失败: %v\n", err)
			return
		}

		// 2. 复制数据
		_, err = d.db.Exec(`
			INSERT INTO connections_new (
				id, network, type, source_ip, destination_ip, source_port, destination_port, 
				host, dns_mode, uid, process, process_path, special_proxy, special_rules, 
				remote_destination, dscp, sniff_host, inbound_ip, inbound_port, inbound_name, 
				inbound_user, source_geoip, destination_geoip, source_ipasn, destination_ipasn, 
				upload, download, start_time, start_timestamp, end_timestamp, chain, rule, rule_payload, source_real_ip, 
				source_real_port, dest_real_ip, dest_real_port, last_seen
			) SELECT 
				id, network, type, source_ip, destination_ip, source_port, destination_port, 
				host, dns_mode, uid, process, process_path, special_proxy, special_rules, 
				remote_destination, dscp, sniff_host, inbound_ip, inbound_port, inbound_name, 
				inbound_user, source_geoip, destination_geoip, source_ipasn, destination_ipasn, 
				upload, download, start_time, start_timestamp, end_timestamp, chain, rule, rule_payload, source_real_ip, 
				source_real_port, dest_real_ip, dest_real_port, last_seen
			FROM connections
		`)
		if err != nil {
			log.Error("复制数据失败: %v\n", err)
			return
		}

		// 3. 删除旧表
		_, err = d.db.Exec("DROP TABLE connections")
		if err != nil {
			log.Error("删除旧表失败: %v\n", err)
			return
		}

		// 4. 重命名新表
		_, err = d.db.Exec("ALTER TABLE connections_new RENAME TO connections")
		if err != nil {
			log.Error("重命名表失败: %v\n", err)
			return
		}

		// 5. 重建索引
		_, err = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_connections_host ON connections(host)")
		if err != nil {
			log.Error("创建 host 索引失败: %v\n", err)
		}
		_, err = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_connections_destination_ip ON connections(destination_ip)")
		if err != nil {
			log.Error("创建 destination_ip 索引失败: %v\n", err)
		}
		_, err = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_connections_source_ip ON connections(source_ip)")
		if err != nil {
			log.Error("创建 source_ip 索引失败: %v\n", err)
		}
		_, err = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_connections_end_timestamp ON connections(end_timestamp)")
		if err != nil {
			log.Error("创建 end_timestamp 索引失败: %v\n", err)
		}
		_, err = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_connections_last_seen ON connections(last_seen)")
		if err != nil {
			log.Error("创建 last_seen 索引失败: %v\n", err)
		}
		_, err = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_connections_start_time ON connections(start_time)")
		if err != nil {
			log.Error("创建 start_time 索引失败: %v\n", err)
		}

		log.Info("成功删除 closed_time 字段\n")
	}
}

// SaveConnection 保存连接到数据库
func (d *Database) SaveConnection(conn Connection) error {
	chainStr := ""
	if len(conn.Chain) > 0 {
		chainStr = strings.Join(conn.Chain, ",")
	}

	// 解析 start_time 为时间戳（只在第一次产生）
	startTimestamp := conn.StartTimestamp
	if startTimestamp == 0 && conn.StartTime != "" {
		ts, err := parseToTimestamp(conn.StartTime)
		if err != nil {
			return fmt.Errorf("解析开始时间失败: %v", err)
		}
		startTimestamp = ts
	}

	// 每次保存时都更新 end_timestamp 为当前时间戳
	endTimestamp := time.Now().Unix()

	stmt, err := d.db.Prepare(`INSERT OR REPLACE INTO connections (
		id, network, type, source_ip, destination_ip, source_port, destination_port, 
		host, dns_mode, uid, process, process_path, special_proxy, special_rules, 
		remote_destination, dscp, sniff_host, inbound_ip, inbound_port, inbound_name, 
		inbound_user, source_geoip, destination_geoip, source_ipasn, destination_ipasn, 
		upload, download, start_time, start_timestamp, end_timestamp, chain, rule, rule_payload, source_real_ip, 
		source_real_port, dest_real_ip, dest_real_port, last_seen
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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
		conn.StartTime, startTimestamp, endTimestamp, chainStr, conn.Rule, conn.RulePayload, conn.SourceIP, conn.SourcePort,
		conn.DestIP, conn.DestPort, time.Now().Format("2006-01-02 15:04:05"),
	)
	return err
}

// SaveConnections 保存多个连接到数据库
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
		upload, download, start_time, start_timestamp, end_timestamp, chain, rule, rule_payload, source_real_ip, 
		source_real_port, dest_real_ip, dest_real_port, last_seen
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, conn := range connections {
		chainStr := ""
		if len(conn.Chain) > 0 {
			chainStr = strings.Join(conn.Chain, ",")
		}

		// 解析 start_time 为时间戳（只在第一次产生）
		startTimestamp := conn.StartTimestamp
		if startTimestamp == 0 && conn.StartTime != "" {
			ts, err := parseToTimestamp(conn.StartTime)
			if err != nil {
				return fmt.Errorf("解析开始时间失败: %v", err)
			}
			startTimestamp = ts
		}

		// 每次保存时都更新 end_timestamp 为当前时间戳
		endTimestamp := time.Now().Unix()

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
			conn.StartTime, startTimestamp, endTimestamp, chainStr, conn.Rule, conn.RulePayload, conn.SourceIP, conn.SourcePort,
			conn.DestIP, conn.DestPort, time.Now().Format("2006-01-02 15:04:05"),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetStats 获取统计信息
func (d *Database) GetStats() ([]StatsEntry, error) {
	query := `
		SELECT 
			CASE 
				WHEN host IS NOT NULL AND host != '' THEN host
				WHEN destination_ip IS NOT NULL AND destination_ip != '' THEN destination_ip
				ELSE 'Unknown'
			END AS target,
			CASE 
				WHEN source_ip IS NOT NULL AND source_ip != '' THEN source_ip
				ELSE 'Unknown'
			END AS source_ip,
			SUM(download) AS download,
			SUM(upload) AS upload,
			SUM(download + upload) AS total
		FROM connections
		WHERE (host IS NOT NULL AND host != '') OR (destination_ip IS NOT NULL AND destination_ip != '')
		GROUP BY 
			CASE 
				WHEN host IS NOT NULL AND host != '' THEN host
				WHEN destination_ip IS NOT NULL AND destination_ip != '' THEN destination_ip
				ELSE 'Unknown'
			END,
			CASE 
				WHEN source_ip IS NOT NULL AND source_ip != '' THEN source_ip
				ELSE 'Unknown'
			END
		ORDER BY total DESC
		LIMIT 200
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
		err := rows.Scan(&entry.Target, &entry.SourceIP, &entry.Download, &entry.Upload, &entry.Total)
		if err != nil {
			return nil, err
		}
		stats = append(stats, entry)
		count++
	}

	return stats, nil
}

// GetSourceIPStats 获取SourceIP统计信息
func (d *Database) GetSourceIPStats() ([]StatsEntry, error) {
	query := `
		SELECT 
			source_ip AS target,
			SUM(download) AS download,
			SUM(upload) AS upload,
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

// GetGanttData 获取甘特图数据
func (d *Database) GetGanttData(date string) ([]GanttEntry, error) {
	// 计算日期范围的时间戳
	dateStart, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("解析日期失败: %v", err)
	}
	dateEnd := dateStart.Add(24 * time.Hour)

	// 构建查询，使用时间戳范围查询（更高效）
	query := `
		SELECT 
			source_ip, start_timestamp, end_timestamp
		FROM connections
		WHERE start_timestamp >= ? AND start_timestamp < ?
		AND source_ip IS NOT NULL AND source_ip != ''
		ORDER BY source_ip, start_timestamp
	`

	rows, err := d.db.Query(query, dateStart.Unix(), dateEnd.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 按sourceIP组织数据
	ganttData := make(map[string][144]bool)

	for rows.Next() {
		var sourceIP string
		var startTimestamp, endTimestamp int64

		err := rows.Scan(&sourceIP, &startTimestamp, &endTimestamp)
		if err != nil {
			return nil, err
		}

		// 使用时间戳直接转换为 time.Time（无需字符串解析）
		startTimeParsed := time.Unix(startTimestamp, 0)
		endTimeParsed := time.Unix(endTimestamp, 0)

		// 如果 end_timestamp 为 0，使用当前时间
		if endTimestamp == 0 {
			endTimeParsed = time.Now()
		}

		// 获取或创建时间槽
		timeSlots := ganttData[sourceIP]

		// 计算从开始时间到结束时间之间的所有10分钟时间段
		slotStart := time.Date(startTimeParsed.Year(), startTimeParsed.Month(), startTimeParsed.Day(), 0, 0, 0, 0, startTimeParsed.Location())
		for i := 0; i < 144; i++ {
			slotBegin := slotStart.Add(time.Duration(i*10) * time.Minute)
			slotEnd := slotBegin.Add(10 * time.Minute)

			// 检查连接是否在这个时间段内活跃
			// 只有当连接开始时间早于时间段结束时间 且 连接结束时间晚于时间段开始时间时，才认为在该时间段内活跃
			if (startTimeParsed.Before(slotEnd) || startTimeParsed.Equal(slotEnd)) &&
				(endTimeParsed.After(slotBegin) || endTimeParsed.Equal(slotBegin)) {
				timeSlots[i] = true
			}
		}

		ganttData[sourceIP] = timeSlots
	}

	// 转换为切片
	var result []GanttEntry
	for sourceIP, timeSlots := range ganttData {
		result = append(result, GanttEntry{
			SourceIP:  sourceIP,
			TimeSlots: timeSlots,
		})
	}

	// 按SourceIP进行自然排序
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

// GetOpenConnectionIDs 获取数据库中所有未关闭的连接ID
func (d *Database) GetOpenConnectionIDs() ([]string, error) {
	rows, err := d.db.Query("SELECT id FROM connections WHERE end_timestamp = 0")
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
func (d *Database) SetConnectionClosedTime(id string) error {
	endTimestamp := time.Now().Unix()
	lastSeen := time.Now().Format("2006-01-02 15:04:05")
	stmt, err := d.db.Prepare("UPDATE connections SET end_timestamp=?, last_seen=? WHERE id=? AND end_timestamp = 0")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(endTimestamp, lastSeen, id)
	return err
}

// UpdateClosedTime 更新连接的关闭时间
func (d *Database) UpdateClosedTime(id string) error {
	endTimestamp := time.Now().Unix()
	stmt, err := d.db.Prepare("UPDATE connections SET end_timestamp=? WHERE id=?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	result, err := stmt.Exec(endTimestamp, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		utils.GetLogger().Warn("警告: 没有找到ID为 %s 的记录\n", id)
	} else {
		utils.GetLogger().Info("成功更新 %d 条记录的end_timestamp为 %d\n", rowsAffected, endTimestamp)
	}

	return nil
}

// CleanupOldData 清理过期数据
func (d *Database) CleanupOldData(days int) error {
	cutoffTime := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")

	// 批量删除过期数据
	stmt, err := d.db.Prepare("DELETE FROM connections WHERE last_seen < ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	result, err := stmt.Exec(cutoffTime)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	utils.GetLogger().Info("清理了 %d 条过期记录\n", rowsAffected)
	return nil
}

// Vacuum 执行数据库维护，回收空间
func (d *Database) Vacuum() error {
	utils.GetLogger().Info("开始执行数据库VACUUM操作\n")

	_, err := d.db.Exec("VACUUM")
	if err != nil {
		return err
	}

	utils.GetLogger().Info("数据库VACUUM操作完成\n")
	return nil
}

// GetDatabaseSize 获取数据库文件大小
func (d *Database) GetDatabaseSize() (int64, error) {
	fileInfo, err := os.Stat("./data/clash_statistics.db")
	if err != nil {
		return 0, err
	}
	return fileInfo.Size(), nil
}

// Close 关闭数据库连接
func (d *Database) Close() error {
	return d.db.Close()
}
