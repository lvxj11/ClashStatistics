# API 接口

## 接口清单

- `/` - 重定向到目标统计页面
- `/host-stats` - 目标统计页面
- `/sourceip-stats` - 客户端统计页面
- `/online-time` - 在线状态页面（**不再有数据源下拉**，统一跟随数据源页的「在线默认」单选源，页顶静态显示该源名并提供跳转链接；没有可用在线默认源时显示引导、不请求 `/api/gantt`）
- `/client-names` - 客户端名称管理页面
- `/data-sources` - 数据源管理页面（三个独立开关：采集 / 汇总 / 在线默认；空表时给出「添加 Clash 源 / 添加 IPFIX 源」引导）
- `/api/stats` - 按目标（域名/IP）聚合的统计数据（只汇总「汇总」开关开启的 Clash 源 + 升级遗留的无来源数据；IPFIX 源不参与）
- `/api/sourceip-stats` - 按客户端（源IP）聚合的统计数据（同上汇总范围）
- `/api/target-detail` - 某个目标的明细（客户端 × 线路 × 规则），参数 `target`（汇总范围同统计页）
- `/api/sourceip-detail` - 某个客户端的明细（目标 × 线路 × 规则），参数 `ip`（汇总范围同统计页）
- `/api/gantt` - 在线状态数据（按天的 5 分钟时间槽），参数 `date`、`source`（**真实数据源 ID**，严格按该源隔离取数：clash 源读 `connections WHERE source=该ID`、ipfix 源读 `flow_slots WHERE source=该ID`；未知 / 空源返回空结果，不再有硬编码 `clash`）
- `/api/sources` - `GET` 返回在线状态页跟随的最小信息：`{"default":"<在线默认源ID>","default_name":"<显示名>","has_sources":true/false}`。v3.0.0 取消下拉后不再返回候选列表；`default` 取「在线默认」单选源，未显式设置时回落第一个启用「采集」的源；所有源恒可被设为在线默认，不按 `enabled` 过滤
- `/api/data-sources` - 数据源定义：`GET` 读取全部、`PUT` 新增 / 更新、`DELETE?id=` 删除。三个开关字段：`enabled`（采集，只控制是否起采集器落库）、`in_summary`（汇总，只控制该 clash 源是否进入目标 / 客户端统计，ipfix 强制 0）、`is_default`（在线默认，单选，设默认时后端清掉其它源）。含 `thresholds` / `custom_thresholds`（自定义在线热度阈值，5 个逗号分隔的递增正整数，`PUT` 时校验）。凭据：`GET` 只返回 `credential_configured` / `credential_source`（`env` / `table` / 空），**不回显明文或混淆串**；`PUT` 用只写字段 `credential_secret` 收明文密钥（混淆后存表、留空表示不修改），或用 `credential_ref` 指向环境变量（env 优先）
- `/api/client-names` - 客户端名称：`GET` 读取（返回 `names` 与 `knownIPs`）、`PUT` 设置 / 批量导入、 `DELETE?ip=` 清除手动配置
- `/refresh` - 手动触发一次数据采集；无启用的 Clash 数据源时返回 `400`，提示先到数据源页添加

## 公共查询参数

统计 / 明细类接口（`/api/stats`、`/api/sourceip-stats`、`/api/target-detail`、`/api/sourceip-detail`）共用这些参数：

| 参数 | 说明 |
|---|---|
| `startTime` / `endTime` | `datetime-local` 格式（`2006-01-02T15:04`），按本地时区解析 |
| `q` | 按表格第一列模糊搜索，交给服务端过滤 |
| `sort` | 排序字段白名单：`target` / `download` / `upload` / `total` / `connections` / `peers` / `first` / `last`；明细接口的第一列用 `key`。不在白名单里的值会回落到默认排序 |
| `order` | `asc` / `desc`，默认 `desc` |

## 响应结构

统计 / 明细类接口返回 `{Rows, Total, Truncated}`：`Total` 是符合条件的真实条数，`Truncated` 表示被单表上限（500 行，定义在 `database.maxTableRows`）截断。排序发生在 `LIMIT` 之前，所以截断的是"排好序的前 500 行"。

`/api/gantt` 返回 `[]GanttRow`，每行 `{SourceIP, SlotIndexes, SlotValues, RawValues}`：只传非零时间槽，三个数组一一对应（下标即 5 分钟槽序号）。**`SlotValues` 是 1–6 的活跃强度档位**，不是绝对连接数 —— 按当前跟随源自己的阈值（默认 `2,20,50,150,254`，可在管理页按源自定义）归一化，颜色只表示相对本源有多忙；`RawValues` 是该槽的真实并发连接数（Clash 为代理并发连接，推送源为按五元组去重的并发连接），供前端悬停 / 峰值展示绝对数值。v3.0.0 起一次只展示「在线默认」那一个源、按该源 ID 严格隔离取数。详见 [多数据源在线状态](data-sources.md)。
