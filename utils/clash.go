package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ConnectionInfo 表示 Clash 连接信息
type ConnectionInfo struct {
	DownloadSpeed float64      `json:"downloadSpeed"`
	UploadSpeed   float64      `json:"uploadSpeed"`
	Connections   []Connection `json:"connections"`
	DownloadTotal float64      `json:"downloadTotal"`
	UploadTotal   float64      `json:"uploadTotal"`
	Timestamp     time.Time    `json:"-"` // 时间戳
}

// Connection 表示单个连接
type Connection struct {
	ID          string   `json:"id"`
	Chain       []string `json:"chain"`
	Rule        string   `json:"rule"`
	RulePayload string   `json:"rulePayload"`
	Download    int64    `json:"download"`
	Upload      int64    `json:"upload"`
	SourceIP    string   `json:"srcIP"`
	SourcePort  string   `json:"srcPort"`
	DestPort    string   `json:"dstPort"`
	DestIP      string   `json:"dstIP"`
	StartTime   string   `json:"start"`
	Metadata    Metadata `json:"metadata"` // 保留原始结构用于解析
	// 从 Metadata 展开的字段（用于数据库存储）
	Network           string      `json:"-"` // 不从 JSON 解析，而是从 Metadata 映射
	ConnectionType    string      `json:"-"` // 避免与 Go 关键字冲突
	SourceIPAddr      string      `json:"-"`
	DestinationIP     string      `json:"-"`
	SourcePortNum     string      `json:"-"`
	DestinationPort   string      `json:"-"`
	Host              string      `json:"-"`
	DNSMode           string      `json:"-"`
	Uid               int         `json:"-"`
	Process           string      `json:"-"`
	ProcessPath       string      `json:"-"`
	SpecialProxy      string      `json:"-"`
	SpecialRules      string      `json:"-"`
	RemoteDestination string      `json:"-"`
	DSCP              int         `json:"-"`
	SniffHost         string      `json:"-"`
	InboundIP         string      `json:"-"`
	InboundPort       string      `json:"-"`
	InboundName       string      `json:"-"`
	InboundUser       string      `json:"-"`
	SourceGeoIP       interface{} `json:"-"` // 可能为 null
	DestinationGeoIP  interface{} `json:"-"` // 可能为 null
	SourceIPASN       string      `json:"-"`
	DestinationIPASN  string      `json:"-"`
}

// Metadata 包含连接元数据
type Metadata struct {
	Network           string      `json:"network"`
	Type              string      `json:"type"`
	SourceIP          string      `json:"sourceIP"`
	DestIP            string      `json:"destinationIP"`
	SourcePort        string      `json:"sourcePort"`
	DestPort          string      `json:"destinationPort"`
	Host              string      `json:"host"`
	DNSMode           string      `json:"dnsMode"`
	Uid               int         `json:"uid"`
	Process           string      `json:"process"`
	ProcessPath       string      `json:"processPath"`
	SpecialProxy      string      `json:"specialProxy"`
	SpecialRules      string      `json:"specialRules"`
	RemoteDestination string      `json:"remoteDestination"`
	DSCP              int         `json:"dscp"`
	SniffHost         string      `json:"sniffHost"`
	InboundIP         string      `json:"inboundIP"`
	InboundPort       string      `json:"inboundPort"`
	InboundName       string      `json:"inboundName"`
	InboundUser       string      `json:"inboundUser"`
	SourceGeoIP       interface{} `json:"sourceGeoIP"`      // 可能为 null
	DestinationGeoIP  interface{} `json:"destinationGeoIP"` // 可能为 null
	SourceIPASN       string      `json:"sourceIPASN"`
	DestinationIPASN  string      `json:"destinationIPASN"`
}

// GetConnectionsFromClash 从 Clash API 获取连接信息
func GetConnectionsFromClash(clashHost, clashSecret string, clashInterval int) (*ConnectionInfo, error) {
	GetLogger().Debug("调用 Clash API 获取连接信息\n")

	// 重试配置
	maxRetries := 3
	retryDelay := 1 * time.Second

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		// 创建带超时的HTTP客户端
		client := &http.Client{
			Timeout: time.Duration(clashInterval/2) * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     30 * time.Second,
			},
		}

		url := fmt.Sprintf("http://%s/connections?interval=%d", clashHost, clashInterval)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lastErr = err
			GetLogger().Warn("创建请求失败 (尝试 %d/%d): %v\n", i+1, maxRetries, err)
			if i < maxRetries-1 {
				time.Sleep(retryDelay)
				retryDelay *= 2 // 指数退避
				continue
			}
			return nil, lastErr
		}

		// 添加认证头
		if clashSecret != "" {
			req.Header.Set("Authorization", "Bearer "+clashSecret)
		}

		// 添加请求头
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "ClashStatistics")

		GetLogger().Debug("发送请求到 Clash API: %s\n", url)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			GetLogger().Warn("请求失败 (尝试 %d/%d): %v\n", i+1, maxRetries, err)
			if i < maxRetries-1 {
				time.Sleep(retryDelay)
				retryDelay *= 2 // 指数退避
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
			GetLogger().Warn("请求失败 (尝试 %d/%d), 状态码: %d\n", i+1, maxRetries, resp.StatusCode)
			resp.Body.Close()
			if i < maxRetries-1 {
				time.Sleep(retryDelay)
				retryDelay *= 2 // 指数退避
				continue
			}
			return nil, lastErr
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			GetLogger().Warn("读取响应失败 (尝试 %d/%d): %v\n", i+1, maxRetries, err)
			if i < maxRetries-1 {
				time.Sleep(retryDelay)
				retryDelay *= 2 // 指数退避
				continue
			}
			return nil, lastErr
		}

		var connectionInfo ConnectionInfo
		err = json.Unmarshal(body, &connectionInfo)
		if err != nil {
			lastErr = err
			GetLogger().Warn("解析响应失败 (尝试 %d/%d): %v\n", i+1, maxRetries, err)
			if i < maxRetries-1 {
				time.Sleep(retryDelay)
				retryDelay *= 2 // 指数退避
				continue
			}
			return nil, lastErr
		}

		// 设置时间戳
		connectionInfo.Timestamp = time.Now()

		// 处理每个连接，将 metadata 字段映射到 Connection 结构体的顶层字段
		var wg sync.WaitGroup

		wg.Add(len(connectionInfo.Connections))
		for i := range connectionInfo.Connections {
			go func(idx int) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						GetLogger().Error("处理连接时发生 panic: %v\n", r)
					}
				}()

				conn := &connectionInfo.Connections[idx]
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
			}(i)
		}
		wg.Wait()

		return &connectionInfo, nil
	}

	return nil, fmt.Errorf("所有尝试均失败，最后错误: %v", lastErr)
}
