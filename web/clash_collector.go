package web

import (
	"sync"
	"time"

	"clash-statistics/database"
	"clash-statistics/discovery"
	"clash-statistics/utils"
)

// clashCollector 保留改造前 periodicallyRefreshData 的全部行为，只是把
// host / interval / secret 改为从数据源定义读取。
// 名称发现（DHCP/mDNS）由 Server 独立常驻启动，不依赖本采集器；这里仅在拿到
// Clash 快照时顺带补充候选，停掉 Clash 源不会影响名称发现。
type clashCollector struct {
	db       *database.Database
	resolver *discovery.Resolver
	src      database.DataSource
	stopOnce sync.Once
	stop     chan struct{}
}

func newClashCollector(s *Server, src database.DataSource) Collector {
	return &clashCollector{
		db:       s.db,
		resolver: s.resolver,
		src:      src,
		stop:     make(chan struct{}),
	}
}

func (c *clashCollector) Name() string { return c.src.ID }

func (c *clashCollector) Start() { go c.run() }

func (c *clashCollector) Stop() {
	c.stopOnce.Do(func() { close(c.stop) })
}

func (c *clashCollector) run() {
	log := utils.GetLogger()
	interval := c.src.Interval
	if interval <= 0 {
		interval = 1000
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
	defer ticker.Stop()

	host := c.src.Address
	// 凭据按数据源定义解析：credential_ref 指向的环境变量优先，否则解混淆表内值。
	// 真值只在内存里用，不进日志 / 接口。
	secret := resolveSecret(c.src)

	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			connectionInfo, err := utils.GetConnectionsFromClash(host, secret, interval)
			if err != nil {
				log.Error("获取连接信息失败: %v\n", err)
				continue
			}

			dbConnections := make([]database.Connection, len(connectionInfo.Connections))
			for i, conn := range connectionInfo.Connections {
				dbConnections[i] = database.FromClashConnection(conn)
			}

			c.resolver.ObserveConnections(dbConnections)

			if err := c.db.SaveConnections(dbConnections); err != nil {
				log.Error("保存连接信息到数据库失败: %v\n", err)
			} else {
				log.Info("成功保存 %d 个连接到数据库\n", len(dbConnections))
			}
		}
	}
}
