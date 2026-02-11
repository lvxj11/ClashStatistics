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

// LoadConfig 从 config.ini 加载配置，并支持环境变量覆盖
func LoadConfig() (*Config, error) {
	config := &Config{
		ClashHost:         "127.0.0.1:9090",
		ClashSecret:       "",
		ClashInterval:     1000,
		LogLevel:          "warn", // 默认日志级别为警告
		WebPort:           "8080", // 默认Web端口为8080
		DBCleanupDays:     7,       // 默认保留7天数据
		DBVacuumInterval:  24,      // 默认每24小时执行一次VACUUM
	}

	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return config, fmt.Errorf("创建数据目录失败: %v", err)
	}

	// 1. 检查是否存在有效的环境变量
	if hasValidEnvironmentVariables() {
		// 存在有效环境变量，直接使用环境变量配置
		GetLogger().Debug("检测到有效环境变量，使用环境变量配置\n")
	} else {
		// 2. 检查配置文件是否存在，不存在则生成默认配置
		configPath := "./data/config.ini"
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			// 配置文件不存在，生成默认配置文件
			err := GenerateDefaultConfig(config)
			if err != nil {
				return config, err
			}
			// 生成默认配置文件后返回错误，提示用户修改配置
			return config, fmt.Errorf("配置文件不存在，已生成默认配置文件 ./data/config.ini，请根据需要修改配置后重新启动程序")
		}

		// 3. 加载配置文件
		file, err := os.Open(configPath)
		if err == nil {
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
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])

					// 移除引号（如果存在）
					value = strings.Trim(value, "\"'")

					switch currentSection {
					case "clash":
						switch strings.ToLower(key) {
						case "host":
							config.ClashHost = value
						case "secret":
							config.ClashSecret = value
						case "interval":
							intVal, err := strconv.Atoi(value)
							if err == nil {
								config.ClashInterval = intVal
							}
						}
					case "server":
						switch strings.ToLower(key) {
						case "log_level":
							config.LogLevel = value
						case "web_port":
							config.WebPort = value
						case "db_cleanup_days":
							if days, err := strconv.Atoi(value); err == nil {
								config.DBCleanupDays = days
							}
						case "db_vacuum_interval":
							if interval, err := strconv.Atoi(value); err == nil {
								config.DBVacuumInterval = interval
							}
						}
					}

				}
			}
		}
	}

	// 4. 环境变量覆盖配置（最高优先级）
	if host := os.Getenv("CLASH_HOST"); host != "" {
		config.ClashHost = host
		GetLogger().Debug("使用环境变量覆盖 ClashHost: %s\n", host)
	}
	if secret := os.Getenv("CLASH_SECRET"); secret != "" {
		config.ClashSecret = secret
		GetLogger().Debug("使用环境变量覆盖 ClashSecret\n")
	}
	if interval := os.Getenv("CLASH_INTERVAL"); interval != "" {
		if intVal, err := strconv.Atoi(interval); err == nil {
			config.ClashInterval = intVal
			GetLogger().Debug("使用环境变量覆盖 ClashInterval: %d\n", intVal)
		}
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.LogLevel = logLevel
		GetLogger().Debug("使用环境变量覆盖 LogLevel: %s\n", logLevel)
	}
	if webPort := os.Getenv("WEB_PORT"); webPort != "" {
		config.WebPort = webPort
		GetLogger().Debug("使用环境变量覆盖 WebPort: %s\n", webPort)
	}
	if cleanupDays := os.Getenv("DB_CLEANUP_DAYS"); cleanupDays != "" {
		if days, err := strconv.Atoi(cleanupDays); err == nil {
			config.DBCleanupDays = days
			GetLogger().Debug("使用环境变量覆盖 DBCleanupDays: %d\n", days)
		}
	}
	if vacuumInterval := os.Getenv("DB_VACUUM_INTERVAL"); vacuumInterval != "" {
		if interval, err := strconv.Atoi(vacuumInterval); err == nil {
			config.DBVacuumInterval = interval
			GetLogger().Debug("使用环境变量覆盖 DBVacuumInterval: %d\n", interval)
		}
	}

	return config, nil
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
