# gxyregistery 服务注册模块架构

## 概述

`gxyregistery` 是基于 GoFrame `gsvc.Registry` 接口的多后端服务注册/发现模块。支持 Consul、etcd、Redis 三种后端，通过统一抽象层对外提供服务注册、发现、选择、变更监听能力。

核心职责：
- 各服务节点启动时注册自身信息（服务名、节点名、地址、版本）
- 消费者通过服务名查询可用节点列表
- 提供负载均衡选择策略（随机、轮询、一致性哈希）
- 监听服务列表变更并推送给消费者

## 架构分层

```
┌─────────────────────────────────────────────────────┐
│  消费者层 (gxyservice.ServiceApp / gxyactor)         │
│  GetServiceInfo / GetHashServices / Select           │
├─────────────────────────────────────────────────────┤
│  抽象层 (registery.go / types.go / selector.go)      │
│  IRegistery 接口 + ServiceSelector 策略              │
├─────────────────────────────────────────────────────┤
│  适配层 (redis_registrar / consul_registrar / etcd)    │
│  将不同后端适配为统一的 gsvc.Registry                 │
├──────────────────────┬──────────────────────────────┤
│  Redis  │  Consul (GoFrame 改造版)     │
│  redis/redis.go         │  consul/*.go                 │
└──────────────────────┴──────────────────────────────┘
```

### 各层职责

**抽象层** — `registery.go` + `types.go`
- `IRegistery` 接口定义 Register / Search / UnRegister / GetHashServices
- `ServiceInfo` 数据结构，实现 GoFrame 的 `gsvc.Service` 接口
- `StartWatch(key)` — 启动异步 Watch 协程，2s 防抖合并变更

**选择器层** — `selector.go`  
- 三种内置负载均衡策略：
  - `RandomSelector` — 随机选择
  - `RoundRobinSelector` — 轮询（带 singleflight 原子索引）
  - `ConsistentHashSelector` — 一致性哈希（MD5 + AVL 树虚拟节点环）
- 由 `gxyservice.ServiceApp.GetServiceInfo(name, key, selector)` 调用

**适配层** — `*_registrar.go`
- 工厂函数 `NewRegistery()` 根据 `registery.type` 配置选择后端
- 各后端封装为 `gsvc.Registry` 实现

## 数据流

### 服务注册

```
节点启动
  → gxyservice.serviceApp.Init()
    → gxyregistery.NewRegistery()   // 根据配置选择后端
    → NewServiceInfo(name, nodeName, nodeHost, version, weight)
    → registry.Register(ctx, svcInfo)
      → 后端实现:
        - Consul: AgentServiceRegistration + TTL 心跳协程
        - Redis:    Redis HSET + HEXPIRE (30s TTL) + 20s 心跳循环
```

### 服务发现

```
消费者请求
  → gxyservice.ServiceApp.GetServiceInfo(name, key, selector)
    → registry.GetHashServices(ctx, name)
      → 首次 → Search(ctx, name) + 启动 Watch
      → 后续 → 从缓存 services map 读取
    → selector.Select(ctx, name, key, hashServices)
    → 返回 *ServiceInfo
```

### 变更推送

```
后端检测到变更 (Watch 机制):
  Consul → watch.Plan (consul API 长轮询) → eventChan 通知
  Redis  → 无主动 Watch，依赖 Search 拉取查询

registery.go:
  StartWatch(name) → goroutine
    → watcher.Proceed() 阻塞等待
    → toServiceInfos → compareServiceInfos(旧, 新)
    → 有变化 → 2s 防抖 → updateServices
    → services map 更新 + seq ++
```

## 后端实现

### Redis

**存储**: Redis Hash `gserver:svc:{serviceName}`，field=pod名，value=JSON

```json
field: "role-0"
value: {"Name":"role","NodeName":"role-0@18af...","Version":"v1.0.0","Weight":0,"NodeHost":"10.244.0.152:19001"}
```

**寻址方式**:
NodeHost 在注册时已通过 POD_IP 注入真实 IP，直接从 Redis Hash 的 value 中读取即可。

**心跳机制**:
- 注册时设置 `HEXPIRE key 30 FIELDS 1 field` (Redis 7.4+)
- 后台协程每 20s 对所有活跃 field 批量续期 (`HEXPIRE`)
- 心跳跟踪表: `map[redisKey]set[fieldName]`

**配置** (`config/*.toml`):
```toml
[registery.redis]
interval = 10
interval = 10                                    # poll 间隔(秒)
```

### Consul (本地和 K8s 当前使用)

**存储**: Consul Service Catalog，`/v1/agent/service/register`

**注册参数**:
- Service ID = `GetKey()` → `gserver-{NodeName}-{Name}:{NodeHost}`
- Tags = [serviceName, version]
- Meta = {data: 完整 JSON, ...}
- TTL 健康检查: 注册时 PassTTL，后台协程每 healthCheckInterval 续期

**发现**:
- `Search` → `Health().Service(name, ...)` (仅 HealthPassing)
- `Watch` → `watch.Plan{type: "service"}` Consul 长轮询 → eventChan

### etcd (保留但未使用)

基于 GoFrame 官方的 `contrib/registry/etcd/v2` 封装。启动时执行连接测试（Search prefix="test", 3s 超时），失败则阻断启动。

## 数据结构

### ServiceData (序列化到存储)

| 字段     | JSON  | 说明                                 |
|----------|-------|--------------------------------------|
| Name     | Name  | 服务名/actor kind                    |
| NodeName | NodeName | 节点实例名 `{pod}@{timestamp}`     |
| Version  | Version | 服务版本号                          |
| Weight   | Weight | 权重                                |
| NodeHost | NodeHost | 节点地址 `{host}:{port}`           |

### ServiceInfo 关键方法 (实现 gsvc.Service)

| 方法         | 返回值                                            |
|-------------|--------------------------------------------------|
| GetKey()    | `gserver-{NodeName}-{Name}:{NodeHost}`           |
| GetPrefix() | `gserver-{NodeName}-{Name}`                      |
| GetValue()  | ServiceData 的 JSON                              |
| GetEndpoints() | 从 NodeHost 解析的 Endpoint                    |
| GetMetadata()  | {weight, node_name, host}                      |

### HashServices (缓存+版本)

```go
type HashServices struct {
    ServiceInfos []*ServiceInfo  // 服务节点列表
    Hash         string          // 变更序列号 (seq), 每次更新递增
}
```

## 选择器详解

### 一致性哈希 (ConsistentHashSelector)

**用途**: Actor 路由定位 — 同一种 actor kind 的请求按 key 映射到固定节点

**实现**:
1. 每个服务类型维护一个 AVL 树哈希环
2. 每个物理节点映射 `virtualNodeCount`（默认10）个虚拟节点
3. 哈希函数: MD5 的前 4 字节 → uint32
4. 查询时计算 key 的哈希，在环上找第一个 ≥ 该哈希的节点（环回绕）

**重建条件**: 当 `HashServices.Hash` 与缓存不一致时重建环（服务列表发生了变更）

### 轮询 (RoundRobin)

- 每个服务维护一个 `StrIntMap` 索引
- `singleflight.Group` 保证并发安全
- 每次 Select 后索引 +1 并取模

## 配置

```toml
# config/all.toml
[registery]
    type = "consul"
    # type = "etcd"
    # type = "redis"

[registery.redis]
    interval = 10

[registery.consul]
    address = "127.0.0.1:8500"
    ttl = "20s"
    refresh_ttl = "10s"

[registery.etcd]
    etcd_servers = ["127.0.0.1:2379"]
    update_interval = "10s"
    log_level = "error"
```

## Key 格式约定

所有后端的 key 格式一致:

```
gserver-{NodeName}-{ServiceName}:{NodeHost}
                       ^^^^
        extractPodName 取这个字段剥离 ---角色依赖这个提取 pod 名
```

示例:
```
gserver-role-0@18af226f4813b1dc-role:10.244.0.142:19001
                               ---- Actor kind / 服务名
```

## 与 Actor 系统的关系

`gxyactor.activator_manager` 在激活 actor 时:

```go
// 确定 actor 应该路由到哪个节点
serviceInfo := gxyservice.ServiceApp().GetServiceInfo(ctx, kind, key,
    gxyregistery.ConsistentHashSelector())
// kind = "chat_channel", key = "1_10086"
// → 查 gserver:svc:chat_channel
// → 按 key 做一致性哈希选择节点
// → 返回该节点的地址，建立 gRPC 连接
```

所以 actor kind 名（如 `chat_channel`）同时也是注册中心的 service name，两类 actor 的路由都依赖这个模块：
- **有状态 actor**（role/guild）→ 一致性哈希选节点
- **可激活 actor**（chat_channel）→ 同上，但定位到节点后还需 activator 创建实例
