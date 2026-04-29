# 架构图

## 系统整体架构

```
                              ┌─────────────────────────────────────────────────┐
                              │                  Cluster                      │
                              │                                               │
  ┌──────────┐  TCP           │  ┌─────────────────────────────────────────┐   │
  │          │ ──────────────► │  │            Node 1 (gate+role)          │   │
  │          │                │  │                                         │   │
  │          │  TCP           │  │  ┌─────────┐    ┌──────────────────┐    │   │
  │          │ ──────────────► │  │  │ Gateway  │    │  Actor System    │    │   │
  │  Client  │                │  │  │   App    │    │  (protoactor-go) │    │   │
  │  (TCP)   │                │  │  └────┬────┘    └────────┬─────────┘    │   │
  │          │                │  │       │                   │              │   │
  │          │  ServerMsg     │  │  ┌────▼───────────────────▼─────────┐    │   │
  │          │ ◄────────────── │  │  │         Session Actor            │    │   │
  │          │                │  │  │  · 握手认证                       │    │   │
  └──────────┘                │  │  │  · 消息转发                       │    │   │
                              │  │  · 超时检测                       │    │   │
                              │  │  └────────────┬───────────────────┘    │   │
                              │  │               │ CallSync(ClientMsg)     │   │
                              │  │  ┌────────────▼───────────────────┐    │   │
                              │  │  │       RoleMain Actor           │    │   │
                              │  │  │  ┌────────┐ ┌────────┐        │    │   │
                              │  │  │  │ Basic  │ │  Bag   │        │    │   │
                              │  │  │  └────────┘ └────────┘        │    │   │
                              │  │  │  ┌────────┐ ┌────────┐        │    │   │
                              │  │  │  │ Public │ │ Extra  │        │    │   │
                              │  │  │  └────────┘ └────────┘        │    │   │
                              │  │  └───────────────────────────────┘    │   │
                              │  └─────────────────────────────────────────┘   │
                              │                        │ remote               │
                              │                        │                      │
                              │  ┌─────────────────────────────────────────┐   │
                              │  │           Node 2 (role)                │   │
                              │  │                                         │   │
                              │  │  ┌──────────────────────────────────┐  │   │
                              │  │  │     RoleMain Actor (远程)         │  │   │
                              │  │  │  Basic │ Bag │ Public │ Extra   │  │   │
                              │  │  └──────────────────────────────────┘  │   │
                              │  └─────────────────────────────────────────┘   │
                              │                                               │
                              └───────────────────────────────────────────────┘
```

## Node 内部模块树

```
                              rootModule (ModuleBase)
                                   │
                              ┌────▼────┐
                              │  Node   │  core/gxynode/node.go
                              │ (gxynode)│  读取配置, 组装模块
                              └────┬────┘
                                   │ loadApp (递归)
               ┌───────┬───────┬───┼───┬───────┬───────┐
               │       │       │   │   │       │       │
        ┌──────▼──┐ ┌──▼──┐ ┌─▼─┐ │ ┌─▼───┐ ┌─▼───┐ ┌─▼──┐
        │redisApp │ │pgxApp│ │actor│ │ │service│ │roleApp│ │gateApp│
        │         │ │(GORM)│ │ App │ │       │ │       │ │      │
        │ Redis   │ │      │ │     │ │       │ │Schema │ │Network│
        └─────────┘ └─────┘ │     │ │       │ │       │ │      │
                           └──┬──┘ │       │ └───────┘ └──────┘
                              │    │       │
                       ┌──────▼──┐ └───┬───┘
                       │activator│     │roleService
                       │Manager  │     │
                       │         │     │ RegisterActorKind
                       │Router   │     │  → RoleMain
                       │Pool(x5) │     │
                       │Locator  │     │
                       └─────────┘     │
                                      └──────┐
                               ┌──────┴──┐ ┌────────┐
                               │ httpApp │ │  mqApp │
                               └─────────┘ └────────┘
```

## 消息流转详解

```
   Client                 Session Actor              RoleMain Actor
     │                         │                          │
     │  ── TCP Connect ──►     │                          │
     │                         │  ── Spawn ──►            │
     │                         │                          │
     │  ── ReqHandShake ──►    │                          │
     │                         │  ActivateRole(roleID)     │
     │                         │─────────────────────────►│
     │                         │                          │
     │                         │  ◄──── PID ─────────────│
     │                         │  Watch(RolePid)          │
     │  ◄── RspHandShake ──    │                          │
     │                         │                          │
     │  ── ReqAccountLogin ──► │                          │
     │                         │  ── CallSync ──────────► │
     │                         │     ClientMsg{id, msg}    │
     │                         │                          │
     │                         │              HandleClientMsg
     │                         │              MsgHandler 路由
     │                         │              → ReqAccountLogin()
     │                         │                          │
     │                         │  ◄── ServerMsg ─────────│
     │  ◄── RspAccountLogin ─  │     {msg}                │
     │                         │                          │
     │  ── ReqBagInfo ──►      │                          │
     │                         │  ── CallSync ──────────► │
     │                         │                          │  → RoleBag.Handle
     │                         │  ◄── ServerMsg ─────────│
     │  ◄── RspBagInfo ──      │                          │
     │                         │                          │
     │  ── ReqBasicInfo ──►    │                          │
     │                         │  ── CallSync ──────────► │
     │                         │                          │  → RoleBasic.Handle
     │                         │  ◄── ServerMsg ─────────│
     │  ◄── RspBasicInfo ──    │                          │
```

## Actor 分布式定位

```
   ┌──────────────┐       ┌──────────────┐       ┌──────────────┐
   │   Node 1     │       │   Node 2     │       │   Node 3     │
   │              │       │              │       │              │
   │ RoleMain    │       │ RoleMain    │       │ RoleMain    │
   │  (role:100) │       │  (role:101) │       │  (role:102) │
   │              │       │              │       │              │
   │ activator   │       │ activator   │       │ activator   │
   │  Router     │       │  Router     │       │  Router     │
   │  Pool(x5)   │       │  Pool(x5)   │       │  Pool(x5)   │
   └──────┬───────┘       └──────┬───────┘       └──────┬───────┘
          │                      │                      │
          └──────────────────────┼──────────────────────┘
                                 │
                          ┌──────▼──────┐
                          │    Redis     │
                          │              │
                          │  ┌────────┐  │
                          │  │Locator │  │  Actor 位置映射
                          │  │        │  │  actor:role:100 → PID(node1)
                          │  │SETNX   │  │  actor:role:101 → PID(node2)
                          │  │TTL=40s │  │  actor:role:102 → PID(node3)
                          │  └────────┘  │
                          │              │
                          │  ┌────────┐  │
                          │  │Service │  │  服务节点注册
                          │  │Registry│  │  role → [node1, node2, node3]
                          │  └────────┘  │  (一致性哈希选择)
                          └──────────────┘

查找流程:
  ActivateActor("role", "100")
    → Redis Locator 查找 "actor:role:100"
    → 找到 → 返回 PID(node1)
    → 未找到 → Service Registry 一致性哈希选节点
    → 向选中节点的 activatorRouter 发送 ActorActive
    → Router 包装为 hashableActorActive → Pool 路由
    → actorActivator spawn Actor, SETNX 注册到 Locator
    → 返回 PID

路由架构:
  Remote Node
    → activatorRouter (外部入口)
      → hashableActorActive (包装, 提供 Hash() 方法)
        → ConsistentHashPool (5 实例)
          → actorActivator (按 ID 哈希选中)
            → SpawnNamed(actorID)

续约流程:
  actorActivator (30s tick)
    → Lua 批量 SETEX 续约所有 child actors

注销流程:
  actor.Terminated → Lua 条件删除 (校验值匹配)
```

## 数据持久化模型

```
   ┌─────────────────────────────────────────────────┐
   │              RoleMain Actor                     │
   │                                                 │
   │  ┌─────────────────────────────────────────┐    │
   │  │       600s PersistTick                  │    │
   │  │                                         │    │
   │  │  for each module:                       │    │
   │  │    1. modState.IsDirty()?               │    │
   │  │       YES → db.Save(modState)           │    │
   │  │              GORM 自动 INSERT/UPDATE     │    │
   │  │              modState.ClearDirty()      │    │
   │  │       NO  → skip (无变更)               │    │
   │  └────────────────┬────────────────────────┘    │
   └────────────────────┼────────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────────┐
   │                PostgreSQL                       │
   │                                                 │
   │  ┌──────────────────┐  role_id (primaryKey)     │
   │  │ role_basic        │  role_name, head          │
   │  │                   │  login_tm, logout_tm      │
   │  │                   │  create_tm, vip_lv        │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────┐  role_id (primaryKey)     │
   │  │ role_bag          │  goods (JSONB)            │
   │  │                   │  (GoodsMap: map[int]*Good) │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────┐  role_id (primaryKey)     │
   │  │ role_public       │  name, head, create_time  │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────┐  role_id (primaryKey)     │
   │  │ role_extra        │  cron_tm                  │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────┐  account (primaryKey)     │
   │  │ role_account      │  role_id (uniqueIndex)    │
   │  └──────────────────┘                          │
   └─────────────────────────────────────────────────┘
```

## 消息类型层次

```
  proto.Message (protobuf)
       │
       ├── pb.ReqHandShake        ← 客户端首次连接
       ├── pb.RspHandShake
       ├── pb.ReqAccountLogin     ← 登录
       ├── pb.RspAccountLogin
       ├── pb.ReqAccountLogout    ← 登出
       ├── pb.ReqBagInfo          ← 背包查询
       ├── pb.RspBagInfo
       ├── pb.ReqBasicInfo        ← 基础信息查询
       ├── pb.RspBasicInfo
       ├── pb.ReqBasicSetName     ← 修改名称
       ├── pb.RspBasicSetName
       ├── pb.ReqBasicSetHead     ← 修改头像
       ├── pb.RspBasicSetHead
       ├── pb.NotifyBagUpdate     ← 背包变更推送
       ├── ... (各业务协议)
       │
       ├── pb.ClientMsg           ← Session → Role 封装
       │     id: string
       │     msg:  anypb.Any
       │
       ├── pb.ServerMsg           ← Role → Session 封装
       │     msg:  anypb.Any
       │
       ├── pb.ActorActive         ← 激活虚拟 Actor
       │     kind: string
       │     id:   string
       │
       ├── pb.ActorStop           ← 停止 Actor
       │     reason: string
       │
       └── pb.ActorError          ← Actor 错误响应
             reason: string
```

---
*Last updated: 2026-04-29*
