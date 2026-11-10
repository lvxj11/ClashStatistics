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
  -e CLASH_HOST=192.168.1.100:9090 \
  -e CLASH_SECRET=your_secret \
  -v clash-statistics-data:/app/data \
  --restart unless-stopped \
  lvxj11/clash-statistics:latest
```

浏览器打开 `http://<宿主机IP>:8080`。

> 在线状态除了 Clash 代理，还可接收路由器 Traffic Flow / IPFIX 推送作为第二个数据源。需要时追加 `-p 2055:2055/udp -e IPFIX_LAN=192.168.1.0/24`（`IPFIX_LAN` 改成你的局域网网段），并在路由器侧把 Traffic Flow 目标指向宿主机。数据源也可在页面 `/data-sources` 里增删改、启停、切换，改动即时生效。详见下方「更多文档」的《多数据源在线状态》。

> **注意：必须配置数据源。** 默认的 `CLASH_HOST=127.0.0.1:9090` 在容器里指向容器自身，连不到宿主机上的 Clash。请填宿主机的**局域网 IP**；若 Clash 与容器在同一台机器上，可用 `host.docker.internal`（Linux 需追加 `--add-host=host.docker.internal:host-gateway`）或 `--network host`。
>
> 另外，若既没有 `config.toml`、又没有任何环境变量，程序首次启动会生成一份默认 `config.toml` **然后退出**并提示修改后重启。所以要么传环境变量（推荐），要么先在挂载卷里放好 `config.toml`。传环境变量时程序仍会生成一份默认 `config.toml`，但环境变量优先级更高，可忽略它。

## 配置

优先级（逐项覆盖）：**环境变量 > `/app/data/config.toml` > 默认值**。只写要改的那几项即可。

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `CLASH_HOST` | `127.0.0.1:9090` | Clash / Mihomo 外部控制器地址（对应 `external-controller`），支持 Clash 与 Mihomo |
| `CLASH_SECRET` | 空 | 外部控制器密钥（对应 `secret`）；设置后请求带 `Authorization: Bearer <secret>`，留空则不鉴权 |
| `CLASH_INTERVAL` | `1000` | 轮询间隔（毫秒）。同时作为 `/connections?interval=` 传给 API，越小越容易抓到短连接，请求也更频繁 |
| `WEB_PORT` | `8080` | Web 服务监听端口（容器内） |
| `LOG_LEVEL` | `warn` | `debug` / `info` / `warn` / `error` |
| `DB_CLEANUP_DAYS` | `30` | 连接数据保留天数，启动时清理一次，之后每 24 小时清理一次 |
| `DB_VACUUM_INTERVAL` | `24` | SQLite `VACUUM` 间隔（小时），启动时不执行 |
| `IPFIX_PORT` | `2055` | 路由器 IPFIX 推送的 UDP 监听端口（容器内）。需发布 `-p 2055:2055/udp` 才能收到推送 |
| `IPFIX_LAN` | 自动推断 /24 | LAN 网段，只有落在这里的源地址算客户端；非 /24 网络务必显式配置 |
| `IPFIX_EXCLUDE_IPS` | 空 | 排除 IP（逗号分隔），通常填网关 |
| `TZ` | `Asia/Shanghai` | 容器时区 |

> 上面 `IPFIX_*` 是首次启动写入内置数据源的默认值；运行时真实配置在页面 `/data-sources` 维护，凭据只存环境变量引用、不回显明文。

也可以不用环境变量，只挂一份配置文件。路径必须是 `/app/data/config.toml`，格式为 TOML（字符串要加引号）：

```toml
[clash]
host = "192.168.1.100:9090"
secret = "your_secret"
interval = 1000

[server]
log_level = "warn"
web_port = "8080"
db_cleanup_days = 30
db_vacuum_interval = 24

[ipfix]
port = 2055
lan = "192.168.1.0/24"
exclude_ips = ""
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
      CLASH_HOST: "192.168.1.100:9090"
      CLASH_SECRET: "your_secret"
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
# 用与原来相同的 -v 挂载和环境变量重新 docker run
```

数据库会自动建表 / 迁移，升级不需要手动处理数据；只要 `/app/data` 挂载不变，历史统计就都在。也可以固定版本标签，例如 `lvxj11/clash-statistics:2.1.0`。

> **迁移基线为 v1.0.4**：升级新增 `activity`、`flow_slots`、`data_sources` 三张表，`connections` / `client_names` 结构不变、无数据迁移（`activity` 为已停写停查的旧聚合表，仅按保留天数清理残留；在线状态推送源改用 `flow_slots` 后从升级时刻重新积累热度）。v1.0.3 及更早版本的历史迁移代码已移除，请先用 v1.0.4 过渡再升级。

## 标签

- `latest` —— 最新发布版本
- `2.1.0`、`2.0.0`、`1.0.4`、`1.0.3` … —— 与 GitHub Release 对应的语义化版本

## 更多文档

- 配置与部署：https://github.com/lvxj11/ClashStatistics/blob/main/docs/configuration.md
- 多数据源在线状态：https://github.com/lvxj11/ClashStatistics/blob/main/docs/data-sources.md
- 客户端名称：https://github.com/lvxj11/ClashStatistics/blob/main/docs/client-names.md
- API 接口：https://github.com/lvxj11/ClashStatistics/blob/main/docs/api.md
- 项目说明：https://github.com/lvxj11/ClashStatistics/blob/main/docs/project.md
- 问题反馈：https://github.com/lvxj11/ClashStatistics/issues
