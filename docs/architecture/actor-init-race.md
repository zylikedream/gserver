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

### Claim 先于 Spawn

Activator 在 `SpawnNamed` 前执行 Redis 原子 Claim。Claim 返回其他 owner 时只回复 `ActorLocateRetry`，由调用方重新定位；Redis 命中也必须请求 owner Activator，不能直接构造 PID。只有 owner Activator 确认本地 activation 存在时才返回 PID。

### Owner 元数据随 actor 传递

Claim 成功返回 `ActorOwner{NodeID, Epoch}`。Activator 通过 `SpawnNamed(props, id, id, owner)` 将 owner 传给 actor；Role activation 用该 epoch 推进 PostgreSQL fence，所有保存事务先锁定 exact `roleID + nodeID + epoch`。

### Touch 确认失败时条件清理

Touch 等待仍在 goroutine 中执行，但 goroutine 只把结果发回 Activator mailbox，不直接访问 actor context 或本地 map。Activator 在 mailbox 内串行完成：

- Touch 期间把同一 actor 的并发请求合并到 pending waiters；
- Touch 成功后才把 PID 发布到本地 `ActorMgr` 并回复所有 waiters；
- Touch 失败时停止 actor，使用 `nodeInstanceName + epoch + lease token` compare-and-delete owner，并回复 `ActorError`；
- `Terminated` 先到时清理 pending activation，并拒绝全部 waiters。

```go
owner, acquired, err := locator.claim(ctx, kind, id)
pid, err := SpawnNamed(props, id, id, owner)
pending[id] = {pid, owner, waiters}
// goroutine: Call(Touch) → LocalSend(activator, touchResult)
// activator mailbox: publish PID or conditional Release
```

### 局限

- **10s 延迟**：`Call(Touch)` 最长等 10s 超时才能判定 actor 死亡。
- **误判**：actor 在 Init 成功之后、Touch 到达之前的短暂窗口内崩溃，也会被判定为 Init 失败。
- Redis TTL 不能单独解决网络分区；Role 持久化必须执行 epoch fencing。

## 完整时序

```
Client                    Activator                    Actor
  │                          │                           │
  │── ActivateActor(id) ────→│                           │
  │                          │── Claim Lua ──────────────→ Redis
  │                          │←─ owner + epoch ──────────│
  │                          │── SpawnNamed(id, owner) ──→│
  │                          │   ←──────── PID ──────────│
  │                          │── Call(Touch) ───────────→│  (goroutine)
  │                          │                           │── Init() → fail
  │                          │                           │── Stop()
  │                          │←─ local touch result ─────│
  │                          │── conditional Release ────→ Redis
  │←── ActorError ───────────│
  │  (err != nil)             │                           │
```

修复前：Activator 返回 PID，Client 以为成功。
修复后：Activator 返回 ActorError，Client 收到错误。
