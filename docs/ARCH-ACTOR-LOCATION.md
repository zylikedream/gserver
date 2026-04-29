# GServer Actor 定位机制

## 概述

GServer 基于 **protoactor-go** 实现虚拟 Actor 模式，通过**两层定位**机制实现分布式 actor 的查找和路由：

```
┌─────────────────────────────────────────────────────────────────┐
│                         调用方                                    │
│  ActivateRole(roleID) → ActivateActor("role", "12345")         │
└────────────────────────────┬────────────────────────────────────┘
                             │
         ┌───────────────────┴───────────────────┐
         │  1. 先查 Redis Locator                 │
         │     key = "actor:role:12345"          │
         │     如找到 → 直接返回 PID               │
         └───────────────────┬───────────────────┘
                             │ 未命中
         ┌───────────────────┴───────────────────┐
         │  2. 查服务发现 (Consul/etcd)          │
         │     找 kind=role 的节点               │
         │     用 ConsistentHashSelector 选节点  │
         └───────────────────┬───────────────────┘
                             │ 找到节点
         ┌───────────────────┴───────────────────┐
         │  3. 向目标节点发 ActorActive 消息     │
         │     节点 spawn actor 并返回 PID       │
         └───────────────────────────────────────┘
```

---

## 核心组件

### 1. activatorMeta — Actor 类型元数据

```go
type activatorMeta struct {
    Kind      string              // "role"
    Props     *actor.Props         // actor 启动配置
    Activator PID                  // activatorRouter PID (外部入口)
    Pool      PID                  // consistent-hash pool PID (内部)
    mgr       *ActorMgr            // 本地 ActorMgr，管理该类型的所有 actor PID
}
```

### 2. activatorRouter — 外部路由入口

每个 actor 类型有一个 `activatorRouter`，负责接收远程 `ActorActive` 消息并包装为 `hashableActorActive` 转发到本地一致性哈希池：

```go
type activatorRouter struct {
    *ActorBase
    poolPID PID   // 指向 consistent-hash pool
}
```

### 3. actorActivator — Actor 工厂

一致性哈希池中的每个实例是一个 `actorActivator`，负责：
- 接收 `hashableActorActive{Id, Kind}` 消息
- Spawn 对应 actor
- 注册到 Redis Locator
- 返回 PID 给调用方

```go
type actorActivator struct {
    *ActorBase
    kind    string                // actor 类型
    manager *activatorManager     // 父管理器
    childs  map[PID]string        // child PID → actor ID 映射
    meta    *activatorMeta        // 类型元数据
}
```

### 4. activatorManager — 全局 Actor 管理器

```go
type activatorManager struct {
    gxymodule.ModuleBase
    locator        *gxylocator.Locator  // Redis 定位器
    activatorMetas map[string]*activatorMeta // kind → 元数据
}
```

### 5. Redis Locator — Actor 位置缓存

key 格式: `gserver:locate:node:actor:{kind}:{id}`
value: `{"Address":"node1:8080","Id":"role/12345"}`

TTL = 40s，每 30s refresh 一次。

---

## 定位流程详解

### ActivateActor(kind, id) 的完整时序

```
1. key = "actor:role:12345"

2. locator.LocateNode(key)
   → Redis GET "gserver:locate:node:actor:role:12345"
   → 命中 → 反序列化 → 返回 PID → 完成

3. 未命中 + spawn=true:
   → ServiceApp().GetServiceInfo(ctx, "role", key, ConsistentHashSelector)
   → 从 Consul/etcd 找 "role" 服务，用 key 做一致性哈希选节点

4. 找到节点 nodeHost:
   → 向 nodeHost 上的 activatorRouter 发 ActorActive{Id:"12345", Kind:"role"}

5. activatorRouter 接收:
   → 包装为 hashableActorActive{ActorActive, hash: "12345"}
   → RequestWithCustomSender 发送到 Pool

6. ConsistentHashPool 路由到某个 actorActivator:
   → actorProps = meta.Props.Clone()
   → SpawnNamed(props, "12345")
   → go Call(pid, Touch) 确保 actor 启动完成
   → registerActor(key, selfPID) → Redis SETEX(ttl=40s)
   → meta.mgr.Add(id, pid) → 本地 ActorMgr 缓存
   → 返回 PID 给调用方
```

### RegisterActorKind(kind, producer) — Actor 类型注册

```
1. 创建 actorProps = actor.PropsFromProducer(producer)
2. 创建 5 节点 ConsistentHashPool → poolPID
3. SpawnNamed("ActivatorPool_role", poolPID)
4. 创建 activatorRouter → routerPID
5. SpawnNamed("ActivatorRouter_role", routerPID)
6. meta.mgr = NewActorMgr("actorMgr_role") → 本地 PID 索引
```

### 路由架构

```
Remote Node (调用方)
  │
  │  ActorActive{kind, id}
  ▼
activatorRouter (SpawnNamed: "ActivatorRouter_{kind}")
  │
  │  包装为 hashableActorActive{ActorActive, hash: id}
  ▼
ConsistentHashPool (5 实例, SpawnNamed: "ActivatorPool_{kind}")
  │
  │  按 id 哈希选择实例
  ▼
actorActivator[k]
  │
  │  SpawnNamed(id) + SETNX 注册
  ▼
返回 PID → Remote Node
```

---

## 三层存储

| 层级 | 存储 | 用途 |
|------|------|------|
| **L1** | `activatorMeta.mgr` (内存 `map[id]PID`) | 本地 actor PID 缓存，节点内快速查找 |
| **L2** | Redis Locator | 跨节点定位，key=`actor:kind:id`，TTL=40s |
| **L3** | Consul/etcd | 服务发现，找 kind 对应的节点 |

---

## 关键设计

### 虚拟 Actor 模式

Actor 是"虚拟"的 — 看起来一直存在，但只在第一次访问时才真正 spawn：
- Locator 中注册 → 给人感觉一直在线
- 实际 actor 进程可能不存在
- 消息发到已注册的 PID，protoactor 自动路由

### 一致性哈希选节点

```go
ServiceApp().GetServiceInfo(ctx, kind, key, ConsistentHashSelector())
```

用 `key = "actor:role:12345"` 做 ConsistentHash，选择服务节点。保证**相同 ID 始终路由到同一节点**，实现有状态的负载均衡。

### 两级路由

远程消息先进 `activatorRouter`（外部入口），再包装为 `hashableActorActive` 转发到一致性哈希池：
- Router 是普通 Actor，有一个固定 PID，方便远程节点寻址
- Pool 内部按 `hashableActorActive.Hash()` (即 actor ID) 做一致性哈希分发

### Actor 续约机制

```
actorActivator 启动时注册 TimerTick:
  interval = 30s
  → Lua 批量 SETEX 续约所有 child actors
防止 TTL 过期导致"孤儿 actor"
```

### actorActivator 接收的内部消息

- `*hashableActorActive` — spawn 新的 actor 实例
- `*actor.Terminated` — actor 停止时从本地 mgr 移除并注销 Redis

---

## 代码入口

**外部调用**:
```go
// src/lib/actor.go
func ActivateRole(roleID int64, spawnIfNotExist ...bool) (PID, error)
  → gxyactor.ActivateActor("role", strconv.Itoa(int(roleID)), spawnIfNotExist...)
```

**核心实现**:
```go
// core/gxyactor/activator_manager.go
ActivateActor(kind, id, spawn)  →  L2 Redis → L3 服务发现 → spawnActor
spawnActor(node, kind, id)      →  向远程 router 发 ActorActive
RegisterActorKind(ctx)          →  L1 本地注册 + L2 Redis 续约
```
