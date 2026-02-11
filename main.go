package main

import (
	"fmt"

	"clash-statistics/config"
	"clash-statistics/database"
	"clash-statistics/web"
)

var (
	db *database.Database
)

func main() {
	// 加载配置
	config, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		return
	}
	
	fmt.Printf("Clash Host: %s\n", config.ClashHost)
	fmt.Printf("Clash Secret: %s\n", config.ClashSecret)
	fmt.Printf("Clash Interval: %d\n", config.ClashInterval)
	
	
	// 初始化数据库
	db, err = database.InitDB()
	if err != nil {
		fmt.Printf("初始化数据库失败: %v\n", err)
		return
	}
	defer db.Close()
	
	// 创建服务器实例
	server := web.NewServer(config, db)
	
	// 启动 Web 服务器
	server.Start("8080")
}