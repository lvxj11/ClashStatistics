package utils

import (
	"fmt"
	"net"
	"os"
	"strconv"

	toml "github.com/pelletier/go-toml/v2"
)

// Config 只存系统级配置。数据源定义（Clash / IPFIX 地址、端口、凭据等）不在
// 这里，运行时只认 data_sources 表；config.toml / 环境变量都不再参与数据源配置。
type Config struct {
	LogLevel         string // 日志级别：debug, info, warn, error
	WebPort          string // Web服务器端口号
	DBCleanupDays    int    // 数据库数据保留天数
	DBVacuumInterval int    // 数据库VACUUM执行间隔（小时）
}

// DefaultLANCIDR 取本机第一个非回环 IPv4 所在的 /24 作为默认 LAN 网段。
// 这不是配置源：只在 /data-sources 快捷添加 IPFIX 源时，作为创建表单里可改的预填建议。
// 多数家用网络就是这个形态；不是 /24 时应在管理页显式配置。
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

// LoadConfig 加载系统级配置。
// 优先级：环境变量 > config.toml > 默认值（逐项覆盖，而非整体二选一）。
// 首次启动生成默认 config.toml 后继续启动、不退出，让全新部署能看到空表引导。
func LoadConfig() (*Config, error) {
	config := &Config{
		LogLevel:         "warn", // 默认日志级别为警告
		WebPort:          "8080", // 默认Web端口为8080
		DBCleanupDays:    30,     // 默认保留30天数据
		DBVacuumInterval: 24,     // 默认每24小时执行一次VACUUM
	}

	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return config, fmt.Errorf("创建数据目录失败: %v", err)
	}

	configPath := "./data/config.toml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 首次启动：生成一份只含 [server] 的默认配置，然后继续启动（不退出）。
		// 生成失败（例如挂载卷权限不足）不算致命：仍可用环境变量 / 默认值运行。
		if err := GenerateDefaultConfig(); err != nil {
			GetLogger().Warn("生成默认配置文件失败，已忽略，继续以环境变量和默认值运行: %v\n", err)
		}
	} else {
		if err := loadConfigFile(config, configPath); err != nil {
			return config, err
		}
	}

	// 环境变量覆盖配置（最高优先级，逐项覆盖）
	applyEnvOverrides(config)

	return config, nil
}

// configItem 描述一个系统配置项：对应的环境变量、显示用的名字，以及怎么赋值。
// 环境变量覆盖与默认值共用这张表，新增配置项只改一处。
// CLASH_SECRET 不在这里：它不是系统配置，而是数据源 credential_ref 指向的可选
// 凭据环境变量，由 web 层在解析数据源时按需 os.Getenv 读取。
type configItem struct {
	Env   string
	Name  string                // 仅用于日志显示，如 server.web_port
	Apply func(*Config, string) // 统一用字符串传入，数值项在这里自己转换
}

var configItems = []configItem{
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
}

// configFile 对应 config.toml 的结构，只含 [server] 系统项。
type configFile struct {
	Server struct {
		LogLevel         string `toml:"log_level"`
		WebPort          string `toml:"web_port"`
		DBCleanupDays    int    `toml:"db_cleanup_days"`
		DBVacuumInterval int    `toml:"db_vacuum_interval"`
	} `toml:"server"`
}

// loadConfigFile 读取 config.toml，覆盖已有配置项。
// 只覆盖文件里真正写了的项，没写的保持默认值 —— 配置可以只写要改的那几项。
func loadConfigFile(config *Config, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var f configFile
	if err := toml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("解析配置文件 %s 失败: %v", configPath, err)
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

// GenerateDefaultConfig 生成只含 [server] 的默认 config.toml。
// 数据源不写进配置文件：全新部署初始 0 数据源，由用户在 /data-sources 页添加。
func GenerateDefaultConfig() error {
	content := `# ClashStatistics 配置文件（TOML 格式）
# 这里只放系统级配置；数据源（Clash / IPFIX 地址、端口、凭据等）请在网页
# /data-sources 页添加与管理，本文件不参与数据源配置。

[server]
# 日志级别：debug, info, warn, error
log_level = "warn"
# Web 服务器端口号
web_port = "8080"
# 数据库数据保留天数
db_cleanup_days = 30
# 数据库 VACUUM 执行间隔（小时）
db_vacuum_interval = 24
`

	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}

	if err := os.WriteFile("./data/config.toml", []byte(content), 0644); err != nil {
		return fmt.Errorf("生成默认配置文件失败: %v", err)
	}

	GetLogger().Println("已生成默认配置文件 ./data/config.toml（仅系统项）；数据源请在 /data-sources 页添加")
	return nil
}
