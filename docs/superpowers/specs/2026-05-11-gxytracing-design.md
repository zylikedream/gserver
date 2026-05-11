# gxytracing — OpenTelemetry 分布式追踪设计

## 概述

在 gserver 中接入 OpenTelemetry 分布式追踪，trace 数据导出到 Tempo，通过 Grafana 统一查看。

## 架构

```
TCP 客户端 → Gateway Actor
                │
           ReceiverMiddleware
          （无 trace context → 自动创建根 span）
                │
           SenderMiddleware
          （注入 trace context 到 MessageEnvelope.Header）
                ↓
           Role Actor / Guild Actor / ⋯
                │
           ReceiverMiddleware
          （提取 trace context → 创建子 span）
                │
           SenderMiddleware (再次注入)
                ↓
           ⋯ 链路自动串联
```

**关键结论**：protoactor-go 自带的 `opentelemetry.TracingMiddleware()`（SpawnMiddleware + SenderMiddleware + ReceiverMiddleware）覆盖 actor 间全链路。入口点（TCP gateway）收到首条消息时 ReceiverMiddleware 发现无 trace context，自动创建根 span，无需手动处理。

唯一需要手动注入的场景是 actor 内部调用外部 HTTP 服务（见后文）。

## 改动清单

### 1. 新增包 `core/gxytrace/`

TracerProvider 初始化，其他代码不依赖此包（middleware 通过 `otel.SetTracerProvider` 全局注册）。

**trace.go** — Init + Shutdown：

```go
func InitTracerProvider(ctx context.Context, serviceName, endpoint string) (func(), error) {
    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(endpoint),
        otlptracegrpc.WithInsecure(),
    )
    res, _ := resource.Merge(resource.Default(),
        resource.NewWithAttributes(semconv.SchemaURL,
            semconv.ServiceName(serviceName),  // "gate" / "game"
        ))
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(res),
    )
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})
    return func() { _ = tp.Shutdown(ctx) }, nil
}
```

**carrier.go** — MessageEnvelope header 的 TextMapCarrier 实现（入口点手动注入用，如果不需要可省略）：

```go
type envelopeCarrier struct{ header map[string]string }

func (c envelopeCarrier) Get(key string) string     { return c.header[key] }
func (c envelopeCarrier) Set(key, val string)       { c.header[key] = val }
func (c envelopeCarrier) Keys() []string {
    keys := make([]string, 0, len(c.header))
    for k := range c.header { keys = append(keys, k) }
    return keys
}
```

### 2. 修改 `core/gxyactor/system.go` — 挂载 Middleware

在 `OnModInit` 中，`NewActorSystem` 之后配置 TracingMiddleware：

```go
func (a *actorApp) OnModInit(ctx context.Context) error {
    a.system = actor.NewActorSystem(actor.WithLoggerFactory(glogAdapterLogging))
    config := remote.Configure(a.host, 0)
    a.remote = remote.NewRemote(a.system, config)
    a.remote.Start()

    // 追加 tracing middleware 到 RootContext
    a.system.Root = a.system.Root.WithSpawnMiddleware(opentelemetry.TracingMiddleware())

    a.activatorMgr = NewActivatorManager(a.nodeName, a.nodeInstanceName)
    a.AddModule(ctx, a.activatorMgr)
    return nil
}
```

`TracingMiddleware()` 内部展开为：
- `SpawnMiddleware` — 新 actor 继承父 actor 的 span
- `SenderMiddleware` — 发送消息时注入 trace context 到 envelope header
- `ReceiverMiddleware` — 收到消息时提取 trace context 创建子 span

三者组合后，所有通过 `system.Root` 创建和通信的 actor 自动获得链路追踪。

### 3. 修改 `core/gxyactor/actor.go` — 将 active span 注入到 a.ctx

ReceiverMiddleware 创建 span 后通过 `setActiveSpan(c.Self(), span)` 保存，HandleMessage 需要能拿到这个 span。在 `ActorInitMsg` 阶段桥接过来：

```go
case *ActorInitMsg:
    // 把 middleware 的 active span 注入到 actor context
    // GetActiveSpan 在没有 span 时也会创建新 span，保证不返回 nil
    span := opentelemetry.GetActiveSpan(a.Actx)
    a.ctx = trace.ContextWithSpan(a.ctx, span)
    a.msgHandler.AddHandler(a.actor)
    if err := a.actor.DelayInit(a.ctx); err != nil {
        return gerror.Wrap(err, "delay init actor error")
    }
```

之后 HandleMessage 收到的 `ctx` 就带 trace context，可以做链路关联：

```go
func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
    span := trace.SpanFromContext(ctx)
    span.SetAttributes(attribute.String("role_id", r.Id()))
    // ...
}
```

### 4. 新增配置项 — `[trace]`

在 TOML 中配置 OTLP 端点：

```toml
[trace]
    endpoint = "localhost:4317"
```

所有节点共用。Tempo 默认端口 4317（gRPC）。**

### 5. TracerProvider 初始化时机

采用与 `gxymetrics` 相同的 App 模块模式：

```go
func NewTraceApp(nodeName string) *traceApp {
    // 从 config 读取 endpoint
    ...
}

func (t *traceApp) OnModInit(ctx context.Context) error {
    shutdown, err := gxytrace.InitTracerProvider(ctx, t.nodeName, t.endpoint)
    ...
}
```

在 `node.main.go` 中注册为 app，TracingMiddleware 依赖全局 `otel.SetTracerProvider`，所以 `gxytrace` 必须在 `gxyactor` 之前初始化。

**初始化顺序要求**：
1. `gxymodule`（基础框架）
2. `gxytrace`（设置全局 TracerProvider ← 必须先初始化）
3. `gxyactor`（创建 ActorSystem + 挂载 TracingMiddleware ← 依赖 TracerProvider）
4. `gxymq` / `gxypgx` / `gxyredis` / 业务 apps

### 6. 基础设施 — Docker Compose

新增 Tempo 服务：

```yaml
tempo:
    image: grafana/tempo:latest
    container_name: gserver-tempo
    command: ["-config.file=/etc/tempo.yaml"]
    ports:
      - "4317:4317"    # OTLP gRPC
      - "4318:4318"    # OTLP HTTP
    volumes:
      - ./tempo/tempo.yaml:/etc/tempo.yaml
      - tempo-data:/tmp/tempo
    restart: unless-stopped
```

Tempo 配置 `docker/tempo/tempo.yaml`：

```yaml
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: "0.0.0.0:4317"

ingester:
  trace_idle_period: 10s
  max_block_duration: 30m

compactor:
  compaction:
    block_retention: 24h
```

### 7. Grafana 数据源 — Tempo

`docker/grafana/provisioning/datasources/datasources.yml` 追加：

```yaml
  - name: Tempo
    type: tempo
    access: proxy
    url: http://tempo:3200
```

### 8. HTTP 调用（可选）

如果 actor 需要调用外部 HTTP 服务并保持链路，用 `otelhttp` 包装：

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

var httpClient = &http.Client{
    Transport: otelhttp.NewTransport(http.DefaultTransport),
}

func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
    // ctx 自带 trace context，otelhttp 自动注入到 HTTP headers
    resp, _ := httpClient.Get("https://api.example.com/data")
    // ...
}
```

## 数据流示例

```
Trace: abc123
├── Span: *Session/HandleClientMsg          (ReceiverMiddleware 自动创建根 span)
│   ├── Span: *ChannelActor/HandleMessage    (SenderMiddleware → ReceiverMiddleware)
│   │   └── Span: HTTP GET /api/data         (otelhttp 自动注入)
│   └── Span: *RoleMain/HandleMessage        (SenderMiddleware → ReceiverMiddleware)
│       └── Span: *GuildActor/HandleMessage  (再次传递)
```

每个 span 自动带有 ActorPID、ActorType、MessageType 属性。

## 依赖

当前 go.mod 中 OTel 为 indirect，需转为 direct 并新增：
- `go.opentelemetry.io/otel` → direct
- `go.opentelemetry.io/otel/sdk` → direct
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` → new
- `go.opentelemetry.io/otel/semconv` → new
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` → 可选

## 实施步骤

1. 创建 `core/gxytrace/` 包（TracerProvider + carrier）
2. 创建 `gxytrace` app 模块，注册到 node.main.go
3. `go.mod` 添加 OTel 直接依赖
4. 修改 `gxyactor/system.go` — 挂载 TracingMiddleware
5. 修改 `gxyactor/actor.go` — 桥接 active span 到 a.ctx
6. 配置 TOML `[trace]` 段
7. docker-compose 添加 Tempo
8. Grafana provisioning 添加 Tempo 数据源
9. 部署验证：启动 → 触发 actor 通信 → Tempo 查询
