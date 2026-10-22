# Prometheus 监控指标

## 目标与结构

`core/gxymetrics` 是节点固定加载的基础 App，独立暴露 `/metrics` 和 Go pprof：

```text
gateway/role/chat/friend/guild
  └─ gxymetrics HTTP（各节点独立端口）
       ├─ /metrics
       └─ /debug/pprof/*
             ↓ 15s scrape
      Prometheus :9092 → Grafana :3000
```

- 配置入口：`config/*.toml` 的 `[metrics]`
- 指标定义：`core/gxymetrics/collectors.go`
- 注册与 HTTP 服务：`core/gxymetrics/metrics.go`
- Prometheus 配置：`deploy/docker/prometheus.yml`
- Grafana Dashboard：`deploy/docker/grafana/dashboards/gserver-metrics.json`
- 同步阻塞采样阈值为 10ms；mutex 采样率为 1%

## 指标清单

### 基础设施

| 指标 | 类型 | 标签 | 来源 | 说明 |
|------|------|------|------|------|
| `tcp_connections` | Gauge | `role` | `core/gxynet/peer` | server/connector 当前连接数 |
| `actor_active_count` | Gauge | `kind` | `core/gxyactor` | 当前活跃 Actor 数 |
| `actor_messages_total` | Counter | `kind` | `core/gxyactor` | Actor 处理消息数 |
| `actor_message_duration_seconds` | Histogram | `kind` | `core/gxyactor` | Actor 消息处理耗时 |
| `db_query_duration_seconds` | Histogram | — | `core/gxypgx` | GORM 查询与写入耗时 |
| `redis_request_duration_seconds` | Histogram | `cmd` | `core/gxyredis` | Redis 命令耗时 |
| `redis_errors_total` | Counter | `cmd` | `core/gxyredis` | Redis 命令失败数（`redis.Nil` 不计） |
| `mq_queue_depth` | Gauge | — | `core/gxymq` | 待处理消息数 |
| `mq_queue_capacity` | Gauge | — | `core/gxymq` | 队列容量（`depth/capacity` 即饱和度） |
| `mq_handler_errors_total` | Counter | — | `core/gxymq` | 消息处理失败数（不重试，直接丢弃） |

### 网关与 Role

| 指标 | 类型 | 标签 | 来源 | 说明 |
|------|------|------|------|------|
| `online_players` | Gauge | — | Gateway Session | 当前节点已握手在线玩家数 |
| `gateway_packets_total` | Counter | `type`, `result` | `core/gxynet/peer/tcpserver.go` | 网关入口：`type=message`（解码成功、进入处理）或 `frame`（解码/长度失败）；`result=ok/error` |
| `session_disconnect_total` | Counter | `reason` | Gateway Session | Session 断开原因 |
| `client_requests_total` | Counter | `msg_id`, `msg_name`, `result` | `role_main.go` | 客户端协议请求量 |
| `client_request_duration_seconds` | Histogram | `msg_id`, `msg_name`, `result` | `role_main.go` | 客户端协议处理耗时 |
| `role_login_total` | Counter | `result` | `role_main.go` | 登录结果 |
| `role_logout_total` | Counter | `reason` | `role_main.go` | 登出原因 |

### 跨模块路由

| 指标 | 类型 | 标签 | 来源 | 说明 |
|------|------|------|------|------|
| `role_notify_publish_total` | Counter | `msg_type`, `result`, `target` | `src/lib/rolelib/rolelib.go` | RoleNotify 发布结果 |
| `role_notify_consume_total` | Counter | `msg_type`, `result` | `src/lib/rolelib/rolelib.go` | RoleNotify 消费结果 |
| `actor_locate_total` | Counter | `kind`, `result` | `activator_manager.go` | Actor 定位命中、未命中与节点失效 |

## 分桶与 exemplar

- `actor_message_duration_seconds`、`db_query_duration_seconds`、`redis_request_duration_seconds` 使用**亚毫秒分桶**（0.1ms ~ 5s）。`prometheus.DefBuckets` 从 5ms 起步，这三类样本会几乎全部落进第一个桶，做延迟优化时没有分辨率。
- `actor_message_duration_seconds`、`client_request_duration_seconds`、`db_query_duration_seconds`、`redis_request_duration_seconds` 通过 `gxymetrics.ObserveWithTrace` 附带 **exemplar**（`trace_id`），可从慢样本直接跳到对应 trace（DB 能看到具体 SQL、Redis 能看到是哪个脚本）。只在被采样的链路上有值；需要在 Prometheus 侧开启 `exemplar-storage`。
- `db_query_duration_seconds` 目前没有标签：分不出读写、也分不出归属 fencing 与业务查询（fencing 走裸 SQL，已由 Raw/Row 回调覆盖计数）；定位单条慢查询依赖 exemplar。

## 追踪与指标的边界

- **聚合看指标**：调用次数、延迟分位、失败率（Redis 按命令名；DB 目前总量）。
- **单请求分解看 span**：DB 每次查询、Redis 每条命令各一个 Client span（`postgres.query` / `redis.<cmd>`），只在**被采样的链路**里产生；没有采样中的父 span 时不会新开 trace——心跳续约、启动探活、定时任务因此不会各自变成一条单 span 的 trace。
- **span 预算**：一条链路通常 ≤10 个（DB 1~5、Redis 0~3）。出现"逐项循环"时用 `gxytrace.WithoutClientSpans(ctx)` 抑制逐条 span，用一个阶段 span + 属性记数量（如 `items=N`），明细交给指标。
- **不记录原始 SQL 与参数值**：只记 `db.system`、`db.operation`（SELECT/INSERT…）、`db.sql.table`；`redis.Nil` 与 `ErrRecordNotFound` 不算错误。
- **MQ**：消息 payload 带 `traceparent`。点对点通知（1:1，`PublishRoleNotify`）消费端以它为**远端父**继续链路；广播（1:N，`Publish`/`PublishToAll`）消费端开独立 trace 并用 **link** 指回生产者，避免单条 trace 的分支数随节点数膨胀。

## 标签约束

- 标签必须是稳定、低基数字符串
- 禁止把 `role_id`、`account_id`、`guild_id`、原始错误文本作为标签
- `result` 使用 `ok`、`error`、`ignored`、`offline`、`not_found`、`node_dead`、`hit`、`miss` 等固定值
- `target` 使用 `local`、`remote`、`offline`、`invalid`、`unknown`
- `type`（网关入口）使用 `message`、`frame`
- `msg_id` 使用协议 ID 字符串，`msg_name` 使用 protobuf 消息名

## 配置

每个节点使用不同端口：

```toml
[metrics]
addr = ":9090"
path = "/metrics"
enabled = true
```

本地 Prometheus 使用 `game-services` job 抓取五个业务节点：

```yaml
scrape_configs:
  - job_name: game-services
    scrape_interval: 15s
    static_configs:
      - targets:
          - host.docker.internal:9090 # gate
          - host.docker.internal:9091 # role
          - host.docker.internal:9093 # chat
          - host.docker.internal:9094 # friend
          - host.docker.internal:9095 # guild
```

`node` 标签由 `deploy/docker/prometheus.yml` 的每个 target 单独配置。

## 常用 PromQL

```promql
# 客户端总 QPS
sum(rate(client_requests_total[1m]))

# 每个协议 p95
histogram_quantile(
  0.95,
  sum by (msg_name, le) (
    rate(client_request_duration_seconds_bucket[5m])
  )
)

# 当前在线玩家
sum(online_players)

# RoleNotify 路由结果
sum by (target, result) (rate(role_notify_publish_total[1m]))

# Actor 定位健康
sum by (kind, result) (rate(actor_locate_total[1m]))

# DB p99
histogram_quantile(0.99, rate(db_query_duration_seconds_bucket[5m]))

# Redis GET p95
histogram_quantile(0.95, rate(redis_request_duration_seconds_bucket{cmd="get"}[5m]))
```

## 本地查看与 pprof

```bash
docker compose -f deploy/docker/docker-compose.yml up -d

go tool pprof http://localhost:9090/debug/pprof/profile?seconds=30
go tool pprof http://localhost:9090/debug/pprof/heap
go tool pprof http://localhost:9090/debug/pprof/block
go tool pprof http://localhost:9090/debug/pprof/mutex
```

Grafana 地址为 `http://localhost:3000`，Prometheus 地址为 `http://localhost:9092`。

> `/metrics` 与 `/debug/pprof/*` **同端口且无鉴权**，`[metrics] addr` 默认监听所有网卡。pprof 的 CPU/heap 采样是可被外部触发的重操作，也会暴露进程内部数据——生产环境不要把该端口暴露到公网，或用网络策略限制到监控网段。

## 添加指标

1. 在 `core/gxymetrics/collectors.go` 定义指标，标签保持低基数
2. 在 `core/gxymetrics/metrics.go` 的 `prometheus.MustRegister` 中注册
3. 在最终业务边界埋点，成功与失败路径都更新指标
4. 更新本文档和 Grafana Dashboard
