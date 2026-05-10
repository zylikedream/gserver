# Actor Init 失败与 PID 返回的竞态

## 问题描述

当 actor 的 `Init` 方法失败时，`ActivateActor` 仍然返回一个合法的 PID 和 `nil error`。
调用方拿到 PID 后发消息，但 actor 实际已停止，消息石沉大海或 `Call` 超时。

## 根因

Protoactor-go 的 **Spawn 与 Init 分离** 的设计导致：

```
spawnActor (同步)                       actor goroutine (异步)
─────────────────────────────           ────────────────────────────
SpawnNamed("id")
  ↓
PID 注册进 process table  ← 此时已返回
  ↓
registerActor (Redis)
  ↓
goroutine: Call(Touch) ──────────→     *actor.Started → Init(args)
  ↓                                    失败 → Stop()
Touch 超时/报错                          actor 已死亡
  ↓
ActorPidResponse{Pid} ──→ caller 拿到 PID，完全不知道 Init 失败
```

关键点：

1. **`SpawnNamed` 只注册进程条目**，不等待 `Init`。只要 name 不冲突就立即返回 PID。
2. **`Init` 在处理 `*actor.Started` 系统消息时调用**，在 actor 自己的 goroutine 中异步执行。
3. **Activator 的 Touch 确认** 发送 `Touch` 消息给 actor，但 `Init` 失败后 actor 已停，`Touch` 必然超时。
4. **错误被吞**：`Touch` 的 error 被 `_, _ =` 丢弃，`ActorPidResponse` 照发不误。

## 修复

### Init 参数解析（层 C — ChannelActor 自身防御）

`ChannelActor.Init` 改为从 `a.Self().Id`（格式 `"{channelType}_{channelID}"`）解析参数，
不依赖外部传入 init args。适用于 `ChannelActor` 这个具体场景。

参见 `src/apps/chat/channel_actor.go` 的 `Init` 方法。

### Touch 确认失败时返回错误（层 A — Activator 通用防御）

Activator 的 Touch goroutine 在 `Call(Touch)` 失败时：
- 清理 Redis locate key 和一致性哈希成员
- 向调用方回复 `ActorError`

```go
go func(id string) {
    if _, err := a.Call(pid, &actor.Touch{}, 2*time.Second); err != nil {
        a.deRegisterActor(a.ctx, getActorLocateKey(a.kind, id))
        a.meta.mgr.Remove(id)
        delete(a.childs, pid)
        a.Send(sender, ActorError("actor init failed or actor died"))
        return
    }
    a.Send(sender, &remote.ActorPidResponse{Pid: pid})
}(msg.Id)
```

### 局限

- **2s 延迟**：`Call(Touch)` 必须等超时才能判定 actor 死亡。
- **误判**：actor 在 Init 成功之后、Touch 到达之前的短暂窗口内崩溃，也会被判定为 Init 失败。
两者在实践中概率很低，可接受。

## 完整时序

```
Client                    Activator                    Actor
  │                          │                           │
  │── ActivateActor(id) ────→│                           │
  │                          │── SpawnNamed(id) ────────→│
  │                          │   ←──────── PID ──────────│
  │                          │── registerActor(Redis)    │
  │                          │── Call(Touch) ───────────→│  (goroutine)
  │                          │                           │── Init() → fail
  │                          │                           │── Stop()
  │                          │   Touch 超时              │
  │                          │── deRegisterActor(Redis)  │
  │←── ActorError ──────────│                           │
  │  (err != nil)            │                           │
```

修复前：Activator 返回 PID，Client 以为成功。
修复后：Activator 返回 ActorError，Client 收到错误。
