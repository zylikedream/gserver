# 服务发现

GServer 的服务发现基于 GoFrame 的 `gsvc.Registry` 接口，支持 Consul 和 etcd 两种后端。

## 设计

```
┌─────────────────────────────────────────────────────┐
│                   ServiceApp                         │
│  全局管理器，维护节点地址缓存、服务注册/发现            │
├─────────────────────────────────────────────────────┤
│  ┌─────────────────────┐  ┌───────────────────────┐ │
│  │  Consul Registrar   │  │  etcd Registrar        │ │
│  │  带 TTL 健康检查     │  │  (GoFrame 官方实现)    │ │
│  └─────────┬───────────┘  └───────────────────────┘ │
│            │ Consul Watcher                          │
│  ┌─────────▼───────────┐                            │
│  │  本地地址缓存         │                            │
│  │  (Watcher 自动更新)   │                            │
│  └─────────────────────┘                            │
└─────────────────────────────────────────────────────┘
```

## 注册

### 注册流程

1. ServiceApp 启动时向 Consul 注册自身服务
2. 服务信息包含：
   - **ServiceID**: `{serviceName}-{nodeInstanceName}`
   - **Address**: 节点 IP
   - **Port**: 端口（动态分配）
   - **Metadata**: `kind`、`nodeInstanceName` 等自定义数据
3. 后台 goroutine 定期发送 TTL 心跳维持健康状态

### TTL 健康检查

```
┌─────────────────────────────────────────────┐
│  Consul 注册 → PassTTL(initial)              │
│  └→ goroutine: ticker(10s) → PassTTL        │
│       ├→ 失败 → 重试 3 次 (间隔 2s)          │
│       └→ 连续失败 → 停止心跳（节点标记为不健康）│
│  TTL: 20s → 超过未心跳则标记为 unhealthy      │
└─────────────────────────────────────────────┘
```

## 发现

### Search 接口

直接查询 Consul Health API（`/v1/health/service/{name}`）：

```go
func (r *Registry) Search(ctx context.Context, in SearchInput) ([]Service, error)
```

### Watcher 机制（`consul_watcher.go`）

使用 Consul Watch Plan 监听服务变化：

1. 初始化时查询全量服务列表
2. 通过 Consul watch plan 订阅变更事件
3. 变更时自动更新本地缓存
4. 通过 `eventChan` 通知调用方

```go
type Watcher struct {
    services  []gsvc.Service  // 当前服务列表（缓存）
    eventChan chan struct{}    // 变更通知通道
    plan      *watch.Plan      // Consul watch plan
}
```

## 选择器（`selector.go`）

| 选择策略 | 说明 |
|----------|------|
| `RandomSelector()` | 随机选择 |
| `RoundRobinSelector()` | 轮询 |
| `ConsistentHashSelector()` | 一致性哈希（用于 Actor 定位） |

## 服务注册流程

### Node 启动时的注册链

1. Node 启动 → 加载配置 → `registerApps()`
2. Actor App 初始化（启动 Remote，获得动态地址）
3. Service App 启动 → 使用 `NodeInstanceName` 注册到 Consul
4. 地址格式：`{host}:{dynamic_port}`（来自 protoactor-go Remote）

### 服务注销

节点停止时 → `OnModStop` → Consul Deregister

## 地址解析

`GetAddressByNodeName(ctx, kind, nodeInstanceName)`：

1. 通过 Consul Search 查询 kind 服务的所有实例
2. 匹配 metadata 中的 `nodeInstanceName`
3. 返回匹配实例的地址（`host:port`）

## 配置

```toml
[registery]
  [registery.consul]
  address = "127.0.0.1:8500"
  ttl = "20s"
  refresh_ttl = "10s"
```

## 源码位置

| 文件 | 说明 |
|------|------|
| `core/gxyregistery/consul_registrar.go` | Consul 注册中心初始化 |
| `core/gxyregistery/consul/consul.go` | Consul Registry 实现 |
| `core/gxyregistery/consul/consul_watcher.go` | Consul Watch Plan 监听 |
| `core/gxyregistery/consul/consul_discovery.go` | 服务查询（Search） |
| `core/gxyregistery/etcd_registrar.go` | etcd 注册中心 |
| `core/gxyregistery/selector.go` | 节点选择策略 |
| `core/gxyservice/service_app.go` | 服务 App 管理器 |
