package discovery

import (
	"net"
	"strings"
)

// nameHit 是一条 "IP → 候选名称" 的解析结果。
type nameHit struct {
	ip   string
	name string
}

// ---------- DHCP option 12 ----------

// parseDHCP 从 DHCP 报文中提取 (客户端 IP, 主机名)。
//
// 验证时抓到的 Request 包同时带有 option 50（Requested-IP）与 option 12（Hostname），
// 可以直接得到 IP→主机名，不必做 MAC/ACK 关联。报文来自网络，下面所有读取都做边界检查。
func parseDHCP(data []byte, src net.IP) (string, string, bool) {
	// BOOTP 固定头 236 字节 + 4 字节 magic cookie(0x63825363)
	if len(data) < 240 {
		return "", "", false
	}
	if data[236] != 0x63 || data[237] != 0x82 || data[238] != 0x53 || data[239] != 0x63 {
		return "", "", false
	}

	var (
		hostname    string
		requestedIP string
		msgType     = -1
	)
	off := 240
	for off < len(data) {
		code := data[off]
		off++
		if code == 255 { // End
			break
		}
		if code == 0 { // Pad，无长度字节
			continue
		}
		if off >= len(data) {
			break
		}
		l := int(data[off])
		off++
		if off+l > len(data) {
			break
		}
		val := data[off : off+l]
		off += l
		switch code {
		case 12: // Hostname
			hostname = string(val)
		case 50: // Requested-IP
			if l == 4 {
				requestedIP = net.IP(append([]byte(nil), val...)).String()
			}
		case 53: // DHCP Message Type（1 字节）
			if l >= 1 {
				msgType = int(val[0])
			}
		}
	}

	// 跳过服务端应答（OFFER/ACK/NAK），里面的 option 12 不是客户端自报的主机名
	if msgType == 2 || msgType == 5 || msgType == 6 {
		return "", "", false
	}

	host := cleanLabel(hostname)
	if host == "" || looksRandom(host) {
		return "", "", false
	}

	ip := ""
	if requestedIP != "" && requestedIP != "0.0.0.0" && requestedIP != "<nil>" {
		ip = requestedIP
	} else if src != nil {
		v4 := src.To4()
		if v4 != nil && !v4.Equal(net.IPv4zero) {
			ip = v4.String()
		}
	}
	if ip == "" {
		return "", "", false
	}
	return ip, host, true
}

// ---------- mDNS / DNS ----------

func u16(data []byte, off int) (int, bool) {
	if off < 0 || off+2 > len(data) {
		return 0, false
	}
	return int(data[off])<<8 | int(data[off+1]), true
}

// readName 读取 DNS 名称，处理 0xC0xx 压缩指针，返回 (名称, 紧随其后的偏移, 是否成功)。
// 名称以指针结尾时，"紧随其后"指指针本身之后（2 字节），而不是指针目标处。
func readName(data []byte, off int) (string, int, bool) {
	var parts []string
	cur := off
	next := off
	jumped := false
	jumps := 0
	for {
		if cur >= len(data) {
			return "", 0, false
		}
		length := int(data[cur])
		if length == 0 {
			cur++
			if !jumped {
				next = cur
			}
			break
		}
		if length&0xC0 == 0xC0 { // 压缩指针
			if cur+1 >= len(data) {
				return "", 0, false
			}
			ptr := (length&0x3F)<<8 | int(data[cur+1])
			if !jumped {
				next = cur + 2
			}
			jumps++
			if jumps > 16 || ptr >= len(data) {
				return "", 0, false
			}
			cur = ptr
			jumped = true
			continue
		}
		if length&0xC0 != 0 { // 保留的长度值
			return "", 0, false
		}
		cur++
		if cur+length > len(data) {
			return "", 0, false
		}
		parts = append(parts, string(data[cur:cur+length]))
		cur += length
	}
	return strings.Join(parts, "."), next, true
}

// parseMDNS 从一条 mDNS/DNS 报文里提取候选名称。
//   - A(1) / AAAA(28) 记录的 owner 是 ".local" 主机名，RDATA 是 IP → 直接得到 IP→主机名
//   - TXT(16) 里的 fn= / md= 是设备自报的友好名（如音箱），映射到报文源 IP
//
// 服务实例名（含下划线的 PTR/SRV owner）由 cleanHostname 过滤掉，不当作主机名。
func parseMDNS(data []byte, src net.IP) []nameHit {
	if len(data) < 12 {
		return nil
	}
	qd, _ := u16(data, 4)
	an, _ := u16(data, 6)
	ns, _ := u16(data, 8)
	ar, _ := u16(data, 10)

	off := 12
	// 跳过问题段
	for i := 0; i < qd; i++ {
		_, no, ok := readName(data, off)
		if !ok || no+4 > len(data) {
			return nil
		}
		off = no + 4
	}

	srcIP := ""
	if src != nil {
		if v4 := src.To4(); v4 != nil {
			srcIP = v4.String()
		} else {
			srcIP = src.String()
		}
	}

	hits := []nameHit{}
	seen := map[string]bool{}
	add := func(ip, name string) {
		if ip == "" || name == "" {
			return
		}
		key := ip + "\x00" + name
		if seen[key] {
			return
		}
		seen[key] = true
		hits = append(hits, nameHit{ip: ip, name: name})
	}

	total := an + ns + ar
	for i := 0; i < total; i++ {
		name, no, ok := readName(data, off)
		if !ok {
			break
		}
		off = no
		if off+10 > len(data) {
			break
		}
		typ, _ := u16(data, off)
		rdlen, _ := u16(data, off+8)
		rdata := off + 10
		if rdata+rdlen > len(data) {
			break
		}
		rd := data[rdata : rdata+rdlen]
		switch typ {
		case 1: // A
			if rdlen == 4 {
				add(net.IP(append([]byte(nil), rd...)).String(), cleanHostname(name))
			}
		case 28: // AAAA
			if rdlen == 16 {
				add(net.IP(append([]byte(nil), rd...)).String(), cleanHostname(name))
			}
		case 16: // TXT：若干段 length-prefixed 字符串，找 fn= / md=
			for _, txt := range txtStrings(rd) {
				if friendly := txtFriendlyName(txt); friendly != "" {
					add(srcIP, cleanFriendly(friendly))
				}
			}
		}
		off = rdata + rdlen
	}
	return hits
}

// txtStrings 拆开 TXT RDATA 里的 length-prefixed 字符串。
func txtStrings(rd []byte) []string {
	parts := []string{}
	off := 0
	for off < len(rd) {
		l := int(rd[off])
		off++
		if off+l > len(rd) {
			break
		}
		parts = append(parts, string(rd[off:off+l]))
		off += l
	}
	return parts
}

// txtFriendlyName 从 "key=value" 里取 fn= / md= 的值。
func txtFriendlyName(s string) string {
	if idx := strings.IndexByte(s, '='); idx >= 0 {
		key := strings.ToLower(s[:idx])
		if key == "fn" || key == "md" {
			return s[idx+1:]
		}
	}
	return ""
}

// ---------- 名称清洗 ----------

// looksRandom 过滤 UUID 型、长十六进制串、带长数字尾巴的临时名称。
func looksRandom(s string) bool {
	hex, total := 0, 0
	for _, r := range s {
		if r == '-' || r == ':' {
			continue
		}
		total++
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			hex++
		}
	}
	if total == 0 {
		return true
	}
	if total >= 24 && hex*5 >= total*4 { // 大段十六进制且占比高
		return true
	}
	run := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			run++
			if run >= 8 {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

// cleanHostname 清洗 ".local" 主机名：去尾点、去 .local、拒绝服务实例名/随机名。
func cleanHostname(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".")
	if len(s) >= 6 && strings.EqualFold(s[len(s)-6:], ".local") {
		s = s[:len(s)-6]
	}
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > 48 {
		return ""
	}
	if strings.ContainsAny(s, "_.") { // 服务实例名 / 域名片段，不是主机名
		return ""
	}
	if looksRandom(s) {
		return ""
	}
	return s
}

// cleanFriendly 清洗设备友好名（允许空格、下划线、点），只挡随机串和超长。
func cleanFriendly(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len([]rune(s)) > 48 {
		return ""
	}
	if looksRandom(s) {
		return ""
	}
	return s
}

// cleanLabel 最小清洗（DHCP 主机名、入站用户名等已由设备/配置给出的标识）：
// 去首尾空白、去尾点、去控制字符、限长。不做随机名过滤，交给调用方按需判断。
func cleanLabel(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, s)
	if s == "" || len([]rune(s)) > 64 {
		return ""
	}
	return s
}
