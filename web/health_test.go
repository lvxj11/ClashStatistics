package web

import (
	"strings"
	"testing"
	"time"
)

// TestClashHealth 校验 Clash 健康状态的跃迁：
// 未探测 → waiting；连败 < 3 → ok（成功过）；连败 ≥ 3 → fail；成功清零回 ok。
func TestClashHealth(t *testing.T) {
	cases := []struct {
		name string
		h    SourceHealth
		want HealthState
	}{
		{"启动后未探测", SourceHealth{}, HealthWaiting},
		{"刚成功一次", SourceHealth{LastOkAt: 100}, HealthOK},
		{"失败1次未到阈值", SourceHealth{LastOkAt: 100, FailCount: 1}, HealthOK},
		{"失败2次未到阈值", SourceHealth{LastOkAt: 100, FailCount: 2}, HealthOK},
		{"失败3次达阈值", SourceHealth{LastOkAt: 100, FailCount: 3}, HealthFail},
		{"失败4次", SourceHealth{LastOkAt: 100, FailCount: 4}, HealthFail},
		// 启动后从没成功、且开始连败：达到阈值要判失败（不能停在 waiting）
		{"启动即连败达阈值", SourceHealth{FailCount: 3, LastErrAt: 100}, HealthFail},
		{"启动即连败未达阈值", SourceHealth{FailCount: 2, LastErrAt: 100}, HealthWaiting},
	}
	for _, c := range cases {
		if got := clashHealth(c.h); got != c.want {
			t.Errorf("%s: clashHealth(%+v) = %q, want %q", c.name, c.h, got, c.want)
		}
	}
}

// TestIPFIXHealth 校验 IPFIX 健康按「距最近成功的时间」在读取时推导：
// 没首包 → waiting；< 5min → ok；≥ 5min → fail。
func TestIPFIXHealth(t *testing.T) {
	now := time.Unix(1000000, 0)
	cases := []struct {
		name   string
		lastOk int64
		want   HealthState
	}{
		{"还没收到首包", 0, HealthWaiting},
		{"刚收到包", now.Unix(), HealthOK},
		{"4分59秒前", now.Unix() - int64(4*time.Minute+59*time.Second)/int64(time.Second), HealthOK},
		{"正好5分钟", now.Unix() - int64(ipfixStaleAfter/time.Second), HealthFail},
		{"6分钟前", now.Unix() - 360, HealthFail},
	}
	for _, c := range cases {
		got := ipfixHealth(SourceHealth{LastOkAt: c.lastOk}, now)
		if got != c.want {
			t.Errorf("%s: ipfixHealth(lastOk=%d) = %q, want %q", c.name, c.lastOk, got, c.want)
		}
	}
}

// TestHealthDisabledNotReported 确认 disabled 不参与 Clash/IPFIX 推导，
// 由接口层按「采集是否启用」决定，这里只确认常量值稳定（前端契约）。
func TestHealthDisabledNotReported(t *testing.T) {
	if HealthDisabled != "disabled" {
		t.Errorf("disabled 状态字符串变了，前端契约会断: %q", HealthDisabled)
	}
	for _, s := range []HealthState{HealthWaiting, HealthOK, HealthFail} {
		if strings.TrimSpace(string(s)) == "" {
			t.Errorf("状态 %q 不应为空串", s)
		}
	}
}
