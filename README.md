# Clash Statistics

一个通过 Clash API 获取信息并进行统计后以网页呈现的应用。

## 功能特性

- 通过 Clash API 获取连接信息
- 实时统计网络连接数据
- 网页界面展示统计数据
- 数据自动保存到 SQLite 数据库
- 支持自定义配置
- 目标统计：按访问的目标（域名/IP）聚合，点开看每个客户端 × 线路 × 规则 的用量明细
- 客户端统计：按客户端（源IP）聚合，点开看它访问了哪些目标、走的哪条线路与命中的规则
- 在线状态：按天展示每个客户端的在线情况（由存活连接体现）
- 统计与筛选都在服务端完成，支持按时间范围查询
- 深浅两套主题，右上角一键切换并记忆选择（默认跟随系统偏好）
- 响应式设计，适配各种屏幕尺寸
- 实时数据刷新和 API 刷新功能

## 配置

应用读取运行目录下的 `./data/config.ini`。首次运行且没有任何环境变量时会生成该文件并提示修改；容器/裸机都可以通过环境变量配置，无需配置文件。

配置优先级（逐项覆盖）：**环境变量 > config.ini > 默认值**

`config.ini` 包含以下配置项：

| 配置项 | 环境变量 | 默认值 |
|--------|----------|--------|
| `host` | `CLASH_HOST` | `127.0.0.1:9090` |
| `secret` | `CLASH_SECRET` | 空 |
| `interval` | `CLASH_INTERVAL` | `1000` |
| `web_port` | `WEB_PORT` | `8080` |
| `log_level` | `LOG_LEVEL` | `warn` |
| `db_cleanup_days` | `DB_CLEANUP_DAYS` | `7` |
| `db_vacuum_interval` | `DB_VACUUM_INTERVAL` | `24` |

即：设置了的环境变量覆盖对应的配置项，没设置的继续使用配置文件中的值，两者都没有才用默认值。

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

- `data/` - 存放 SQLite 数据库文件、日志文件和配置文件（`config.ini`）
- `main.go` - 主程序文件
- `web/` - Web 服务器相关代码
- `database/` - 数据库操作相关代码
- `utils/` - 工具函数

## API 接口

- `/` - 重定向到目标统计页面
- `/host-stats` - 目标统计页面
- `/sourceip-stats` - 客户端统计页面
- `/online-time` - 在线状态页面
- `/api/stats` - 按目标（域名/IP）聚合的统计数据，支持 `startTime` / `endTime`
- `/api/sourceip-stats` - 按客户端（源IP）聚合的统计数据，支持 `startTime` / `endTime`
- `/api/target-detail` - 某个目标的明细（客户端 × 线路 × 规则 的组合逐条列出），参数 `target`
- `/api/sourceip-detail` - 某个客户端的明细（目标 × 线路 × 规则 的组合逐条列出），参数 `ip`
- `/api/gantt` - 返回在线状态甘特图数据的 JSON 数据，参数 `date`
- `/refresh` - 手动刷新数据

`startTime` / `endTime` 均为 `datetime-local` 格式（`2006-01-02T15:04`）并按本地时区解析。

## 版本信息

当前版本：v0.8.0

## 技术栈

- Go 语言
- SQLite 数据库
- Tailwind CSS + daisyUI（**预编译**，运行时不加载 CDN、不需要 Node）
- Alpine.js（本地提供）

## 前端资源

页面样式基于 Tailwind CSS + daisyUI，但已提前编译成静态文件 `web/static/app.css`，
Alpine.js 也下载到了 `web/static/alpine.min.js`，两者都随二进制分发：

- 运行时**不访问任何外部 CDN**，断网或 CDN 不可用时页面依然正常显示
- 容器镜像里不需要 Node，构建和运行都只依赖 Go
- 打开页面需要拉取的资源从约 1.4MB（含浏览器端实时编译）降到约 145KB

修改模板里的 class 之后，需要重新生成 CSS 并提交（仅此一步需要 Node）：

```bash
npm install          # 首次
npm run build:css    # 输入 web/css/input.css，输出 web/static/app.css
```

## 目录结构

补充：`web/css/` 为样式编译入口，`web/static/` 为编译产物与静态资源，两者都会被
`//go:embed` 打进二进制。

## 注意事项

- 首次运行时会自动创建数据库文件
- 定期清理过期数据，默认保留 7 天
- 支持手动刷新数据和 API 刷新功能
- 所有统计数据都存储在 SQLite 数据库中，确保数据持久化
