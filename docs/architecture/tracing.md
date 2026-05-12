# OpenTelemetry 链路追踪设计

## 背景

在基于 protoactor-go 的 Actor 模型中，消息在不同 Actor 之间异步传递。当一次用户请求需要经过 gateway → role → guild 等多个 actor 时，缺乏跨 actor 的调用链追踪，无法定位延迟瓶颈和排错。

目标：在不动原有 actor 通信模式的前提下，实现跨 actor 的 trace 传递。

## 设计思路

**不使用 protoactor-go 的 middleware 机制。** trace 上下文通过消息 header 传递，span 存在 actor 的 `a.ctx` 中，不存全局 map。

```
每条消息到达 → doReceive 从 header 提取 trace → 创建 span → 存入 a.ctx
→ 下游 Send/Call 读到 a.ctx 中的 span → inject 到出站消息 header
→ 消息处理完 → 清理 a.ctx 中的 span
```

## 架构

```
┌──────────────────────────────────────────────────────────┐
│ helper.go                                                │
│  Send/LocalSend/Call/CallSync (exported)                │
│    → 透传 ctx 给 app.send/app.call                       │
├──────────────────────────────────────────────────────────┤
│ system.go                                                │
│  app.send(ctx)           ←── 单一下行出口，所有消息经过   │
│    ├─ injectTrace(ctx, msg) → 有 span? → 发 envelope     │
│    └─ 无 span? → 发裸消息                                │
│                                                          │
│  app.call(ctx)  → 创建 Future envelope → app.send(ctx)   │
│  app.callSync   → 创建 Sender envelope  → app.send(ctx)  │
│  app.localSend  → app.send(ctx)                          │
├──────────────────────────────────────────────────────────┤
│ actor.go                                                 │
│  doReceive default case                                   │
│    → Extract header → Start span → 注入 a.ctx            │
│    → HandleMessage → defer 清理 a.ctx → defer span.End() │
└──────────────────────────────────────────────────────────┘
```

## 组件详解

### 1. 入站：span 创建（actor.go doReceive）

每条业务消息到达 actor 时，从 `MessageHeader` 提取 trace 上下文，创建 span 挂到 `a.ctx`：

```go
// doReceive default case
carrier := readonlyHeaderCarrier{a.Actx.MessageHeader()}
extCtx := otel.GetTextMapPropagator().Extract(a.ctx, carrier)
_, span := otel.Tracer("gserver/actor").Start(extCtx, fmt.Sprintf("%T", msg))
defer span.End()
savedCtx := a.ctx
a.ctx = trace.ContextWithSpan(a.ctx, span)
defer func() { a.ctx = savedCtx }()
```

关键：`a.ctx` 是 ActorBase 的持久化字段，消息处理完后必须恢复，否则 span 会泄漏到后续消息（如 timer 回调）。

### 2. 出站：trace 注入（system.go injectTrace + send）

所有出站消息经过 `app.send`，`injectTrace` 在此处统一处理：

```go
func (a *actorApp) send(ctx context.Context, pid PID, message any) error {
    if env := injectTrace(ctx, message); env != nil {
        a.system.Root.Send(pid, env)
        return nil
    }
    a.system.Root.Send(pid, message)
    return nil
}
```

`injectTrace` 使用 type switch 兼容已包装/未包装两种情况：

```go
func injectTrace(ctx context.Context, msg any) *actor.MessageEnvelope {
    env := &actor.MessageEnvelope{}
    switch msg := msg.(type) {
    case *actor.MessageEnvelope:
        env = msg   // call/callSync 已建好 Future envelope，直接复用
    default:
        env.Message = msg
    }
    span := trace.SpanFromContext(ctx)
    if !span.SpanContext().IsValid() {
        return nil
    }
    carrier := messageEnvelopeCarrier{envelope: env}
    otel.GetTextMapPropagator().Inject(ctx, carrier)
    return env
}
```

- 无有效 span → 返回 nil，send 发裸消息（不产生额外 envelope）
- 有 span → 注入 W3C Trace Context header，发 envelope
- 消息已是 `*MessageEnvelope`（来自 `call`/`callSync`）→ 直接复用，不二次包装

### 3. call/callSync：委托给 send

`call` 和 `callSync` 不再自行管理 Root.Send，而是创建 envelope 后委托给 `send`：

```go
func (a *actorApp) call(ctx context.Context, pid PID, message any, timeout time.Duration) (any, error) {
    future := actor.NewFuture(a.system, timeout)
    env := &actor.MessageEnvelope{
        Message: message,
        Sender:  future.PID(),
    }
    err := a.send(ctx, pid, env)
    if err != nil {
        return nil, err
    }
    return future.Result()
}
```

`callSync` 同理，将 `Sender` 设为调用方传入的 PID。

### 4. 为什么不用 RootContext.RequestFuture

protoactor-go 提供了 `RootContext.RequestFuture` 用于发送消息并等待回复：

```go
func (rc *RootContext) RequestFuture(pid *PID, message interface{}, timeout time.Duration) *Future {
    future := NewFuture(rc.system, timeout)
    env := &MessageEnvelope{
        Message: message,
        Sender:  future.PID(),
    }
    rc.sendUserMessage(pid, env)  // 走 middleware 或 pid.sendUserMessage
    return future
}
```

我们没有直接用这个方法，原因有二：

**trace 注入无切入点。** `RequestFuture` 内部创建完 `MessageEnvelope` 后立刻发送，调用方在方法返回前插不进 trace header。要用就必须走 middleware 拦截，但 middleware 会导致 EndpointWriter 收到被包装的 `*remoteDeliver` 而崩溃（详见 EndpointWriter 兼容性问题）。

**envelope 不嵌套则无 middleware。** 移除 middleware 后，`Root.Send` 直接调用 `pid.sendUserMessage`，消息按原样送达。我们自己建的 `MessageEnvelope`（含 future PID 和 trace header）可以顺利到达接收方，不会被额外包装。

我们的方案拆解了 `RequestFuture`：

```
RequestFuture (黑盒)
  → 内部创建 envelope(消息 + futurePID)
  → sendUserMessage → [middleware? → 包装]
  → pid.sendUserMessage
  → 返回 future           ← 调用方无法插手

手工 call (白盒)
  → NewFuture → 拿到 futurePID                ← 显式
  → 建 envelope(消息 + futurePID)             ← 显式
  → inject trace header 到 envelope           ← 显式
  → app.send(ctx, pid, envelope)              ← 统一出口
    → injectTrace: 已有 envelope → 直接复用
    → Root.Send → pid.sendUserMessage
  → future.Result()                           ← 显式
```

每一层都在控制之下，没有黑盒包装，没有中间件副作用。

### 4.1 为什么 SenderMiddleware 会触发 EndpointWriter 崩溃

`SenderMiddleware` 的问题不在于 middleware 本身，而在于 **protoactor-go 的 `RootContext` 同时被用户代码和框架内部代码使用**。

**消息路径拆解：**

```
EndpointManager 内部发送 *remoteDeliver 给 EndpointWriter：
  Root.Send(endpoint.writer, rd)
    → rc.sendUserMessage(pid, rd)
      → senderMiddleware != nil ?
          → 包装为 MessageEnvelope{Message: rd}
          → 走 middleware 链
          → pid.sendUserMessage(actorSystem, envelope)     ← 带包装
      → senderMiddleware == nil ?
          → pid.sendUserMessage(actorSystem, rd)           ← 裸消息
```

**EndpointWriter 使用自定义 mailbox（endpointWriterMailbox）**，它不直接投递单个消息到 actor 的 Receive。关键在 mailbox 的 `run()` 方法（`endpoint_writer_mailbox.go:107`）：

```go
func (m *endpointWriterMailbox) run() {
    for {
        // ... system messages ...

        var ok bool
        if msg, ok = m.userMailbox.PopMany(int64(m.batchSize)); ok {
            m.invoker.InvokeUserMessage(msg)   // msg 是 []interface{}，即 PopMany 返回的批
        } else {
            return
        }
        runtime.Gosched()
    }
}
```

`PopMany` 一次性取出多条消息返回 `[]interface{}`，整个切片作为一条"用户消息"投递给 actor。actor 的 `Receive` 收到 `ctx.Message()` 返回的就是这个 `[]interface{}`。随后 `sendEnvelopes` 对切片中的每个元素自己做硬类型断言：

```go
func (w *EndpointWriter) sendEnvelopes(ctx actor.Context) {
    batch := ctx.Message().([]interface{})       // mailbox 投递的批
    for _, tmp := range batch {
        rd := tmp.(*remoteDeliver)                // 硬断言！不是 *remoteDeliver 就 panic
        ...
    }
}
```

**崩溃链条：**

1. EndpointManager 通过 `Root.Send(endpoint.writer, rd)` 发送 `*remoteDeliver`
2. 有 SenderMiddleware 时，`sendUserMessage` 将其包装为 `*MessageEnvelope{Message: *remoteDeliver}`
3. mailbox 收到的是 `*MessageEnvelope` 而不是 `*remoteDeliver`
4. `sendEnvelopes` 执行 `tmp.(*remoteDeliver)` → 类型断言失败 → panic

**根因：`ctx.Message()` 的自动解包对 EndpointWriter 无效**

普通 actor 的 Receive 通过 `ctx.Message()` 获取消息，该方法内部调用 `UnwrapEnvelopeMessage(ctx.messageOrEnvelope)`：

```go
func (ctx *actorContext) Message() interface{} {
    return UnwrapEnvelopeMessage(ctx.messageOrEnvelope)
}
```

这意味着收到 `*MessageEnvelope{Message: rawMsg}` 时，actor 拿到的实际是 `rawMsg`。**如果 EndpointWriter 走标准 actor 消息路径，即使有 middleware 包装也不会崩溃**——`ctx.Message()` 会自动解掉 envelope。

但 EndpointWriter 不走这条路。它的自定义 mailbox 把消息累积成 `[]interface{}` 后一次性投递，Receive 收到的是 `[]interface{}`，而 `sendEnvelopes` 对**切片中的每个元素**自己做硬类型断言：

```go
func (w *EndpointWriter) sendEnvelopes(ctx actor.Context) {
    batch := ctx.Message().([]interface{})   // 取回整个批
    for _, tmp := range batch {
        rd := tmp.(*remoteDeliver)            // ← 逐个断言，不用 ctx.Message()
        ...
    }
}
```

这里的 `tmp` 是 mailbox 投递时 `[]interface{}` 中的原始元素，**不经过 `ctx.Message()` 的自动解包**。middleware 包装后，每个元素是 `*MessageEnvelope`，断言就失败了。

**即使加 receiver middleware 也没用**——`sendEnvelopes` 的 `tmp.(*remoteDeliver)` 断言发生在 Receive 方法内部，是 Go 代码的直接类型断言，不经过 protoactor-go 的消息路由层。任何 middleware 都无法干预这段代码。

**还有一个被忽视的根因：`ctx.Message()` 不处理 `[]interface{}` 里的 envelope。** 看 `actorContext.Message()` 的实现：

```go
func (ctx *actorContext) Message() interface{} {
    return UnwrapEnvelopeMessage(ctx.messageOrEnvelope)
}
```

`UnwrapEnvelopeMessage` 只检查**顶层**消息是否是 `*MessageEnvelope`。当 mailbox 投递的是 `[]interface{}{envelope1, envelope2, ...}` 时，顶层是 `[]interface{}`，不是 `*MessageEnvelope`，于是原样返回。切片内部的 envelope 不会被递归解包。

如果 protoactor-go 在这一步做了递归处理——检测到 `[]interface{}` 后遍历每个元素解包——那么 EndpointWriter 即使有 middleware 也不会崩溃，因为 `sendEnvelopes` 拿到的 batch 中每个元素已经是 `*remoteDeliver` 了：

```
现在：   ctx.Message() → []interface{}{MessageEnvelope{*remoteDeliver}, ...}
如果递归：ctx.Message() → []interface{}{*remoteDeliver, ...}  ← 断言通过
```

但框架没有这样做。`UnwrapEnvelopeMessage` 的语义就是"解一层 envelope"，不承担递归解包 `[]interface{}` 的责任。**EndpointWriter 的自定义 mailbox + batch 投递 与 Middleware 的 envelope 包装，两者正好在 `ctx.Message()` 不会递归解包这个盲区上碰撞**——一个把消息装进了 slice，一个把消息装进了 envelope，slice 内部的 envelope 没人处理。

所以核心矛盾是：**自定义 mailbox + 批量投递 + 硬类型断言 + `ctx.Message()` 不递归解包**，四者共同导致了 middleware 与 EndpointWriter 无法兼容。

**更深层的原因：框架设计边界模糊**

`EndpointManager` 和 `EndpointWriter` 是 protoactor-go remote 模块的内部组件，但它们使用 `RootContext.Send`（全局 Root）来通信。当用户在 `RootContext` 上注册 `SenderMiddleware` 时，**无法区分"这是用户消息需要包装"和"这是框架内部消息不需要包装"**——所有经过 `RootContext` 的消息都会被包装。

移除 middleware 后：

```
Root.Send(endpoint.writer, rd)
  → senderMiddleware == nil
  → pid.sendUserMessage(actorSystem, rd)      ← 裸 *remoteDeliver
  → mailbox 收到 *remoteDeliver
  → sendEnvelopes tmp.(*remoteDeliver)         ← 断言成功 ✓
```

**结论：SenderMiddleware 不适合在 Root 级别注册。** 如果需要在 actor 级别做 sender 拦截，protoactor-go 的 `actor.WithSenderMiddleware` 可以在 actor Props 上注册，粒度更细，不影响框架内部通信。但我们实测后发现也不需要——trace 注入收拢到 `send` 方法后更简洁。

### 5. helper.go：薄透传

导出函数只做类型转换和委托，不处理 trace：

```go
func Send(ctx context.Context, pid PID, message proto.Message) error {
    return app.send(ctx, pid, message)
}
func Call(ctx context.Context, pid PID, message proto.Message, timeout time.Duration) (any, error) {
    return app.call(ctx, pid, message, timeout)
}
```

## 数据流

```
gateway OnMessage(ctx=context.Background())
  → Send(ctx, rolePID, msg)
    → app.send → injectTrace: 无 span → 裸消息

role actor doReceive default case
  → header 空 → Start → 创建根 span（新 trace）
  → a.ctx = trace.ContextWithSpan(a.ctx, span)
  → HandleMessage → Send(a.ctx, guildPID, req)
    → app.send → injectTrace: 读到 span → 注入 W3C  header
    → 发送 MessageEnvelope{Header: trace, Message: req}

guild actor doReceive default case
  → Extract header → 还原父 span context
  → Start → 创建子 span
  → HandleMessage

返回 → defer 恢复 a.ctx → defer span.End()
```

## 与内置 OTel middleware 对比

| 特性 | 内置 middleware | 我们的方案 |
|------|---------------|-----------|
| 存储 | sync.Map (PID → span) | a.ctx 字段（actor 本地） |
| 中间件依赖 | Sender + Receiver + Spawn 三层 | 无中间件 |
| 出站注入 | 全局拦截全部消息 | 收拢到 `send` 一个方法 |
| envelope 嵌套 | RequestFuture 产生嵌套，需额外处理 | type switch 直接复用 |
| EndpointWriter 兼容性 | SenderMiddleware 会包装 *remoteDeliver，导致崩溃 | 无 middleware，不受影响 |

## tracer 名称

使用 `gserver/actor` 作为 tracer name，span name 使用消息类型名 `fmt.Sprintf("%T", msg)`。
