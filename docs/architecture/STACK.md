# Technology Stack

## Language & Runtime

- **Language:** Go (Golang)
- **Go Version:** 1.25.1
- **Module Path:** `gserver`

## Core Frameworks

### Actor System
- **protoactor-go** (`github.com/asynkron/protoactor-go`) — Actor 模型框架，提供 Actor 生命周期管理、消息传递、Supervisor 策略、Remote 远程通信
- **protoactor-go/remote** — 基于 protobuf 的跨节点 Actor 远程调用
- **protoactor-go/router** — 一致性哈希池路由，用于 Actor 激活分发

### Web Framework
- **GoFrame** (`github.com/gogf/gf/v2`) — 提供 CLI 命令解析 (`gcmd`)、配置管理 (`gcfg`)、日志 (`glog`)、校验 (`gValidator`)、工具函数 (`gconv`, `gstr`, `gutil`) 等
- 未使用 GoFrame 的 HTTP 路由/数据库 ORM 部分，仅作为工具库

### Protocol & Serialization
- **protobuf** (`google.golang.org/protobuf`) — 所有网络消息和 RPC 消息使用 protobuf 定义
- **anypb** — 使用 `google.protobuf.Any` 包装动态消息类型，实现多态消息传递
- **protojson** — protobuf 与 JSON 互转，用于 Actor PID 在 Redis 中的序列化

## Data Storage

### Primary Database
- **PostgreSQL** via **GORM** (`gorm.io/gorm`, `gorm.io/driver/postgres`) — 角色数据持久化
  - `AutoMigrate` 自动创建/更新表结构
  - `db.Save()` 自动判断 INSERT/UPDATE
  - `db.First()` 查询单条记录
  - JSONB 列存储复杂结构（自定义 `Value()`/`Scan()` 方法）
  - 脏标记机制（`MarkDirty`/`IsDirty`）减少不必要的写入
  - 连接池管理（底层 `database/sql`）

### Cache & Location
- **Redis** (`github.com/redis/go-redis/v9`) — 三重用途
  1. **Actor Locator** — 存储 Actor 类型+ID → PID 的映射关系（TTL 40s，30s 批量续约，SETNX 原子注册）
  2. **Service Registry** — 服务注册与发现（一致性哈希选择器）
  3. **UID Generator** — 分布式自增 ID 生成

## Network

### TCP Server
- **gnet v2** (`github.com/panjf2000/gnet/v2`) — 高性能事件驱动网络框架
  - LTPV 自定义封包协议 (Length + Type + Path + Value)
  - Endpoint 抽象连接端点
  - BaseEventHandler 事件回调接口

### Remote Communication
- **protoactor-go/remote** — Actor 跨节点通信，基于 protobuf 序列化
- Actor System 启动时绑定 host:port，通过 `remote.Configure` 配置

## Service Discovery

- **Consul** (`github.com/hashicorp/consul/api`) — 服务注册与发现（默认）
- **etcd** (`go.etcd.io/etcd/client/v3`) — 可选的服务注册与发现后端
- 通过 `core/gxyregistery/` 统一抽象，配置选择后端

## Message Queue

- **Redis Pub/Sub** — 基于 Redis 的消息发布订阅（`core/gxymq/`）
- **Apache Pulsar** — 可选的高吞吐消息队列后端
- 优先级处理：Critical / High / Normal

## Utilities

### UID Generation
- **自定义 UID 生成器** (`src/util/uid`) — 基于 Redis INCR 的分布式自增 ID 生成

### Timer
- **自定义定时器** (`core/gxytimer`) — Actor 内置定时器系统
  - 支持 Tick（重复间隔）、Once（单次延迟）、Cron（定时任务）
  - 定时器状态可持久化恢复（CronState 接口）

### Message Routing
- **自定义消息路由** (`core/gxyutil`) — 基于反射的 protobuf 消息自动分发
  - 扫描导出方法，按参数类型名自动路由
  - 支持两种签名：`(ctx, *Req) (*Rsp, error)` 和 `(ctx, *Req) error`

## Build & Tooling

- **Makefile** — 构建入口（`make build`、`make pb`）
- **Git** — 版本控制（含 submodule: `protocol/`、`gameconfig/`）
- **Protocol Buffers** — `.proto` 文件编译生成 Go 代码（`protocol/pb/`）

## Configuration

- **GoFrame 配置系统** — 通过 `g.Cfg()` 读取 TOML 配置文件
- **命令行参数** — 通过 `gcmd` 解析 `--config` 参数指定配置文件路径
- 配置项包括：节点名称、主机地址、应用列表、PostgreSQL 连接串、Redis 地址、服务发现后端等

## Additional Dependencies

- **go-linq** (`github.com/ahmetb/go-linq`) — LINQ 风格集合操作（背包模块使用）
- **gookit/goutil** (`github.com/gookit/goutil/reflects`) — 反射工具补充
- **pkg/errors** (`github.com/pkg/errors`) — 带堆栈的错误包装

---
*Last updated: 2026-04-29*
