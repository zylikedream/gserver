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

## Grafana Tempo 查看

### 访问方式

1. 启动监控栈：`docker compose -f deploy/docker/docker-compose.yml up -d`
2. 访问 Grafana：`http://localhost:3000` → Explore → 选择 Tempo 数据源
3. 或直接在 TraceQL 中输入 trace ID 查询

### TraceQL 查询示例

```traceql
# 按 trace ID 查询
bbd290366897a750673a41d307ec8ccc

# 查询所有 actor 消息
{resource.service.name = "gate"}

# 查询某类消息
{name = "*pb.ReqChatSendChannel"}

# 查询慢请求（>100ms）
{duration > 100ms}
```

### Trace 视图解读

在 Grafana Explore 中打开一条 trace 后，页面分为三个区域：

**1. 顶部元信息**

- **Trace ID** — 唯一标识，可复制分享
- **Start time** — 请求发起时间
- **Duration** — 整条 trace 总耗时
- **Services** — 经过的服务数量（如 gate、game）
- **Overview 时间线** — 彩色条带可视化各 span 的起止和时长

**2. Span 列表**

每个 span 显示：`{service}: {operation} (duration)`

示例 — 一条聊天请求的 trace（6 spans，3.62ms）：

```
gate: *pb.ReqChatSendChannel (565.14μs)           ← 根 span
  └─ game: *pb.ReqChatSendChannel (1.41ms)         ← 跨节点调用
       ├─ *pb.ReqChannelSend (255.91μs)             ← 频道消息发送
       ├─ *pb.NotifyChatChannel (313μs)             ← 通知订阅者
       ├─ gate: *pb.NotifyChatChannel (328.79μs)    ← 回推 gateway
       └─ *pb.RspChatSendChannel (293.44μs)         ← 响应返回
```

**3. 怎么读 span 的层级关系**

- **根 span**（无缩进）— 请求入口，通常是 gateway session actor 收到客户端消息时创建
- **子 span**（缩进）— 由父 span 的 `send`/`call` 触发，trace context 通过 MessageEnvelope header 传播
- **同级的多个子 span** — 父 span 处理过程中先后发起的多个下游调用（如先写频道、再通知、最后响应）
- **服务名前缀**（`gate:` / `game:`）— span 创建时所在的节点，没有前缀的 span 与父 span 同节点

**4. 实例解读：以 ReqGuildInfo 为例**

Trace ID: `36a1ce56eb544771d6f6f9b16a178fa5`，4 spans，4.77ms，2 services。

```
gate: *pb.ReqGuildInfo (628.19μs)             ← 根 span，gateway 收到请求
  ├─ game: *pb.ReqGuildInfo (2.85ms)           ← 跨节点：gate → game 远程调用
  │    └─ *pb.ReqGuildInfo (1.51ms)            ← game 内部：guild actor 处理查询
  └─ gate: *pb.RspGuildInfo (432.4μs)          ← 响应回 gateway
```

Overview 时间线对应：

```
0μs          628μs     1.19ms     2.39ms     3.58ms    4.77ms
├─────────────┃──────────────────────────────────────────────┤
│gate:Req     ████                                              │ gate 收到请求，转发
│game:Req             ┃████████████████████████████████        │ game 处理
│  *pb.Req             ┃  ████████████████████████             │ game 内部 actor 处理
│gate:Rsp              ┃                                ████   │ 响应回 gate
├─────────────┃──────────────────────────────────────────────┤
```

注意 `game:ReqGuildInfo` 和 `gate:RspGuildInfo` 是同级子 span（都是 `gate:ReqGuildInfo` 的子）。`game:Req` 处理完毕后，game 通过 `respond` 发回响应，gate 收到响应创建了 `gate:RspGuildInfo` span。两个同级 span 之间没有重叠，是串行的。

**5. Span 时间关系模式**

在 Overview 时间线中，每个 span 是一条彩色横条。以下是常见的位置关系：

**模式一：子 span 串行嵌套（依次调用）**

```
父 span ████████████████████████████████████
子 span 1  ████████████
子 span 2              ██████████████
子 span 3                            ████████████
```

子 span 1 结束后子 span 2 才开始，是串行执行。Actor 在 `HandleMessage` 中依次调用 `Call(A)` → `Call(B)` → `Respond(C)` 就会产生这种模式。上面的 ReqGuildInfo trace 就是典型的串行链路。

**模式二：子 span 之间有间隔（等待）**

```
父 span ████████████████████████████████████
子 span 1  ████████
                   ← 间隔
子 span 2              ██████████
```

间隔可能代表：
- **网络等待** — `Call()` 发出请求后，等待远端 actor 处理并返回的 RTT。在 ReqGuildInfo trace 中，`game:ReqGuildInfo` 开始前有一小段间隔，就是跨节点消息传输的网络延迟
- **异步事件等待** — actor 等待某个异步操作完成（如 DB 查询返回、等待定时器）
- **本地计算** — 序列化/反序列化、业务逻辑等不产生 span 的 CPU 时间

**模式三：子 span 时间重叠（并发执行）**

```
父 span ████████████████████████████████████
子 span 1    ████████████████████
子 span 2         ████████████████████████
                  ^^^^^^^^
                  重叠：两个子 span 同时在执行
```

在 Actor 模型中，一个 actor 向**多个**下游 actor 分别 `Send` 异步消息（不等待返回），每个下游 actor 各自创建 span，这些 span 就会重叠 — 它们在不同的 actor 进程中并行处理。

常见于**广播/通知**场景，例如聊天频道通知多个在线玩家：
- game actor 先后 `Send` 通知给多个 gate 节点
- 各 gate 节点的 session actor 并行收到通知、各自创建 span
- 这些 span 在时间线上重叠

**模式四：同名 span（跨节点传播，不是重复处理）**

```
gate: *pb.ReqGuildInfo  ████████████████████
  └─ game: *pb.ReqGuildInfo  ████████████
       └─ *pb.ReqGuildInfo  ████████
```

同一个 operation 出现多次但 service 不同（`gate:` vs `game:`），这是**跨节点 actor 调用**：
1. `gate: *pb.ReqGuildInfo` — gateway session actor 收到请求，创建根 span
2. `game: *pb.ReqGuildInfo` — 通过 `Call` 转发到 game 节点，trace context 随 MessageEnvelope header 传播，远端 actor 提取后创建子 span
3. `*pb.ReqGuildInfo`（无前缀）— game 节点内部的 actor 间调用，与父 span 同节点

**模式五：子 span 超出父 span 范围（异常）**

```
父 span ████████████████
子 span    ██████████████████████   ← 超出父 span
```

正常情况下子 span 必须在父 span 时间范围内。超出说明时间记录有误 — 通常原因是 `a.ctx` 中的 span 泄漏（`doReceive` 的 `savedCtx` 恢复逻辑未正确执行），或 goroutine 中使用了错误的 ctx。

**模式六：多个独立根 span（链路断裂）**

```
根 span A  ████████████
根 span B                  ████████████
根 span C                                ████████████
```

三个 span 没有 parent-child 关系，各自是独立的 trace。可能原因：
- 调用时 ctx 中没有有效 span（如 `context.Background()`）
- 消息走了不经过 `send` 的路径（直接 `Root.Send`、timer 回调）
- 跨网络调用时 trace header 丢失

**6. 排查思路**

- **整条链路慢** → 对比各子 span 耗时占比，找最大的那个
- **某个环节慢** → 展开对应 span 查看详情，看 `actor_kind` 和 `msg` 属性定位具体 actor 和消息类型
- **子 span 之间有大间隔** → 可能是网络延迟（跨节点 Call 的 RTT）或本地计算瓶颈，需结合 span 的 service 名判断
- **子 span 重叠** → 正常的并发行为（异步 Send 多个下游），不需要处理
- **链路断裂**（独立根 span）→ 检查调用是否使用了正确的 `ctx`，是否经过 `send`/`Call`
- **span 超出父 span 范围** → 检查 `doReceive` 中 `savedCtx` 恢复逻辑是否正确

### IUnspanMessage

实现了 `IUnspanMessage` 接口的消息会跳过 span 创建（在 `doReceive` 中直接走 `handleMessage`）。用于 Actor 内部消息（如 `ActorActive` 等），避免产生大量无意义的离散 trace。
