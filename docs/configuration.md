# 配置与部署

配置文件只放**系统级配置**（本面板自身），不放数据源定义。数据源（Clash / IPFIX 地址、端口、LAN 网段、阈值、凭据等）全部在网页 `/data-sources` 页添加与管理，存于数据库 `data_sources` 表，改动即时生效、无需重启。

> **配置来源收敛（v2.2.0）**：运行时数据源定义只认 `data_sources` 这一张表；`config.toml` / 环境变量都不再参与数据源配置，也不做任何「一次性初始化 / 播种」。全新部署初始 0 数据源，打开在线状态页会显示「暂无数据源」引导。已有的 `data_sources` 行不受影响。

## 配置文件

配置文件是 **TOML 格式**，路径为运行目录下的 `./data/config.toml`，只含 `[server]` 一段：

```toml
[server]
log_level = "warn"
web_port = "8080"
db_cleanup_days = 30
db_vacuum_interval = 24
```

文件里没写的项会保留默认值，所以只需要写要改的那几项。字符串必须加引号（TOML 语法要求）。

**首次运行**会生成该文件并**直接继续启动**（不退出），好让全新部署能看到 `/data-sources` 的空表引导。生成失败（例如挂载卷权限不足）也不致命：仍可用环境变量 / 默认值运行。

配置优先级（逐项覆盖）：**环境变量 > config.toml > 默认值** —— 设置了的环境变量覆盖对应的配置项，没设置的继续用配置文件里的值，两者都没有才用默认值。

### `[server]` —— 本面板自身

| 配置项 | 环境变量 | 默认值 | 说明 |
|--------|----------|--------|------|
| `web_port` | `WEB_PORT` | `8080` | Web 服务监听端口 |
| `log_level` | `LOG_LEVEL` | `warn` | 日志级别：`debug` / `info` / `warn` / `error`，写入 `data/clash_statistics.log` |
| `db_cleanup_days` | `DB_CLEANUP_DAYS` | `30` | 连接数据的保留天数。启动时清理一次，之后每 24 小时清理一次 |
| `db_vacuum_interval` | `DB_VACUUM_INTERVAL` | `24` | SQLite `VACUUM` 的执行间隔（小时），用于回收删除数据后的空闲空间。**启动时不执行**，避免拖慢启动 |

## 数据源在哪里配

到页面 `/data-sources` 添加与管理：

- 快捷按钮「添加 Clash 源 / 添加 IPFIX 源」只预填默认值（端口、间隔、LAN 网段自动推断等），保存前可改。
- Clash 鉴权密钥两种填法（二选一，**环境变量优先**）：
  - 在「凭据引用」填环境变量名（如 `CLASH_SECRET`），真值由该环境变量提供（适合 Docker / 高级用法，设 `CLASH_SECRET=xxx` 环境变量）；
  - 或直接在「API 密钥」里填写，密钥**轻量可逆混淆后存进 `data_sources` 表**，页面与接口都不回显。这只是防止随手翻库看到明文，不是加密。
- IPFIX 的 LAN 网段、端口等都在该页维护；`utils.DefaultLANCIDR()` 只作为快捷按钮里可改的预填建议，不是配置源。

详见 [多数据源在线状态](data-sources.md)。

## 使用方法

1. 确保 Clash / 路由器侧按需配置好（Clash 外部控制器地址，或路由器 Traffic Flow 推送目标）
2. （可选）修改 `config.toml` 的系统项，或用环境变量覆盖
3. 构建并运行（构建产物统一输出到 `dist/`，已在 `.gitignore` 中排除）：

   ```bash
   go build -o dist/clash-statistics .
   cd dist && ./clash-statistics
   ```

   注意：**运行时产生的文件都在当前工作目录的 `data/` 下**（数据库、日志、配置文件），所以建议像上面那样在 `dist/` 里运行 —— 项目根目录就只会留下源码。如果在仓库根目录执行 `go run main.go`，则会在根目录生成 `data/`。

4. 打开浏览器访问 `http://localhost:8080`，先到 `/data-sources` 添加数据源

## Docker

Docker 部署同样只认表：镜像里不含任何数据源默认配置，`-e CLASH_HOST` 之类一键起已取消。按需发布 UDP 端口接收路由器推送，例如 `-p 2055:2055/udp`（compose 里写 `"2055:2055/udp"`）。数据源在页面添加；若用环境变量提供 Clash 密钥，可设 `CLASH_SECRET` 并让数据源的「凭据引用」指向它。详见 [Docker Hub 镜像说明](docker-hub.md)。
