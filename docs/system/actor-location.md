# Actor 定位与节点注册

## 概述

Actor 定位系统管理 grain（虚拟 actor）在集群中的位置：gate 收到客户端请求时，通过定位系统找到目标 actor 的 PID（进程 ID），将消息路由到正确的节点。系统采用两层注册架构——Redis 做 actor 级缓存，Consul 做节点级服务发现。

## 数据结构

### Redis — Actor 定位缓存

Key 格式：`gserver:locate:node:actor:{kind}:{id}`

Value 为 JSON 序列化的 `ActorPid`：
```json
{"address": "192.168.1.100:34567", "id": "role_100008"}
```

| 属性 | 值 |
|------|-----|
| TTL | 40s（`ActorLocateTTL`） |
| 续约 | 每 30s 批量 `SETEX` 续一次（`renewAllActors`） |
| 注册方式 | spawn 时 `SETNX`，续约时 `SETEX` |
| 清理 | actor terminate 时 Lua CAS 删除（值匹配才删） |

### Consul — 节点服务注册

Service Key：`gserver-{nodeName}-{serviceName}`

| 属性 | 值 |
|------|-----|
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
      ├─ ① Redis Lookup ───────────────────────────────┐
      │   LocateNode(key) → pidInfo                    │
      │   ↓                                            │
      │   命中 → 反序列化 PID，直接返回（不验证存活）     │ ← Bug 点
      │   未命中 → 进入 ②                              │
      │                                                │
      ├─ ② Consul Lookup (only when Redis misses) ─────┤
      │   GetServiceInfo("role", key, consistentHash)   │
      │   ↓                                            │
      │   找到节点 → spawnActor(node, kind, id)          │
      │   无可用节点 → 返回错误                          │
      │                                                │
      └─ spawnActor ───────────────────────────────────┘
           Send ActorActive 到目标节点 activator router
           → consistent hash pool 选 activator
           → SpawnNamed(props, actorID)
           → SETNX 注册到 Redis（key 被抢则停止并报错）
```

### Actor 生命周期（注册/续约/注销）

```
spawn 成功
  → registerActor(): SETNX key, TTL=40s
  → 启动续约定时器

每 30s
  → renewAllActors(): 批量 SETEX 所有 child key, TTL=40s

actor terminate（正常退出）
  → deRegisterActor(): Lua CAS 删除 key（验证值匹配）

进程崩溃（异常退出）
  → Redis key 残留，直到 TTL=40s 过期
  → Consul TTL=10s 过期后摘掉节点
```

## 两层注册的脱节问题

### 时序

```
服务器 A 运行中
  Consul: game-1@192.168.1.100:34567 (healthy, TTL 10s)
  Redis:  actor:role:100008 → {address:"192.168.1.100:34567", id:"role_100008"} (TTL 40s)

服务器 A 崩溃/重启（新端口 34568）
  T+0s  Consul: 旧 entry 开始 TTL 倒计时
         Redis:  旧 entry 还有 40s 存活
         getActor(role, 100008) → 命中 Redis → 返回旧地址 34567
         → endpoint_writer 连旧地址 → 疯狂重试

  T+10s Consul: 旧 entry TTL 过期，从服务列表摘掉
         Redis:  旧 entry 还有 30s 存活
         getActor(role, 100008) → 依然命中 Redis → 依然连旧地址

  T+40s Redis: 旧 entry TTL 过期，自动删除
         getActor(role, 100008) → Redis miss → 查 Consul → 拿到新节点地址
         → 一切恢复正常
```

### 根因

| 层 | TTL | 存活性判断 | getActor 是否用到 |
|---|---|---|---|
| Redis actor 缓存 | 40s | 无（只判断 key 是否存在） | **是**（优先查） |
| Consul 节点健康 | 10s | 有（健康检查 + TTL） | **仅 Redis miss 时** |

Redis TTL（40s）>> Consul TTL（10s），Redis 命中时 Consul 的健康信息完全被架空。

## 错误码

| 错误 | 说明 |
|------|------|
| key already registered by another node | `SETNX` 注册失败，其他节点已持有（由 TTL 残留或并发触发） |
| registration failed, key taken by another node | activator 收到 `SETNX` 失败的返回，停止本地 actor |
| find actor node failed | Redis miss 后 Consul 也查不到健康节点 |

## 代码位置

| 文件 | 说明 |
|------|------|
| core/gxyactor/activator_manager.go | getActor、spawnActor、renewAllActors、RegisterActorKind |
| core/gxyactor/system.go | actorApp 启动 remote、activatorManager 生命周期 |
| core/gxylocator/gxylocator.go | LocateNode、MustRegisterActor（SETNX）、RegisterBatchActor（SETEX） |
| core/gxylocator/script.go | Lua 脚本（批量注册、CAS 注销） |
| core/gxyservice/service_app.go | 服务注册/发现，GetServiceInfo → Consul |
| core/gxyregistery/types.go | ServiceInfo 结构体 |
| src/apps/gateway/internal/logic/session.go | 客户端接入，调用 ActivateRole |
| src/lib/actor.go | GetRoleActor / ActivateRole 封装 |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 定位缓存 | Redis，TTL=40s | 减少 Consul 查询 + 跨节点 spawn 开销 |
| 注册互斥 | SETNX + 续约用 SETEX | 防双写，启动时清理后能自动恢复 |
| 注销原子性 | Lua CAS（值匹配才删） | 防止误删其他节点的新注册 |
| 批量续约 | Lua 脚本循环 SETEX | 减少续约开销 |
| 节点选择 | 一致性哈希 | 同一 ID 稳定路由到同一节点 |
| 随机端口 | remote.Configure(host, 0) | 避免端口冲突（副作用：重启后地址全变） |
