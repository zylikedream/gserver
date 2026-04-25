# External Integrations

## PostgreSQL

### 连接配置
- 连接串从 GoFrame 配置文件读取 (game.toml)
- 连接池: pgxpool, min/max 可配置
- 数据库: gserver

### 数据模型 (Tables)

| Table | 用途 | 主键 |
|-------|------|------|
| `role_account` | 账号-角色映射 | `account` (unique), `role_id` (unique) |
| `role_persist_state` | 角色基础信息 | `role_id` (unique) |
| `role_sign_state` | 签到数据 | `role_id` (unique) |
| `role_bag_state` | 背包数据 | `role_id` (unique) |
| `role_public_state` | 角色公开信息 | `role_id` (unique) |
| `role_extra_persist_state` | 角色扩展数据（定时器状态） | `role_id` (unique) |
| `role_activity_persist_state` | 活动数据 | `role_id` (unique) |

### 操作模式
- **读取**: `FindOne` by `role_id` — pgx 查询单行，反射填充结构体
- **写入**: `UpsertOne` — `INSERT ... ON CONFLICT (role_id) DO UPDATE SET ...`，JSONB 列存储复杂结构
- **脏检查**: JSONB 合并，跳过未变更的模块
- **定时保存**: 5s 间隔 Tick，登出/停止时强制保存
- **并发安全**: 依赖 Actor 单例保证（Redis SETNX + TTL），无需乐观锁

### 结构体映射
- `db:"snake_case"` — 列名映射
- `db:"inline"` — 嵌入结构体展平
- `db:"-"` — 排除非持久化字段
- Map/slice 字段自动序列化为 JSONB

### 代码入口
- 封装层: `core/gxypgx/pgx.go` → `PGXApp` struct (pgxpool 连接池管理)
- 使用方: `src/apps/role/internal/logic/role_main.go` (save/load)
- 账号管理: `src/apps/role/internal/logic/role_account.go`

## Redis

### 用途 1: Grain Locator (定位器)
- **Key 格式**: `gserver:locate:node:actor:{kind}:{id}`
- **Value**: PID 的 JSON 序列化 (`pb.ActorPid` via protojson)
- **注册**: SETNX 原子注册，防止重复激活
- **TTL**: 40 秒
- **续约**: 30 秒间隔，Lua 批量 SETEX
- **注销**: Lua 条件删除（校验值匹配）
- **代码**: `core/gxylocator/gxylocator.go`, `core/gxylocator/script/locate.lua`

### 用途 2: Service Registry (服务注册)
- **操作**: 注册/注销/查询服务节点
- **选择器**: 一致性哈希 (ConsistentHash)
- **代码**: `core/gxyregistery/`, `core/gxyservice/service_app.go`

### 用途 3: UID Generator
- **分布式自增 ID**: 基于 Redis INCR
- **用途**: 角色ID生成 (`role` key)
- **代码**: `src/util/uid/uid.go`

### 连接管理
- 封装层: `core/gxyredis/`
- 全局访问: `gxyredis.Redis()`

## Service Discovery

### Consul (默认)
- **代码**: `core/gxyregistery/`
- **用途**: 服务节点注册与发现，健康检查

### etcd (可选)
- **代码**: `core/gxyregistery/`
- **用途**: 替代 Consul 的服务注册后端

### 选择策略
- Random — 随机选择
- RoundRobin — 轮询选择
- ConsistentHash — 一致性哈希（虚拟节点 + AVL 树环）

## Protoactor-go Remote

### 跨节点通信
- Actor System 启动时绑定 `host:port`
- 远程消息通过 protobuf 序列化传输
- `ActorCallMessage` — 远程 RPC 调用消息
- `PushMessage` — 远程推送消息

### 集群配置
- 集群名: `gcluster` (常量，预留)
- 节点发现: 通过 Service Registry (Consul/etcd)
- 消息路由: protoactor-go 内置 PID 路由

## Message Queue

### Redis Pub/Sub (默认)
- **代码**: `core/gxymq/`
- **优先级**: Critical / High / Normal 三级处理

### Apache Pulsar (可选)
- **代码**: `core/gxymq/`
- **用途**: 高吞吐场景的消息队列

---
*Last updated: 2026-04-22*
