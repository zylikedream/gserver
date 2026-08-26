# gserver：一个基于 Actor 模型的 Go 游戏服务器框架

gserver 是一个用 Go 编写的分布式游戏服务器框架，基于 Actor 模型和 protoactor-go。它将游戏后端常见的需求——玩家状态管理、模块间通信、服务发现、配置管理——封装为可复用的基础设施，业务模块可以独立开发和部署。

---

## 为什么选用 Actor 模型

游戏服务器和普通 Web 服务有几个区别：

1. **状态必须常驻内存**。一个在线玩家的背包、任务、属性，操作频率很高（可能每秒多次）。如果每次操作都从数据库读写，延迟和吞吐都无法满足。玩家数据需要一直放在内存里。

2. **逻辑间有大量交叉调用**。好友上线可能触发通知，通知可能触发任务进度更新，任务完成可能触发背包奖励发放。模块间不是独立的，而是频繁互相调用。

3. **需要故障隔离**。一个玩家的逻辑不能阻塞其他玩家。一个玩家触发了 bug，只应该影响他自己。

这三种需求，传统架构各有痛点。单线程模型无法利用多核，吞吐有上限。共享内存 + 互斥锁的模型下，锁的粒度和顺序很难搞对。

Actor 模型对这三个问题给出了直接的解法：

- 每个玩家、公会、聊天频道是一个 Actor。Actor 创建后状态就留在内存里，直到它被销毁。
- Actor 内部是串行的——同一个 Actor 同时只处理一条消息，不需要锁。Actor 之间只通过消息通信，不共享内存。
- 单个 Actor 崩溃，不影响其他 Actor。

gserver 使用 [protoactor-go](https://github.com/asynkron/protoactor-go) 作为 Actor 运行时，它的 remote 功能让 Actor 可以分布在不同进程甚至不同机器上——业务代码不需要关心 Actor 实际跑在哪里，发消息就行了。

## Module 机制

除了 Actor 模型，gserver 的另一个核心设计是 Module 系统。

框架内的一切都是 Module——不管是基础设施组件（Redis、数据库、Actor 系统本身），还是业务 App（role、chat、gateway）。所有 Module 遵循统一的接口：

- `OnModInit` — 初始化。在这个阶段完成配置读取、Schema 迁移、子 Module 加载。
- `OnModStart` — 启动。开始接收请求、建立连接。
- `OnModStartAfter` — 启动后回调。所有子 Module 启动完成后调用，用于需要依赖其他 Module 已就绪的逻辑。
- `OnModStopBefore` / `OnModStop` — 停止前/停止。释放资源、保存状态。

Module 之间是树形结构。父 Module 调用 `AddModule` 加载子 Module，子 Module 还可以再加载自己的子 Module。启动时从根节点开始，深度优先遍历，保证父 Module 先于子 Module 启动；停止时逆序，子 Module 先释放，父 Module 后释放。

举个例子：

```
Node (根)
├── Actor     ← 先启动
├── Redis     ← 先启动
├── Service   ← 依赖 Actor，在 OnModStartAfter 中注册
├── Role      ← 业务 App
│   └── Broadcast  ← Role 的子 Module
└── Chat      ← 业务 App
```

这个设计让模块的组合非常灵活。每个 App 只需要实现这几个方法，然后由 `gxynode` 按 TOML 配置加载。开发环境把所有 App 加到一个 Node 里方便调试，生产环境拆到不同进程，代码写法完全一样。

---

## 分层结构

gserver 按职责分为五层：

### 基础设施层

基础设施层是框架的核心，包含 18 个模块，不涉及任何业务逻辑：

**应用框架**。`gxymodule` 定义了模块生命周期（初始化 → 启动 → 停止）和依赖管理。`gxyapp` 是业务 App 的抽象基类，业务模块继承它来接入框架。`gxynode` 是进程入口，读取 TOML 配置并加载指定的 App。

**Actor 系统**。`gxyactor` 封装 protoactor-go，提供 Actor 的激活、通信和生命周期管理。`gxyservice` 实现基于 Actor 的服务 RPC。`gxymq` 提供消息队列，用于 Actor 间的异步通信。

**网络层**。`gxynet` 处理 TCP 连接（gnet v2 事件驱动），包含 LTPV 编解码和会话管理。`gxyhttp` 提供 HTTP 端点。

**中间件集成**。框架内置了 PostgreSQL（`gxypgx`）、Redis（`gxyredis`）、Consul（`gxyregistery`）、分布式锁（`gxylock`）和定时器（`gxytimer`）。

**可观测性**。`gxylog` 提供结构化日志，`gxytrace` 对接 Tempo 链路追踪，`gxymetrics` 暴露 Prometheus 指标。

### 业务支撑层

业务支撑层提供跨模块共享的能力：Actor 操作工具（获取角色/公会 Actor）、广播消息、Token 签发与验证。

### 业务层

业务层包含 7 个可独立部署的模块：

| 模块 | 职责 |
|------|------|
| gateway | 客户端连接管理，会话生命周期，Token 验证，消息路由 |
| account | 账号注册、登录、认证 |
| role | 角色数据：属性、背包、任务、种植、偷取 |
| chat | 频道聊天 |
| friend | 好友关系管理 |
| guild | 公会 |
| thanks | 感谢/打赏 |

每个模块都有独立的配置模板，可以在 `gxynode` 中按需组合。开发时可以将所有模块跑在一个进程里，生产时选择拆分部署。

### 协议层

客户端协议使用 Protobuf 定义。消息有固定的命名约定：`Req` 前缀为客户端请求，`Rsp` 前缀为服务端响应，`Notify` 前缀为服务端推送。数据结构以 `P` 前缀命名，枚举以 `E` 前缀命名。消息号按文件分配，每文件保留 100 个 ID。

服务端内部消息（如 Actor 启停、频道注册）定义在 `protocol/server/` 下，不设消息号。

### 部署层

框架的部署方案通过配置模板系统实现。每个模块的配置（数据库地址、端口、密钥）由 Jinja2 模板定义，环境变量从 env 文件注入。`gen_config.py` 将二者合并，生成运行时使用的 TOML 配置。部署方式支持 Docker Compose 和 Kubernetes（通过 OpenKruiseGame 管理有状态游戏服）。

---

## 技术栈

| 组件 | 选型 |
|------|------|
| 语言 | Go |
| Actor 框架 | protoactor-go |
| 应用框架 | GoFrame v2 |
| 协议 | Protobuf |
| 数据库 | PostgreSQL |
| 缓存 | Redis |
| 服务发现 | Consul |
| 监控 | Prometheus + Grafana |
| 链路追踪 | Tempo |
| 容器编排 | Docker + OpenKruiseGame (K8s) |

---

> GitHub: [github.com/zylikedream/gserver](https://github.com/zylikedream/gserver)
