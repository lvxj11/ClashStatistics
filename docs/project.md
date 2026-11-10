# 项目说明

## 与其他面板的区别

Clash 生态里已经有很多很棒的面板（Yacd、MetaCubeXD、OpenClash 等），它们擅长的是**管理**：切换节点、调整策略组、观察实时连接、修改规则。

**本面板完全不涉及 Clash 的配置** —— 不提供任何修改代理设置的能力，也不打算做。它只读取 Clash API 暴露出来的连接信息，其余全部是统计：

| | 常见 Clash 面板 | 本面板 |
|---|---|---|
| 定位 | 管理代理（节点 / 策略 / 规则） | **只做统计** |
| 视角 | 当前实时连接 | 历史聚合，可按天回溯（默认保留 30 天） |
| 关注点 | 连接通不通、走的哪个节点 | 谁 / 哪个目标用了多少流量、什么时候在线 |
| 数据 | 通常不留存 | 落 SQLite，可查询任意历史区间 |
| 对 Clash 的影响 | 会写入配置 | **只读，不写任何配置** |

## 它要解决的两个问题

1. **流量归因** —— 查看是哪些**用户**（源 IP）或哪些**目标**（域名/IP）用掉了过多流量，并可下钻到"某个目标被谁访问、走的哪条线路、命中了哪条规则"。
2. **使用时段监控** —— 按天呈现每个设备的在线时段与活跃强度（由存活代理连接或路由器去重后的并发连接体现），用来观察**某个设备是否在不应使用的时间仍有连接**，比如孩子的手机。在线状态支持多数据源切换，详见 [多数据源在线状态](data-sources.md)。

## 目录结构

- `dist/` - 构建产物目录（本地与 CI 都输出到这里，已被 git 排除；本地运行时的 `data/` 也在其下）
- `data/` - 运行时目录：SQLite 数据库、日志、配置文件（`config.toml`）。相对**当前工作目录**创建 —— 在 `dist/` 下运行则位于 `dist/data/`；Docker 部署时由挂载卷提供（见 `docker-compose.yml`）
- `main.go` - 主程序文件
- `web/` - Web 服务器、API、采集器（Clash 轮询 / IPFIX UDP 监听）与页面模板
- `database/` - 数据库操作相关代码（连接统计 `connections`、推送源并发连接 `flow_slots`、数据源 `data_sources`、客户端名称）
- `ipfix/` - NetFlow v9 / IPFIX 报文解码（纯协议解析，可单独测试）
- `discovery/` - 客户端名称自动发现（DHCP option 12 / mDNS 被动监听与解析）
- `utils/` - 工具函数
- `docs/` - 详细文档

补充：`web/css/` 为样式编译入口，`web/static/` 为编译产物与静态资源，两者都会被
`//go:embed` 打进二进制。

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

## 注意事项

- 首次运行会创建数据库与一份只含系统项的 `config.toml`，然后直接启动；数据源定义只认 `data_sources` 表，**不做任何播种**，全新部署初始 0 数据源（到 `/data-sources` 页添加）。删光数据源后重启保持空表
- Clash 鉴权密钥可在管理页直接填写（轻量混淆后存表、不回显）或用环境变量引用提供；环境变量优先
- 定期清理过期数据，默认保留 30 天；`flow_slots`（推送源并发连接）与 `connections` 共用同一清理边界，v2.0.0 遗留的 `activity` 表也随同一边界清理残留数据
- 支持手动刷新数据和 API 刷新功能
- 所有统计数据都存储在 SQLite 数据库中，确保数据持久化

## 版本与迁移基线

- v2.0.0 起支持多数据源在线状态（数据源入表 + 管理页、IPFIX 推送采集、活跃强度 1–6 档归一化）；**v2.1.0** 把 IPFIX 源改为按五元组去重的真实并发连接数（`flow_slots` 表），两源单位统一为「并发连接数」；**v2.2.0** 把数据源配置收敛为只认 `data_sources` 表（去掉播种、配置段与数据源 env），并支持表内轻量混淆保存 Clash 密钥。
- **数据库迁移基线为 v1.0.4**：升级新增 `activity`、`flow_slots`、`data_sources` 三张表（`CREATE TABLE IF NOT EXISTS`），`connections` 与 `client_names` 结构不变，**无数据迁移**。`activity` 为旧聚合表，已停写停查，仅按保留天数清理残留。后加的列（`thresholds` / `custom_thresholds` / `credential_value`）用幂等 `ALTER TABLE` 补齐。
- **v1.0.3 及更早版本的历史迁移 / 兼容代码已移除**（旧索引清理、`inbound_name` 名称清理等）。从这些版本升级请先用 v1.0.4 过渡，再升到 v2.2.0。详见 [多数据源在线状态](data-sources.md)。
