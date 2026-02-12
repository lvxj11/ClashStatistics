# Clash Statistics

一个通过 Clash API 获取信息并进行统计后以网页呈现的应用。

## 功能特性

- 通过 Clash API 获取连接信息
- 实时统计网络连接数据
- 网页界面展示统计数据
- 数据自动保存到 SQLite 数据库
- 支持自定义配置
- 按 Host/IP 统计流量数据
- 按 SourceIP 统计流量数据
- SourceIP 在线时间分析（甘特图）
- SourceIP 筛选功能，支持多 IP 筛选
- 响应式设计，适配各种屏幕尺寸
- 实时数据刷新和 API 刷新功能

## 配置

应用会读取同目录下的 `config.ini` 文件，包含以下配置项：

- `host`: Clash API 主机地址，默认为 `127.0.0.1:9090`
- `secret`: Clash API 认证密钥，默认为空
- `interval`: 数据刷新间隔（毫秒），默认为 `1000`
- `web_port`: Web 服务器端口，默认为 `8080`
- `log_level`: 日志级别，默认为 `info`
- `db_cleanup_days`: 数据库清理天数，默认为 `7`
- `db_vacuum_interval`: 数据库 vacuum 间隔（小时），默认为 `24`

## 使用方法

1. 确保 Clash 服务正在运行
2. 根据需要修改 `config.ini` 配置文件
3. 运行应用：
   ```bash
   # 直接运行
   go run main.go
   
   # 或构建后运行
   go build -o clash-statistics .
   ./clash-statistics
   ```
4. 打开浏览器访问 `http://localhost:8080`

## 目录结构

- `data/` - 存放 SQLite 数据库文件
- `web/templates/` - 存放网页模板文件
- `config.ini` - 应用配置文件
- `main.go` - 主程序文件
- `web/` - Web 服务器相关代码
- `database/` - 数据库操作相关代码
- `utils/` - 工具函数

## API 接口

- `/` - 重定向到 Host/IP 统计页面
- `/host-stats` - Host/IP 统计页面
- `/sourceip-stats` - SourceIP 统计页面
- `/online-time` - SourceIP 在线时间页面
- `/api/stats` - 返回 Host/IP 统计数据的 JSON 数据
- `/api/sourceip-stats` - 返回 SourceIP 统计数据的 JSON 数据
- `/api/gantt` - 返回在线时间甘特图数据的 JSON 数据
- `/refresh` - 手动刷新数据

## 版本信息

当前版本：v0.2.4

## 技术栈

- Go 语言
- SQLite 数据库
- Tailwind CSS
- Alpine.js
- daisyUI

## 注意事项

- 首次运行时会自动创建数据库文件
- 定期清理过期数据，默认保留 7 天
- 支持手动刷新数据和 API 刷新功能
- 所有统计数据都存储在 SQLite 数据库中，确保数据持久化