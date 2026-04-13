# External Integrations

## MongoDB

### 连接配置
- 连接串从 GoFrame 配置文件读取
- 数据库名: `galaxy`
- 连接池: min/max 配置
- 连接超时: 3s
- 副本集: `rs0`

### 数据模型 (Collections)

| Collection | 用途 | 主键 |
|-----------|------|------|
| `role_account` | 账号-角色映射 | `account` (unique), `role_id` (unique) |
| `role_persist_state` | 角色基础信息 | `role_id` (unique) |
| `role_sign_state` | 签到数据 | `role_id` (unique) |
| `role_bag_state` | 背包数据 | `role_id` (unique) |
| `role_public_state` | 角色公开信息 | `role_id` (unique) |
| `role_extra_persist_state` | 角色扩展数据（定时器状态） | `role_id` (unique) |
| `role_activity_persist_state` | 活动数据 | `role_id` (unique) |

### 操作模式
- **读取**: `FindOne` by `role_id`
- **写入**: `ReplaceOne` with upsert + 乐观锁 (version 字段)
- **脏检查**: 对象 hash 对比，跳过未变更的模块
- **定时保存**: 5s 间隔 Tick，登出/停止时强制保存

### 代码入口
- 封装层: `core/gxymongo/mongo.go` → `mongoApp` struct
- 使用方: `apps/role/internal/logic/role_main.go` (save/load)
- 账号管理: `apps/role/internal/logic/role_account.go`

## Redis

### 用途 1: Grain Locator (定位器)
- **Key 格式**: `gserver:locate:node:actor:{kind}:{id}`
- **Value**: PID 的 JSON 序列化 (`pb.ActorPid`)
- **TTL**: 40 秒
- **刷新间隔**: 30 秒
- **代码**: `core/gxylocator/gxylocator.go`

### 用途 2: Service Registry (服务注册)
- **操作**: 注册/注销/查询服务节点
- **选择器**: 一致性哈希 (ConsistentHash)
- **代码**: `core/gxyregistery/`, `core/gxyservice/service_app.go`

### 用途 3: UID Generator
- **分布式自增 ID**: 基于 Redis INCR
- **用途**: 角色ID生成 (`role` key)
- **代码**: `util/uid/uid.go`

### 连接管理
- 封装层: `core/gxyredis/`
- 全局访问: `gxyredis.Redis()`

## Protoactor-go Remote

### 跨节点通信
- Actor System 启动时绑定 `host:port`
- 远程消息通过 protobuf 序列化传输
- `ActorCallMessage` — 远程 RPC 调用消息
- `PushMessage` — 远程推送消息

### 集群配置
- 集群名: `gcluster`
- 节点发现: 通过 Redis Service Registry
- 消息路由: protoactor-go 内置 PID 路由

---
*Last updated: 2026-04-13*
