# 系统架构概览

GServer 是一个基于 **Actor 模型**的分布式游戏服务器，使用 [protoactor-go](https://github.com/asynkron/protoactor-go) 作为 Actor 运行时，[GoFrame v2](https://goframe.org) 作为工具框架。

## 架构分层

```
┌─────────────────────────────────────────────────────┐
│                      Node                            │
│  (gxynode.Node — 进程入口，模块树的根)                 │
├─────────────────────────────────────────────────────┤
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐ │
│  │  Redis   │ │   PGX    │ │   MQ     │ │  HTTP  │ │
│  │  (缓存)   │ │ (持久化)  │ │ (消息队列)│ │ (HTTP) │ │
│  └──────────┘ └──────────┘ └──────────┘ └────────┘ │
│  ┌─────────────────────────────────────────────────┐ │
│  │              Actor System                        │ │
│  │  protoactor-go + Activator + Remote              │ │
│  └─────────────────────────────────────────────────┘ │
│  ┌──────────────┐          ┌──────────────────────┐  │
│  │  Gate App    │          │    Role App           │  │
│  │  (TCP网关)   │◄────────►│  (玩家角色 Grain)      │  │
│  └──────────────┘          └──────────────────────┘  │
│  ┌─────────────────────────────────────────────────┐ │
│  │           Service Discovery                     │ │
│  │     Consul (注册/发现) + Watcher (缓存)           │ │
│  └─────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
```

## 模块系统（Module System）

`core/gxymodule/` — 所有功能单元以模块形式组织，形成父子树结构。

### 生命周期

```
Init → Start → StartAfter → StopBefore → Stop
```

### 接口定义

```go
type IModule interface {
    GetModName() string
    OnModInit(ctx context.Context) error
    OnModStart(ctx context.Context) error
    OnModStartAfter(ctx context.Context) error
    OnModStopBefore(ctx context.Context) error
    OnModStop(ctx context.Context) error
    BaseModule() *ModuleBase
}
```

- **Init**: 注册依赖、初始化子模块
- **Start**: 启动服务（监听端口、启动 goroutine）
- **StartAfter**: 依赖其他模块的后置初始化
- **StopBefore**: 停止前的清理
- **Stop**: 停止服务、释放资源

`ModuleBase` 提供默认空实现，子类按需覆盖。

## 节点（Node）

`core/gxynode/node.go` — 每个进程是一个 Node，是模块树的根节点。

### 启动流程

1. `node/main.go` 创建 `Node` 并添加到 `rootModule`
2. `Node.OnModInit`: 加载配置（名称、地址、App 列表）、生成 `NodeInstanceName`、初始化日志、注册内置 App
3. `Node.OnModStart`: 固定加载基础 App：**metrics → trace → redis → pgx → actor → service → http → mq**
4. 再按 `node.apps` 加载业务 App（如 `["gate", "role"]`），最后加载 `thanks`

### NodeInstanceName

格式: `{nodeName}@{timestamp_nano}`，如 `game-2@1743529200000000000`

每次进程启动生成唯一值，用于 Actor 定位和 Consul 服务注册。

## 注册的 App

| App 名称 | 包 | 说明 |
|----------|-----|------|
| `metrics` | `core/gxymetrics/` | Prometheus 指标与 pprof |
| `trace` | `core/gxytrace/` | OpenTelemetry 链路追踪 |
| `redis` | `core/gxyredis/` | Redis 客户端 |
| `pgx` | `core/gxypgx/` | PostgreSQL（GORM） |
| `mq` | `core/gxymq/` | 消息队列（Redis/Pulsar） |
| `actor` | `core/gxyactor/` | Actor 系统 |
| `http` | `core/gxyhttp/` | HTTP 服务器 |
| `service` | `core/gxyservice/` | 服务注册发现 |
| `account` | `src/apps/account/` | 登录预检与账号服务 |
| `chat` | `src/apps/chat/` | 聊天服务 |
| `friend` | `src/apps/friend/` | 好友服务 |
| `role` | `src/apps/role/` | 玩家角色 |
| `gate` | `src/apps/gateway/` | TCP 网关 |
| `guild` | `src/apps/guild/` | 公会服务 |
| `thanks` | `src/apps/thanks/` | 进程退出致谢模块 |

## App 与 Service

### App（`core/gxyapp/`）

顶层模块，全局注册（`RegisterApp(name, app)`），从 TOML 配置加载。

```go
type IApp interface {
    gxymodule.IModule
    AppName() string
}
```

### Service（`core/gxyservice/`）

可被服务发现寻址的 App，扩展了 `ServiceName`、`Weight`、`Version`、`Host` 方法。

- `Service` 结构体嵌入 `ModuleBase`
- `ServiceApp()` 管理 Consul 服务的注册与查询
- `GetAddressByNodeName(ctx, kind, nodeInstanceName)` 通过 nodeInstanceName 查找节点地址

## Actor 系统

详见 [actor-system.md](actor-system.md)。

## 核心子系统

| 系统 | 位置 | 说明 |
|------|------|------|
| 模块系统 | `core/gxymodule/` | 生命周期管理、模块树 |
| Actor 系统 | `core/gxyactor/` | protoactor-go 封装、Activator |
| 网络 | `core/gxynet/` | TCP (gnet v2)、LTPV 编解码 |
| 服务发现 | `core/gxyregistery/` | Consul/etcd、Watcher、选择器 |
| 持久化 | `core/gxypgx/` | GORM + PostgreSQL |
| 缓存 | `core/gxyredis/` | Redis 客户端 |
| 定时器 | `core/gxytimer/` | Cron 定时器 |
| HTTP | `core/gxyhttp/` | HTTP 服务器 |
| 消息队列 | `core/gxymq/` | Redis PubSub / Pulsar |
| 日志 | `core/gxylog/` | zap 日志库 |

## 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| Actor 定位 | Redis(nodeInstanceName) → Consul(地址) | 两层解耦，Redis 存轻量标识，Consul 负责地址解析 |
| Redis TTL | 12h，不续期 | 节点宕机后 key 自动过期，无需手动清理 |
| 服务注册 | Consul + TTL 健康检查 | 与 GoFrame 原生 gsvc 接口兼容 |
| 模块加载顺序 | 依赖先行（redis→pgx→actor→service→业务） | 确保下层基础设施在上层之前就绪 |
| 消息传递 | protoactor-go 的 PID 寻址 | 支持跨进程透明通信 |
| 持久化 | GORM + AutoMigrate | 自动建表迁移，降低运维成本 |
