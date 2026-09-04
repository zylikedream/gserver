# Actor 系统

GServer 基于 [protoactor-go](https://github.com/asynkron/protoactor-go) 构建 Actor 系统，封装在 `core/gxyactor/` 中。

## 架构层次

```
┌──────────────────────────────────────────────────────────┐
│                    actorApp (全局单例)                      │
│         protoactor-go ActorSystem + Remote                 │
├──────────────────────────────────────────────────────────┤
│                 activatorManager                           │
│    管理所有 Actor Kind，维护 activatorMeta 映射              │
├─────────────┬──────────────────┬──────────────────────────┤
│  Router     │  ConsistentHash  │  Activator Instance       │
│  (外部入口)  │  Pool (路由)      │  (实际创建/管理 Actor)     │
└─────────────┴──────────────────┴──────────────────────────┘
```

## 核心组件

### actorApp（`system.go`）

全局单例，封装 protoactor-go 的 `ActorSystem` 和 `Remote`。

```go
type actorApp struct {
    system          *actor.ActorSystem
    remote          *remote.Remote
    nodeName        string
    nodeInstanceName string
    host            string
    activatorMgr    *activatorManager
}
```

- **Init**: 创建 ActorSystem、启动 Remote、初始化 activatorManager
- **地址**: `system.Address()` 返回 `{ip}:{port}`（端口由 protoactor-go 动态分配）
- 提供 `Send`、`Call`、`LocalSend`、`SpawnNamed`、`ActivateActor` 等全局函数

### IActor 接口（`actor.go`）

```go
type IActor interface {
    Init(ctx context.Context, args []any) error
    DelayInit(ctx context.Context) error
    Terminate(ctx context.Context, err error)
    Timer() *ActorTimer
    Self() PID
    HandleMessage(ctx context.Context, msg any) error
    actor.Actor
}
```

**生命周期**：

1. `actor.Started` → `Init(ctx, args)` — 初始化
2. `ActorInitMsg` (自发) → `DelayInit(ctx)` — 延迟初始化（此时 handler 已注册）
3. 消息循环 → `HandleMessage(ctx, msg)` — 处理业务消息
4. `actor.Stopped` → `Terminate(ctx, err)` — 清理资源

### ActorBase（`actor.go`）

默认基类，提供通用能力：

- `Receive` — protoactor-go 消息入口，包 try-catch 异常保护
- `CallSync` — 异步请求（带 sender）
- `Call` — 同步请求（RequestFuture）
- `Send` / `LocalSend` — 远程/本地消息发送
- `Respond` — 回应调用方
- `AutoHandleMsg` — 通过反射 Handler 自动分发消息
- `Stop` — 停止自身
- `SpawnNamed` / `Spawn` — 创建子 Actor

## Activator 系统

### 架构

每个 Actor Kind 有三层结构：

```
                   ┌──────────────────────┐
                   │  ActivatorRouter      │  — 外部节点入口，接收 pb.ActorActive
                   │  (固定名称路由 Actor)  │    包装为 hashableActorActive 转发到 Pool
                   └──────────┬───────────┘
                              │ 按 Actor ID 一致性哈希
                   ┌──────────▼───────────┐
                   │  ConsistentHashPool   │  — 5 个 actorActivator 实例
                   │  (一致哈希路由池)      │   按 ID 路由到固定的 activator
                   └──────────┬───────────┘
                              │
                   ┌──────────▼───────────┐
                   │  actorActivator       │  — 真正创建 Actor 实例
                   │  (负责创建/管理 Actor)  │   Claim/Release、维护 childs
                   └──────────────────────┘
```

### 激活流程

```
客户端请求 ActivateActor(kind, id)
  │
  ▼
① 查询 Redis key: gserver:locate:node:actor:{kind}:{id}
  │
  ├── 有有效 owner
  │     ├── Consul 解析 owner 地址失败 → fail closed
  │     └── 请求 owner Activator
  │           ├── 本地 ActorMgr 命中 → 返回 PID
  │           └── 本地 activation 缺失 → 条件删除 owner，RetryLocate
  │
  └── 无有效 owner
        └── 通过一致性哈希选择节点 → 发送 ActorActive
              │
              ▼
② 目标节点的 ActivatorRouter 收到 ActorActive
  │
  ▼
③ 转发到 ConsistentHashPool → 落到固定 actorActivator
  │
  ▼
④ Claim 成功后 SpawnNamed，并进入 pending Touch
  │
  ├── Touch 成功 → 发布到本地 ActorMgr，向所有 waiters 返回 PID
  ├── Touch 失败 → 停止 actor、条件释放 owner、返回 ActorError
  └── Claim 指向其他 owner → RetryLocate
```

### Redis 定位 Key

```
gserver:locate:node:actor:{kind}:{id}  →  {nodeInstanceName}|{epoch}|{leaseToken}
gserver:locate:node:actor:role:10001   →  game-2@1743529200000000000|7|game-2@1743529200000000000
```

- owner key 不设置 TTL；只有对应节点 lease token 精确匹配时才有效
- 节点 lease：`gserver:locate:node:lease:{nodeInstanceName}`，TTL 15 秒，节点 heartbeat 续期
- 续租 token 不匹配立即 self-fence；Redis 错误超过本地安全 deadline 时终止进程
- 激活前用 Lua Claim 原子检查并更新 owner；接管时递增 epoch
- 正常退出或 Touch 失败使用 compare-and-delete
- Redis 错误不当作定位 miss；Redis 命中必须经过 owner Activator 验活

### 注册 Actor Kind

```go
// role_service.go
gxyactor.RegisterActorKind("role", func() gxyactor.IActor {
    return logic.NewRoleMain()
})
```

## 跨节点通信

1. 本地 Actor → 直接 PID 发消息
2. 远程 Actor → protoactor-go Remote 层序列化 → 网络传输 → 反序列化处理
3. 所有跨节点消息必须是 `proto.Message`

## 监督策略

```go
actor.NewOneForOneStrategy(10, 3*time.Second, decider)
// decider: 所有错误 → StopDirective
```

失败的子 Actor 会被停止，由父级或 Activator 处理后续。

## 源码位置

| 文件 | 内容 |
|------|------|
| `core/gxyactor/actor.go` | IActor 接口、ActorBase 实现 |
| `core/gxyactor/actor_mgr.go` | 本地 PID 管理器 |
| `core/gxyactor/system.go` | actorApp 全局单例、protoactor 集成 |
| `core/gxyactor/helper.go` | 全局函数（Send/Call/ActivateActor 等） |
| `core/gxyactor/activator_manager.go` | Activator 管理器、路由池 |
| `core/gxyactor/actor_locator.go` | Redis lease、Claim/Release、epoch 和 self-fence |
| `core/gxyactor/actor_timer.go` | Actor 定时器 |
| `core/gxyactor/logger.go` | protoactor 日志适配 |
