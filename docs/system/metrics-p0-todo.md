# P0 Metrics TODO

## Goal

补齐首批 P0 监控指标，用于观察入口 QPS、协议耗时、在线规模、连接断开、RoleNotify 投递，以及 Actor 定位健康。

## Existing Metrics

当前已存在并继续保留：

| 指标 | 类型 | 说明 |
|---|---|---|
| `tcp_connections` | Gauge | 当前 TCP 连接数 |
| `actor_active_count` | Gauge | 当前活跃 Actor 数 |
| `actor_messages_total` | Counter | Actor 处理消息总数 |
| `actor_message_duration_seconds` | Histogram | Actor 消息处理耗时 |
| `db_query_duration_seconds` | Histogram | GORM 查询/写入耗时 |
| `redis_request_duration_seconds` | Histogram | Redis 命令耗时 |

## P0 Implemented

| 指标 | 类型 | 标签 | 埋点位置 | 说明 |
|---|---|---|---|---|
| `online_players` | Gauge | - | `gateway/internal/logic/session.go` | 当前节点已握手在线玩家数 |
| `client_requests_total` | Counter | `msg_id`, `msg_name`, `result` | `role/internal/logic/role_main.go` | 客户端协议请求量和成功/失败量 |
| `client_request_duration_seconds` | Histogram | `msg_id`, `msg_name`, `result` | `role/internal/logic/role_main.go` | 客户端协议处理耗时，用于定位慢协议 |
| `gateway_packets_total` | Counter | `type`, `result` | `gxynet/peer/tcpserver.go` | 网关入口包 QPS 和解包失败量 |
| `session_disconnect_total` | Counter | `reason` | `gateway/internal/logic/session.go` | Session 断开原因分布 |
| `role_login_total` | Counter | `result` | `role/internal/logic/role_main.go` | 登录成功/失败量 |
| `role_logout_total` | Counter | `reason` | `role/internal/logic/role_main.go` | 登出原因分布 |
| `role_notify_publish_total` | Counter | `msg_type`, `result`, `target` | `src/lib/role.go` | RoleNotify 发布量，区分 local/remote/offline/error |
| `role_notify_consume_total` | Counter | `msg_type`, `result` | `src/lib/role.go` | RoleNotify 消费和本地投递结果 |
| `actor_locate_total` | Counter | `kind`, `result` | `gxyactor/activator_manager.go` | Actor 定位命中、未命中、节点失效、失败 |

## Suggested Semantics

- `result`: 使用稳定低基数字符串，如 `ok`, `error`, `ignored`, `offline`, `not_found`, `node_dead`, `hit`, `miss`。
- `target`: RoleNotify 使用 `local`, `remote`, `offline`, `invalid`, `unknown`。
- `msg_name`: 使用 protobuf 消息名，如 `ReqBagInfo`。
- `msg_id`: 使用客户端协议 ID 字符串。
- `reason`: 使用归一化低基数字符串，不记录原始错误文本。
- 不要把 `role_id`, `account_id`, `guild_id` 等高基数字段作为标签。

## Useful PromQL

```promql
# 客户端业务 QPS
sum(rate(client_requests_total[1m]))

# 按协议查看 QPS
sum by (msg_name) (rate(client_requests_total[1m]))

# 每个协议 p95 耗时
histogram_quantile(
  0.95,
  sum by (msg_name, le) (
    rate(client_request_duration_seconds_bucket[5m])
  )
)

# 最慢的 10 个协议
topk(10,
  histogram_quantile(
    0.95,
    sum by (msg_name, le) (
      rate(client_request_duration_seconds_bucket[5m])
    )
  )
)

# 错误最多的协议
topk(10,
  sum by (msg_name) (
    rate(client_requests_total{result="error"}[5m])
  )
)

# 当前在线玩家数
sum(online_players)

# RoleNotify 发布 QPS
sum by (target, result) (rate(role_notify_publish_total[1m]))

# Actor 定位健康
sum by (kind, result) (rate(actor_locate_total[1m]))
```

## Notes

- Redis/PostgreSQL 本体健康交给 `redis_exporter` 和 `postgres_exporter`。
- 应用内仍保留 DB/Redis 访问耗时和错误率指标，用于观察业务侧真实延迟。
- 第一批指标已放在 `core/gxymetrics` 统一定义并注册，再分别在 gateway、role、RoleNotify、activator 埋点。
