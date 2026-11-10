package main

import (
	"flag"
	"fmt"

	"clash-statistics/database"
	"clash-statistics/utils"
	"clash-statistics/web"
)

func main() {
	showVersion := flag.Bool("v", false, "输出版本号")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Clash Statistics Dashboard v%s\n", utils.Version)
		return
	}

	// 加载系统级配置（日志、端口、保留天数等）。首次启动会生成默认 config.toml
	// 但不退出、直接继续启动，全新部署才能看到「暂无数据源」的空表引导。
	config, err := utils.LoadConfig()
	if err != nil {
		// 这里直接使用 fmt 输出错误信息，因为日志系统还未初始化
		fmt.Printf("加载配置失败: %v\n", err)
		return
	}

	// 使用配置中的日志级别初始化日志系统
	if err := utils.InitLoggerWithLevel(config.LogLevel); err != nil {
		fmt.Printf("初始化日志系统失败: %v\n", err)
		return
	}
	defer utils.GetLogger().Close()

	log := utils.GetLogger()

	// 输出系统级启动信息，同时显示在控制台和日志文件中。
	// 数据源定义只认 data_sources 表：全新部署初始为 0 源，不做任何播种；
	// 下面的启动日志只打印表里实际生效的数据源。
	log.Startup("Log Level: %s\n", config.LogLevel)
	log.Startup("Web Port: %s\n", config.WebPort)

	// 初始化数据库
	db, err := database.InitDB()
	if err != nil {
		log.Error("初始化数据库失败: %v\n", err)
		return
	}
	defer db.Close()

	// 启动日志只打印表里实际生效的数据源；凭据只显示来源，绝不打印明文或混淆串。
	if sources, err := db.GetEnabledSources(); err == nil {
		for _, src := range sources {
			switch src.Type {
			case "clash":
				credSource := "未配置"
				switch src.CredentialSource() {
				case "env":
					credSource = "环境变量 " + src.CredentialRef
				case "table":
					credSource = "数据表"
				}
				log.Startup("数据源 %s (clash): %s, 间隔 %d ms, 凭据来源 %s\n",
					src.ID, src.Address, src.Interval, credSource)
			case "ipfix":
				log.Startup("数据源 %s (ipfix): UDP %d, LAN %s\n",
					src.ID, src.Port, src.LANCIDRs)
			}
		}
	}

	// 创建服务器实例
	server := web.NewServer(config, db)

	// 启动 Web 服务器，使用配置中的端口号
	server.Start(config.WebPort)
}
