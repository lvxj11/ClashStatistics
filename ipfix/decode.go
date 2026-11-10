// Package ipfix 解码 NetFlow v9 / IPFIX(v10) 的模板与数据流记录。
// 它是纯协议解析，不依赖数据库与 Web 层，便于单独测试；采集与落库在 web 包。
package ipfix

import (
	"encoding/binary"
	"net"
)

// 热度条需要的 IE（IANA IPFIX Information Elements）。
// RouterOS Traffic Flow 实测导出的 IPv4 模板就是这 8 个字段。
const (
	ieFirstForwarded = 22 // flowStartSysUpTime：相对设备启动的毫秒
	ieLastForwarded  = 21 // flowEndSysUpTime
	ieSrcIPv4        = 8  // sourceIPv4Address：转换前的原地址，即 LAN 客户端 IP
	ieDstIPv4        = 12 // destinationIPv4Address
	ieSrcIPv6        = 27 // sourceIPv6Address
	ieDstIPv6        = 28 // destinationIPv6Address
)

// Field 是模板里的一个 Information Element。
type Field struct {
	IE     uint16
	Length uint16
}

// Flow 是一条解码后的流记录，只保留热度条需要的字段。
// FirstFwd / LastFwd 是相对设备启动的毫秒值，需结合 exportTime 反推绝对时间。
type Flow struct {
	SrcIP    net.IP
	DstIP    net.IP
	FirstFwd uint32
	LastFwd  uint32
	IsV6     bool
}

// Decoder 缓存模板。模板会周期性重发，同 id 覆盖即可；它不是 goroutine 安全的，
// 由单个 UDP 读取 goroutine 独占使用。
type Decoder struct {
	templates map[uint16][]Field
}

func NewDecoder() *Decoder {
	return &Decoder{templates: make(map[uint16][]Field)}
}

// Decode 解析一个 NetFlow v9 / IPFIX 报文，返回流记录与报文头的 Export Time
// （绝对 Unix 秒）。报文损坏或模板未就绪时安静返回已有结果，不影响采集循环。
func (d *Decoder) Decode(data []byte) ([]Flow, uint32, error) {
	if len(data) < 16 {
		return nil, 0, nil
	}

	version := binary.BigEndian.Uint16(data[0:2])
	var exportTime uint32
	var header int
	var declaredLen int

	switch version {
	case 10: // IPFIX: version, length, exportTime, sequence, observationDomain
		declaredLen = int(binary.BigEndian.Uint16(data[2:4]))
		exportTime = binary.BigEndian.Uint32(data[4:8])
		header = 16
	case 9: // NetFlow v9: version, count, sysUpTime, unixSecs, sequence, sourceID
		exportTime = binary.BigEndian.Uint32(data[8:12])
		header = 20
	default:
		return nil, 0, nil
	}
	if len(data) < header {
		return nil, 0, nil
	}

	end := len(data)
	if version == 10 && declaredLen >= header && declaredLen < end {
		end = declaredLen
	}

	flows := []Flow{}
	off := header
	for off+4 <= end {
		setID := binary.BigEndian.Uint16(data[off : off+2])
		setLen := int(binary.BigEndian.Uint16(data[off+2 : off+4]))
		if setLen < 4 || off+setLen > end {
			break
		}
		body := data[off+4 : off+setLen]
		off += setLen

		switch {
		case setID == 2: // Template Set
			d.parseTemplate(body)
		case setID == 3: // Options Template Set：热度条用不上
		case setID >= 256: // Data Set，setID 就是 templateID
			flows = append(flows, d.parseData(setID, body)...)
		}
	}
	return flows, exportTime, nil
}

func (d *Decoder) parseTemplate(body []byte) {
	off := 0
	for off+4 <= len(body) {
		tid := binary.BigEndian.Uint16(body[off : off+2])
		cnt := int(binary.BigEndian.Uint16(body[off+2 : off+4]))
		off += 4
		if tid == 0 || cnt == 0 {
			return
		}

		fields := make([]Field, 0, cnt)
		ok := true
		for i := 0; i < cnt; i++ {
			if off+4 > len(body) {
				ok = false
				break
			}
			ft := binary.BigEndian.Uint16(body[off : off+2])
			fl := binary.BigEndian.Uint16(body[off+2 : off+4])
			off += 4
			if ft&0x8000 != 0 { // enterprise bit：后面还有 4 字节企业号
				if off+4 > len(body) {
					ok = false
					break
				}
				off += 4
				ft &^= 0x8000
			}
			fields = append(fields, Field{IE: ft, Length: fl})
		}
		if !ok {
			return
		}
		d.templates[tid] = fields
	}
}

func (d *Decoder) parseData(tid uint16, body []byte) []Flow {
	tpl, ok := d.templates[tid]
	if !ok || len(tpl) == 0 {
		return nil
	}
	recLen := 0
	for _, f := range tpl {
		recLen += int(f.Length)
	}
	if recLen <= 0 {
		return nil
	}

	flows := []Flow{}
	off := 0
	for off+recLen <= len(body) {
		var f Flow
		hasSrc := false
		isV6 := false
		for _, field := range tpl {
			raw := body[off : off+int(field.Length)]
			off += int(field.Length)
			switch field.IE {
			case ieSrcIPv4:
				if field.Length == 4 {
					f.SrcIP = net.IPv4(raw[0], raw[1], raw[2], raw[3])
					hasSrc = true
				}
			case ieDstIPv4:
				if field.Length == 4 {
					f.DstIP = net.IPv4(raw[0], raw[1], raw[2], raw[3])
				}
			case ieSrcIPv6, ieDstIPv6:
				if field.Length == 16 {
					isV6 = true
				}
			case ieFirstForwarded:
				if field.Length == 4 {
					f.FirstFwd = binary.BigEndian.Uint32(raw)
				}
			case ieLastForwarded:
				if field.Length == 4 {
					f.LastFwd = binary.BigEndian.Uint32(raw)
				}
			}
		}
		f.IsV6 = isV6
		if hasSrc || isV6 {
			flows = append(flows, f)
		}
	}
	return flows
}
