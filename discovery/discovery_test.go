package discovery

import (
	"testing"

	"clash-statistics/database"
)

// TestObserveConnectionsIgnoresInboundName 回归：inbound_name 是代理侧入站名，
// 所有客户端共用（如 DEFAULT-TUN）。过去靠"值只对应单个 IP"的守卫放行，但单次
// 快照里只有一个设备在线时共享常量也会看似唯一，导致把 DEFAULT-TUN 当成客户端
// 名称。现在该来源整体移除；inbound_user 仍应正常作为候选。
func TestObserveConnectionsIgnoresInboundName(t *testing.T) {
	r := &Resolver{best: map[string]candidate{}}
	r.ObserveConnections([]database.Connection{
		{SourceIPAddr: "192.168.56.96", InboundName: "DEFAULT-TUN", InboundUser: "客厅电视"},
	})

	c, ok := r.best["192.168.56.96"]
	if !ok {
		t.Fatalf("inbound_user 应产生候选，实际没有")
	}
	if c.name != "客厅电视" || c.source != srcInboundUser {
		t.Fatalf("候选不符: %#v", c)
	}
	if len(r.best) != 1 {
		t.Fatalf("不应有额外候选: %#v", r.best)
	}

	// 只有 inbound_name、且只有一个 IP（旧守卫会误判为"唯一"）时，必须完全忽略
	r2 := &Resolver{best: map[string]candidate{}}
	r2.ObserveConnections([]database.Connection{
		{SourceIPAddr: "192.168.56.96", InboundName: "DEFAULT-TUN"},
	})
	if len(r2.best) != 0 {
		t.Fatalf("inbound_name 不应产生任何候选，实际: %#v", r2.best)
	}
}
