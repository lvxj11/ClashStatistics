# Clash Statistics

只读采集 Clash / Mihomo 的 `/connections`，把连接数据落到 SQLite，提供可按天回溯的流量统计面板。

> **它只统计，不管理**：不读取、不修改任何 Clash 配置，不能切换节点、改策略组或改规则。

镜像 `lvxj11/clash-statistics` · 架构 `linux/amd64`、`linux/arm64` · 端口 `8080/tcp`（Web）、`2055/udp`（可选，路由器 IPFIX 推送）

- Docker Hub：https://hub.docker.com/r/lvxj11/clash-statistics
- 源码与完整文档：https://github.com/lvxj11/ClashStatistics

## 快速启动

```bash
docker run -d \
  --name clash-statistics \
  -p 8080:8080 \
  -v clash-statistics-data:/app/data \
  --restart unless-stopped \
  lvxj11/clash-statistics:latest
```

浏览器打开 `http://<宿主机IP>:8080`，然后到页面 `/data-sources` 添加数据源。

> **数据源在网页里配，不在环境变量里配。** 全新部署初始 0 数据源，打开在线状态页会显示「暂无数据源」。点管理页的「添加 Clash 源 / 添加 IPFIX 源」快捷按钮即可（只预填默认值，保存前可改）：
> - Clash 源：填外部控制器地址（如 `192.168.1.100:9090`）。密钥可直接在页面填写（轻量混淆后存数据表、不回显），也可填环境变量名（如 `CLASH_SECRET`）由环境变量提供（环境变量优先）。若用环境变量，启动时加 `-e CLASH_SECRET=your_secret`。
> - IPFIX 源：填 LAN 网段与 UDP 端口，并在路由器侧把 Traffic Flow 目标指向宿主机；接收推送时启动容器加 `-p 2055:2055/udp`。这些数据源配置都在网页里完成，与环境变量无关。
>
> 若 Clash 与容器在同一台机器上，地址可用 `host.docker.internal`（Linux 需追加 `--add-host=host.docker.internal:host-gateway`）或 `--network host`；默认的 `127.0.0.1:9090` 在容器里指向容器自身，连不到宿主机上的 Clash。

> 首次启动会在 `/app/data` 生成一份只含系统项的默认 `config.toml`，然后**直接继续启动**（不会退出）；系统项也可用环境变量覆盖。

## 配置

配置文件 `/app/data/config.toml` 只放系统级配置，数据源全部在网页 `/data-sources` 页管理。优先级（逐项覆盖）：**环境变量 > `/app/data/config.toml` > 默认值**。只写要改的那几项即可。

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `WEB_PORT` | `8080` | Web 服务监听端口（容器内） |
| `LOG_LEVEL` | `warn` | `debug` / `info` / `warn` / `error` |
| `DB_CLEANUP_DAYS` | `30` | 连接数据保留天数，启动时清理一次，之后每 24 小时清理一次 |
| `DB_VACUUM_INTERVAL` | `24` | SQLite `VACUUM` 间隔（小时），启动时不执行 |
| `CLASH_SECRET` | 空 | **可选**。仅当某数据源的「凭据引用」填了 `CLASH_SECRET` 时作为该源密钥；也可以不设，改在页面直接填密钥 |
| `TZ` | `Asia/Shanghai` | 容器时区 |

> `CLASH_HOST` / `CLASH_INTERVAL` / `IPFIX_PORT` / `IPFIX_LAN` / `IPFIX_EXCLUDE_IPS` 等数据源相关环境变量已取消，不做任何「一次性注入 / 播种」。数据源定义只认数据表，删光后重启保持空表。

也可以不用环境变量，只挂一份配置文件。路径必须是 `/app/data/config.toml`，格式为 TOML（字符串要加引号），**只含 `[server]` 系统项**：

```toml
[server]
log_level = "warn"
web_port = "8080"
db_cleanup_days = 30
db_vacuum_interval = 24
```

## 数据持久化

容器内工作目录是 `/app`，运行时文件全部写在 `/app/data/`：SQLite 数据库、`clash_statistics.log`、`config.toml`。**务必挂载 `/app/data`**，否则容器重建后统计数据会丢失。

镜像以非 root 用户 `appuser`（UID/GID `1000`）运行。用宿主机目录做绑定挂载时，需保证该目录对 UID 1000 可写：

```bash
mkdir -p ./data && chown 1000:1000 ./data
```

然后加 `-v "$(pwd)/data:/app/data"`。使用 Docker 命名卷（快速启动里的 `clash-statistics-data`）则无需手动改权限。

## docker compose

```yaml
services:
  clash-statistics:
    image: lvxj11/clash-statistics:latest
    ports:
      - "8080:8080"
      - "2055:2055/udp"   # 可选：接收路由器 IPFIX 推送
    volumes:
      - ./data:/app/data
    environment:
      # 只放系统级配置；数据源请在网页 /data-sources 页添加
      CLASH_SECRET: "your_secret"   # 可选：数据源凭据引用指向它时生效
      TZ: Asia/Shanghai
    user: "1000:1000"
    restart: unless-stopped
```

## 客户端名称自动发现（可选）

可为客户端 IP 手动指定名称与备注；另有被动监听 DHCP option 12 / mDNS 的自动识别作为兜底。自动识别需要能看到局域网的广播 / 组播：

| 网络模式 | 自动识别 |
|---|---|
| `--network host` | 可用，最简单 |
| macvlan（同网段 IP） | 可用，本项目实际部署方式 |
| 默认 bridge | 通常收不到广播 / 组播，自动识别会安静降级，手动名称完全不受影响 |

默认 bridge 下日志出现 `DHCP 监听不可用` / `mDNS 监听不可用` 属正常降级，手动配置与统计功能不受影响。如需放行 DHCP 的低端口 67，可加 `--sysctl net.ipv4.ip_unprivileged_port_start=0`。

## 升级

```bash
docker pull lvxj11/clash-statistics:latest
docker stop clash-statistics && docker rm clash-statistics
# 用与原来相同的 -v 挂载重新 docker run
```

数据库会自动建表 / 迁移，升级不需要手动处理数据；只要 `/app/data` 挂载不变，历史统计与已配置的数据源就都在。也可以固定版本标签，例如 `lvxj11/clash-statistics:2.2.0`。

> **迁移基线为 v1.0.4**：升级新增 `activity`、`flow_slots`、`data_sources` 三张表，`connections` / `client_names` 结构不变、无数据迁移（`activity` 为已停写停查的旧聚合表，仅按保留天数清理残留；在线状态推送源改用 `flow_slots` 后从升级时刻重新积累热度）。后加的列（`thresholds` / `custom_thresholds` / `credential_value`）用幂等 `ALTER TABLE` 补齐。v1.0.3 及更早版本的历史迁移代码已移除，请先用 v1.0.4 过渡再升级。

## 标签

- `latest` —— 最新发布版本
- `2.2.0`、`2.1.0`、`2.0.0`、`1.0.4` … —— 与 GitHub Release 对应的语义化版本

## 更多文档

- 配置与部署：https://github.com/lvxj11/ClashStatistics/blob/main/docs/configuration.md
- 多数据源在线状态：https://github.com/lvxj11/ClashStatistics/blob/main/docs/data-sources.md
- 客户端名称：https://github.com/lvxj11/ClashStatistics/blob/main/docs/client-names.md
- API 接口：https://github.com/lvxj11/ClashStatistics/blob/main/docs/api.md
- 项目说明：https://github.com/lvxj11/ClashStatistics/blob/main/docs/project.md
- 问题反馈：https://github.com/lvxj11/ClashStatistics/issues
