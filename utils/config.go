package utils

import (
	"fmt"
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
}

// hasValidEnvironmentVariables 检查是否存在有效的环境变量
func hasValidEnvironmentVariables() bool {
	// 检查关键环境变量
	if os.Getenv("CLASH_HOST") != "" {
		return true
	}
	if os.Getenv("CLASH_SECRET") != "" {
		return true
	}
	if os.Getenv("CLASH_INTERVAL") != "" {
		return true
	}
	if os.Getenv("LOG_LEVEL") != "" {
		return true
	}
	if os.Getenv("WEB_PORT") != "" {
		return true
	}
	if os.Getenv("DB_CLEANUP_DAYS") != "" {
		return true
	}
	if os.Getenv("DB_VACUUM_INTERVAL") != "" {
		return true
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
		DBCleanupDays:    7,      // 默认保留7天数据
		DBVacuumInterval: 24,     // 默认每24小时执行一次VACUUM
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
	content := `# ClashStatistics 配置文件（TOML 格式）
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
db_cleanup_days = 7
# 数据库 VACUUM 执行间隔（小时）
db_vacuum_interval = 24
`

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
