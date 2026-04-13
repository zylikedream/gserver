# Technology Stack

## Language & Runtime

- **Language:** Go (Golang)
- **Go Version:** 1.23+ (inferred from go.mod dependencies)
- **Module Path:** `gserver`

## Core Frameworks

### Actor System
- **protoactor-go** (`github.com/asynkron/protoactor-go`) — Actor 模型框架，提供 Actor 生命周期管理、消息传递、Supervisor 策略、Remote 远程通信
- **protoactor-go/remote** — 基于 protobuf 的跨节点 Actor 远程调用

### Web Framework
- **GoFrame** (`github.com/gogf/gf/v2`) — 提供 CLI 命令解析 (`gcmd`)、配置管理 (`gcfg`)、日志 (`glog`)、校验 (`gValidator`)、工具函数 (`gconv`, `gstr`, `gutil`) 等
- 未使用 GoFrame 的 HTTP 路由/数据库 ORM 部分，仅作为工具库

### Protocol & Serialization
- **protobuf** (`google.golang.org/protobuf`) — 所有网络消息和 RPC 消息使用 protobuf 定义
- **anypb** — 使用 `google.protobuf.Any` 包装动态消息类型，实现多态消息传递
- **protojson** — protobuf 与 JSON 互转，用于 Grain PID 在 Redis 中的序列化

## Data Storage

### Primary Database
- **MongoDB** (`go.mongodb.org/mongo-driver`) — 角色数据持久化
  - 使用 `ReplaceOne` + Upsert 模式保存
  - 乐观锁（version 字段）防止并发冲突
  - 脏检查（对象 hash 对比）减少不必要的写入

### Cache & Location
- **Redis** (`github.com/redis/go-redis/v9`) — 双重用途
  1. **Grain Locator** — 存储 Grain 类型+ID → PID 的映射关系（TTL 40s，30s 刷新）
  2. **Service Registry** — 服务注册与发现（一致性哈希选择器）
  3. **UID Generator** — 分布式自增 ID 生成

## Network

### TCP Server
- **自定义网络层** (`core/gxynet`) — 基于 Go 标准库 `net` 实现的 TCP 长连接服务器
  - Endpoint 抽象连接端点
  - Message 封装消息帧
  - BaseEventHandler 事件回调接口

### Remote Communication
- **protoactor-go/remote** — Actor 跨节点通信，基于 protobuf 序列化
- Actor System 启动时绑定 host:port，通过 `remote.Configure` 配置

## Utilities

### UID Generation
- **自定义 UID 生成器** (`util/uid`) — 基于 Redis 的分布式自增 ID 生成

### Timer
- **自定义定时器** (`core/gxytimer`) — Actor 内置定时器系统
  - 支持 Tick（重复间隔）、Once（单次延迟）、Cron（定时任务）
  - 定时器状态可持久化恢复（CronState 接口）

### ETS (Erlang Term Storage)
- **util/ets** — 类似 Erlang ETS 的内存表，用于高效键值查找

## Build & Tooling

- **Makefile** — 构建入口
- **Git** — 版本控制
- **Protocol Buffers** — `.proto` 文件编译生成 Go 代码（`protocol/pb/`）

## Configuration

- **GoFrame 配置系统** — 通过 `g.Cfg()` 读取配置文件
- **命令行参数** — 通过 `gcmd` 解析 `--config` 参数指定配置文件路径
- 配置项包括：节点名称、主机地址、应用列表、MongoDB 连接串、Redis 地址等

---
*Last updated: 2026-04-13*
