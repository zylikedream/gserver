# Actor 系统

GServer 基于 [Ergo](https://github.com/AsynkronIT/proto) 构建通用 Actor runtime，通过 `core/gxyactor/` 中的 GServer adapter 向业务提供 runtime-neutral seam。

## 架构层次

```
┌──────────────────────────────────────────────────────────┐
│                    actorApp (全局单例)                      │
│       Ergo Node + GServer runtime adapter + Network       │
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

全局单例，封装 Ergo Node/Network 与 GServer adapter，并维护节点身份和 Activator 生命周期。

- **Init**：创建 Ergo Node、启动网络、初始化 `activatorManager`。
- **地址**：Node 的传输地址由 Ergo network 提供；业务使用 `nodeInstanceName` 通过服务发现解析地址，不把地址写入 Actor owner key 或 PID。
- **公共操作**：提供 runtime-neutral 的 `Send`、`Call`、`Respond`、`Stop`、`ActivateActor` 等操作；Ergo `gen.*` 类型不出现在业务 API。

### GServer Actor contract（`actor.go`）

业务 Actor 只实现以下契约，不嵌入具体 runtime 类型：

- `Init(ctx, args)`：ProcessInit 阶段的同步初始化。
- `DelayInit(ctx)`：handler 注册后的延迟初始化；完成前不接收普通业务消息。
- `HandleMessage(ctx, message)`：在 mailbox 中串行处理 opaque 业务消息。
- `Terminate(ctx, err)`：ProcessTerminate 阶段清理资源。
- `Timer()`、`Self()`：分别提供 timer 管理和规范化 GServer PID。

生命周期固定为 `ProcessInit → Init → handler 注册 → DelayInit → message handling → ProcessTerminate → Terminate`。初始化成功并经激活确认后才发布 PID 到本地 `ActorMgr`；handler error 或 panic 按 one-for-one stop 语义终止当前 Actor。

### ActorBase

默认基类通过 GServer adapter 接入 Ergo mailbox，提供：

- handler 自动分发、panic 捕获、日志和 trace 上下文传递；
- `Call`、`Send`、`Respond`、`Stop` 等 runtime-neutral 操作；
- timer 事件经 mailbox 串行投递，并在 Terminate 前取消；
- 初始化失败不发布 PID，Terminate 只执行一次。

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
                   │  actorActivator       │  — 真正创建 Actor 进程
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
  │           ├── 本地 ActorMgr 命中 → 返回规范化 PID
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
④ Claim 成功后创建 Ergo Process，并等待 Init/DelayInit 确认
  │
  ├── 确认成功 → 发布到本地 ActorMgr，向所有 waiters 返回 PID
  ├── 确认失败 → 停止 process、条件释放 owner、返回 ActorInitFailed
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
- 正常退出或初始化失败使用 compare-and-delete
- Redis 错误不当作定位 miss；Redis 命中必须经过 owner Activator 验活

## 跨节点通信

1. 本地 Actor → 通过 GServer adapter 直接向规范化 PID 投递
2. 远程 Actor → Ergo Network 传输注册的 `GServerEnvelope` → 解码 protobuf bytes → mailbox 处理
3. `GServerEnvelope` 的 `Type` 是稳定的 protobuf message type identifier，`Data` 是 `proto.Marshal` 生成的 bytes
4. 业务包不依赖 Ergo EDF 或 Ergo `gen.*` 类型；未知类型和 malformed payload 返回 typed wire error

## 监督策略

Ergo 配置 one-for-one stop supervision：

- handler 返回 error 或 panic → 停止失败 Actor；不自动 restart 有状态 Role Actor；
- ProcessTerminate 停止 timer、执行业务 Terminate，并按 `nodeInstanceName + epoch + leaseToken` 条件 Release；
- 重新激活必须重新经过 Claim 和 PostgreSQL `role_actor_fence`，监督器不承担 ownership fencing。

## 节点关闭

节点进入 drain 后停止新 activation 和 Spawn，等待已有 Actor 保存并正常终止；超时后保存并断线重连。节点无法在安全截止时间内确认 Redis lease 续租时必须 self-fence，停止新请求并终止进程。节点崩溃后只能在 lease 到期且 epoch 递增后被接管，不迁移在线 Actor 的 PID、mailbox 或内存状态。

## 源码位置

| 文件 | 内容 |
|------|------|
| `core/gxyactor/actor.go` | GServer Actor contract、ActorBase 实现 |
| `core/gxyactor/actor_mgr.go` | 本地规范化 PID 管理器 |
| `core/gxyactor/system.go` | actorApp、Ergo adapter 集成 |
| `core/gxyactor/helper.go` | 全局函数（Send/Call/ActivateActor 等） |
| `core/gxyactor/activator_manager.go` | Activator 管理器、路由池、初始化确认 |
| `core/gxyactor/actor_locator.go` | Redis lease、Claim/Release、epoch 和 self-fence |
| `core/gxyactor/actor_timer.go` | Actor 定时器 |
| `core/gxyactor/internal/ergo/` | Ergo runtime、PID、网络 envelope 适配器（仅 runtime 边界） |
| `core/gxyservice/service_app.go` | 以 `nodeInstanceName` 注册 Consul，`GetAddressByNodeName` |

权威契约见 [ADR 0008](adr-0008-ergo-actor-runtime.md)；Redis 所有权和 PostgreSQL fencing 见 [ADR 0006](adr-0006-actor-location-ownership.md) 与 [Actor 定位](actor-location.md)。
