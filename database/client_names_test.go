package database

import (
	"os"
	"testing"
)

// TestEffectiveName 校验"手动优先"的生效名称计算（纯函数，无需 DB）。
func TestEffectiveName(t *testing.T) {
	rec := &ClientName{ManualName: "客厅电视", AutoName: "android-dhcp", AutoSource: "dhcp_option12"}
	if name, src := effectiveName(rec); name != "客厅电视" || src != "manual" {
		t.Fatalf("手动应优先，得到 name=%q src=%q", name, src)
	}
	rec.ManualName = ""
	if name, src := effectiveName(rec); name != "android-dhcp" || src != "dhcp_option12" {
		t.Fatalf("无手动时应回退自动，得到 name=%q src=%q", name, src)
	}
	rec.AutoName = ""
	if name, src := effectiveName(rec); name != "" || src != "" {
		t.Fatalf("都为空时应为空，得到 name=%q src=%q", name, src)
	}
}

// TestClientNameRoundTrip 校验列名隔离：写自动名称不碰手动列；清除手动后自动名称仍在。
func TestClientNameRoundTrip(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	db, err := InitDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const ip = "192.168.1.5"
	find := func() *ClientName {
		recs, err := db.GetClientNames()
		if err != nil {
			t.Fatal(err)
		}
		for i := range recs {
			if recs[i].IP == ip {
				return &recs[i]
			}
		}
		return nil
	}

	// 只有自动名称
	if err := db.UpsertAutoName(ip, "android-dhcp", "dhcp_option12", 100); err != nil {
		t.Fatal(err)
	}
	r := find()
	if r == nil || r.Name != "android-dhcp" || r.Source != "dhcp_option12" {
		t.Fatalf("自动名称未生效: %#v", r)
	}

	// 写手动名称：生效值变手动，自动列保持不变
	if err := db.UpsertManualName(ip, "客厅电视", "卧室旧盒子"); err != nil {
		t.Fatal(err)
	}
	r = find()
	if r == nil || r.Name != "客厅电视" || r.Source != "manual" {
		t.Fatalf("手动未覆盖自动: %#v", r)
	}
	if r.AutoName != "android-dhcp" || r.ManualName != "客厅电视" || r.Note != "卧室旧盒子" {
		t.Fatalf("列发生串写: %#v", r)
	}

	// 再写一次自动名称：手动列不能被改动
	if err := db.UpsertAutoName(ip, "new-mdns", "mdns", 200); err != nil {
		t.Fatal(err)
	}
	r = find()
	if r.ManualName != "客厅电视" || r.Note != "卧室旧盒子" {
		t.Fatalf("写自动名称时改动了手动列: %#v", r)
	}
	if r.Name != "客厅电视" {
		t.Fatalf("手动应仍为生效值: %#v", r)
	}

	// 清除手动：自动名称回退为兜底
	if err := db.ClearManualName(ip); err != nil {
		t.Fatal(err)
	}
	r = find()
	if r == nil || r.Name != "new-mdns" || r.Source != "mdns" {
		t.Fatalf("清除手动后应回退到自动名称: %#v", r)
	}
}
