package utils

import (
	"fmt"
	"net"
	"os"
	"strconv"

	toml "github.com/pelletier/go-toml/v2"
)

// Config 存储配置信息
type Config struct {
	ClashHost        string
	ClashSecret      string
	ClashInterval    int
	LogLevel         string // 日志级别：debug, info, warn, error
	WebPort          string // Web服务器端口号
	DBCleanupDays    int    // 数据库数据保留天数
	DBVacuumInterval int    // 数据库VACUUM执行间隔（小时）

	// IPFIX 采集器的 bootstrap 默认值。数据源运行时的真实配置在 data_sources 表，
	// 这里只用于首次启动（表为空）时写入内置 IPFIX 源，以及兼容直接用 env 部署。
	IPFIXPort    int    // UDP 监听端口
	IPFIXLAN     string // LAN 网段（逗号分隔），为空时自动推断本机 /24
	IPFIXExclude string // 排除 IP（逗号分隔），通常填网关
}

// DefaultLANCIDR 取本机第一个非回环 IPv4 所在的 /24 作为默认 LAN 网段。
// 多数家用网络就是这个形态；不是 /24 时应在管理页或 IPFIX_LAN 显式配置。
func DefaultLANCIDR() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		return fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2])
	}
	return ""
}

// hasValidEnvironmentVariables 检查是否存在有效的环境变量
func hasValidEnvironmentVariables() bool {
	for _, key := range []string{
		"CLASH_HOST", "CLASH_SECRET", "CLASH_INTERVAL", "LOG_LEVEL", "WEB_PORT",
		"DB_CLEANUP_DAYS", "DB_VACUUM_INTERVAL",
		"IPFIX_PORT", "IPFIX_LAN", "IPFIX_EXCLUDE_IPS",
	} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

// LoadConfig 加载配置
// 优先级：环境变量 > config.toml > 默认值（逐项覆盖，而非整体二选一）
func LoadConfig() (*Config, error) {
	config := &Config{
		ClashHost:        "127.0.0.1:9090",
		ClashSecret:      "",
		ClashInterval:    1000,
		LogLevel:         "warn", // 默认日志级别为警告
		WebPort:          "8080", // 默认Web端口为8080
		DBCleanupDays:    30,     // 默认保留30天数据
		DBVacuumInterval: 24,     // 默认每24小时执行一次VACUUM
		IPFIXPort:        2055,
		IPFIXLAN:         DefaultLANCIDR(),
		IPFIXExclude:     "",
	}

	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return config, fmt.Errorf("创建数据目录失败: %v", err)
	}

	configPath := "./data/config.toml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 配置文件不存在，尝试生成默认配置文件
		if err := GenerateDefaultConfig(config); err != nil {
			// 生成失败（例如数据目录不可写、容器挂载卷权限不足）不算致命错误：
			// 有环境变量时可以纯靠环境变量运行，没有时才需要把问题暴露给用户
			if hasValidEnvironmentVariables() {
				GetLogger().Warn("生成默认配置文件失败，已忽略，继续以环境变量和默认值运行: %v\n", err)
			} else {
				return config, err
			}
		} else if !hasValidEnvironmentVariables() {
			// 裸机首次运行：提示用户修改配置后重启
			return config, fmt.Errorf("配置文件不存在，已生成默认配置文件 ./data/config.toml，请根据需要修改配置后重新启动程序")
		}
	} else {
		// 加载配置文件
		if err := loadConfigFile(config, configPath); err != nil {
			return config, err
		}
	}

	// 环境变量覆盖配置（最高优先级，逐项覆盖）
	applyEnvOverrides(config)

	return config, nil
}

// configItem 描述一个配置项：对应的环境变量、显示用的名字，以及怎么赋值。
// 环境变量覆盖与默认值共用这张表，新增配置项只改一处
type configItem struct {
	Env   string
	Name  string                // 仅用于日志显示，如 clash.host
	Apply func(*Config, string) // 统一用字符串传入，数值项在这里自己转换
}

var configItems = []configItem{
	{"CLASH_HOST", "clash.host", func(c *Config, v string) { c.ClashHost = v }},
	{"CLASH_SECRET", "clash.secret", func(c *Config, v string) { c.ClashSecret = v }},
	{"CLASH_INTERVAL", "clash.interval", func(c *Config, v string) {
		if n, err := strconv.Atoi(v); err == nil {
			c.ClashInterval = n
		}
	}},
	{"LOG_LEVEL", "server.log_level", func(c *Config, v string) { c.LogLevel = v }},
	{"WEB_PORT", "server.web_port", func(c *Config, v string) { c.WebPort = v }},
	{"DB_CLEANUP_DAYS", "server.db_cleanup_days", func(c *Config, v string) {
		if n, err := strconv.Atoi(v); err == nil {
			c.DBCleanupDays = n
		}
	}},
	{"DB_VACUUM_INTERVAL", "server.db_vacuum_interval", func(c *Config, v string) {
		if n, err := strconv.Atoi(v); err == nil {
			c.DBVacuumInterval = n
		}
	}},
	{"IPFIX_PORT", "ipfix.port", func(c *Config, v string) {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.IPFIXPort = n
		}
	}},
	{"IPFIX_LAN", "ipfix.lan", func(c *Config, v string) { c.IPFIXLAN = v }},
	{"IPFIX_EXCLUDE_IPS", "ipfix.exclude_ips", func(c *Config, v string) { c.IPFIXExclude = v }},
}

// configFile 对应 config.toml 的结构
// 只在反序列化时使用，运行时仍用扁平的 Config，避免改动所有调用方
type configFile struct {
	Clash struct {
		Host     string `toml:"host"`
		Secret   string `toml:"secret"`
		Interval int    `toml:"interval"`
	} `toml:"clash"`
	Server struct {
		LogLevel         string `toml:"log_level"`
		WebPort          string `toml:"web_port"`
		DBCleanupDays    int    `toml:"db_cleanup_days"`
		DBVacuumInterval int    `toml:"db_vacuum_interval"`
	} `toml:"server"`
	IPFIX struct {
		Port       int    `toml:"port"`
		LAN        string `toml:"lan"`
		ExcludeIPs string `toml:"exclude_ips"`
	} `toml:"ipfix"`
}

// loadConfigFile 读取 config.toml，覆盖已有配置项
// 只覆盖文件里真正写了的项，没写的保持默认值 —— 配置可以只写要改的那几项
func loadConfigFile(config *Config, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var f configFile
	if err := toml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("解析配置文件 %s 失败: %v", configPath, err)
	}

	if f.Clash.Host != "" {
		config.ClashHost = f.Clash.Host
	}
	if f.Clash.Secret != "" {
		config.ClashSecret = f.Clash.Secret
	}
	if f.Clash.Interval > 0 {
		config.ClashInterval = f.Clash.Interval
	}
	if f.Server.LogLevel != "" {
		config.LogLevel = f.Server.LogLevel
	}
	if f.Server.WebPort != "" {
		config.WebPort = f.Server.WebPort
	}
	if f.Server.DBCleanupDays > 0 {
		config.DBCleanupDays = f.Server.DBCleanupDays
	}
	if f.Server.DBVacuumInterval > 0 {
		config.DBVacuumInterval = f.Server.DBVacuumInterval
	}
	if f.IPFIX.Port > 0 {
		config.IPFIXPort = f.IPFIX.Port
	}
	if f.IPFIX.LAN != "" {
		config.IPFIXLAN = f.IPFIX.LAN
	}
	if f.IPFIX.ExcludeIPs != "" {
		config.IPFIXExclude = f.IPFIX.ExcludeIPs
	}

	return nil
}

// applyEnvOverrides 用环境变量逐项覆盖配置（最高优先级）
func applyEnvOverrides(config *Config) {
	for _, item := range configItems {
		value := os.Getenv(item.Env)
		if value == "" {
			continue
		}
		item.Apply(config, value)
		GetLogger().Debug("使用环境变量覆盖 %s: %s\n", item.Name, value)
	}
}

// GenerateDefaultConfig 生成默认的 config.toml
func GenerateDefaultConfig(config *Config) error {
	content := fmt.Sprintf(`# ClashStatistics 配置文件（TOML 格式）
# 请根据实际情况修改以下配置

[clash]
# Clash / Mihomo 外部控制器的地址与端口
host = "127.0.0.1:9090"
# 外部控制器密钥（没有设置就留空）
secret = ""
# 数据获取间隔（毫秒）
interval = 1000

[server]
# 日志级别：debug, info, warn, error
log_level = "warn"
# Web 服务器端口号
web_port = "8080"
# 数据库数据保留天数
db_cleanup_days = 30
# 数据库 VACUUM 执行间隔（小时）
db_vacuum_interval = 24

[ipfix]
# 路由器流量推送（Traffic Flow / IPFIX）的 UDP 监听端口
port = 2055
# LAN 网段（逗号分隔）；留空时按本机 IPv4 自动推断 /24
lan = %q
# 排除 IP（逗号分隔），通常填网关，避免网关自身流量算成一个客户端
exclude_ips = ""
`, config.IPFIXLAN)

	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}

	err := os.WriteFile("./data/config.toml", []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("生成默认配置文件失败: %v", err)
	}

	GetLogger().Println("已生成默认配置文件 ./data/config.toml，请根据需要修改配置后重新启动程序")
	return nil
}
