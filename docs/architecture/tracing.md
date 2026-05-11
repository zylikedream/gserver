# OpenTelemetry 链路追踪设计

## 背景

在基于 protoactor-go 的 Actor 模型中，消息在不同 Actor 之间异步传递。当一次用户请求需要经过 gateway → role → guild 等多个 actor 时，缺乏跨 actor 的调用链追踪，无法定位延迟瓶颈和排错。

目标：在不动原有 actor 通信模式的前提下，实现跨 actor 的 trace 传递。

## Protoactor-go 内置 OTel middleware 分析

protoactor-go 在 `actor/middleware/opentelemetry` 下提供了内置的 OTel 集成，实现方式是三层 middleware：

1. **SenderMiddleware** — 发送消息时，通过 `getActiveSpan(c.Self())` 从全局 `sync.Map` 中查找当前 actor 的 span，注入到 `MessageEnvelope` header
2. **ReceiverMiddleware** — 收到消息时，从 header 提取 trace 上下文，创建子 span 并通过 `setActiveSpan(c.Self(), span)` 存入 `sync.Map`
3. **SpawnMiddleware** — Spawn 子 actor 时处理 span 继承

### 放弃使用的原因

| 问题 | 说明 |
|------|------|
| **全局 sync.Map 查找** | 每次 Send/Call 都走一次 `sync.Map.Load`，即使没有 trace 也在查 |
| **跨 actor 状态耦合** | span 通过 PID → sync.Map 关联，actor 销毁时需要清理 map，异常路径容易泄漏 |
| **中间层过多** | RequestFuture 场景下会嵌套 envelope，内置 middleware 需要复杂逻辑辨认外层 |
| **ReceiverMiddleware 语义不符** | 内置 ReceiverMiddleware 把提取到的 span 写回 sync.Map 而非 actor 自身的上下文 |
| **与项目的 module 系统集成不足** | 需要额外配置才能与现有生命周期绑定 |

核心矛盾：内置 middleware 以"中间件"方式工作，本质是全局拦截，而我们需要的是"span 随消息流自然传递"。

## 设计方案

### 核心思想

**span 存在 actor 的 context 中，不存全局 map。**

```
每条消息到达 → 从 header 提取 trace → 创建 span → 存入 a.ctx → 下游 Send/Call 读到 → inject 到出站消息 → 消息处理完 → 清理 a.ctx 中的 span
```

### span 存储：a.ctx

`ActorBase` 持有 `ctx context.Context`，所有 actor 方法共享。span 通过 `trace.ContextWithSpan(a.ctx, span)` 挂载到 a.ctx 上，下游代码通过 `a.Context()` 或 `a.ctx` 获取。

关键约束：a.ctx 是**持久化字段**，span 必须在消息处理完后清理，否则会泄漏到后续无关消息（如 timer 回调）。

```go
// default case — 业务消息处理
carrier := readonlyHeaderCarrier{a.Actx.MessageHeader()}
extCtx := otel.GetTextMapPropagator().Extract(a.ctx, carrier)
_, span := otel.Tracer("gserver/actor").Start(extCtx, fmt.Sprintf("%T", msg))
defer span.End()
savedCtx := a.ctx
a.ctx = trace.ContextWithSpan(a.ctx, span)
defer func() { a.ctx = savedCtx }()
```

defer 顺序保证：
1. `a.ctx = savedCtx` — 先恢复无 span 的 context
2. `span.End()` — 再结束 span

### trace header 注入：injectTrace

`helper.go` 中的 Send、Call、LocalSend、CallSync 四个出口函数统一通过 `injectTrace` 注入 trace header：

```
injectTrace(ctx, msg)
  → trace.SpanFromContext(ctx)
  → SpanContext.IsValid()? → 否：返回 nil（不包装）
  → 是：创建 MessageEnvelope，注入 W3C Trace Context header
  → 发送 envelope 而非裸消息
```

```go
func injectTrace(ctx context.Context, msg any) *actor.MessageEnvelope {
    span := trace.SpanFromContext(ctx)
    if !span.SpanContext().IsValid() {
        return nil
    }
    env := &actor.MessageEnvelope{Message: msg}
    carrier := messageEnvelopeCarrier{envelope: env}
    otel.GetTextMapPropagator().Inject(ctx, carrier)
    return env
}
```

### RequestFuture envelope 嵌套处理：tracePropagationMiddleware

`Call` 在 protoactor-go 内部会走 `RequestFuture`，将消息再包一层 envelope。自定义 SenderMiddleware 解决嵌套：

```go
func tracePropagationMiddleware() actor.SenderMiddleware {
    return func(next actor.SenderFunc) actor.SenderFunc {
        return func(c actor.SenderContext, target *actor.PID, envelope *actor.MessageEnvelope) {
            if inner, ok := envelope.Message.(*actor.MessageEnvelope); ok && len(inner.Header) > 0 {
                for _, k := range inner.Header.Keys() {
                    envelope.SetHeader(k, inner.Header.Get(k))
                }
                envelope.Message = inner.Message
            }
            next(c, target, envelope)
        }
    }
}
```

注册在 `ActorSystem.Root`，所有出站消息经过：

```go
a.system.Root.WithSenderMiddleware(tracePropagationMiddleware())
```

### header 提取与适配

protoactor-go 的 `MessageHeader` 是 `ReadonlyMessageHeader` 接口，只提供 `Get`/`Keys`，没有 `Set`。定义适配器实现 `propagation.TextMapCarrier`：

```go
type readonlyHeaderCarrier struct {
    actor.ReadonlyMessageHeader
}

func (readonlyHeaderCarrier) Set(key, value string) {} // 只读，no-op
```

写入侧使用 `messageEnvelopeCarrier` 适配可写的 `MessageHeader`：

```go
type messageEnvelopeCarrier struct {
    envelope *actor.MessageEnvelope
}
func (c messageEnvelopeCarrier) Get(key string) string { return c.envelope.GetHeader(key) }
func (c messageEnvelopeCarrier) Set(key, val string)   { c.envelope.SetHeader(key, val) }
func (c messageEnvelopeCarrier) Keys() []string        { return c.envelope.GetHeaderKeys() }
```

### 根 span 创建

当消息入口（如 TCP gateway 收到客户端请求）没有父 trace 时，`Extract` 没有可提取的上下文，`Start` 自动创建新 trace 的根 span。不需要额外处理。

### Timer / 系统消息的隔离

非 default case（ActorTimerMsg、Stopped 等）不创建 span。由于 a.ctx 已在 default case 结束时清理干净，这些路径不会意外继承 stale span。

## 数据流

```
gateway OnMessage(ctx=context.Background())
  → Send(ctx, rolePID, msg)
    → injectTrace: no valid span → 不包装
    → 发送裸消息

role actor doReceive default case
  → header 为空 → Extract 无数据
  → Start → 创建根 span（新 trace）
  → a.ctx = trace.ContextWithSpan(a.ctx, span)
  → HandleMessage

  处理中调用 Send(a.ctx, guildPID, req)
    → injectTrace: 读到 span → 注入 W3C Trace Context
    → 发送带 header 的 MessageEnvelope

guild actor doReceive default case
  → header 有 trace 数据
  → Extract → 还原父 span context
  → Start → 创建子 span（继承同一个 trace）
  → a.ctx = trace.ContextWithSpan(a.ctx, span)
  → HandleMessage

  返回 → defer 清理 a.ctx → defer span.End()
```

## 与内置 middleware 的关键差异

| | 内置 OTel middleware | 我们的方案 |
|--|-------------------|-----------|
| 存储 | sync.Map (PID → span) | a.ctx 字段 |
| 查找 | 每次 Send 都 Load sync.Map | trace.SpanFromContext(ctx) —— 直接读取 context 链 |
| 耦合 | 全局 → 所有 actor 共享 map | 局部 → 每个 actor 独立 |
| 清理 | 需要显式调用 delete | 消息处理完 defer 自动恢复 |
| 嵌套 envelope | 需要额外适配 | 自定义 SenderMiddleware 一层解决 |
| 配置 | 三层 middleware 需全部注册 | 一个 middleware + 入口函数封装 |

## tracer 名称

使用 `gserver/actor` 作为 tracer name，span name 使用消息类型名 `fmt.Sprintf("%T", msg)`。
