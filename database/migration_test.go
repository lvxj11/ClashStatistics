package database

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

// v2.2.0 的真实库结构（升级基线）：connections 无 source 列、id 单主键；
// data_sources 有 credential_value、无 in_summary。
// 关键：这里刻意还原真实库的「物理列顺序」——last_seen_timestamp 是后来用
// ALTER TABLE 追加在表尾的（真实库 PRAGMA 显示它在第 37 列），而 v3.0.0 的
// connectionsSchema 把它放在 start_timestamp 之后。两者顺序不一致，迁移必须按
// 列名复制，用 SELECT * 按位置复制会串行错位。此 fixture 就是这条回归的守卫。
const v220ConnectionsSchema = `CREATE TABLE connections (
	id TEXT PRIMARY KEY,
	network TEXT, type TEXT, source_ip TEXT, destination_ip TEXT,
	source_port TEXT, destination_port TEXT, host TEXT, dns_mode TEXT,
	uid INTEGER, process TEXT, process_path TEXT, special_proxy TEXT,
	special_rules TEXT, remote_destination TEXT, dscp INTEGER, sniff_host TEXT,
	inbound_ip TEXT, inbound_port TEXT, inbound_name TEXT, inbound_user TEXT,
	source_geoip TEXT, destination_geoip TEXT, source_ipasn TEXT, destination_ipasn TEXT,
	upload INTEGER, download INTEGER, start_time TEXT,
	start_timestamp INTEGER DEFAULT 0,
	chain TEXT, chains TEXT, rule TEXT, rule_payload TEXT,
	source_real_ip TEXT, source_real_port TEXT, dest_real_ip TEXT, dest_real_port TEXT,
	last_seen_timestamp INTEGER DEFAULT 0
);`

const v220DataSourcesSchema = `CREATE TABLE data_sources (
	id TEXT PRIMARY KEY, type TEXT NOT NULL, name TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1, address TEXT NOT NULL DEFAULT '',
	port INTEGER NOT NULL DEFAULT 0, lan_cidrs TEXT NOT NULL DEFAULT '',
	exclude_ips TEXT NOT NULL DEFAULT '', interval INTEGER NOT NULL DEFAULT 0,
	credential_ref TEXT NOT NULL DEFAULT '', credential_value TEXT NOT NULL DEFAULT '',
	is_default INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0,
	thresholds TEXT NOT NULL DEFAULT '', custom_thresholds INTEGER NOT NULL DEFAULT 0
);`

// chdirTemp 切到独立临时目录（InitDB 固定读写 ./data/clash_statistics.db），
// 测试结束恢复，保证不触碰真实库。
func chdirTemp(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

// makeV220DB 构造一个 v2.2.0 形态的库并灌入存量数据；withCredentialValue=false
// 时模拟低于 v2.2.0 的库（缺 credential_value 列），用于校验基线拦截。
func makeV220DB(t *testing.T, withCredentialValue bool) {
	t.Helper()
	if err := os.MkdirAll("./data", 0755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "./data/clash_statistics.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(v220ConnectionsSchema); err != nil {
		t.Fatal(err)
	}
	dsSchema := v220DataSourcesSchema
	if !withCredentialValue {
		// 模拟旧库：没有 credential_value 列
		dsSchema = `CREATE TABLE data_sources (
			id TEXT PRIMARY KEY, type TEXT NOT NULL, name TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1, address TEXT NOT NULL DEFAULT ''
		);`
	}
	if _, err := db.Exec(dsSchema); err != nil {
		t.Fatal(err)
	}
	// 存量 connections：迁移后 source 应被填为 ''。特意给 last_seen_timestamp、
	// chain、rule 这些「建表 DDL 顺序 vs 真实物理顺序」不一致的列填上可区分的值 ——
	// 若迁移按位置 SELECT * 复制，这些值会串到相邻列，下面的断言就会失败。
	if _, err := db.Exec(`INSERT INTO connections
		(id, host, source_ip, start_timestamp, last_seen_timestamp, upload, download, chain, rule)
		VALUES ('legacy-1', 'a.example', '10.0.0.1', 100, 222, 1, 2, 'PROXY-A', 'RULE-A'),
		       ('legacy-2', 'b.example', '10.0.0.2', 300, 444, 3, 4, 'PROXY-B', 'RULE-B')`); err != nil {
		t.Fatal(err)
	}
	if withCredentialValue {
		if _, err := db.Exec(`INSERT INTO data_sources
			(id, type, name, enabled, is_default) VALUES
			('clash', 'clash', 'Clash 代理', 1, 0),
			('ipfix', 'ipfix', '路由器流量', 1, 1)`); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func pragmaHasColumn(t *testing.T, db *Database, table, col string) bool {
	t.Helper()
	cols, err := tableColumns(db.db, table)
	if err != nil {
		t.Fatal(err)
	}
	return cols[col]
}

// TestMigrateV220ToV300 覆盖规格 point C 的核心：v2.2.0 → v3.0.0 幂等迁移。
func TestMigrateV220ToV300(t *testing.T) {
	chdirTemp(t)
	makeV220DB(t, true)

	db, err := InitDB()
	if err != nil {
		t.Fatalf("v2.2.0 升级到 v3.0.0 应成功: %v", err)
	}

	// connections 重建为 (source,id)：source 列存在，存量行 source=''，数据不丢
	if !pragmaHasColumn(t, db, "connections", "source") {
		t.Fatal("迁移后 connections 缺少 source 列")
	}
	var cnt int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE source = ''`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 2 {
		t.Errorf("存量 connections 应保留且 source=''，got %d 行", cnt)
	}
	// 逐列核对易错位的字段：物理顺序不一致时，按位置复制会把 last_seen_timestamp
	// 串到 chain、chain 串到 rule…… 这里断言每个值都仍在自己的列里。
	var host, chain, rule string
	var startTS, lastSeen, upload, download int64
	if err := db.db.QueryRow(`SELECT host, start_timestamp, last_seen_timestamp, upload, download, chain, rule
		FROM connections WHERE id = 'legacy-1'`).Scan(
		&host, &startTS, &lastSeen, &upload, &download, &chain, &rule); err != nil {
		t.Fatal(err)
	}
	if host != "a.example" || startTS != 100 || lastSeen != 222 ||
		upload != 1 || download != 2 || chain != "PROXY-A" || rule != "RULE-A" {
		t.Errorf("存量数据被破坏（列错位）: host=%q start=%d lastSeen=%d up=%d down=%d chain=%q rule=%q",
			host, startTS, lastSeen, upload, download, chain, rule)
	}

	// in_summary 列补齐：clash 默认 1、ipfix 强制 0
	if !pragmaHasColumn(t, db, "data_sources", "in_summary") {
		t.Fatal("迁移后 data_sources 缺少 in_summary 列")
	}
	var clashSummary, ipfixSummary int
	if err := db.db.QueryRow(`SELECT in_summary FROM data_sources WHERE id = 'clash'`).Scan(&clashSummary); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT in_summary FROM data_sources WHERE id = 'ipfix'`).Scan(&ipfixSummary); err != nil {
		t.Fatal(err)
	}
	if clashSummary != 1 || ipfixSummary != 0 {
		t.Errorf("in_summary 迁移错误: clash=%d(应为1) ipfix=%d(应为0)", clashSummary, ipfixSummary)
	}

	// 存量无来源数据默认纳入汇总：目标统计能看到 a.example / b.example
	stats, err := db.GetTargetStats(0, 9999999999, "", "download", "desc")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range stats.Rows {
		seen[row.Target] = true
	}
	if !seen["a.example"] || !seen["b.example"] {
		t.Errorf("存量无来源数据应默认出现在目标统计里，got rows %v", seen)
	}
	var summaryCnt int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE ` + summarySourceCond).Scan(&summaryCnt); err != nil {
		t.Fatal(err)
	}
	if summaryCnt != 2 {
		t.Errorf("存量无来源数据应默认纳入汇总，got %d 行", summaryCnt)
	}

	db.Close()

	// 幂等：再开一次库不应报错、不应重复复制数据
	db2, err := InitDB()
	if err != nil {
		t.Fatalf("重复迁移（幂等）不应报错: %v", err)
	}
	defer db2.Close()
	if err := db2.db.QueryRow(`SELECT COUNT(*) FROM connections`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 2 {
		t.Errorf("重复迁移后行数应保持 2，got %d", cnt)
	}
}

// TestMigrationBaselineRejectsPreV220 校验低于 v2.2.0 的库明确报错、不静默迁移。
func TestMigrationBaselineRejectsPreV220(t *testing.T) {
	chdirTemp(t)
	makeV220DB(t, false) // 缺 credential_value 列 = 低于 v2.2.0

	db, err := InitDB()
	if err == nil {
		db.Close()
		t.Fatal("低于 v2.2.0 的库应报错，不应静默迁移")
	}
}
