package web

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"clash-statistics/database"
	"clash-statistics/ipfix"
	"clash-statistics/utils"
)

// ipfixCollector 接收路由器 UDP 推送的 NetFlow v9 / IPFIX 流记录，按五元组去重写入
// flow_slots 表，产出真实并发连接数。落地实测结论（2026-11-10）：8 字段模板、只算
// 上行（srcAddr 落 LAN）、撤 NAT/MAC、忽略 IPv6 模板、排除网关自身 IP；长流被
// active-flow-timeout 切片不影响计数（按 五元组 + 5 分钟槽 去重）。
type ipfixCollector struct {
	db       *database.Database
	src      database.DataSource
	conn     *net.UDPConn
	decoder  *ipfix.Decoder
	lanNets  []*net.IPNet
	exclude  map[string]bool
	boot     *bootEstimator
	stopOnce sync.Once
	stop     chan struct{}
}

func newIPFIXCollector(s *Server, src database.DataSource) Collector {
	return &ipfixCollector{
		db:      s.db,
		src:     src,
		decoder: ipfix.NewDecoder(),
		boot:    newBootEstimator(64),
		exclude: parseIPSet(src.ExcludeIPs),
		lanNets: parseCIDRs(src.LANCIDRs),
		stop:    make(chan struct{}),
	}
}

func (c *ipfixCollector) Name() string { return c.src.ID }

func (c *ipfixCollector) Start() {
	log := utils.GetLogger()
	port := c.src.Port
	if port <= 0 {
		port = 2055
	}
	if len(c.lanNets) == 0 {
		log.Warn("数据源 %s 未配置有效 LAN 网段，将不归属任何客户端，请在 /data-sources 管理页配置\n", c.src.ID)
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Error("解析 UDP 监听地址失败: %v\n", err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		// 端口占用等失败只让这个源降级，不影响主服务与其它源
		log.Error("数据源 %s 监听 UDP %d 失败: %v\n", c.src.ID, port, err)
		return
	}
	_ = conn.SetReadBuffer(1 << 20)
	c.conn = conn
	go c.run()
}

func (c *ipfixCollector) Stop() {
	c.stopOnce.Do(func() {
		close(c.stop)
		if c.conn != nil {
			c.conn.Close()
		}
	})
}

func (c *ipfixCollector) run() {
	log := utils.GetLogger()
	if c.conn == nil {
		return
	}
	defer c.conn.Close()

	buf := make([]byte, 65535)
	for {
		select {
		case <-c.stop:
			return
		default:
		}

		// 用短 deadline 让 stop 能及时生效，超时不算错误
		c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, peer, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			select {
			case <-c.stop:
				return
			default:
				log.Warn("读取流报文失败: %v\n", err)
				continue
			}
		}
		c.handlePacket(buf[:n], peer.IP)
	}
}

func (c *ipfixCollector) handlePacket(data []byte, peer net.IP) {
	log := utils.GetLogger()
	flows, exportTime, err := c.decoder.Decode(data)
	if err != nil || exportTime == 0 || len(flows) == 0 {
		return
	}

	// first/last-forwarded 是相对设备启动的毫秒值：用本报文内最大的 lastFwd
	// 配合报文头的绝对 Export Time 反推 bootTime，再换算成绝对区间。
	var maxLast uint32
	for _, f := range flows {
		if f.LastFwd > maxLast {
			maxLast = f.LastFwd
		}
	}
	if maxLast == 0 {
		return
	}
	c.boot.add(int64(exportTime) - int64(maxLast)/1000)
	boot := c.boot.value()
	if boot == 0 {
		return
	}

	peerStr := peer.String()
	// 一个报文的所有上行流收集成一批，SaveFlows 内部按 (五元组, 槽) 去重，
	// 只开一个事务；不做 TTL / 内存连接表，避免重启边界与五元组复用的歧义。
	records := make([]database.FlowRecord, 0, len(flows))
	for _, f := range flows {
		if f.IsV6 || f.SrcIP == nil {
			continue // IPv6 模板一期忽略
		}
		ip := f.SrcIP.String()
		if !c.inLAN(f.SrcIP) {
			continue // 只算上行：源地址必须落在 LAN 网段
		}
		if c.exclude[ip] || ip == peerStr {
			continue // 排除配置里的 IP 与路由器（导出源）自身流量
		}

		start := boot + int64(f.FirstFwd)/1000
		end := boot + int64(f.LastFwd)/1000
		if end < start {
			continue
		}
		records = append(records, database.FlowRecord{
			SrcIP:    ip,
			SrcPort:  f.SrcPort,
			DstIP:    f.DstIP.String(),
			DstPort:  f.DstPort,
			Protocol: f.Protocol,
			Start:    start,
			End:      end,
		})
	}
	if len(records) == 0 {
		return
	}
	if err := c.db.SaveFlows(c.src.ID, records); err != nil {
		log.Error("保存流记录失败: %v\n", err)
	}
}

func (c *ipfixCollector) inLAN(ip net.IP) bool {
	for _, n := range c.lanNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDRs(s string) []*net.IPNet {
	out := []*net.IPNet{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func parseIPSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

// bootEstimator 维护最近若干次 bootTime 估计的中位数，抵抗单次抖动。
// 实测 exportTime - lastFwd/1000 的全距约 ±3s，中位数足够稳定。
type bootEstimator struct {
	samples []int64
	idx     int
}

func newBootEstimator(n int) *bootEstimator {
	return &bootEstimator{samples: make([]int64, n)}
}

func (b *bootEstimator) add(v int64) {
	b.samples[b.idx%len(b.samples)] = v
	b.idx++
}

func (b *bootEstimator) value() int64 {
	n := b.idx
	if n > len(b.samples) {
		n = len(b.samples)
	}
	if n == 0 {
		return 0
	}
	vals := make([]int64, n)
	copy(vals, b.samples[:n])
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return vals[n/2]
}
