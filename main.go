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

	// 首先加载配置，以获取日志级别
	config, err := utils.LoadConfig()
	if err != nil {
		// 配置文件不存在时，生成了默认配置文件并退出
		// 这里直接使用fmt输出错误信息，因为日志系统还未初始化
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

	// 输出启动信息，同时显示在控制台和日志文件中
	log.Startup("Clash Host: %s\n", config.ClashHost)
	log.Startup("Clash Secret: %s\n", config.ClashSecret)
	log.Startup("Clash Interval: %d\n", config.ClashInterval)
	log.Startup("Log Level: %s\n", config.LogLevel)
	log.Startup("Web Port: %s\n", config.WebPort)
	log.Startup("IPFIX UDP Port: %d\n", config.IPFIXPort)

	// 初始化数据库
	db, err := database.InitDB()
	if err != nil {
		log.Error("初始化数据库失败: %v\n", err)
		return
	}
	defer db.Close()

	// 首次启动（含从 v1.0.4 升级）写入内置数据源；之后完全以 data_sources 表为准，
	// 采集器与在线状态页都从这张表读，不覆盖用户在管理页的改动
	if err := db.EnsureDefaultSources(database.BootstrapConfig{
		ClashHost:     config.ClashHost,
		ClashInterval: config.ClashInterval,
		IPFIXPort:     config.IPFIXPort,
		IPFIXLAN:      config.IPFIXLAN,
		IPFIXExclude:  config.IPFIXExclude,
	}); err != nil {
		log.Error("初始化默认数据源失败: %v\n", err)
		return
	}

	// 创建服务器实例
	server := web.NewServer(config, db)

	// 启动 Web 服务器，使用配置中的端口号
	server.Start(config.WebPort)
}
