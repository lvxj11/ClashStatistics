# Clash Statistics

一个通过 Clash API **只读**获取连接信息、做统计与回溯，并以网页呈现的应用。

> **一句话：它只统计，不管理。**

## 与其他面板的区别（先说清楚）

Clash 生态里已经有很多很棒的面板（Yacd、MetaCubeXD、OpenClash 等），它们擅长的是**管理**：切换节点、调整策略组、观察实时连接、修改规则。

**本面板完全不涉及 Clash 的配置** —— 不提供任何修改代理设置的能力，也不打算做。它只读取 Clash API 暴露出来的连接信息，其余全部是统计：

| | 常见 Clash 面板 | 本面板 |
|---|---|---|
| 定位 | 管理代理（节点 / 策略 / 规则） | **只做统计** |
| 视角 | 当前实时连接 | 历史聚合，可按天回溯（默认保留 7 天） |
| 关注点 | 连接通不通、走的哪个节点 | 谁 / 哪个目标用了多少流量、什么时候在线 |
| 数据 | 通常不留存 | 落 SQLite，可查询任意历史区间 |
| 对 Clash 的影响 | 会写入配置 | **只读，不写任何配置** |

## 它要解决的两个问题

1. **流量归因** —— 查看是哪些**用户**（源 IP）或哪些**目标**（域名/IP）用掉了过多流量，并可下钻到"某个目标被谁访问、走的哪条线路、命中了哪条规则"。
2. **使用时段监控** —— 按天呈现每个设备的在线时段与并发强度（由存活连接体现），用来观察**某个设备是否在不应使用的时间仍有连接**，比如孩子的手机。

## 功能特性

- 通过 Clash API 只读获取连接信息，不修改任何配置
- 目标统计：按访问的目标（域名/IP）聚合，点开看每个客户端 × 线路 × 规则 的用量明细
- 客户端统计：按客户端（源IP）聚合，点开看它访问了哪些目标、走的哪条线路与命中的规则
- 在线状态：按天展示每个客户端的在线时段与并发连接强度（横向时间热度图）
- 客户端名称：为客户端 IP 手动指定名称与备注（独立管理页），并提供可选的自动识别（DHCP option 12 / mDNS）作为兜底辅助
- 统计与筛选都在服务端完成，支持按时间范围查询
- 表格支持服务端搜索与点表头排序，单表最多 500 行，超出时提示用搜索缩小范围
- 明细弹窗可按线路 / 规则拆分查看，同样支持搜索与排序
- 数据自动保存到 SQLite 数据库，定期清理过期数据（默认保留 7 天）
- 支持 `config.toml`（TOML 格式）与环境变量两种配置方式
- 深浅两套主题，右上角一键切换并记忆选择（默认跟随系统偏好）
- 响应式设计，适配各种屏幕尺寸
- 实时数据刷新和 API 刷新功能

## 配置

配置文件是 **TOML 格式**，路径为运行目录下的 `./data/config.toml`：

```toml
[clash]
host = "127.0.0.1:9090"
secret = ""
interval = 1000

[server]
log_level = "warn"
web_port = "8080"
db_cleanup_days = 7
db_vacuum_interval = 24
```

文件里没写的项会保留默认值，所以只需要写要改的那几项。字符串必须加引号（TOML 语法要求，例如 `host = "10.0.1.2:9090"`）。

首次运行且没有任何环境变量时会生成该文件并提示修改；容器 / 裸机都可以通过环境变量配置，无需配置文件。

配置优先级（逐项覆盖）：**环境变量 > config.toml > 默认值** —— 设置了的环境变量覆盖对应的配置项，没设置的继续用配置文件里的值，两者都没有才用默认值。

### `[clash]` —— 数据源

本面板从这里**只读**取 `/connections`，不改任何配置。

| 配置项 | 环境变量 | 默认值 | 说明 |
|--------|----------|--------|------|
| `host` | `CLASH_HOST` | `127.0.0.1:9090` | 外部控制器的地址与端口（对应 Clash 配置里的 `external-controller`）。支持 Clash 与 Mihomo |
| `secret` | `CLASH_SECRET` | 空 | 外部控制器的鉴权密钥（对应 Clash 配置里的 `secret`）。设置后请求会带 `Authorization: Bearer <secret>`；留空则不鉴权 |
| `interval` | `CLASH_INTERVAL` | `1000` | 轮询间隔（毫秒）。它同时作为 `/connections?interval=` 参数传给 API，Clash 会按该间隔返回连接快照；间隔越小越容易抓到短连接，但请求也更频繁 |

### `[server]` —— 本面板自身

| 配置项 | 环境变量 | 默认值 | 说明 |
|--------|----------|--------|------|
| `web_port` | `WEB_PORT` | `8080` | Web 服务监听端口 |
| `log_level` | `LOG_LEVEL` | `warn` | 日志级别：`debug` / `info` / `warn` / `error`，写入 `data/clash_statistics.log` |
| `db_cleanup_days` | `DB_CLEANUP_DAYS` | `7` | 连接数据的保留天数。启动时清理一次，之后每 24 小时清理一次 |
| `db_vacuum_interval` | `DB_VACUUM_INTERVAL` | `24` | SQLite `VACUUM` 的执行间隔（小时），用于回收删除数据后的空闲空间。**启动时不执行**，避免拖慢启动 |

## 使用方法

1. 确保 Clash 服务正在运行
2. 根据需要修改 `config.toml` 配置文件
3. 构建并运行（构建产物统一输出到 `dist/`，已在 `.gitignore` 中排除）：
   ```bash
   go build -o dist/clash-statistics .
   cd dist && ./clash-statistics
   ```
   
   注意：**运行时产生的文件都在当前工作目录的 `data/` 下**（数据库、日志、配置文件），所以建议像上面那样在 `dist/` 里运行 —— 项目根目录就只会留下源码。如果在仓库根目录执行 `go run main.go`，则会在根目录生成 `data/`。

4. 打开浏览器访问 `http://localhost:8080`

## 客户端名称（可选）

默认界面只显示客户端 IP。可以为某个 IP 指定一个便于识别的名称和备注。名称只是辅助信息，
**IP 始终是主键**：分组、排序、搜索、明细下钻都按 IP 进行，不会因名称改变。

打开顶部的 **客户端名称** 标签进入独立管理页（`/client-names`），可以：

- 单条新增 / 编辑：填写 IP、名称、备注
- 批量导入：每行一条 `IP 名称 备注`（空格 / 制表符 / 逗号分隔，备注可省，`#` 开头为注释）
- 清除：删除某 IP 的手动配置（自动识别的名称保留为兜底）

### 显示规则

- 统计页、在线状态页、明细弹窗里，名称以 `IP (名称)` 的形式跟在 IP 后面；没有名称时只显示 IP
- IP 与名称是两个独立元素，IP 用等宽字体、名称弱化显示，便于单独复制 IP
- **备注只在鼠标悬浮（`title`）时显示**，不进入表格正文
- 内容用 `x-text` 转义输出，不存在 XSS 注入面
- 现有页面只读，不提供内联编辑；编辑统一在管理页进行

### 手动优先 / 自动兜底

数据库用 `client_names` 表单独保存映射，`manual_name`/`note` 与 `auto_name`/`auto_source` 分列，互不覆盖：

- **手动配置第一优先**：`manual_name` 非空时，展示一律忽略自动值（自动值仍保留，清除手动后可回退）
- 自动识别只是兜底辅助，**不保证一定能获取到**，也不预设一定能拿到
- 自动来源优先级：`DHCP option 12 > mDNS > 连接元数据（inbound_user 等）`，手动始终高于全部自动来源

自动识别来自被动监听（不需要、也不会去适配路由器租约 API）：

- 监听 `DHCP Request` 广播里的 option 12（主机名）与 option 50（Requested-IP），直接得到 IP→主机名
- 加入 `224.0.0.251:5353` mDNS 组播，解析 `.local` 主机名与设备友好名；启动与周期发送 DNS-SD 触发查询
- 顺带从连接元数据（`inbound_user` / `inbound_name` / `process`）补充候选
- 候选经清洗（过滤 UUID、长数字临时名、服务实例名）后，按优先级写入 `auto_name`
- 任一步失败（端口被占、无组播、无权限、无网卡）只记一条警告并安静降级，不影响手动配置和现有页面

### 部署与网络

被动监听需要能看到局域网的广播 / 组播：

| 部署方式 | 说明 |
|---|---|
| 裸机 | 最简单，直接用局域网网卡 |
| Docker `network_mode: host` | 直接用宿主机网络栈，可收到广播 / 组播 |
| Docker **macvlan**（同网段 IP） | 可用，能保留广播 / 组播（本项目实际部署方式）；注意 macvlan 下容器不能通过 `127.0.0.1` 访问宿主机上的 Clash |
| Docker 默认 bridge | 通常收不到局域网广播 / 组播，自动识别会降级，但手动配置完全可用 |

实现与部署形态无关：不硬编码网卡，交由系统选择默认组播网卡；`ipvlan L3`、无组播环境、
低端口无权限等都会自动降级。容器里低端口（67）由 `net.ipv4.ip_unprivileged_port_start=0` 放行；
裸机绑定 67 端口可能需要相应权限，绑不上即跳过 DHCP 监听。

### 暂未实现

- 按名称搜索：现有搜索是发往后端在 SQL 聚合阶段用 `LIKE` 过滤的，改名搜索会牵动现有查询逻辑，暂不做
- PTR 反向 DNS：需要本地权威反向区域，当前网络只有公网 DNS 转发，暂不做
- 路由器 DHCP 租约 API 适配（OpenWrt / RouterOS / 爱快）：需按品牌 / 版本适配且要认证，代价过高，不做
- ARP/MAC OUI、NetBIOS / WS-Discovery 主动探测：覆盖面窄、代价不划算，不做

## 目录结构

- `dist/` - 构建产物目录（本地与 CI 都输出到这里，已被 git 排除；本地运行时的 `data/` 也在其下）
- `data/` - 运行时目录：SQLite 数据库、日志、配置文件（`config.toml`）。相对**当前工作目录**创建 —— 在 `dist/` 下运行则位于 `dist/data/`；Docker 部署时由挂载卷提供（见 `docker-compose.yml`）
- `main.go` - 主程序文件
- `web/` - Web 服务器、API 与页面模板相关代码
- `database/` - 数据库操作相关代码
- `discovery/` - 客户端名称自动发现（DHCP option 12 / mDNS 被动监听与解析）
- `utils/` - 工具函数

## API 接口

- `/` - 重定向到目标统计页面
- `/host-stats` - 目标统计页面
- `/sourceip-stats` - 客户端统计页面
- `/online-time` - 在线状态页面
- `/client-names` - 客户端名称管理页面
- `/api/stats` - 按目标（域名/IP）聚合的统计数据
- `/api/sourceip-stats` - 按客户端（源IP）聚合的统计数据
- `/api/target-detail` - 某个目标的明细（客户端 × 线路 × 规则），参数 `target`
- `/api/sourceip-detail` - 某个客户端的明细（目标 × 线路 × 规则），参数 `ip`
- `/api/gantt` - 在线状态数据（按天的 5 分钟时间槽），参数 `date`
- `/api/client-names` - 客户端名称：`GET` 读取（返回 `names` 与 `knownIPs`）、`PUT` 设置 / 批量导入、 `DELETE?ip=` 清除手动配置
- `/refresh` - 手动触发一次数据采集

统计 / 明细类接口（`/api/stats`、`/api/sourceip-stats`、`/api/target-detail`、`/api/sourceip-detail`）共用这些参数：

| 参数 | 说明 |
|---|---|
| `startTime` / `endTime` | `datetime-local` 格式（`2006-01-02T15:04`），按本地时区解析 |
| `q` | 按表格第一列模糊搜索，交给服务端过滤 |
| `sort` | 排序字段白名单：`target` / `download` / `upload` / `total` / `connections` / `peers` / `first` / `last`；明细接口的第一列用 `key`。不在白名单里的值会回落到默认排序 |
| `order` | `asc` / `desc`，默认 `desc` |

返回 `{Rows, Total, Truncated}`：`Total` 是符合条件的真实条数，`Truncated` 表示被单表上限（500 行，定义在 `database.maxTableRows`）截断。排序发生在 `LIMIT` 之前，所以截断的是"排好序的前 500 行"。


## 版本信息

当前版本：v1.0.3

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
