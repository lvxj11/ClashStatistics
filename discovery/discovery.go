// Package discovery 提供"可选的"客户端名称自动发现：被动监听 DHCP option 12 与
// mDNS，用一条共享的 监听→候选缓存→写 client_names.auto_name 通道落地。
//
// 设计原则：
//   - 自动名称只是兜底辅助，绝不预设一定能拿到。任何一步失败（端口被占、无组播、
//     无权限、无网卡）都只记一条警告并安静降级，不影响手动配置和现有页面。
//   - 手动配置始终优先：这里只写 auto_* 列，展示时手动非空即忽略自动值。
//   - 纯 Go 实现（net + syscall），不引入 cgo / libpcap / 新依赖，CGO_ENABLED=0 可构建。
package discovery

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"clash-statistics/database"
	"clash-statistics/utils"
)

// 自动来源及其优先级（数值越大越优先）。
// 用户实测 DHCP option 12 的名称最贴合识别需求，因此 DHCP > mDNS；
// 手动配置在展示层单独判优先，不在此列。
const (
	srcDHCP        = "dhcp_option12"
	srcInboundUser = "inbound_user"
	srcMDNS        = "mdns"
	srcInboundName = "inbound_name"
	srcProcess     = "process"
)

var sourcePriority = map[string]int{
	srcDHCP:        40,
	srcInboundUser: 35,
	srcMDNS:        30,
	srcInboundName: 10,
	srcProcess:     5,
}

const (
	flushInterval       = 5 * time.Second
	mdnsTriggerInterval = 60 * time.Second
	readTimeout         = 500 * time.Millisecond
)

var mdnsAddr = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

type candidate struct {
	name     string
	source   string
	priority int
	updated  int64
}

// Resolver 持有进程内的候选缓存，并把最优候选定期落库。
type Resolver struct {
	db *database.Database

	mu    sync.Mutex
	best  map[string]candidate // ip -> 当前最优自动候选
	dirty bool                 // 有新候选待落库

	// 仅在 flushLoop 这一条 goroutine 里访问，无需加锁
	written map[string]string // ip -> 上次写库的 (name+source) 签名

	stop     chan struct{}
	stopOnce sync.Once
}

// NewResolver 创建解析器（不启动监听，需显式调用 Start）。
func NewResolver(db *database.Database) *Resolver {
	return &Resolver{
		db:      db,
		best:    map[string]candidate{},
		written: map[string]string{},
		stop:    make(chan struct{}),
	}
}

// Start 启动落库循环与两个被动监听。各自独立，任一失败只降级自己。
func (r *Resolver) Start() {
	utils.GetLogger().Info("客户端名称自动发现启动（DHCP option 12 / mDNS，失败将安静降级）\n")
	go r.flushLoop()
	go r.startMDNS()
	go r.startDHCP()
}

// Stop 停止所有后台 goroutine（正常服务不会调用，便于测试与未来平滑退出）。
func (r *Resolver) Stop() {
	r.stopOnce.Do(func() { close(r.stop) })
}

// offer 把一条候选放进缓存。优先级更高者胜出；同优先级下名称变化视为刷新。
func (r *Resolver) offer(ip, name, source string) {
	if ip == "" || name == "" {
		return
	}
	prio := sourcePriority[source]
	now := time.Now().Unix()

	r.mu.Lock()
	defer r.mu.Unlock()
	cur, exists := r.best[ip]
	replace := !exists ||
		prio > cur.priority ||
		(prio == cur.priority && name != cur.name)
	if replace {
		r.best[ip] = candidate{name: name, source: source, priority: prio, updated: now}
		r.dirty = true
	}
}

func (r *Resolver) flushLoop() {
	tick := time.NewTicker(flushInterval)
	defer tick.Stop()
	r.flush()
	for {
		select {
		case <-r.stop:
			return
		case <-tick.C:
			r.flush()
		}
	}
}

// flush 把发生变化的候选写到 auto_* 列。只写变化过的 IP，避免每 5 秒全量重写。
func (r *Resolver) flush() {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return
	}
	snapshot := make(map[string]candidate, len(r.best))
	for ip, c := range r.best {
		snapshot[ip] = c
	}
	r.dirty = false
	r.mu.Unlock()

	for ip, c := range snapshot {
		sig := c.name + "\x00" + c.source
		if r.written[ip] == sig {
			continue
		}
		if err := r.db.UpsertAutoName(ip, c.name, c.source, c.updated); err != nil {
			utils.GetLogger().Warn("写入自动客户端名称失败 (%s): %v\n", ip, err)
			r.mu.Lock()
			r.dirty = true // 下次重试
			r.mu.Unlock()
			continue
		}
		r.written[ip] = sig
	}
}

// ObserveConnections 利用每次轮询拿到的连接快照，补充基于元数据的候选。
// inbound_user 通常是刻意设置的标识；inbound_name / process 往往是所有客户端共享的
// 代理侧值，只有当某个值只对应单个 IP 时才当作识别名（否则没有区分度，直接丢弃）。
func (r *Resolver) ObserveConnections(conns []database.Connection) {
	type meta struct {
		users map[string]bool
		names map[string]bool
		procs map[string]bool
	}
	byIP := map[string]*meta{}
	valueIPs := map[string]map[string]bool{} // value -> 出现过该值的 IP 集合

	note := func(value, ip string) {
		set := valueIPs[value]
		if set == nil {
			set = map[string]bool{}
			valueIPs[value] = set
		}
		set[ip] = true
	}

	for _, c := range conns {
		ip := c.SourceIPAddr
		if ip == "" {
			continue
		}
		u := cleanLabel(c.InboundUser)
		n := cleanLabel(c.InboundName)
		p := cleanLabel(basename(c.Process))
		m := byIP[ip]
		if m == nil {
			m = &meta{map[string]bool{}, map[string]bool{}, map[string]bool{}}
			byIP[ip] = m
		}
		if u != "" {
			m.users[u] = true
			note(u, ip)
		}
		if n != "" {
			m.names[n] = true
			note(n, ip)
		}
		if p != "" {
			m.procs[p] = true
			note(p, ip)
		}
	}

	for ip, m := range byIP {
		for u := range m.users {
			if !looksRandom(u) {
				r.offer(ip, u, srcInboundUser)
			}
		}
		for n := range m.names {
			if len(valueIPs[n]) == 1 && !looksRandom(n) {
				r.offer(ip, n, srcInboundName)
			}
		}
		for p := range m.procs {
			if len(valueIPs[p]) == 1 && !looksRandom(p) {
				r.offer(ip, p, srcProcess)
			}
		}
	}
}

// basename 取进程字段的最后一段路径（Windows 与 Unix 分隔符都处理）。
func basename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// ---------- mDNS ----------

func (r *Resolver) startMDNS() {
	log := utils.GetLogger()
	// nil 网卡 = 交由系统选择默认组播网卡（通常是默认路由所在网卡）。
	// 裸机 / host 网络 / macvlan 下都指向局域网那块网卡，无需硬编码接口名。
	recv, err := net.ListenMulticastUDP("udp4", nil, mdnsAddr)
	if err != nil {
		log.Warn("mDNS 监听不可用，已跳过（不影响手动配置）: %v\n", err)
		return
	}
	send, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Warn("mDNS 触发查询不可用，已跳过: %v\n", err)
		recv.Close()
		return
	}
	log.Info("mDNS 监听已启动（224.0.0.251:5353）\n")
	r.mdnsLoop(recv, send)
}

func (r *Resolver) mdnsLoop(recv, send *net.UDPConn) {
	defer recv.Close()
	defer send.Close()

	// 启动时先发一轮，设备重连/缓存刷新时再靠周期触发补上
	r.sendMDNSTriggers(send)
	trigger := time.NewTicker(mdnsTriggerInterval)
	defer trigger.Stop()

	buf := make([]byte, 9000)
	for {
		select {
		case <-r.stop:
			return
		case <-trigger.C:
			r.sendMDNSTriggers(send)
		default:
		}

		recv.SetReadDeadline(time.Now().Add(readTimeout))
		n, src, err := recv.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if r.isStopping() {
				return
			}
			continue // 单个报文的偶发错误，不影响后续
		}
		r.handleMDNS(buf[:n], src.IP)
	}
}

// handleMDNS 包一层 recover：报文来自局域网，任何畸形数据都不该拖垮整个进程。
func (r *Resolver) handleMDNS(data []byte, src net.IP) {
	defer func() {
		if rec := recover(); rec != nil {
			utils.GetLogger().Warn("解析 mDNS 报文异常，已忽略该报文: %v\n", rec)
		}
	}()
	for _, hit := range parseMDNS(data, src) {
		r.offer(hit.ip, hit.name, srcMDNS)
	}
}

// sendMDNSTriggers 发若干标准 DNS-SD 查询，唤醒设备主动播报 .local 名称。
// 纯被动等公告不可靠（mDNS 重公告间隔可长达一小时），主动查询是必需的一环。
func (r *Resolver) sendMDNSTriggers(send *net.UDPConn) {
	for _, qname := range []string{
		"_services._dns-sd._udp.local",
		"_googlecast._tcp.local",
		"_airplay._tcp.local",
	} {
		q := buildPTRQuery(qname)
		if _, err := send.WriteToUDP(q, mdnsAddr); err != nil {
			utils.GetLogger().Debug("发送 mDNS 触发查询失败: %v\n", err)
		}
	}
}

// buildPTRQuery 构造一条 QTYPE=PTR 的 mDNS 查询报文。
func buildPTRQuery(qname string) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[2:4], 0) // Flags: 标准查询
	binary.BigEndian.PutUint16(buf[4:6], 1) // QDCOUNT = 1
	for _, label := range strings.Split(strings.TrimSuffix(qname, "."), ".") {
		b := []byte(label)
		if len(b) > 63 {
			b = b[:63]
		}
		buf = append(buf, byte(len(b)))
		buf = append(buf, b...)
	}
	buf = append(buf, 0)     // 名称结束
	buf = append(buf, 0, 12) // QTYPE = PTR
	buf = append(buf, 0, 1)  // QCLASS = IN
	return buf
}

// ---------- DHCP option 12 ----------

func (r *Resolver) startDHCP() {
	log := utils.GetLogger()
	// 绑定到 :67 接收发往 255.255.255.255:67 的 DHCP 广播。
	// SO_REUSEADDR 允许与本机可能存在的 DHCP 服务共存；SO_BROADCAST 允许接收广播。
	// 低端口在容器里由 net.ipv4.ip_unprivileged_port_start=0 放行；裸机需要相应权限，
	// 绑不上就降级（mDNS 与手动配置不受影响）。
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var sockErr error
			if err := c.Control(func(fd uintptr) {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				if sockErr == nil {
					sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
				}
			}); err != nil {
				return err
			}
			return sockErr
		},
	}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":67")
	if err != nil {
		log.Warn("DHCP 监听不可用，已跳过（不影响手动配置）: %v\n", err)
		return
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return
	}
	log.Info("DHCP option 12 监听已启动（:67）\n")
	r.dhcpLoop(conn)
}

func (r *Resolver) dhcpLoop(conn *net.UDPConn) {
	defer conn.Close()
	buf := make([]byte, 2048)
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		conn.SetReadDeadline(time.Now().Add(time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if r.isStopping() {
				return
			}
			continue
		}
		r.handleDHCP(buf[:n], src.IP)
	}
}

// handleDHCP 同样包一层 recover，隔离畸形 DHCP 报文。
func (r *Resolver) handleDHCP(data []byte, src net.IP) {
	defer func() {
		if rec := recover(); rec != nil {
			utils.GetLogger().Warn("解析 DHCP 报文异常，已忽略该报文: %v\n", rec)
		}
	}()
	if ip, host, ok := parseDHCP(data, src); ok {
		r.offer(ip, host, srcDHCP)
	}
}

func (r *Resolver) isStopping() bool {
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
}
