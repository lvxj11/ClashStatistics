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

// TestClashGanttUnchanged 校验改造后单 Clash 源仍按 [start,last_seen] 逐槽累加并
// 输出与旧实现等价的稀疏结构（值变为档位，但槽位置一一对应）。
func TestClashGanttUnchanged(t *testing.T) {
	db, date := withTempDB(t)
	loc := time.Now().Location()
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := dayStart.Add(10 * time.Hour).Unix()

	// 直接写表以精确控制区间：SaveConnections 会把 last_seen 强制刷成当前时间
	for _, c := range []struct {
		id, ip   string
		from, to int64
	}{
		{"c1", "192.168.1.5", start, start + 600},
		{"c2", "192.168.1.5", start, start + 600},
		{"c3", "192.168.1.9", start, start},
	} {
		if _, err := db.db.Exec(`INSERT INTO connections (id, source_ip, start_timestamp, last_seen_timestamp)
			VALUES (?, ?, ?, ?)`, c.id, c.ip, c.from, c.to); err != nil {
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

// TestActivityRoundTrip 校验推送源落库即聚合：一条跨 3 槽的流累加两次后，
// 查询能还原成正确的稀疏行，且档位落在 1-6。
func TestActivityRoundTrip(t *testing.T) {
	db, date := withTempDB(t)
	loc := time.Now().Location()
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := dayStart.Add(10 * time.Hour).Unix()
	end := start + 12*60

	for i := 0; i < 2; i++ {
		if err := db.SaveActivity("ipfix", "192.168.1.5", start, end); err != nil {
			t.Fatal(err)
		}
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
	// 累加两次 → 每槽原始流数 2，同时验证 RawValues 带出的是绝对数值而非档位
	if len(rows[0].RawValues) != 3 {
		t.Fatalf("RawValues 应与槽一一对应, got %d", len(rows[0].RawValues))
	}
	for _, v := range rows[0].RawValues {
		if v != 2 {
			t.Fatalf("原始流数应为 2, got %d", v)
		}
	}

	// Clash 源不应看到 ipfix 的数据，两个源严格隔离
	clashRows, err := db.GetGanttData("clash", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(clashRows) != 0 {
		t.Fatalf("Clash 源不应混入 activity 数据: %#v", clashRows)
	}
}

// TestCustomThresholdsPerSource 校验阈值按数据源行读取：开启自定义后同一份原始
// 活跃量映射到不同档位，不影响其它源，且 RawValues 始终是原始值。
func TestCustomThresholdsPerSource(t *testing.T) {
	db, date := withTempDB(t)
	loc := time.Now().Location()
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := dayStart.Add(10 * time.Hour).Unix()

	// 同一槽累加 21 条流 → 原始值 21
	for i := 0; i < 21; i++ {
		if err := db.SaveActivity("ipfix", "192.168.1.5", start, start); err != nil {
			t.Fatal(err)
		}
	}

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
		t.Fatalf("clash 源不应受 ipfix 影响，仍用默认阈值, got %v", got)
	}
}
