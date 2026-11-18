package database

import (
	"os"
	"testing"
	"time"
)

// TestDefaultThresholds 保证默认阈值与改造前前端 heatColor 的 6 档逐档一致 ——
// 这是「单 Clash 源零回归」的核心保证。
func TestDefaultThresholds(t *testing.T) {
	cases := []struct {
		raw  uint16
		want int
	}{
		{0, 0}, {1, 1}, {2, 1}, {3, 2}, {20, 2}, {21, 3},
		{50, 3}, {51, 4}, {150, 4}, {151, 5}, {254, 5}, {255, 6}, {1000, 6},
	}
	for _, c := range cases {
		if got := toIntensity(c.raw, DefaultThresholds); got != c.want {
			t.Errorf("toIntensity(%d, 默认) = %d, want %d", c.raw, got, c.want)
		}
	}
}

// TestParseThresholds 校验自定义阈值：必须 5 个、正整数、严格递增、≤65535。
func TestParseThresholds(t *testing.T) {
	for _, s := range []string{
		"2,20,50,150,254",
		"2, 50, 200, 600, 900",
		"1,2,3,4,5",
		"2,20,50,150,65535",
	} {
		got, err := ParseThresholds(s)
		if err != nil {
			t.Errorf("ParseThresholds(%q) 应合法: %v", s, err)
			continue
		}
		if len(got) != 5 {
			t.Errorf("ParseThresholds(%q) 应返回 5 个值, got %v", s, got)
		}
	}
	for _, s := range []string{
		"", "2,20,50,150", "2,20,50,150,254,300",
		"2,20,20,150,254", "254,150,50,20,2",
		"0,20,50,150,254", "-1,20,50,150,254",
		"a,20,50,150,254", "2,20,50,150,65536",
	} {
		if _, err := ParseThresholds(s); err == nil {
			t.Errorf("ParseThresholds(%q) 应报错", s)
		}
	}
}

func withTempDB(t *testing.T) (*Database, string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	db, err := InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, time.Now().Format("2006-01-02")
}

// mustUpsert 建一个真实数据源。v3.0.0 起 GetGanttData 按 data_sources 里的真实
// 类型 / ID 路由，测试必须先注册源，不能再依赖硬编码 "clash" / "ipfix"。
func mustUpsert(t *testing.T, db *Database, id, typ string, inSummary bool) {
	t.Helper()
	if err := db.UpsertSource(DataSource{
		ID: id, Type: typ, Name: id, Enabled: true, InSummary: inSummary,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestClashGanttUnchanged 校验 v3.0.0 单 Clash 源仍按 [start,last_seen] 逐槽累加并
// 输出与旧实现等价的稀疏结构（值变为档位，但槽位置一一对应）。
func TestClashGanttUnchanged(t *testing.T) {
	db, date := withTempDB(t)
	mustUpsert(t, db, "clash", "clash", true)
	loc := time.Now().Location()
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := dayStart.Add(10 * time.Hour).Unix()

	// 直接写表以精确控制区间：SaveConnections 会把 last_seen 强制刷成当前时间。
	// 所有行必须显式打上 source='clash'，在线状态严格按源隔离取数。
	for _, c := range []struct {
		id, ip   string
		from, to int64
	}{
		{"c1", "192.168.1.5", start, start + 600},
		{"c2", "192.168.1.5", start, start + 600},
		{"c3", "192.168.1.9", start, start},
	} {
		if _, err := db.db.Exec(`INSERT INTO connections (source, id, source_ip, start_timestamp, last_seen_timestamp)
			VALUES ('clash', ?, ?, ?, ?)`, c.id, c.ip, c.from, c.to); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.GetGanttData("clash", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("应有 2 行, got %d: %#v", len(rows), rows)
	}
	if rows[0].SourceIP != "192.168.1.5" || rows[1].SourceIP != "192.168.1.9" {
		t.Fatalf("应按 IP 自然排序: %#v", rows)
	}
	// c1+c2 覆盖 3 个槽、每槽原始值 2 → 档位 1
	if len(rows[0].SlotIndexes) != 3 {
		t.Fatalf("c1+c2 应跨 3 个槽, got %d", len(rows[0].SlotIndexes))
	}
	for _, v := range rows[0].SlotValues {
		if v != 1 {
			t.Fatalf("原始值 2 应归一化为档位 1, got %d", v)
		}
	}
	// 档位之外还要把原始活跃量一并带出，供悬停展示绝对数值
	if len(rows[0].RawValues) != 3 {
		t.Fatalf("RawValues 应与槽一一对应, got %d", len(rows[0].RawValues))
	}
	for _, v := range rows[0].RawValues {
		if v != 2 {
			t.Fatalf("原始活跃量应为 2, got %d", v)
		}
	}
	// c3 只占 1 个槽
	if len(rows[1].SlotIndexes) != 1 {
		t.Fatalf("c3 应占 1 个槽, got %d", len(rows[1].SlotIndexes))
	}
}

// TestClashGanttSourceIsolation 校验 v3.0.0 在线状态按 clash 源 ID 严格隔离：
// 不同 clash 源写入 connections 的数据互不串台，存量 source=” 也不出现在在线状态。
func TestClashGanttSourceIsolation(t *testing.T) {
	db, date := withTempDB(t)
	mustUpsert(t, db, "a", "clash", true)
	mustUpsert(t, db, "b", "clash", true)
	loc := time.Now().Location()
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := dayStart.Add(10 * time.Hour).Unix()

	insert := func(source, id, ip string) {
		if _, err := db.db.Exec(`INSERT INTO connections (source, id, source_ip, start_timestamp, last_seen_timestamp)
			VALUES (?, ?, ?, ?, ?)`, source, id, ip, start, start); err != nil {
			t.Fatal(err)
		}
	}
	insert("a", "1", "192.168.1.5")
	insert("b", "2", "192.168.1.9")
	insert("", "3", "192.168.1.100") // 升级前遗留，在线状态看不到

	rowsA, err := db.GetGanttData("a", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsA) != 1 || rowsA[0].SourceIP != "192.168.1.5" {
		t.Fatalf("源 a 只应看到自己的连接: %#v", rowsA)
	}
	rowsB, err := db.GetGanttData("b", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsB) != 1 || rowsB[0].SourceIP != "192.168.1.9" {
		t.Fatalf("源 b 只应看到自己的连接: %#v", rowsB)
	}
	// 未知 / 空源返回空，绝不回落到全表混合
	rowsUnknown, err := db.GetGanttData("nope", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsUnknown) != 0 {
		t.Fatalf("未知源应返回空: %#v", rowsUnknown)
	}
}

// TestFlowSlotsDedup 校验 B 方案的核心语义：相同五元组的多个流分片（同一长连接被
// active-flow-timeout 切片或重复上报）在同一 5 分钟槽只算 1 条并发连接；不同五元组
// 才算多条。查询出的 RawValues 即真实并发连接数，与切片长度无关。
func TestFlowSlotsDedup(t *testing.T) {
	db, date := withTempDB(t)
	mustUpsert(t, db, "ipfix", "ipfix", false)
	mustUpsert(t, db, "clash", "clash", true)
	loc := time.Now().Location()
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := dayStart.Add(10 * time.Hour).Unix()
	end := start + 12*60 // 跨 3 个槽

	rec := func(srcPort uint16) FlowRecord {
		return FlowRecord{
			SrcIP: "192.168.1.5", SrcPort: srcPort, DstIP: "8.8.8.8",
			DstPort: 443, Protocol: 6, Start: start, End: end,
		}
	}

	// 同一连接被切成 3 个分片（五元组完全相同，且都覆盖 3 个槽）：
	// 每个 (五元组, 槽) 去重后只留 1 行。
	if err := db.SaveFlows("ipfix", []FlowRecord{
		rec(12345), rec(12345), rec(12345),
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := db.GetGanttData("ipfix", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SourceIP != "192.168.1.5" {
		t.Fatalf("应有 1 行, got %#v", rows)
	}
	if len(rows[0].SlotIndexes) != 3 {
		t.Fatalf("应跨 3 个槽, got %d", len(rows[0].SlotIndexes))
	}
	for _, v := range rows[0].SlotValues {
		if v < 1 || v > 6 {
			t.Fatalf("档位越界: %d", v)
		}
	}
	for _, v := range rows[0].RawValues {
		if v != 1 {
			t.Fatalf("3 个同五元组分片去重后每槽并发连接应为 1, got %d", v)
		}
	}

	// 再上报一条不同源端口的连接：每槽并发连接数应变成 2。
	if err := db.SaveFlows("ipfix", []FlowRecord{rec(12346)}); err != nil {
		t.Fatal(err)
	}
	rows, err = db.GetGanttData("ipfix", date)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range rows[0].RawValues {
		if v != 2 {
			t.Fatalf("两个不同五元组每槽并发连接应为 2, got %d", v)
		}
	}

	// Clash 源走 connections、IPFIX 走 flow_slots，两个源严格隔离
	clashRows, err := db.GetGanttData("clash", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(clashRows) != 0 {
		t.Fatalf("Clash 源不应混入 flow_slots 数据: %#v", clashRows)
	}
}

// TestCustomThresholdsPerSource 校验阈值按数据源行读取：开启自定义后同一份原始
// 活跃量映射到不同档位，不影响其它源，且 RawValues 始终是原始值。
func TestCustomThresholdsPerSource(t *testing.T) {
	db, date := withTempDB(t)
	loc := time.Now().Location()
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := dayStart.Add(10 * time.Hour).Unix()

	// 同一槽上报 21 条不同五元组的连接 → 去重后原始并发连接数 21
	flows := make([]FlowRecord, 21)
	for i := range flows {
		flows[i] = FlowRecord{
			SrcIP: "192.168.1.5", SrcPort: uint16(10000 + i), DstIP: "8.8.8.8",
			DstPort: 443, Protocol: 6, Start: start, End: start,
		}
	}
	if err := db.SaveFlows("ipfix", flows); err != nil {
		t.Fatal(err)
	}

	// 注册 ipfix 源（GetGanttData 需按真实类型路由）
	mustUpsert(t, db, "ipfix", "ipfix", false)

	// 未开启自定义：默认阈值 {2,20,50,150,254} 下 21 → 3 档
	rows, err := db.GetGanttData("ipfix", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SlotValues[0] != 3 {
		t.Fatalf("默认阈值下原始 21 应为 3 档: %#v", rows)
	}

	// 给 ipfix 源开启自定义阈值 100,200,300,400,500 → 21 → 1 档
	if err := db.UpsertSource(DataSource{
		ID: "ipfix", Type: "ipfix", Name: "IPFIX", Enabled: true,
		CustomThresholds: true, Thresholds: "100,200,300,400,500",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = db.GetGanttData("ipfix", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SlotValues[0] != 1 {
		t.Fatalf("自定义阈值下原始 21 应为 1 档: %#v", rows)
	}
	if rows[0].RawValues[0] != 21 {
		t.Fatalf("RawValues 应始终是原始值 21, got %d", rows[0].RawValues[0])
	}

	if got := db.sourceThresholds("ipfix"); len(got) != 5 || got[0] != 100 {
		t.Fatalf("ipfix 源应读到自定义阈值, got %v", got)
	}
	if got := db.sourceThresholds("clash"); len(got) != 5 || got[0] != 2 {
		t.Fatalf("未注册的 clash 源回落到默认阈值, got %v", got)
	}
}

// TestSummarySourceFilter 校验 v3.0.0 目标 / 客户端统计（含明细弹窗）的来源过滤：
// 只纳入「升级前遗留 source=”」与「勾选汇总的 clash 源」，ipfix / 未勾选的 clash
// 源都不进统计；且明细弹窗与主统计口径一致。
func TestSummarySourceFilter(t *testing.T) {
	db, _ := withTempDB(t)
	mustUpsert(t, db, "a", "clash", true)   // 纳入汇总
	mustUpsert(t, db, "b", "clash", false)  // 不纳入汇总
	mustUpsert(t, db, "ip", "ipfix", false) // ipfix 恒不进统计

	insert := func(source, id, host string) {
		if _, err := db.db.Exec(`INSERT INTO connections
			(source, id, host, source_ip, download, upload, start_timestamp, last_seen_timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, source, id, host, "10.0.0."+id, 100, 100, 1, 2); err != nil {
			t.Fatal(err)
		}
	}
	insert("a", "1", "a.example")
	insert("b", "2", "b.example")
	insert("", "3", "legacy.example")
	insert("ip", "4", "ipfix.example")

	// 目标统计：只剩 a.example + legacy.example 两组
	stats, err := db.GetTargetStats(0, 0, "", "total", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 {
		t.Fatalf("汇总应只含 a + 存量两组, got %d: %#v", stats.Total, stats.Rows)
	}
	for _, r := range stats.Rows {
		if r.Target == "b.example" || r.Target == "ipfix.example" {
			t.Fatalf("不应出现未纳入汇总的源数据: %#v", stats.Rows)
		}
	}

	// 明细弹窗与主统计同口径：纳入的目标有明细，未纳入的目标查不到明细
	detailOK, err := db.GetTargetDetail("a.example", 0, 0, false, false, "", "total", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if len(detailOK.Rows) != 1 {
		t.Fatalf("a.example 应有 1 行明细, got %#v", detailOK.Rows)
	}
	detailNo, err := db.GetTargetDetail("b.example", 0, 0, false, false, "", "total", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if len(detailNo.Rows) != 0 {
		t.Fatalf("b.example 未纳入汇总，明细应为空: %#v", detailNo.Rows)
	}
}
