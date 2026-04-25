# GServer Actor 定位机制

## 概述

GServer 基于 **protoactor-go** 实现 Virtual Actor 模式，通过**两层定位**机制实现分布式 actor 的查找和路由：

```
┌─────────────────────────────────────────────────────────────────┐
│                         调用方                                    │
│  GetRoleGrain(roleID) → GetGrain("role", "12345")              │
└────────────────────────────┬────────────────────────────────────┘
                             │
         ┌───────────────────┴───────────────────┐
         │  1️⃣ 先查 Redis Locator (本地缓存)       │
         │     key = "actor:role:12345"          │
         │     如找到 → 直接返回 PID               │
         └───────────────────┬───────────────────┘
                             │ 未命中
         ┌───────────────────┴───────────────────┐
         │  2️⃣ 查服务发现 (Consul/etcd)          │
         │     找 kind=role 的节点               │
         │     用 ConsistentHashSelector 选节点  │
         └───────────────────┬───────────────────┘
                             │ 找到节点
         ┌───────────────────┴───────────────────┐
         │  3️⃣ 向目标节点发 ActorActive 消息     │
         │     节点 spawn grain 并返回 PID       │
         └───────────────────────────────────────┘
```

---

## 核心组件

### 1. grainMeta — Grain 类型元数据

```go
type grainMeta struct {
    Kind      string              // "role"
    Props     *actor.Props         // actor 启动配置
    Activator PID                  // 固定 5 个 router 的 PID
    mgr       *ActorMgr            // 本地 ActorMgr，管理该类型的所有 grain PID
}
```

### 2. grainActivator — Grain 工厂 Actor

每个 grain 类型有一个 **5 个节点的 ConsistentHashPool router**，负责：
- 接收 `ActorActive{Id, Kind}` 消息
- Spawn 对应 grain actor
- 注册到 Redis Locator
- 返回 PID 给调用方

```go
type grainActivator struct {
    *ActorBase
    kind    string                // grain 类型
    manager *grainManager           // 父管理器
    childs  map[PID]string          // child PID → grain ID 映射
    meta    *grainMeta             // 类型元数据
}
```

### 3. grainManager — 全局 Grain 管理器

```go
type grainManager struct {
    grainLocator *gxylocator.Locator  // Redis 定位器
    grainMetas   map[string]*grainMeta // kind → 元数据
}
```

### 4. Redis Locator — Actor 位置缓存

key 格式: `{prefix}:locate:node:actor:{kind}:{id}`
value: `{"Address":"node1:8080","Id":"role/12345"}`

TTL = 40s，每 30s refresh 一次。

---

## 定位流程详解

### GetGrain(kind, id) 的完整时序

```
1. key = "actor:role:12345"

2. grainLocator.LocateNode(key)
   → Redis GET "gserver:locate:node:actor:role:12345"
   → 命中 → 反序列化 → 返回 PID → 完成

3. 未命中 + spawn=true:
   → ServiceApp().GetServiceInfo(ctx, "role", key, ConsistentHashSelector)
   → 从 Consul/etcd 找 "role" 服务，用 key 做一致性哈希选节点

4. 找到节点 nodeHost:
   → 向 nodeHost 上的 GrainManager router 发 ActorActive{Id:"12345", Kind:"role"}

5. GrainManager router 路由到某个 grainActivator 实例:
   → actorProps = meta.Props.Clone()
   → SpawnNamed(props, "12345")
   → go touch(pid) 确保 actor 启动完成
   → registerGrainNode(key, selfPID) → Redis SETEX(ttl=40s)
   → meta.mgr.Add(id, pid) → 本地 ActorMgr 缓存
   → 返回 PID 给调用方
```

### RegisterGrain(kind, producer) — Grain 类型注册

```
1. 创建 grainProps = actor.PropsFromProducer(producer)
2. 创建 5 节点 ConsistentHashPool router
3. SpawnNamed("GrainManager_role", router)
4. 每个 router 节点是一个 grainActivator actor
5. gmeta.mgr = NewActorMgr("grainMgr_role") → 本地 PID 索引
```

---

## 三层存储

| 层级 | 存储 | 用途 |
|------|------|------|
| **L1** | `grainMeta.mgr` (内存 `map[id]PID`) | 本地 grain PID 缓存，节点内快速查找 |
| **L2** | Redis Locator | 跨节点定位，key=`actor:kind:id`，TTL=40s |
| **L3** | Consul/etcd | 服务发现，找 kind 对应的节点 |

---

## 关键设计

### Virtual Actor (Virtual Actor Pattern)

Grain 是"虚拟"的 — 看起来一直存在，但只在第一次访问时才真正 spawn：
- Locator 中注册 → 给人感觉一直在线
- 实际 actor 进程可能不存在
- 消息发到已注册的 PID，protoactor 自动路由

### 一致性哈希选节点

```go
ServiceApp().GetServiceInfo(ctx, kind, key, ConsistentHashSelector)
```

用 `key = "actor:role:12345"` 做 ConsistentHash，选择服务节点。保证**相同 ID 始终路由到同一节点**，实现有状态的负载均衡。

### Grain 续约机制

```
registerGrain 时启动 TimerTick:
  interval = 30s
  → Redis SETEX(key, pidInfo, 40s)
防止 TTL 过期导致"孤儿 grain"
```

### grainActivator 接收的内部消息

- `*pb.ActorActive` — spawn 新的 grain 实例
- `*ActorCheckResult` — touch 确认后记录到本地 mgr
- `*actor.Terminated` — grain 停止时从本地 mgr 移除

---

## 代码入口

**外部调用**:
```go
// src/lib/grain.go
GetRoleGrain(roleID int64, spawnIfNotExist ...bool) PID
  → gxyactor.GetGrain("role", strconv.Itoa(roleID), spawnIfNotExist...)
```

**核心实现**:
```go
// core/gxyactor/grain_manager.go
GetGrain(kind, id, spawn)  →  L2 Redis → L3 服务发现 → spawnGrain
spawnGrain(node, kind, id) →  向远程 activator 发 ActorActive
registerGrain(ctx)         →  L1 本地注册 + L2 Redis 续约
```
