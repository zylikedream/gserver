# GServer

基于 **Actor 模型**的分布式游戏服务器框架，使用 [protoactor-go](https://github.com/asynkron/protoactor-go) 运行时和 [GoFrame v2](https://goframe.org) 工具库。
服务器中以开发种花游戏为例，展示如何使用 Actor 模型实现分布式游戏逻辑。
配置系统使用luban来管理, 前后端通信使用protobuf

## 已有功能
| 功能 | 描述 |
|------|------|
| 角色系统 | 角色创建、登录、注销、角色数据持久化 |
| 背包系统 | 背包创建、物品添加、物品移除 |
| 地块系统 | 地块创建 |
| 花系统 | 花创建、收获、花产量 |
| 任务系统 | 任务创建、任务完成、任务奖励 |
| 鲜花订单系统 | 订单创建、订单完成 |
| 好友系统 | 好友关系、好友列表、好友申请 |
| 公会系统 | 公会创建、加入会员、会员管理 |
| 聊天系统 | 群聊、私聊、聊天记录 |
| 偷花系统 | 偷花、查看花、花产量 |
| 邮件系统 | 发送邮件、接收邮件 |

**Notice: 功能基本是claude code生成的，只做了简单的自测和review，可能会包含未知的bug**


## 架构概览

```
┌──────────────────────────────────────────────────────────┐
│                        Node                              │
│             (进程入口，模块树的根)                          │
├──────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────┐│
│  │              Actor System                            ││
│  │    protoactor-go + Activator + Remote                ││
│  ├──────────────────────────────────────────────────────┤│
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐ ││
│  │  │  Redis   │ │   PGX    │ │    MQ    │ │  HTTP  │ ││
│  │  │  (缓存)   │ │ (持久化)  │ │ (消息队列)│ │ (HTTP) │ ││
│  │  └──────────┘ └──────────┘ └──────────┘ └────────┘ ││
│  ├──────────────────────────────────────────────────────┤│
│  │  ┌──────────────┐  ┌──────────────┐  ┌───────────┐ ││
│  │  │  Gate App    │  │  Role App    │  │  Chat App │ ││
│  │  │  (TCP 网关)  │  │  (玩家角色)   │  │  (聊天)   │ ││
│  │  └──────────────┘  └──────────────┘  └───────────┘ ││
│  ├──────────────────────────────────────────────────────┤│
│  │              Service Discovery                       ││
│  │       Consul (注册/发现) + Watcher (缓存)             ││
│  └──────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────┘
```

核心设计：每个进程是一个 **Node**，Node 内以 **Module** 组织功能单元形成模块树，业务逻辑运行在 **Actor** 中，通过 **Service Discovery** 实现跨进程通信。

## 快速开始

开发环境初始化流程见 [docs/public/svr_init.md](docs/public/svr_init.md)（模板化配置生成、基础服务启动、数据重置等完整指南）。

### 依赖

- Go 1.25+
- PostgreSQL
- Redis
- Consul（服务发现）

### 本地环境启动

```bash
# 1. 启动docker环境
docker compose -f deploy/docker/docker-compose.yml up -d 

# 1. 拉取子模块
git submodule update --init --recursive

# 1. 安装依赖
go mod download

# 2. 初始化环境（生成配置、脚本）
./build/script/svr_init.sh dev_xx(你自己的环境)

# 3. 启动gate+单节点（包含 role 等全部模块）
# 3. 启动最小三节点：账号服 + gate + 单节点（包含 role 等全部模块）
go run node/main.go --config config/account.toml
go run node/main.go --config config/gate.toml
go run node/main.go --config config/all.toml
```

### 本地kind部署到K8s
```bash
# 1. 启动 K8s 集群
kind create cluster --name game-cluster --config deploy/k8s/kind-config.yaml

# 3. 第一次部署 OKG 版本, 下面的命令会自动下载okg, 并部署到k8s集群
make deploy-k8s-okg
```
更多命令和文档见 [docs/public/k8s-kind-deploy.md](docs/public/k8s-kind-deploy.md)

### 运行测试

```bash
# go test 需要 -gcflags=-l 禁用内联以兼容 gomonkey
make test

# 单包测试
go test -gcflags=-l ./src/apps/role/...
```

## 项目结构

```
├── node/main.go              # 进程入口
├── core/                     # 分布式框架层
│   ├── gxyactor/             # Actor 系统封装
│   ├── gxynet/               # TCP 网络 (gnet v2)
│   ├── gxypgx/               # PostgreSQL 持久化 (GORM)
│   ├── gxyredis/             # Redis 缓存
│   ├── gxyregistery/         # 服务发现 (Consul/etcd)
│   ├── gxymodule/            # 模块生命周期管理
│   ├── gxytimer/             # Cron 定时器
│   ├── gxylog/               # 日志 (zap)
│   ├── gxymetrics/           # Prometheus 指标
│   ├── gxytrace/             # OpenTelemetry 链路追踪
│   └── gxyhttp/              # HTTP 服务
├── src/apps/                 # 业务应用
│   ├── gateway/              # TCP 网关（连接管理、协议路由）
│   ├── role/                 # 玩家角色（核心玩法）
│   ├── chat/                 # 聊天
│   ├── friend/               # 好友
│   └── guild/                # 公会
├── protocol/                 # 协议定义 (protobuf)
├── gameconfig/               # 游戏配置表
├── config/                   # 运行配置 (TOML)
├── deploy/                   # 部署配置 (Docker/K8s)
└── docs/                     # 架构设计文档
```

## 核心概念

### 模块系统 (Module)

所有功能单元以 **Module** 形式组织，生命周期：`Init → Start → StartAfter → StopBefore → Stop`

```go
type IModule interface {
    OnModInit(ctx context.Context) error
    OnModStart(ctx context.Context) error
    OnModStop(ctx context.Context) error
}
```

### Actor 模型

基于 protoactor-go，提供：
- **Activator** — 按需激活 Actor（如玩家上线时才创建对应的 Role Actor）
- **Remote** — 跨进程 Actor 通信
- **PID 寻址** — 通过 `{node}:{kind}/{id}` 格式定位 Actor

### 服务发现

- **Consul** — 服务注册与健康检查（TTL 模式）
- **Redis** — Actor 归属缓存（`role_id → node_instance_name`），支持快速重连
- **Watcher** — 本地缓存 Consul 服务地址，避免每次调用都查询

### 持久化

- GORM + PostgreSQL，自动建表迁移
- 按 module 维度拆分状态表，乐观锁版本号冲突检测
- 脏标记（dirty flag）避免无效写入

## 配置

服务配置使用 TOML 格式，支持多节点部署：

```toml
[node]
    name = "game"
    host = "0.0.0.0"
    apps = ["gate", "role", "chat", "friend", "guild"]
[port]
    actor = 25101
[postgres]
    url = "postgresql://..."
[redis]
    addr = "localhost:6379"
    password = "..."
[registery]
    type = "consul"
[registery.consul]
    address = "localhost:8500"
```

## 扩展方式

### 添加新 App

```go
// 1. 创建模块
type MyModule struct {
    gxymodule.ModuleBase
}

func (m *MyModule) OnModInit(ctx context.Context) error {
    // 初始化逻辑
    return nil
}

// 2. 注册到 App
app.RegisterModule(&MyModule{})
```

### 添加新协议

1. 在 `protocol/client/` 下定义 `.proto` 文件
2. 在消息上标注 `option (msg_id) = N;`
3. 运行 `make pb` 生成 Go 代码
4. 在对应 Module 上实现 `ReqXxx(ctx, req) (rsp, error)` 方法，框架自动路由


### 可观测性
- **Prometheus** — 指标采集
- **OpenTelemetry** — 链路追踪
- **Grafana + Tempo** — 可视化展示

## 技术栈

| 组件 | 选型 |
|------|------|
| 语言 | Go 1.25+ |
| Actor 框架 | protoactor-go |
| 工具框架 | GoFrame v2 |
| 网络 | gnet v2 (TCP) |
| 持久化 | GORM + PostgreSQL |
| 缓存 | Redis |
| 服务发现 | Consul / etcd |
| 指标 | Prometheus |
| 链路追踪 | OpenTelemetry (Tempo) |
| 协议 | Protocol Buffers |

## License
MIT

## 更多文档docs/public/

- [服务器初始化指南](docs/public/svr_init.md)
- [核心使用指南](docs/public/core-usage.md)
- [本地kindk8s部署指南](docs/public/k8s-kind-deploy.md)
- [通信格式](docs/public/protocol.md)
- **功能文档** 在docs/public/system/目录下

## todo
1. gate和role限流
2. http的账号服务器和token认证
3. CI:合入前自动验证(make test + k8s 冒烟闸门)
4. 告警:开发阶段看日志为主;稳定后接 Grafana Alerting(本地)+ Alertmanager(k8s)

[MIT](LICENSE)
