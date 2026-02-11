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
	ClashHost     string
	ClashSecret   string
	ClashInterval int
	LogLevel      string // 日志级别：debug, info, warn, error
	WebPort       string // Web服务器端口号
}

// LoadConfig 从 config.ini 加载配置
func LoadConfig() (*Config, error) {
	config := &Config{
		ClashHost:     "127.0.0.1:9090",
		ClashSecret:   "",
		ClashInterval: 1000,
		LogLevel:      "warn", // 默认日志级别为警告
		WebPort:       "8080", // 默认Web端口为8080
	}

	// 检查配置文件是否存在
	if _, err := os.Stat("config.ini"); os.IsNotExist(err) {
		// 配置文件不存在，生成默认配置文件
		err := GenerateDefaultConfig(config)
		if err != nil {
			return config, err
		}
		// 生成默认配置文件后返回错误，提示用户修改配置
		return config, fmt.Errorf("配置文件不存在，已生成默认配置文件 config.ini，请根据需要修改配置后重新启动程序")
	}

	file, err := os.Open("config.ini")
	if err != nil {
		return config, err
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
				}
			}

		}
	}

	return config, scanner.Err()
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
`

	err := os.WriteFile("config.ini", []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("生成默认配置文件失败: %v", err)
	}

	GetLogger().Println("已生成默认配置文件 config.ini，请根据需要修改配置后重新启动程序")
	return nil
}
