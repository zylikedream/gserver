# Actor 定位与节点注册

## 概述

Actor 定位系统管理 grain（虚拟 actor）在集群中的位置：gate 收到客户端请求时，通过定位系统找到目标 actor 所在节点，将消息路由到正确的节点。系统采用两层架构——**Redis 存节点标识**，**Consul 做节点级服务发现**。

核心设计原则：
- **Redis 只存 `nodeInstanceName`，不存 address:port**，因此端口变化不影响缓存有效性
- **TTL 设为 12h，不做续约**，避免周期性续约开销
- **Redis 命中 → 直接构造 PID**：`actor.NewPID(nodeHost, id)`，跳过 spawnActor。因为 nodeInstanceName 匹配即 actor 存活
- **NodeInstanceName**（`nodeName@uid`）每次节点启动时唯一生成，重启后不同，用于检测节点是否重启

## 数据结构

### Redis — Actor 所在节点标识

Key 格式：`gserver:locate:node:actor:{kind}:{id}`

Value 为 `nodeInstanceName`（即 `game-2@18f3a4b2c1d0`）。

| 属性 | 值 |
|------|-----|
| TTL | 12h（`ActorLocateTTL`） |
| 续约 | **无**（不续约，TTL 仅作崩溃安全网） |
| 注册方式 | spawn 成功后 `SET key nodeInstanceName EX 12h` |
| 清理 | actor terminate 时 `DEL key` |

### Consul — 节点服务注册

Service Key：`gserver-{nodeName}-{serviceName}`

| 属性 | 值 |
|------|-----|
| 注册名 | `nodeInstanceName`（`game-2@uid`） |
| TTL | 10s（配置 `registery.consul.ttl`） |
| 续约 | 每 10s 刷新一次 |
| 内容 | `NodeHost` = actor system address（`host:random_port`） |
| 用途 | `GetHashServices` 列举所有健康节点 |

## 核心流程

### GetActor（定位 actor）

```
GetRoleActor(roleID)
  → ActivateActor("role", roleID, spawn=true)
    → getActor("role", roleID, spawn=true)
      │
      ├─ ① Redis Lookup ──────────────────────────────────────┐
      │   GET key → "game-2@18f3a4b2c1d0"                    │
      │   ↓                                                   │
      │   命中 → 提取 "game-2" → Consul 查 game-2 地址         │
      │         ├── 节点存活 → NewPID(nodeHost, id) 直接返回   │
      │         └── 节点已死 → 进入 ②                          │
      │   未命中 → 进入 ②                                     │
      │                                                       │
      └─ ② ConsistentHash Fallback ───────────────────────────┘
           GetServiceInfo("role", key, consistentHash)
           → 选节点 → spawnActor(node, kind, id)
           → spawn → SET key → 返回
```

关键点：
- **Redis 命中后直接返回 NewPID**，跳过 spawnActor。nodeInstanceName 匹配即说明 actor 存活在目标节点上
- **Consul 匹配使用完整 `nodeInstanceName`**：Redis 存的和 Consul 注册的都是 `game-2@uid`，直匹配

### spawnActor（远程创建 actor）

```
spawnActor(nodeHost, kind, id)
  → activator.NewPID(nodeHost, routerName)
  → Call(activator, &ActorActive{Kind, Id})
    → activatorRouter 转发到 consistentHash pool
      → actorActivator.SpawnNamed(props, id)
        ├── ErrNameExists → 返回已有 PID
        └── 成功 → registerActor: SET key EX 12h
                → Touch 确认（异步，验证 Init 是否成功）
                ├── 成功 → 返回 ActorPidResponse
                └── 失败 → 清理 Redis 注册 + 一致性哈希
                          → 返回 ActorError 给调用方
```

### Actor 生命周期

```
node 启动
  → OnModInit 生成 nodeInstanceName = nodeName@unixNano
  → 传给 actorApp 和 serviceApp
  → serviceApp 以 nodeInstanceName 注册 Consul

spawn 成功
  → registerActor(): SET key nodeInstanceName EX 12h

actor terminate（正常退出）
  → deRegisterActor(): DEL key

进程崩溃（异常退出）
  → Redis key 残留（TTL 12h，无续约，残留不影响正确性）
  → Consul TTL 10s 过期后摘掉节点
  → 下次 getActor → Redis 命中 → Consul 查不到 → fallback 到一致性 Hash
```

## 异常场景

### 节点重启（端口变化）

```
服务器 A（game-2）重启，端口从 34567 变 34568
  → 新 nodeInstanceName: game-2@NEWuid
  → Consul 注册新 entry

getActor(role, 100008):
  → Redis 还有旧的 game-2@OLDuid
  → Consul 查 game-2@OLDUid → 不存在（重启后 uid 变了）
  → fallback 一致性 Hash → 选中 game-2（新 entry）
  → spawnActor → actor 重新创建 → SET game-2@NEWuid → 正常
```

### 节点抖动（10min 断网后恢复）

```
节点 B 断网 10min，重新加入
  → 同一 uid，Consul 恢复健康
  → 期间可能部分 actor 被迁移到其他节点
  → 恢复后 actor 仍在，继续服务
```

### 节点抖动导致的数据分叉（TODO）

```
节点 B 抖动 → 误认为下线
  → 玩家在节点 A 创建新 actor（使用 DB 旧数据）
  → 节点 B 恢复后，落地时间到 → 批量写 DB
  → 可能覆盖节点 A 新写入的数据
```

当前版本尚未处理此问题，后续通过分布式事务或版本号解决。

## 代码位置

| 文件 | 说明 |
|------|------|
| `core/gxynode/node.go` | 生成 `NodeInstanceName`，传入 actorApp 和 serviceApp |
| `core/gxyactor/activator_manager.go` | getActor、spawnActor、registerActor（SET）、RegisterActorKind |
| `core/gxyactor/system.go` | actorApp 启动 remote、activatorManager 生命周期 |
| `core/gxyservice/service_app.go` | 以 `nodeInstanceName` 注册 Consul，`GetAddressByNodeName` |
| `core/gxyregistery/types.go` | ServiceInfo 结构体 |
| `src/apps/gateway/internal/logic/session.go` | 客户端接入，调用 ActivateRole |
| `src/lib/actor.go` | GetRoleActor / ActivateRole 封装 |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| Redis 存什么 | `nodeInstanceName`（`nodeName@uid`） | 不依赖端口，重启后 uid 变化自动失效 |
| TTL | 12h，不续约 | 避免续约开销，TTL 仅兜底；注册/注销用 SET/DEL 精确控制 |
| Redis 命中后行为 | NewPID 直接构造 PID，跳过 spawnActor | nodeInstanceName 匹配即 actor 存活；如果 actor 已死，ErrDeadRespond 由 protoactor 返回 |
| 节点标识生命周期 | 每次启动生成新 uid | 用 uid 变化自然检测节点重启 |
| Consul 注册名 | `nodeInstanceName` | 与 Redis 值直接匹配，无需额外映射 |
| 注销方式 | DEL（不带 CAS） | 12h TTL 兜底，极端情况最多 12h 残留 |
| Touch 确认 | 异步 Call(Touch, 2s)，失败返回 ActorError | Init 异步执行，Touch 验证 Init 是否成功；失败时清理脏数据 |
| 节点选择 | 一致性哈希 | 同一 ID 稳定路由到同一节点 |
| 随机端口 | `remote.Configure(host, 0)` | 避免端口冲突 |

## 变更记录

| 日期 | 变更 | 说明 |
|------|------|------|
| 2026-05-02 | v2 重构 | 以 `nodeInstanceName` 代替 `address:port`，去掉续约，去掉 gxylocator |
| 之前 | v1 方案 | Redis 存 JSON ActorPid，TTL=40s，30s 续约；节点重启有 40s 窗口期 |