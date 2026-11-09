package discovery

import (
	"encoding/binary"
	"net"
	"testing"
)

// buildDHCP 组装一条最小 DHCP 报文（固定头 + magic cookie + 选项）。
func buildDHCP(msgType byte, requestedIP net.IP, hostname string) []byte {
	buf := make([]byte, 240) // 236 固定头 + 4 cookie
	buf[236], buf[237], buf[238], buf[239] = 0x63, 0x82, 0x53, 0x63
	// option 53: DHCP Message Type
	buf = append(buf, 53, 1, msgType)
	// option 50: Requested-IP
	if requestedIP != nil {
		v4 := requestedIP.To4()
		buf = append(buf, 50, 4, v4[0], v4[1], v4[2], v4[3])
	}
	// option 12: Hostname
	if hostname != "" {
		buf = append(buf, 12, byte(len(hostname)))
		buf = append(buf, []byte(hostname)...)
	}
	buf = append(buf, 255) // End
	return buf
}

func TestParseDHCPRequest(t *testing.T) {
	pkt := buildDHCP(3, net.ParseIP("192.168.56.14"), "vivo-X300-Ultra-BD")
	ip, host, ok := parseDHCP(pkt, net.IPv4zero)
	if !ok {
		t.Fatalf("期望解析成功，实际失败")
	}
	if ip != "192.168.56.14" {
		t.Fatalf("IP 不符: %q", ip)
	}
	if host != "vivo-X300-Ultra-BD" {
		t.Fatalf("主机名不符: %q", host)
	}
}

func TestParseDHCPFallbackToSourceIP(t *testing.T) {
	// 无 option 50 时，用非 0.0.0.0 的源 IP
	pkt := buildDHCP(1, nil, "fnos")
	ip, host, ok := parseDHCP(pkt, net.ParseIP("192.168.56.77"))
	if !ok || ip != "192.168.56.77" || host != "fnos" {
		t.Fatalf("期望回退到源 IP, 得到 ip=%q host=%q ok=%v", ip, host, ok)
	}
}

func TestParseDHCPRejectServerReply(t *testing.T) {
	pkt := buildDHCP(5, net.ParseIP("192.168.56.14"), "server-supplied")
	if _, _, ok := parseDHCP(pkt, net.IPv4zero); ok {
		t.Fatalf("服务端 ACK 不应被当作客户端主机名")
	}
}

func TestParseDHCPBadCookie(t *testing.T) {
	pkt := buildDHCP(3, net.ParseIP("192.168.56.14"), "fnos")
	pkt[236] = 0
	if _, _, ok := parseDHCP(pkt, net.IPv4zero); ok {
		t.Fatalf("magic cookie 不符应拒绝")
	}
}

func TestParseDHCPUUIDHostnameRejected(t *testing.T) {
	pkt := buildDHCP(3, net.ParseIP("192.168.56.14"), "4edf920b-aa64-919c-33e5-608956d78741")
	if _, _, ok := parseDHCP(pkt, net.IPv4zero); ok {
		t.Fatalf("UUID 型主机名应被过滤")
	}
}

func encodeName(name string) []byte {
	var out []byte
	for _, label := range splitLabels(name) {
		out = append(out, byte(len(label)))
		out = append(out, []byte(label)...)
	}
	out = append(out, 0)
	return out
}

func splitLabels(name string) []string {
	name = trimDot(name)
	if name == "" {
		return nil
	}
	var res []string
	start := 0
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			res = append(res, name[start:i])
			start = i + 1
		}
	}
	res = append(res, name[start:])
	return res
}

func trimDot(s string) string {
	for len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

// buildMDNSResponse 构造一条含 1 条 A 记录 + 1 条 TXT 记录（owner 用压缩指针）的响应。
func buildMDNSResponse(host string, ip net.IP, friendly string) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[2:4], 0x8400) // 响应
	binary.BigEndian.PutUint16(buf[4:6], 0)      // QDCOUNT
	binary.BigEndian.PutUint16(buf[6:8], 1)      // ANCOUNT
	binary.BigEndian.PutUint16(buf[10:12], 1)    // ARCOUNT

	answerNameOff := len(buf)
	// A 记录
	buf = append(buf, encodeName(host)...)
	buf = appendU16(buf, 1) // TYPE A
	buf = appendU16(buf, 1) // CLASS IN
	buf = append(buf, 0, 0, 0, 120)
	v4 := ip.To4()
	buf = appendU16(buf, 4)
	buf = append(buf, v4[0], v4[1], v4[2], v4[3])

	// TXT 记录：owner 用指向 A 记录名称的压缩指针 0xC0 | answerNameOff
	buf = append(buf, 0xC0, byte(answerNameOff))
	buf = appendU16(buf, 16) // TYPE TXT
	buf = appendU16(buf, 1)
	buf = append(buf, 0, 0, 0, 120)
	txt := "fn=" + friendly
	// RDATA = 一个 length-prefixed 字符串：1 字节长度 + 内容
	buf = appendU16(buf, len(txt)+1) // RDLENGTH
	buf = append(buf, byte(len(txt)))
	buf = append(buf, []byte(txt)...)
	return buf
}

func appendU16(b []byte, v int) []byte {
	return append(b, byte(v>>8), byte(v))
}

func TestParseMDNSARecordAndFriendlyTXT(t *testing.T) {
	pkt := buildMDNSResponse("fnos.local", net.ParseIP("192.168.56.14"), "LSX II LT-064cbd")
	hits := parseMDNS(pkt, net.ParseIP("192.168.56.99"))

	byIP := map[string]string{}
	for _, h := range hits {
		byIP[h.ip] = h.name
	}
	if byIP["192.168.56.14"] != "fnos" {
		t.Fatalf("A 记录应得到 fnos→192.168.56.14，实际: %#v", hits)
	}
	if byIP["192.168.56.99"] != "LSX II LT-064cbd" {
		t.Fatalf("TXT 友好名应映射到源 IP，实际: %#v", hits)
	}
}

func TestReadNameCompressionPointer(t *testing.T) {
	// "fnos.local" 后跟一个指回它的指针
	msg := append([]byte{}, encodeName("fnos.local")...)
	msg = append(msg, 0xC0, 0x00)
	name, next, ok := readName(msg, len(encodeName("fnos.local")))
	if !ok || name != "fnos.local" || next != len(msg) {
		t.Fatalf("压缩指针解析失败: name=%q next=%d ok=%v", name, next, ok)
	}
}

func TestCleanHostnameAndLooksRandom(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"fnos.local", "fnos"},
		{"fnos.local.", "fnos"},
		{"NPI076C0B.local", "NPI076C0B"},
		{"vivo-X300-Ultra-BD.local", "vivo-X300-Ultra-BD"},
		{"4edf920b-aa64-919c-33e5-608956d78741.local", ""}, // UUID
		{"fnos-1794193988.local", ""},                      // 长数字临时名
		{"Foo._googlecast._tcp.local", ""},                 // 服务实例名
		{"", ""},
	}
	for _, c := range cases {
		if got := cleanHostname(c.in); got != c.want {
			t.Errorf("cleanHostname(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}
