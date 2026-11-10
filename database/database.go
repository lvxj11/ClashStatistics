package database

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"clash-statistics/utils"

	_ "modernc.org/sqlite"
)

// Database 结构体
type Database struct {
	db *sql.DB
}

// Connection 表示单个连接
type Connection struct {
	ID             string   `json:"id"`
	Chain          []string `json:"chain"`
	Chains         []string `json:"chains"` // 存储原始chains数据
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
	// 最后一次被看到的时间戳：Clash API 不提供关闭事件，这个值每次轮询刷新，
	// 连接消失后便冻结在上一次轮询时刻，误差不超过一个采集间隔
	LastSeenTimestamp int64    `json:"lastSeenTimestamp"`
	Metadata          Metadata `json:"metadata"` // 保留原始结构用于解析
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

// StatsResult 表格数据
// Total 是符合条件的真实条数（不受 LIMIT 影响），用于提示"共 N 行，可用搜索缩小范围"
type StatsResult struct {
	Rows      []StatsEntry // 空切片而不是 nil，避免编码成 null
	Total     int
	Truncated bool // 是否被 maxTableRows 截断
}

// StatsEntry 表示统计条目（按目标或按客户端聚合后的结果）
type StatsEntry struct {
	Target      string // host/destinationIP，或聚合源IP时的客户端 IP
	Download    int64
	Upload      int64
	Total       int64
	Connections int64 // 连接条数
	Peers       int64 // 对侧维度的去重数量：按目标聚合时是客户端数，按客户端聚合时是访问目标数
	Chains      int64 // 出现过的线路数
	FirstSeen   int64 // 最早起始时间（Unix 秒，0 表示未知）
	LastSeen    int64 // 最近一次被观测到的时间（Unix 秒，0 表示未知）
}

// DetailRow 明细里的一行：客户端/目标 × 线路 × 规则 的组合
type DetailRow struct {
	Key         string // 看目标的明细时是客户端，看客户端的明细时是目标
	Chain       string
	Rule        string
	Download    int64
	Upload      int64
	Total       int64
	Connections int64
	FirstSeen   int64
	LastSeen    int64
}

// DetailResponse 明细弹窗的数据
type DetailResponse struct {
	Name       string      // 明细对象，如目标名或客户端 IP
	Subtitle   string      // 副标题，如"共 3 个客户端、2 条线路"
	KeyLabel   string      // 第一列表头：客户端 或 目标
	SplitChain bool        // 是否按线路拆分（决定前端要不要显示线路列）
	SplitRule  bool        // 是否按规则拆分
	Rows       []DetailRow // 空切片而不是 nil，避免编码成 null
	Total      int         // 符合条件的真实条数
	Truncated  bool        // 是否被 maxTableRows 截断
}

// 聚合维度表达式：必须与明细查询里用的是同一套，否则点开看到的量和行上的量对不上
const (
	targetExpr = `CASE
			WHEN host IS NOT NULL AND host != '' THEN host
			WHEN destination_ip IS NOT NULL AND destination_ip != '' THEN destination_ip
			ELSE 'Unknown'
		END`
	sourceIPExpr = `CASE
			WHEN source_ip IS NOT NULL AND source_ip != '' THEN source_ip
			WHEN type = 'Inner' THEN 'Inner'
			ELSE 'Unknown'
		END`
	chainExpr = `CASE
			WHEN chain IS NOT NULL AND chain != '' THEN chain
			ELSE 'Unknown'
		END`
	ruleExpr = `CASE
			WHEN rule IS NOT NULL AND rule != '' THEN rule
			ELSE '-'
		END`
	// start_timestamp 默认为 0，直接 MIN 会得到 1970 年，这里先把它变成 NULL（SQLite 的 MIN/MAX 忽略 NULL）
	firstSeenExpr = `COALESCE(MIN(CASE WHEN start_timestamp > 0 THEN start_timestamp END), 0)`
	lastSeenExpr  = `COALESCE(MAX(NULLIF(last_seen_timestamp, 0)), 0)`
)

// buildTimeWindow 生成时间窗条件
// （last_seen_timestamp 每次轮询都会被刷新，连接断开后冻结）
// 一条连接的活动区间是 [start_timestamp, last_seen_timestamp]，只有与给定时间窗有重叠的连接才计入统计
func buildTimeWindow(startTimestamp, endTimestamp int64) (string, []interface{}) {
	var args []interface{}
	conditions := []string{}
	if startTimestamp > 0 {
		// 窗口开始之后仍然活跃过
		conditions = append(conditions, "last_seen_timestamp >= ?")
		args = append(args, startTimestamp)
	}
	if endTimestamp > 0 {
		// 窗口结束之前已经存在
		conditions = append(conditions, "start_timestamp <= ?")
		args = append(args, endTimestamp)
	}
	return strings.Join(conditions, " AND "), args
}

// GanttRow 是 /api/gantt 的输出
// 288 个时间槽里大部分是 0（终端通常断续在线），只传非零槽能显著减小体积，
// 配合 gzip 效果更明显。SlotIndexes / SlotValues / RawValues 一一对应：
// SlotValues 是 1-6 的活跃强度档位（供前端取色，跨源可比），
// RawValues 是该槽的真实并发连接数（Clash 代理 / 推送源去重后的连接，供悬停展示绝对数值）。
type GanttRow struct {
	SourceIP    string `json:"SourceIP"`
	SlotIndexes []int  `json:"SlotIndexes"`
	SlotValues  []int  `json:"SlotValues"`
	RawValues   []int  `json:"RawValues"`
}

// FromClashConnection 将 utils 中采集到的连接转换为数据库存储结构
func FromClashConnection(conn utils.Connection) Connection {
	return Connection{
		ID:                conn.ID,
		Chain:             conn.Chain,
		Chains:            conn.Chains,
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

// parseToTimestamp 将时间字符串转换为 Unix 时间戳
func parseToTimestamp(timeStr string) (int64, error) {
	if timeStr == "" {
		return 0, nil
	}

	localLoc := time.Now().Location()

	// 尝试 ISO 8601 格式 (RFC3339)
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return t.Unix(), nil
	}

	// 尝试带纳秒的 ISO 8601 格式
	if t, err := time.Parse("2006-01-02T15:04:05.000000000Z", timeStr); err == nil {
		return t.Unix(), nil
	}

	// 尝试旧格式（向后兼容）- 使用本地时区
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, localLoc); err == nil {
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

	db, err := sql.Open("sqlite", "./data/clash_statistics.db")
	if err != nil {
		return nil, err
	}

	// 优化数据库配置
	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		return nil, fmt.Errorf("设置 journal_mode 失败: %v", err)
	}

	_, err = db.Exec("PRAGMA synchronous = NORMAL")
	if err != nil {
		return nil, fmt.Errorf("设置 synchronous 失败: %v", err)
	}

	// 采集 goroutine、/refresh 处理器、维护任务都会写库。
	// SQLite 在多个连接各自持有读快照后再升级为写锁时，会直接返回 SQLITE_BUSY 而不等待，
	// 因此把连接池限制为单连接，由它串行化所有读写
	db.SetMaxOpenConns(1)

	// 写冲突时先等待而不是直接报错
	_, err = db.Exec("PRAGMA busy_timeout = 5000")
	if err != nil {
		return nil, fmt.Errorf("设置 busy_timeout 失败: %v", err)
	}

	_, err = db.Exec("PRAGMA cache_size = -8192") // 使用 8MB 缓存
	if err != nil {
		return nil, fmt.Errorf("设置 cache_size 失败: %v", err)
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
			-- 这条连接最后一次出现在 Clash 返回结果里的时间（秒级时间戳）
			-- 注意：不是真正的关闭时间，Clash API 不提供关闭事件，这个值每次轮询都会被刷新
			last_seen_timestamp INTEGER DEFAULT 0,
			chain TEXT,
			chains TEXT,
			rule TEXT,
			rule_payload TEXT,
			source_real_ip TEXT,
			source_real_port TEXT,
			dest_real_ip TEXT,
			dest_real_port TEXT
		);
	
	-- 创建索引
	CREATE INDEX IF NOT EXISTS idx_connections_host ON connections(host);
	CREATE INDEX IF NOT EXISTS idx_connections_destination_ip ON connections(destination_ip);
	CREATE INDEX IF NOT EXISTS idx_connections_source_ip ON connections(source_ip);
	CREATE INDEX IF NOT EXISTS idx_connections_start_timestamp ON connections(start_timestamp);
	CREATE INDEX IF NOT EXISTS idx_connections_last_seen_timestamp ON connections(last_seen_timestamp);

	-- 客户端名称映射：与 connections 分开存，连接数据被定期清理时名称不会丢。
	-- 手动与自动分列，写自动名称永远碰不到手动列（手动配置为第一优先）。
	CREATE TABLE IF NOT EXISTS client_names (
			ip              TEXT PRIMARY KEY,
			manual_name     TEXT NOT NULL DEFAULT '',
			note            TEXT NOT NULL DEFAULT '',
			auto_name       TEXT NOT NULL DEFAULT '',
			auto_source     TEXT NOT NULL DEFAULT '',
			auto_updated    INTEGER NOT NULL DEFAULT 0,
			manual_updated  INTEGER NOT NULL DEFAULT 0
		);

	-- 活跃度聚合表（v2.0.0 遗留）：B 方案之前推送源按「上行流数」累加写这里。
	-- 现已停写停查，保留 DDL 只为让升级前残留数据按保留天数清理（CleanupActivity）；
	-- 旧数据没有五元组、口径也不同，无法迁移到 flow_slots。
	CREATE TABLE IF NOT EXISTS activity (
			source    TEXT NOT NULL,
			ip        TEXT NOT NULL,
			slot_ts   INTEGER NOT NULL,
			value     INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (source, ip, slot_ts)
		);
	CREATE INDEX IF NOT EXISTS idx_activity_source_slot ON activity(source, slot_ts);

	-- 推送源的并发连接去重表：一行 = 一个 (source, 五元组, 5 分钟槽)。
	-- 主键天然去重，长流被 active-flow-timeout 切片、分片重复上报都只算一条连接。
	-- 查询时按 (src_ip, 槽) COUNT(*) 即真实并发连接数；不存原始流的逐包明细。
	CREATE TABLE IF NOT EXISTS flow_slots (
			source    TEXT NOT NULL,
			src_ip    TEXT NOT NULL,
			src_port  INTEGER NOT NULL,
			dst_ip    TEXT NOT NULL,
			dst_port  INTEGER NOT NULL,
			proto     INTEGER NOT NULL,
			slot_ts   INTEGER NOT NULL,
			PRIMARY KEY (source, src_ip, src_port, dst_ip, dst_port, proto, slot_ts)
		);
	CREATE INDEX IF NOT EXISTS idx_flow_slots_source_slot ON flow_slots(source, slot_ts);

	-- 数据源定义：运行时可增删、频率各异；系统级配置仍留在 config.toml / env。
	-- credential_ref 只保存凭据引用（环境变量名），不存明文。
	CREATE TABLE IF NOT EXISTS data_sources (
			id             TEXT PRIMARY KEY,
			type           TEXT NOT NULL,
			name           TEXT NOT NULL,
			enabled        INTEGER NOT NULL DEFAULT 1,
			address        TEXT NOT NULL DEFAULT '',
			port           INTEGER NOT NULL DEFAULT 0,
			lan_cidrs      TEXT NOT NULL DEFAULT '',
			exclude_ips    TEXT NOT NULL DEFAULT '',
			interval       INTEGER NOT NULL DEFAULT 0,
			credential_ref TEXT NOT NULL DEFAULT '',
		is_default     INTEGER NOT NULL DEFAULT 0,
		sort_order     INTEGER NOT NULL DEFAULT 0,
		thresholds     TEXT NOT NULL DEFAULT '',
		custom_thresholds INTEGER NOT NULL DEFAULT 0
	);
`

	_, err = db.Exec(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("创建表失败: %v", err)
	}

	// v2.0.0 的数据库迁移基线是 v1.0.4：升级新增 activity / flow_slots / data_sources
	// 三张表（上面的 CREATE TABLE IF NOT EXISTS），connections 与 client_names 结构不变。
	// activity 为旧聚合表，已停写停查，仅按保留天数清理残留。
	// v1.0.3 及更早版本的历史兼容代码已移除。
	// 自定义热度阈值是后加的列：CREATE TABLE IF NOT EXISTS 不会改旧表结构，
	// 所以这里用幂等 ALTER 补齐，并把升级前硬编码的 IPFIX 阈值迁到内置 ipfix 行。
	if err := migrateSourceThresholds(db); err != nil {
		return nil, fmt.Errorf("迁移数据源阈值失败: %v", err)
	}
	return &Database{db: db}, nil
}

// SaveConnections 保存多个连接到数据库
func (d *Database) SaveConnections(connections []Connection) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 已存在的连接只需更新会变化的字段：计数值、最后一次被看到的时间，
	// 以及那些首次可能为空、之后才被 sniffer 补全的描述字段。
	// 用 UPSERT 而不是 INSERT OR REPLACE，避免每次轮询都整行删除重插（会把所有索引一起重写）
	stmt, err := tx.Prepare(`INSERT INTO connections (
		id, network, type, source_ip, destination_ip, source_port, destination_port, 
		host, dns_mode, uid, process, process_path, special_proxy, special_rules, 
		remote_destination, dscp, sniff_host, inbound_ip, inbound_port, inbound_name, 
		inbound_user, source_geoip, destination_geoip, source_ipasn, destination_ipasn, 
		upload, download, start_time, start_timestamp, last_seen_timestamp, chain, chains, rule, rule_payload, source_real_ip, 
		source_real_port, dest_real_ip, dest_real_port
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		upload = excluded.upload,
		download = excluded.download,
		last_seen_timestamp = excluded.last_seen_timestamp,
		host = CASE WHEN excluded.host != '' THEN excluded.host ELSE connections.host END,
		sniff_host = CASE WHEN excluded.sniff_host != '' THEN excluded.sniff_host ELSE connections.sniff_host END,
		destination_ip = CASE WHEN excluded.destination_ip != '' THEN excluded.destination_ip ELSE connections.destination_ip END,
		chain = CASE WHEN excluded.chain != '' THEN excluded.chain ELSE connections.chain END,
		rule = CASE WHEN excluded.rule != '' THEN excluded.rule ELSE connections.rule END`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, conn := range connections {
		chainStr := ""
		if len(conn.Chain) > 0 {
			chainStr = strings.Join(conn.Chain, ",")
		}

		chainsStr := ""
		if len(conn.Chains) > 0 {
			chainsStr = strings.Join(conn.Chains, ",")
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

		// 每次保存时都刷新最后一次被看到的时间：连接一旦关闭就不再出现在结果里，
		// 这个值便停在上一次轮询的时刻，误差不超过一个采集间隔
		lastSeenTimestamp := time.Now().Unix()

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
			conn.StartTime, startTimestamp, lastSeenTimestamp, chainStr, chainsStr, conn.Rule, conn.RulePayload, conn.SourceIP, conn.SourcePort,
			conn.DestIP, conn.DestPort,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// maxTableRows 单张表最多返回多少行
// 不分页，但这个量级下 DOM 与传输都无压力；超过时用 Total/Truncated 提示缩小范围
const maxTableRows = 500

// sortColumns 排序字段白名单
// 用户输入绝不能直接拼进 SQL，只能从这里取值；不在表里的一律回落到默认排序。
// 第一列在聚合查询里叫 target、在明细查询里叫 key，两者用同一个排序键
var sortColumns = map[string]string{
	"target":      "target",
	"key":         "key",
	"download":    "download",
	"upload":      "upload",
	"total":       "total",
	"connections": "connections",
	"peers":       "peers",
	"first":       "first_seen",
	"last":        "last_seen",
}

// buildOrderBy 把排序字段与方向转成 ORDER BY 子句
func buildOrderBy(sortKey, order string) string {
	column, ok := sortColumns[sortKey]
	if !ok {
		// 默认按总流量倒序，和改造前的观感一致
		column, order = "total", "desc"
	}
	if order != "asc" {
		order = "desc"
	}
	return fmt.Sprintf("ORDER BY %s %s", column, order)
}

// buildConditions 拼装查询条件：基础过滤 + 时间窗 + 搜索
// 聚合查询、明细查询、总数统计都用这一份，避免三处各拼一遍导致条件不一致
// searchExpr 为空表示不搜索
func buildConditions(baseWhere, searchExpr, search string, startTimestamp, endTimestamp int64) (string, []interface{}) {
	timeWhere, timeArgs := buildTimeWindow(startTimestamp, endTimestamp)

	conditions := []string{baseWhere}
	args := []interface{}{}
	if timeWhere != "" {
		conditions = append(conditions, timeWhere)
		args = append(args, timeArgs...)
	}
	if search != "" && searchExpr != "" {
		conditions = append(conditions, fmt.Sprintf("%s LIKE ? ESCAPE '\\'", searchExpr))
		args = append(args, "%"+escapeLike(search)+"%")
	}

	return strings.Join(conditions, " AND "), args
}

// escapeLike 转义 LIKE 里的通配符，避免用户输入的 % / _ 被当成通配符
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	return replacer.Replace(value)
}

// GetTargetStats 按目标（域名/IP）聚合的统计信息
// search 为空表示不过滤；sortBy/order 决定排序（排序必须发生在 LIMIT 之前，
// 否则被截断的就不是"排好序的前 N 行"）
func (d *Database) GetTargetStats(startTimestamp, endTimestamp int64,
	search, sortBy, order string) (*StatsResult, error) {

	// 必须用括号包起来：baseWhere 自身含 OR，若不加括号，后续 AND 条件
	// 会因为运算符优先级只作用在 OR 的一侧，导致过滤失效
	baseWhere := "((host IS NOT NULL AND host != '') OR (destination_ip IS NOT NULL AND destination_ip != ''))"
	return d.queryStats(baseWhere, targetExpr, sourceIPExpr, startTimestamp, endTimestamp, search, sortBy, order)
}

// GetSourceIPStatsWithRange 按客户端（源IP）聚合的统计信息
func (d *Database) GetSourceIPStatsWithRange(startTimestamp, endTimestamp int64,
	search, sortBy, order string) (*StatsResult, error) {

	baseWhere := "((source_ip IS NOT NULL AND source_ip != '') OR type = 'Inner')"
	return d.queryStats(baseWhere, sourceIPExpr, targetExpr, startTimestamp, endTimestamp, search, sortBy, order)
}

// queryStats 执行聚合查询
// groupExpr: 分组维度表达式；peerExpr: 对侧维度（用于统计"客户端数/访问目标数"）
func (d *Database) queryStats(baseWhere, groupExpr, peerExpr string,
	startTimestamp, endTimestamp int64, search, sortBy, order string) (*StatsResult, error) {

	whereClause, args := buildConditions(baseWhere, groupExpr, search, startTimestamp, endTimestamp)

	// 多取一行用来判断有没有被上限截断：绝大多数情况取不满，
	// 就不必再跑一次聚合去数总数（那等于把聚合做两遍）
	query := fmt.Sprintf(`
		SELECT
			%s AS target,
			SUM(download) AS download,
			SUM(upload) AS upload,
			SUM(download + upload) AS total,
			COUNT(*) AS connections,
			COUNT(DISTINCT %s) AS peers,
			COUNT(DISTINCT %s) AS chains,
			%s AS first_seen,
			%s AS last_seen
		FROM connections
		WHERE %s
		GROUP BY %s
		%s
		LIMIT %d
	`, groupExpr, peerExpr, chainExpr, firstSeenExpr, lastSeenExpr,
		whereClause, groupExpr, buildOrderBy(sortBy, order), maxTableRows+1)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := []StatsEntry{}
	for rows.Next() {
		var entry StatsEntry
		if err := rows.Scan(&entry.Target, &entry.Download, &entry.Upload, &entry.Total,
			&entry.Connections, &entry.Peers, &entry.Chains, &entry.FirstSeen, &entry.LastSeen); err != nil {
			return nil, err
		}
		stats = append(stats, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(stats) <= maxTableRows {
		// 没截断：取到的行数就是总数
		return &StatsResult{Rows: stats, Total: len(stats), Truncated: false}, nil
	}

	// 截断了才需要真实总数，用于提示"共 N 行，可用搜索缩小范围"
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT %s AS target FROM connections WHERE %s GROUP BY %s
		)
	`, groupExpr, whereClause, groupExpr)

	var total int
	if err := d.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	return &StatsResult{
		Rows:      stats[:maxTableRows],
		Total:     total,
		Truncated: true,
	}, nil
}

// GetTargetDetail 某个目标的明细
// 默认只按客户端聚合；splitChain / splitRule 打开时才把线路、规则加进分组维度
func (d *Database) GetTargetDetail(target string, startTimestamp, endTimestamp int64,
	splitChain, splitRule bool, search, sortBy, order string) (*DetailResponse, error) {

	if target == "" {
		return nil, fmt.Errorf("目标不能为空")
	}

	// 过滤条件必须复用聚合用的同一个表达式，否则点开看到的量和行上的量对不上
	baseWhere := fmt.Sprintf("(%s) = ?", targetExpr)
	args := []interface{}{target}

	rows, total, err := d.queryDetailRows(baseWhere, args, startTimestamp, endTimestamp,
		sourceIPExpr, splitChain, splitRule, search, sortBy, order)
	if err != nil {
		return nil, err
	}

	// 明细列表只取 Top N，副标题里的总数要单独统计，否则会显示成被截断后的数量
	peers, chains, err := d.queryDetailCount(baseWhere, args, startTimestamp, endTimestamp, sourceIPExpr)
	if err != nil {
		return nil, err
	}

	return &DetailResponse{
		Name:       target,
		Subtitle:   fmt.Sprintf("共 %d 个客户端、%d 条线路", peers, chains),
		KeyLabel:   "客户端",
		SplitChain: splitChain,
		SplitRule:  splitRule,
		Rows:       rows,
		Total:      total,
		Truncated:  total > len(rows),
	}, nil
}

// GetSourceIPDetail 某个客户端的明细
// 默认只按访问目标聚合；splitChain / splitRule 打开时才把线路、规则加进分组维度
func (d *Database) GetSourceIPDetail(sourceIP string, startTimestamp, endTimestamp int64,
	splitChain, splitRule bool, search, sortBy, order string) (*DetailResponse, error) {

	if sourceIP == "" {
		return nil, fmt.Errorf("客户端不能为空")
	}

	baseWhere := fmt.Sprintf("(%s) = ?", sourceIPExpr)
	args := []interface{}{sourceIP}

	rows, total, err := d.queryDetailRows(baseWhere, args, startTimestamp, endTimestamp,
		targetExpr, splitChain, splitRule, search, sortBy, order)
	if err != nil {
		return nil, err
	}

	peers, chains, err := d.queryDetailCount(baseWhere, args, startTimestamp, endTimestamp, targetExpr)
	if err != nil {
		return nil, err
	}

	return &DetailResponse{
		Name:       sourceIP,
		Subtitle:   fmt.Sprintf("访问了 %d 个目标、%d 条线路", peers, chains),
		KeyLabel:   "目标",
		SplitChain: splitChain,
		SplitRule:  splitRule,
		Rows:       rows,
		Total:      total,
		Truncated:  total > len(rows),
	}, nil
}

// queryDetailRows 在给定过滤条件下查询明细
// peerExpr: 看目标明细时传 sourceIPExpr，看客户端明细时传 targetExpr
// 分组维度由 splitChain / splitRule 决定，未拆分的维度不出现在 SELECT 与 GROUP BY 里
func (d *Database) queryDetailRows(baseWhere string, baseArgs []interface{},
	startTimestamp, endTimestamp int64, peerExpr string, splitChain, splitRule bool,
	search, sortBy, order string) ([]DetailRow, int, error) {

	whereClause, filterArgs := buildConditions(baseWhere, peerExpr, search, startTimestamp, endTimestamp)
	args := append([]interface{}{}, baseArgs...)
	args = append(args, filterArgs...)

	// 第一列始终是对侧维度，线路与规则按需加入
	selects := []string{peerExpr + " AS key"}
	groups := []string{peerExpr}
	if splitChain {
		selects = append(selects, chainExpr+" AS chain")
		groups = append(groups, chainExpr)
	}
	if splitRule {
		selects = append(selects, ruleExpr+" AS rule")
		groups = append(groups, ruleExpr)
	}

	query := fmt.Sprintf(`
		SELECT
			%s,
			SUM(download) AS download,
			SUM(upload) AS upload,
			SUM(download + upload) AS total,
			COUNT(*) AS connections,
			%s AS first_seen,
			%s AS last_seen
		FROM connections
		WHERE %s
		GROUP BY %s
		%s
		LIMIT %d
	`, strings.Join(selects, ", "), firstSeenExpr, lastSeenExpr,
		whereClause, strings.Join(groups, ", "),
		buildOrderBy(sortBy, order), maxTableRows+1)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	// 按查询里实际出现的列顺序扫描
	scanArgs := func(row *DetailRow) []interface{} {
		dest := []interface{}{&row.Key}
		if splitChain {
			dest = append(dest, &row.Chain)
		}
		if splitRule {
			dest = append(dest, &row.Rule)
		}
		return append(dest, &row.Download, &row.Upload, &row.Total, &row.Connections, &row.FirstSeen, &row.LastSeen)
	}

	details := []DetailRow{}
	for rows.Next() {
		var row DetailRow
		if err := rows.Scan(scanArgs(&row)...); err != nil {
			return nil, 0, err
		}
		details = append(details, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if len(details) <= maxTableRows {
		// 没截断：取到的行数就是总数，省掉一次聚合查询
		return details, len(details), nil
	}

	// 截断了才需要真实总数
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT %s FROM connections WHERE %s GROUP BY %s
		)
	`, peerExpr, whereClause, strings.Join(groups, ", "))

	var total int
	if err := d.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	return details[:maxTableRows], total, nil
}

// queryDetailCount 统计明细对象的对侧维度数量与线路数
// peerExpr: 按目标看明细时传 sourceIPExpr，按客户端看时传 targetExpr
func (d *Database) queryDetailCount(baseWhere string, baseArgs []interface{},
	startTimestamp, endTimestamp int64, peerExpr string) (int64, int64, error) {

	whereClause, timeArgs := buildConditions(baseWhere, "", "", startTimestamp, endTimestamp)
	args := append([]interface{}{}, baseArgs...)
	args = append(args, timeArgs...)

	query := fmt.Sprintf(`
		SELECT
			COUNT(DISTINCT %s) AS peers,
			COUNT(DISTINCT %s) AS chains
		FROM connections
		WHERE %s
	`, peerExpr, chainExpr, whereClause)

	var peers, chains int64
	if err := d.db.QueryRow(query, args...).Scan(&peers, &chains); err != nil {
		return 0, 0, err
	}

	return peers, chains, nil
}

func calculateTimeSlotIndex(timestamp int64, dateStart time.Time) int {
	t := time.Unix(timestamp, 0).In(dateStart.Location())
	duration := t.Sub(dateStart)
	minutes := int(duration.Minutes())
	slotIndex := minutes / 5
	if slotIndex < 0 {
		return 0
	}
	if slotIndex > 287 {
		return 287
	}
	return slotIndex
}

// getClashGanttData 从 connections 现算 Clash 代理连接热度（改造前的算法，逐槽保持不变）。
func (d *Database) getClashGanttData(date string, normalize func(uint16) int) ([]GanttRow, error) {
	// 计算日期范围的时间戳（使用本地时区）
	localLoc := time.Now().Location()
	dateStart, err := time.ParseInLocation("2006-01-02", date, localLoc)
	if err != nil {
		return nil, fmt.Errorf("解析日期失败: %v", err)
	}
	dateEnd := dateStart.Add(24 * time.Hour)

	// 构建查询，使用时间戳范围查询（更高效）
	query := `
		SELECT 
			CASE 
				WHEN source_ip IS NOT NULL AND source_ip != '' THEN source_ip
				WHEN type = 'Inner' THEN 'Inner'
				ELSE 'Unknown'
			END AS source_ip,
			start_timestamp, last_seen_timestamp
		FROM connections
		WHERE start_timestamp >= ? AND start_timestamp < ?
		AND ((source_ip IS NOT NULL AND source_ip != '') OR type = 'Inner')
		ORDER BY source_ip, start_timestamp
	`

	rows, err := d.db.Query(query, dateStart.Unix(), dateEnd.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 按sourceIP组织数据。存指针而不是数组值：数组是值类型，
	// 直接 ganttData[ip] 取出来再改再写回会整块拷贝 288 字节
	ganttData := make(map[string]*[288]uint16)

	for rows.Next() {
		var sourceIP string
		var startTimestamp, lastSeenTimestamp int64

		err := rows.Scan(&sourceIP, &startTimestamp, &lastSeenTimestamp)
		if err != nil {
			return nil, err
		}

		// 还没有被刷新过时使用当前时间
		if lastSeenTimestamp == 0 {
			lastSeenTimestamp = time.Now().Unix()
		}

		// 获取或创建时间槽
		timeSlots, ok := ganttData[sourceIP]
		if !ok {
			timeSlots = &[288]uint16{}
			ganttData[sourceIP] = timeSlots
		}

		// 计算开始和最后一次被看到的时间槽序号
		startSlot := calculateTimeSlotIndex(startTimestamp, dateStart)
		endSlot := calculateTimeSlotIndex(lastSeenTimestamp, dateStart)

		// 给从开始到结束的所有时间槽加1
		for i := startSlot; i <= endSlot; i++ {
			if timeSlots[i] < 65535 {
				timeSlots[i]++
			}
		}
	}

	// 稀疏化 + 归一化到 1-6 档 + 自然排序，全部交给各源共用的出口
	return buildGanttRows(ganttData, normalize), nil
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

// CleanupOldData 清理过期数据
func (d *Database) CleanupOldData(days int) error {
	cutoffTimestamp := time.Now().AddDate(0, 0, -days).Unix()

	// 批量删除最后一次被看到的时间早于保留期的记录
	stmt, err := d.db.Prepare("DELETE FROM connections WHERE last_seen_timestamp < ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	result, err := stmt.Exec(cutoffTimestamp)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// flow_slots / activity 与 connections 共用同一个 30 天 cutoff，否则推送源的表会无限增长
	if err := d.CleanupActivity(cutoffTimestamp); err != nil {
		return err
	}
	if err := d.CleanupFlowSlots(cutoffTimestamp); err != nil {
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
