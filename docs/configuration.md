# 配置与部署

## 配置文件

配置文件是 **TOML 格式**，路径为运行目录下的 `./data/config.toml`：

```toml
[clash]
host = "127.0.0.1:9090"
secret = ""
interval = 1000

[server]
log_level = "warn"
web_port = "8080"
db_cleanup_days = 30
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
| `db_cleanup_days` | `DB_CLEANUP_DAYS` | `30` | 连接数据的保留天数。启动时清理一次，之后每 24 小时清理一次 |
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
