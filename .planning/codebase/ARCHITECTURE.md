# Architecture

## 设计哲学

本项目采用 **Actor 模型** 构建分布式游戏服务器，核心灵感来自 Erlang/OTP 和微软 Orleans 的虚拟 Actor（Grain）架构。

**核心原则：**
- 每个玩家角色是一个 Grain（虚拟 Actor），通过唯一 ID 标识
- Grain 可以在集群任意节点上激活，通过 Redis 实现位置透明
- 所有状态变更在单个 Actor 内串行处理，天然避免并发冲突
- 模块化设计：框架层与业务层严格分离

## 架构分层

```
┌─────────────────────────────────────────────────────────┐
│                    Node (进程入口)                        │
│  node/main.go — 读取配置，组装模块树，管理生命周期          │
├─────────────────────────────────────────────────────────┤
│                    App (应用层)                           │
│  gateway  — 网关服务（连接管理、消息路由）                   │
│  role     — 角色服务（Grain 注册、业务逻辑）                │
│  world    — 世界服务（全局单例 Actor，如活动系统）           │
│  redis    — Redis 客户端模块                              │
│  mongo    — MongoDB 客户端模块                            │
│  mq       — 消息队列模块                                  │
│  service  — 服务注册/发现模块                              │
├─────────────────────────────────────────────────────────┤
│                    Core (框架层)                          │
│  gxyactor   — Actor 系统（ActorBase, GrainBase, 定位器）   │
│  gxymodule  — 模块系统（树状结构、生命周期管理）             │
│  gxynet     — 网络层（TCP server, endpoint, message）      │
│  gxymongo   — MongoDB 封装                               │
│  gxyredis   — Redis 封装                                 │
│  gxytimer   — 定时器系统                                  │
│  gxylocator — 分布式定位器                                │
│  gxyservice — 服务注册/发现                               │
│  gxyregistery— 注册中心实现                               │
│  gxyhttp    — HTTP 服务                                   │
│  gxylog     — 日志系统                                    │
├─────────────────────────────────────────────────────────┤
│                    Util (工具层)                          │
│  msg_handler — 基于反射的消息路由                          │
│  ets         — 内存键值表                                 │
│  uid         — 分布式 ID 生成                             │
│  reflect     — 反射工具                                   │
│  common      — 通用工具                                   │
├─────────────────────────────────────────────────────────┤
│                    Protocol (协议层)                      │
│  pb/ — Protobuf 消息定义                                  │
│  message.go — 消息类型常量                                │
└─────────────────────────────────────────────────────────┘
```

## Actor 系统 (核心)

### 类型层次

```
actor.Actor (protoactor-go 接口)
    ↓
IActor (gxyactor 接口)
  - Init(ctx) error
  - DelayInit(ctx) error
  - Terminate(ctx, err)
  - Timer() *ActorTimer
  - Self() PID
    ↓
ActorBase (基础实现)
  - 消息接收分发 (doReceive)
  - Send/Call/LocalSend 消息发送
  - 定时器管理
  - MsgHandler 反射路由
  - Supervisor 策略 (OneForOne, 10次/3秒)
    ↓
IGrain (虚拟 Actor 接口)
  - GrainID() string
    ↓
GrainBase (Grain 实现)
  - 从 ActorContext.InitArgs 提取 GrainID
  - 注册/注销到 Redis Locator
```

### Grain 生命周期

```
1. GetGrain(kind, id) 被调用
2. grainManager 查询 Redis Locator:
   a. 找到 → 返回已有 PID
   b. 未找到 → 通过 Service Registry 选择节点
3. 向目标节点的 grainActivator 发送 ActorActive 消息
4. grainActivator 创建新 Actor:
   a. SpawnNamed (带 ContextDecorator 传递 grainID)
   b. Touch 验证 Actor 已启动
   c. 注册 PID 到 Redis Locator (TTL 40s)
   d. 返回 PID
5. Actor 接收消息处理业务逻辑
6. Actor 停止时:
   a. 从 Redis Locator 注销
   b. 从 grainActivator 的 childs map 移除
   c. 触发 Terminate 回调（保存数据）
```

### GrainActivator 路由

每种 Grain 类型注册一个 `grainActivator`，使用一致性哈希池（5 个实例）：
- 同一类型的 Grain 请求会被哈希到固定的 Activator
- Activator 负责在该节点上 spawn 具体的 Grain Actor
- 通过 protoactor-go 的 `router.NewConsistentHashPool` 实现

## 模块系统 (gxymodule)

### 树状结构

```
ModuleBase
  ├── self   — 自身 IModule 引用
  ├── parent — 父模块引用
  └── childs — 子模块列表

IModule 接口:
  - GetModName() string
  - OnModInit(ctx) error       // 初始化
  - OnModStart(ctx) error      // 启动
  - OnModStartAfter(ctx) error // 启动后（依赖其他模块时使用）
  - OnModStop(ctx) error       // 停止
  - OnModStopBefore(ctx) error // 停止前
```

### 生命周期

```
Init 阶段（深度优先，从父到子）:
  parent.OnModInit → child1.OnModInit → child2.OnModInit

Start 阶段:
  parent.OnModStart → child1.OnModStart → child2.OnModStart
  → child1.OnModStartAfter → child2.OnModStartAfter

Stop 阶段（逆序）:
  child2.OnModStop → child1.OnModStop → parent.OnModStop
```

## 消息流转

### 客户端消息流

```
Client (TCP)
  → gxynet (endpoint 接收)
  → GateHandler.OnMessage
  → Session Actor (LocalSend, 不经过序列化)
  → Session.OnHandleClientMessage
    → handshake: 本地处理，获取 RoleGrain PID
    → data: CallSync(RoleGrain, ClientMsg{path, anypb.Msg})
  → RoleMain Grain (远程或本地)
  → RoleMain.HandleClientMsg
    → anypb.UnmarshalNew 解包
    → MsgHandler.CallWithMsg 反射路由到对应模块方法
    → 返回 ServerMsg{anypb.Msg}
  → Session.OnHandleServerMessage
  → endpoint.SendMsg → Client
```

### 消息类型

| 类型 | 用途 | 序列化 |
|------|------|--------|
| `ClientMsg` | Session → Role 请求 | anypb 包装 |
| `ServerMsg` | Role → Session 响应 | anypb 包装 |
| `ActorCallMessage` | 远程 RPC 调用 | anypb 包装 |
| `PushMessage` | 远程推送 | JSON 序列化 |
| `ActorStop` | 停止 Actor | protobuf |
| `ActorActive` | 激活 Grain | protobuf |
| 本地消息 (any) | 同节点内通信 | 无序列化 |

## 数据持久化模型

### 持久化状态接口

```go
IPersistState interface {
    SetRoleID(roleID int64)
    GetVersion() int64 / SetVersion(version int64)
    GetUpdateAt() time.Time / SetUpdateAt(time.Time)
    GetIndexes() []mongo.IndexModel
}
```

### RolePersistState (基类)

```go
RolePersistState struct {
    RoleID   int64     `bson:"role_id"`
    UpdateAt time.Time `bson:"update_at" hash:"-"`
    Version  int64     `bson:"version" hash:"-"`
}
```

- `hash:"-"` 标签：从脏检查 hash 计算中排除
- `bson:"inline"` 标签：嵌入到子结构体时继承字段

### 保存策略

1. **脏检查**: 计算模块状态 hash，与上次保存的 hash 对比
2. **乐观锁**: `filter = {role_id, version}`，保存时 version+1
3. **Upsert**: 不存在则插入，存在则替换
4. **定时保存**: 5s 间隔 Tick
5. **强制保存**: 建号、登出、Actor 停止时

## 服务发现（双层）

### Layer 1: Service Registry
- **作用**: 服务节点注册与发现
- **存储**: Redis Hash
- **选择**: 一致性哈希 (ConsistentHashSelector)
- **流程**: ServiceApp.OnModStartAfter → 注册所有 IService

### Layer 2: Grain Locator
- **作用**: Grain 实例的精确位置
- **存储**: Redis KV (TTL 40s)
- **流程**: Grain 创建时注册，停止时注销，30s 心跳刷新

### 查找流程

```
GetGrain(kind, id)
  → Redis Locator 查找 → 找到 → 返回 PID
  → 未找到 → Service Registry 选择节点 → spawnGrain → 注册 Locator → 返回 PID
```

## 角色内部架构

```
RoleMain (Grain)
  ├── RoleBasic      — 基础信息 (名称、头像、登录时间)
  ├── RoleSign       — 签到系统
  ├── RoleBag        — 背包系统 (物品、货币)
  ├── RolePublic     — 公开信息 (用于排行榜等)
  └── RoleExtra      — 扩展数据 (Cron 定时器状态)

每个模块:
  - 实现 IRoleModule 接口
  - 继承 RoleModule (内嵌 ModuleBase)
  - 持有独立的 PersistState (独立 MongoDB collection)
  - 通过 MsgHandler 自动路由 protobuf 消息到方法
  - 通过 EventBus 发布/订阅内部事件
```

---
*Last updated: 2026-04-13*
