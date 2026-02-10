# Clash Statistics

一个通过 Clash API 获取信息并进行统计后以网页呈现的应用。

## 功能特性

- 通过 Clash API 获取连接信息
- 实时统计网络连接数据
- 网页界面展示统计数据
- 数据自动保存到 JSON 文件
- 支持自定义配置

## 配置

应用会读取同目录下的 `config.ini` 文件，包含以下配置项：

- `host`: Clash API 主机地址，默认为 `127.0.0.1:9090`
- `secret`: Clash API 认证密钥，默认为空
- `interval`: 数据刷新间隔（毫秒），默认为 `1000`

## 使用方法

1. 确保 Clash 服务正在运行
2. 根据需要修改 `config.ini` 配置文件
3. 运行应用：
   ```bash
   go run main.go
   ```
4. 打开浏览器访问 `http://localhost:8080`

## 目录结构

- `data/` - 存放统计数据的 JSON 文件
- `static/` - 存放静态资源文件
- `templates/` - 存放网页模板文件
- `config.ini` - 应用配置文件
- `main.go` - 主程序文件

## API 接口

- `/` - 主页面，显示统计仪表板
- `/api/connections` - 返回连接信息的 JSON 数据
- `/refresh` - 手动刷新数据