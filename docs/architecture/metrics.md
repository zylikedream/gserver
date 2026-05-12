# Prometheus 监控指标设计

## 背景

GServer 基于分布式 Actor 模型，网关（gate）和游戏逻辑（game）以独立进程运行。需要一套标准化的指标采集体系，覆盖网络连接、Actor 生命周期、数据库/Redis 访问等关键路径，辅助容量规划和性能排查。

## 整体方案

```
┌─────────────────────────────────────────────────┐
│ gserver 节点                                     │
│                                                  │
│  gxymetrics (独立 HTTP :9090/9091)              │
│    ├─ collectors.go  定义 6 个 Prometheus 指标   │
│    └─ metrics.go     注册 + 启动 HTTP server     │
│                                                  │
│  采集点:                                         │
│    gxynet/     → TCP 连接数 (Gauge)              │
│    gxyactor/   → Actor 数量/消息数/延迟 (G/C/H)  │
│    gxypgx/     → DB 查询延迟 (Histogram)         │
│    gxyredis/   → Redis 命令延迟 (Histogram)      │
├──────────────────────────────────────────────────┤
│                                                  │
│  Prometheus (:9092) ← 15s 抓取 ──────────────────┤
│  Grafana (:3000)    ← 查询 Prometheus            │
│  Tempo (:4317)      ← OTLP 接收 trace            │
│                                                  │
│  docker/docker-compose.yml                       │
│  docker/prometheus.yml                           │
└─────────────────────────────────────────────────┘
```

- **指标模块** `gxymetrics` 作为 App 模块注册，在所有基础设施模块之前加载（`node.go` 硬依赖顺序）
- 独立 HTTP 端口暴露 `/metrics`，不与业务端口混用
- 开启 Go runtime block/mutex profiling（block ≥10ms, mutex 1%）

## 指标列表

| 指标名 | 类型 | 标签 | 来源 | 说明 |
|--------|------|------|------|------|
| `tcp_connections` | Gauge | `role` (server/connector) | `gxynet/peer/` | 当前 TCP 连接数 |
| `actor_active_count` | Gauge | `kind` | `gxyactor/actor.go` | 活跃 Actor 数量 |
| `actor_messages_total` | Counter | `kind` | `gxyactor/actor.go` | Actor 处理消息总数 |
| `actor_message_duration_seconds` | Histogram | `kind` | `gxyactor/actor.go` | 消息处理耗时分布 |
| `db_query_duration_seconds` | Histogram | — | `gxypgx/metrics.go` | GORM 数据库查询耗时 |
| `redis_request_duration_seconds` | Histogram | `cmd` | `gxyredis/metrics.go` | Redis 命令耗时 |

`kind` 标签值为 `ActorBase` 初始化时传入的 `actorKind` 字符串（如 `role`、`guild`、`chat`、`session` 等）。

## 埋点方法

### TCP 连接数

在 `gxynet` 的连接回调中直接 Inc/Dec：

```go
// tcpserver.go — 监听侧连接
func (s *TcpServer) OnOpen(conn net.Conn) {
    gxymetrics.TcpConnections.WithLabelValues("server").Inc()
}
func (s *TcpServer) OnClose(conn net.Conn) {
    gxymetrics.TcpConnections.WithLabelValues("server").Dec()
}

// tcpconnector.go — 主动连接侧
// 同理，label 用 "connector"
```

### Actor 指标

在 `ActorBase.Receive` 的生命周期消息中采集，业务代码无需关心：

```go
// actor.go
case *actor.Started:
    gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Inc()
case *actor.Stopped:
    gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Dec()
```

消息处理在 `handleMessage` 统一采集：

```go
func (a *ActorBase) handleMessage(msg any) error {
    start := time.Now()
    err := a.actor.HandleMessage(a.ctx, msg)
    gxymetrics.ActorMessages.WithLabelValues(a.ActorKind()).Inc()
    gxymetrics.ActorMessageDuration.WithLabelValues(a.ActorKind()).Observe(time.Since(start).Seconds())
    return err
}
```

### 数据库查询耗时

GORM 插件模式，注册 before/after callback：

```go
// gxypgx/metrics.go
type metricsPlugin struct{}

func (p *metricsPlugin) Name() string { return "gxymetrics" }

func (p *metricsPlugin) Initialize(db *gorm.DB) error {
    // 注册 Query/Create/Update/Delete 的 Before/After callback
    // After callback 中: DBQueryDuration.Observe(seconds)
}
```

使用时对 DB 实例注册一次即可：

```go
db.Use(&gxypgx.metricsPlugin{})
```

### Redis 命令耗时

Redis Hook 模式，拦截 ProcessHook 和 ProcessPipelineHook：

```go
// gxyredis/metrics.go
type metricsHook struct{}

func (h *metricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        start := time.Now()
        err := next(ctx, cmd)
        gxymetrics.RedisRequestDuration.WithLabelValues(cmd.FullName()).Observe(time.Since(start).Seconds())
        return err
    }
}
// Pipeline 类似，label 固定为 "pipeline"
```

使用时添加到 Redis client：

```go
client.AddHook(&gxyredis.metricsHook{})
```

## 配置

### 节点侧

`config/*.toml` 中配置 metrics 模块：

```toml
[metrics]
addr = ":9090"      # 不同节点用不同端口
path = "/metrics"
enabled = true
```

### Prometheus 抓取

`docker/prometheus.yml`：

```yaml
scrape_configs:
  - job_name: 'gserver'
    scrape_interval: 15s
    static_configs:
      - targets: ['host.docker.internal:9090']
        labels:
          node: 'gate'
      - targets: ['host.docker.internal:9091']
        labels:
          node: 'game'
```

`node` 标签用于 Grafana 面板中按节点筛选。

## Grafana 查看

### 访问方式

1. 启动监控栈：`cd docker && docker-compose up -d`
2. 访问 Grafana：`http://localhost:3000`
3. 数据源已自动配置（Prometheus + Tempo）

### 预置 Dashboard

**GServer Metrics** 面板（UID: `fd163a53`）包含 8 个面板：

| 面板 | PromQL | 说明 |
|------|--------|------|
| TCP Connections | `tcp_connections{node="$node"}` | 当前连接数时序 |
| Active Actors | `actor_active_count{node="$node"}` | 各类 Actor 数量 |
| Message Rate | `rate(actor_messages_total[1m])` | 每秒消息处理量 |
| Message Duration p95 | `histogram_quantile(0.95, rate(actor_message_duration_seconds_bucket[5m]))` | 消息处理延迟 |
| DB Query Duration p95 | `histogram_quantile(0.95, rate(db_query_duration_seconds_bucket[5m]))` | 数据库查询延迟 |
| Redis Duration p95 | `histogram_quantile(0.95, rate(redis_request_duration_seconds_bucket[5m]))` | Redis 延迟 |
| CPU % | `process_cpu_seconds_total` | 进程 CPU 使用率 |
| Goroutines | `go_goroutines` | 协程数量 |

### Dashboard 变量

| 变量 | 类型 | 用途 |
|------|------|------|
| `node` | label_values(tcp_connections) | 按节点筛选 |
| `instance` | label_values(up) | 按实例筛选 |

### 常用查询示例

```promql
# 查看某类 Actor 的消息处理速率
rate(actor_messages_total{kind="role"}[5m])

# 查看数据库查询 p99 延迟
histogram_quantile(0.99, rate(db_query_duration_seconds_bucket[5m]))

# 查看 Redis GET 命令延迟
histogram_quantile(0.95, rate(redis_request_duration_seconds_bucket{cmd="get"}[5m]))

# 查看当前在线玩家数（通过 session actor 数量）
actor_active_count{kind="session"}
```

### pprof 性能分析

Metrics HTTP 端口同时暴露 Go pprof 端点：

```bash
# CPU profile（30秒采样）
go tool pprof http://localhost:9090/debug/pprof/profile?seconds=30

# 内存分配
go tool pprof http://localhost:9090/debug/pprof/heap

# 协程阻塞分析（需 block profile 开启）
go tool pprof http://localhost:9090/debug/pprof/block

# 互斥锁竞争
go tool pprof http://localhost:9090/debug/pprof/mutex

# 在线分析（启动本地 web UI）
go tool pprof -http=:8080 http://localhost:9090/debug/pprof/profile?seconds=30
```

## 添加新指标

1. 在 `core/gxymetrics/collectors.go` 中定义指标变量
2. 在 `core/gxymetrics/metrics.go` 的 `OnModInit` 中 `prometheus.MustRegister`
3. 在业务代码中引入并使用（`WithLabelValues` → `Inc/Dec/Observe`）
