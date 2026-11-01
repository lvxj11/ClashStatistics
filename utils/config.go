package utils

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
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
// 优先级：环境变量 > config.ini > 默认值（逐项覆盖，而非整体二选一）
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

	configPath := "./data/config.ini"
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
			return config, fmt.Errorf("配置文件不存在，已生成默认配置文件 ./data/config.ini，请根据需要修改配置后重新启动程序")
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

// configItem 描述一个配置项在配置文件里的位置、对应的环境变量，以及怎么赋值。
// 文件解析与环境变量覆盖共用这张表，避免"加了配置项只改一处"的漏改
type configItem struct {
	Section string
	Key     string
	Env     string
	Apply   func(*Config, string) // 统一用字符串传入，数值项在这里自己转换
}

var configItems = []configItem{
	{"clash", "host", "CLASH_HOST", func(c *Config, v string) { c.ClashHost = v }},
	{"clash", "secret", "CLASH_SECRET", func(c *Config, v string) { c.ClashSecret = v }},
	{"clash", "interval", "CLASH_INTERVAL", func(c *Config, v string) {
		if n, err := strconv.Atoi(v); err == nil {
			c.ClashInterval = n
		}
	}},
	{"server", "log_level", "LOG_LEVEL", func(c *Config, v string) { c.LogLevel = v }},
	{"server", "web_port", "WEB_PORT", func(c *Config, v string) { c.WebPort = v }},
	{"server", "db_cleanup_days", "DB_CLEANUP_DAYS", func(c *Config, v string) {
		if n, err := strconv.Atoi(v); err == nil {
			c.DBCleanupDays = n
		}
	}},
	{"server", "db_vacuum_interval", "DB_VACUUM_INTERVAL", func(c *Config, v string) {
		if n, err := strconv.Atoi(v); err == nil {
			c.DBVacuumInterval = n
		}
	}},
}

// loadConfigFile 从指定路径读取配置文件，覆盖已有配置项
func loadConfigFile(config *Config, configPath string) error {
	file, err := os.Open(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentSection string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// 处理节名 [clash]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}

		// 处理键值对
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		// 移除引号（如果存在）
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")

		for _, item := range configItems {
			if item.Section == currentSection && item.Key == key {
				item.Apply(config, value)
				break
			}
		}
	}

	return scanner.Err()
}

// applyEnvOverrides 用环境变量逐项覆盖配置
func applyEnvOverrides(config *Config) {
	for _, item := range configItems {
		value := os.Getenv(item.Env)
		if value == "" {
			continue
		}
		item.Apply(config, value)
		GetLogger().Debug("使用环境变量覆盖 %s.%s: %s\n", item.Section, item.Key, value)
	}
}

// GenerateDefaultConfig 生成默认配置文件
func GenerateDefaultConfig(config *Config) error {
	content := `# ClashStatistics 配置文件
# 请根据实际情况修改以下配置

[clash]
# Clash API 地址和端口
host = 127.0.0.1:9090
# Clash API 密钥（如果设置了的话）
secret = 
# 数据获取间隔（毫秒）
interval = 1000

[server]
# 日志级别：debug, info, warn, error
log_level = warn
# Web 服务器端口号
web_port = 8080
# 数据库数据保留天数
db_cleanup_days = 7
# 数据库VACUUM执行间隔（小时）
db_vacuum_interval = 24
`

	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}

	err := os.WriteFile("./data/config.ini", []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("生成默认配置文件失败: %v", err)
	}

	GetLogger().Println("已生成默认配置文件 ./data/config.ini，请根据需要修改配置后重新启动程序")
	return nil
}
