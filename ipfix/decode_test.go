package ipfix

import (
	"encoding/binary"
	"testing"
)

func putU16(b []byte, v uint16) []byte {
	var tmp [2]byte
	binary.BigEndian.PutUint16(tmp[:], v)
	return append(b, tmp[:]...)
}

func putU32(b []byte, v uint32) []byte {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	return append(b, tmp[:]...)
}

// templateSet 构造与 RouterOS 实测一致的 IPv4 模板（8 字段 / 22B）。
func templateSet() []byte {
	fields := []struct{ ie, length uint16 }{
		{60, 1}, {22, 4}, {21, 4}, {7, 2}, {11, 2}, {4, 1}, {8, 4}, {12, 4},
	}
	body := putU16(nil, 258)
	body = putU16(body, uint16(len(fields)))
	for _, f := range fields {
		body = putU16(body, f.ie)
		body = putU16(body, f.length)
	}
	set := putU16(nil, 2)
	set = putU16(set, uint16(4+len(body)))
	return append(set, body...)
}

// dataSet 构造一条 22B 的 IPv4 流记录。
func dataSet() []byte {
	rec := []byte{4}
	rec = putU32(rec, 1000)
	rec = putU32(rec, 2000)
	rec = putU16(rec, 12345)
	rec = putU16(rec, 443)
	rec = append(rec, 6)
	rec = append(rec, 192, 168, 56, 33)
	rec = append(rec, 8, 8, 8, 8)

	set := putU16(nil, 258)
	set = putU16(set, uint16(4+len(rec)))
	return append(set, rec...)
}

func datagram(parts ...[]byte) []byte {
	body := []byte{}
	for _, p := range parts {
		body = append(body, p...)
	}
	header := make([]byte, 16)
	binary.BigEndian.PutUint16(header[0:2], 10)
	binary.BigEndian.PutUint16(header[2:4], uint16(16+len(body)))
	binary.BigEndian.PutUint32(header[4:8], 1731200000)
	return append(header, body...)
}

func TestDecodeIPv4(t *testing.T) {
	d := NewDecoder()
	flows, exportTime, err := d.Decode(datagram(templateSet(), dataSet()))
	if err != nil {
		t.Fatal(err)
	}
	if exportTime != 1731200000 {
		t.Fatalf("exportTime = %d", exportTime)
	}
	if len(flows) != 1 {
		t.Fatalf("应解出 1 条流, got %d", len(flows))
	}
	f := flows[0]
	if f.SrcIP.String() != "192.168.56.33" || f.DstIP.String() != "8.8.8.8" {
		t.Fatalf("地址解析错误: %#v", f)
	}
	if f.FirstFwd != 1000 || f.LastFwd != 2000 {
		t.Fatalf("相对时间戳解析错误: %#v", f)
	}
	if f.IsV6 {
		t.Fatalf("IPv4 记录不应标记 IsV6")
	}
	if f.SrcPort != 12345 || f.DstPort != 443 || f.Protocol != 6 {
		t.Fatalf("五元组端口/协议解析错误: %#v", f)
	}
}

// TestTemplatePersists 校验模板跨报文缓存：先收到模板、再收到纯数据报文也能解码。
func TestTemplatePersists(t *testing.T) {
	d := NewDecoder()
	if _, _, err := d.Decode(datagram(templateSet())); err != nil {
		t.Fatal(err)
	}
	flows, _, err := d.Decode(datagram(dataSet()))
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SrcIP.String() != "192.168.56.33" {
		t.Fatalf("模板未跨报文生效: %#v", flows)
	}
}

// TestDecodeWithoutTemplate 校验模板未就绪时不崩、安静跳过。
func TestDecodeWithoutTemplate(t *testing.T) {
	d := NewDecoder()
	flows, _, err := d.Decode(datagram(dataSet()))
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 0 {
		t.Fatalf("无模板时不应解出流: %#v", flows)
	}
}

// TestDecodeTruncated 校验损坏/截断报文不 panic。
func TestDecodeTruncated(t *testing.T) {
	d := NewDecoder()
	full := datagram(templateSet(), dataSet())
	for cut := 0; cut < len(full); cut += 7 {
		if _, _, err := d.Decode(full[:cut]); err != nil {
			t.Fatalf("截断到 %d 字节时报错: %v", cut, err)
		}
	}
}
