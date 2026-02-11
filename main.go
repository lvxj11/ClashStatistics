package main

import (
	"fmt"

	"clash-statistics/database"
	"clash-statistics/utils"
	"clash-statistics/web"
)

var (
	db *database.Database
)

func main() {
	// 初始化日志系统
	if err := utils.InitLogger(); err != nil {
		fmt.Printf("初始化日志系统失败: %v\n", err)
		return
	}
	defer utils.GetLogger().Close()

	log := utils.GetLogger()

	// 加载配置
	config, err := utils.LoadConfig()
	if err != nil {
		log.Printf("加载配置失败: %v\n", err)
		return
	}

	log.Printf("Clash Host: %s\n", config.ClashHost)
	log.Printf("Clash Secret: %s\n", config.ClashSecret)
	log.Printf("Clash Interval: %d\n", config.ClashInterval)

	// 初始化数据库
	db, err = database.InitDB()
	if err != nil {
		log.Printf("初始化数据库失败: %v\n", err)
		return
	}
	defer db.Close()

	// 创建服务器实例
	server := web.NewServer(config, db)

	// 启动 Web 服务器
	server.Start("8080")
}
