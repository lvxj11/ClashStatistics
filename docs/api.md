# API 接口

## 接口清单

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

## 公共查询参数

统计 / 明细类接口（`/api/stats`、`/api/sourceip-stats`、`/api/target-detail`、`/api/sourceip-detail`）共用这些参数：

| 参数 | 说明 |
|---|---|
| `startTime` / `endTime` | `datetime-local` 格式（`2006-01-02T15:04`），按本地时区解析 |
| `q` | 按表格第一列模糊搜索，交给服务端过滤 |
| `sort` | 排序字段白名单：`target` / `download` / `upload` / `total` / `connections` / `peers` / `first` / `last`；明细接口的第一列用 `key`。不在白名单里的值会回落到默认排序 |
| `order` | `asc` / `desc`，默认 `desc` |

## 响应结构

返回 `{Rows, Total, Truncated}`：`Total` 是符合条件的真实条数，`Truncated` 表示被单表上限（500 行，定义在 `database.maxTableRows`）截断。排序发生在 `LIMIT` 之前，所以截断的是"排好序的前 500 行"。
