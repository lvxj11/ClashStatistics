package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ConnectionInfo 表示 Clash 连接信息
type ConnectionInfo struct {
	DownloadSpeed float64 `json:"downloadSpeed"`
	UploadSpeed   float64 `json:"uploadSpeed"`
	Connections   []Connection `json:"connections"`
	DownloadTotal float64 `json:"downloadTotal"`
	UploadTotal   float64 `json:"uploadTotal"`
}

// Connection 表示单个连接
type Connection struct {
	ID                  string `json:"id"`
	Chain               []string `json:"chain"`
	Rule                string `json:"rule"`
	RulePayload         string `json:"rulePayload"`
	Download            int64 `json:"download"`
	Upload              int64 `json:"upload"`
	SourceIP            string `json:"srcIP"`
	SourcePort          string `json:"srcPort"`
	DestPort            string `json:"dstPort"`
	DestIP              string `json:"dstIP"`
	StartTime           string `json:"start"`
	ClosedTime          string `json:"closed,omitempty"` // 连接关闭时间
	Metadata            Metadata `json:"metadata"` // 保留原始结构用于解析
	// 从 Metadata 展开的字段（用于数据库存储）
	Network             string `json:"-"` // 不从 JSON 解析，而是从 Metadata 映射
	ConnectionType      string `json:"-"` // 避免与 Go 关键字冲突
	SourceIPAddr        string `json:"-"`
	DestinationIP       string `json:"-"`
	SourcePortNum       string `json:"-"`
	DestinationPort     string `json:"-"`
	Host                string `json:"-"`
	DNSMode             string `json:"-"`
	Uid                 int    `json:"-"`
	Process             string `json:"-"`
	ProcessPath         string `json:"-"`
	SpecialProxy        string `json:"-"`
	SpecialRules        string `json:"-"`
	RemoteDestination   string `json:"-"`
	DSCP                int    `json:"-"`
	SniffHost           string `json:"-"`
	InboundIP           string `json:"-"`
	InboundPort         string `json:"-"`
	InboundName         string `json:"-"`
	InboundUser         string `json:"-"`
	SourceGeoIP         interface{} `json:"-"` // 可能为 null
	DestinationGeoIP    interface{} `json:"-"` // 可能为 null
	SourceIPASN         string `json:"-"`
	DestinationIPASN    string `json:"-"`
}

// Metadata 包含连接元数据
type Metadata struct {
	Network string `json:"network"`
	Type    string `json:"type"`
	SourceIP string `json:"sourceIP"`
	DestIP   string `json:"destinationIP"`
	SourcePort string `json:"sourcePort"`
	DestPort string `json:"destinationPort"`
	Host    string `json:"host"`
	DNSMode string `json:"dnsMode"`
	Uid     int    `json:"uid"`
	Process string `json:"process"`
	ProcessPath string `json:"processPath"`
	SpecialProxy string `json:"specialProxy"`
	SpecialRules string `json:"specialRules"`
	RemoteDestination string `json:"remoteDestination"`
	DSCP      int    `json:"dscp"`
	SniffHost string `json:"sniffHost"`
	InboundIP string `json:"inboundIP"`
	InboundPort string `json:"inboundPort"`
	InboundName string `json:"inboundName"`
	InboundUser string `json:"inboundUser"`
	SourceGeoIP interface{} `json:"sourceGeoIP"` // 可能为 null
	DestinationGeoIP interface{} `json:"destinationGeoIP"` // 可能为 null
	SourceIPASN string `json:"sourceIPASN"`
	DestinationIPASN string `json:"destinationIPASN"`
}

// GetConnectionsFromClash 从 Clash API 获取连接信息
func GetConnectionsFromClash(clashHost, clashSecret string, clashInterval int) (*ConnectionInfo, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	url := fmt.Sprintf("http://%s/connections?interval=%d", clashHost, clashInterval)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 添加认证头
	if clashSecret != "" {
		req.Header.Set("Authorization", "Bearer "+clashSecret)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var connectionInfo ConnectionInfo
	err = json.Unmarshal(body, &connectionInfo)
	if err != nil {
		return nil, err
	}

	// 处理每个连接，将 metadata 字段映射到 Connection 结构体的顶层字段
	for i := range connectionInfo.Connections {
		conn := &connectionInfo.Connections[i]
		// 将 metadata 字段映射到顶层字段
		conn.Network = conn.Metadata.Network
		conn.ConnectionType = conn.Metadata.Type
		conn.SourceIPAddr = conn.Metadata.SourceIP
		conn.DestinationIP = conn.Metadata.DestIP
		conn.SourcePortNum = conn.Metadata.SourcePort
		conn.DestinationPort = conn.Metadata.DestPort
		conn.Host = conn.Metadata.Host
		conn.DNSMode = conn.Metadata.DNSMode
		conn.Uid = conn.Metadata.Uid
		conn.Process = conn.Metadata.Process
		conn.ProcessPath = conn.Metadata.ProcessPath
		conn.SpecialProxy = conn.Metadata.SpecialProxy
		conn.SpecialRules = conn.Metadata.SpecialRules
		conn.RemoteDestination = conn.Metadata.RemoteDestination
		conn.DSCP = conn.Metadata.DSCP
		conn.SniffHost = conn.Metadata.SniffHost
		conn.InboundIP = conn.Metadata.InboundIP
		conn.InboundPort = conn.Metadata.InboundPort
		conn.InboundName = conn.Metadata.InboundName
		conn.InboundUser = conn.Metadata.InboundUser
		conn.SourceGeoIP = conn.Metadata.SourceGeoIP
		conn.DestinationGeoIP = conn.Metadata.DestinationGeoIP
		conn.SourceIPASN = conn.Metadata.SourceIPASN
		conn.DestinationIPASN = conn.Metadata.DestinationIPASN

		// 如果 metadata 中的 sourceIP 为空，尝试使用连接本身的 srcIP 字段
		if conn.SourceIPAddr == "" {
			conn.SourceIPAddr = conn.SourceIP
		}
	}

	return &connectionInfo, nil
}