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
                              │  │  │       RoleMain Grain           │    │   │
                              │  │  │  ┌────────┐ ┌────────┐        │    │   │
                              │  │  │  │ Basic  │ │  Sign  │        │    │   │
                              │  │  │  └────────┘ └────────┘        │    │   │
                              │  │  │  ┌────────┐ ┌────────┐        │    │   │
                              │  │  │  │  Bag   │ │Public  │        │    │   │
                              │  │  │  └────────┘ └────────┘        │    │   │
                              │  │  │  ┌────────┐ ┌────────┐        │    │   │
                              │  │  │  │ Extra  │ │Activity│        │    │   │
                              │  │  │  └────────┘ └────────┘        │    │   │
                              │  │  └───────────────────────────────┘    │   │
                              │  └─────────────────────────────────────────┘   │
                              │                        │ remote               │
                              │                        │                      │
                              │  ┌─────────────────────────────────────────┐   │
                              │  │           Node 2 (role)                │   │
                              │  │                                         │   │
                              │  │  ┌──────────────────────────────────┐  │   │
                              │  │  │       RoleMain Grain (远程)       │  │   │
                              │  │  │  Basic │ Sign │ Bag │ Extra ...  │  │   │
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
                              │  Node   │  node/main.go
                              │ (gxynode)│  读取配置, 组装模块
                              └────┬────┘
                                   │ AddModule
               ┌───────────────────┼───────────────────┐
               │                   │                   │
        ┌──────▼──────┐    ┌───────▼───────┐   ┌──────▼──────┐
        │  ActorApp   │    │  ServiceApp   │   │  GateApp    │
        │  (gxyactor) │    │  (gxyservice) │   │  (gateway)  │
        │             │    │               │   │             │
        │ ActorSystem │    │ Service       │   │ Network     │
        │ Remote      │    │ Registry      │   │ SessionMgr  │
        │ grainMgr    │    │               │   │             │
        └──────┬──────┘    └───────┬───────┘   └─────────────┘
               │                   │
        ┌──────▼──────┐    ┌───────▼───────┐
        │grainManager │    │ RoleService   │
        │             │    │ (role Grain)  │
        │ Activator   │    │               │
        │ Pool (×5)   │    │ RegisterGrain │
        │ Locator     │    │  → RoleMain   │
        └─────────────┘    └───────────────┘

        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ HttpApp  │ │  PGXApp  │ │ RedisApp │
        │ MQApp    │ │ (pgx/v5) │ │          │
        └──────────┘ └──────────┘ └──────────┘
```

## 消息流转详解

```
   Client                 Session Actor              RoleMain Grain
     │                         │                          │
     │  ── TCP Connect ──►     │                          │
     │                         │  ── Spawn ──►            │
     │                         │                          │
     │  ── ReqHandShake ──►    │                          │
     │                         │  GetRoleGrain(roleID)     │
     │                         │─────────────────────────►│
     │                         │                          │
     │                         │  ◄──── PID ─────────────│
     │                         │  Watch(RolePid)          │
     │  ◄── RspHandShake ──    │                          │
     │                         │                          │
     │  ── ReqAccountLogin ──► │                          │
     │                         │  ── CallSync ──────────► │
     │                         │     ClientMsg{path, msg}  │
     │                         │                          │
     │                         │              HandleClientMsg
     │                         │              MsgHandler 路由
     │                         │              → ReqAccountLogin()
     │                         │                          │
     │                         │  ◄── ServerMsg ─────────│
     │  ◄── RspAccountLogin ─  │     {path, rsp}          │
     │                         │                          │
     │  ── ReqSignCheckIn ──►  │                          │
     │                         │  ── CallSync ──────────► │
     │                         │                          │  → RoleSign.Handle
     │                         │  ◄── ServerMsg ─────────│
     │  ◄── RspSignCheckIn ──  │                          │
     │                         │                          │
     │  ── ReqItemUse ──►      │                          │
     │                         │  ── CallSync ──────────► │
     │                         │                          │  → RoleBag.Handle
     │                         │  ◄── ServerMsg ─────────│
     │  ◄── RspItemUse ──      │                          │
```

## Grain 分布式定位

```
   ┌──────────────┐       ┌──────────────┐       ┌──────────────┐
   │   Node 1     │       │   Node 2     │       │   Node 3     │
   │              │       │              │       │              │
   │ RoleMain    │       │ RoleMain    │       │ RoleMain    │
   │  (role:100) │       │  (role:101) │       │  (role:102) │
   │              │       │              │       │              │
   │ Activator   │       │ Activator   │       │ Activator   │
   │  (hash pool)│       │  (hash pool)│       │  (hash pool)│
   └──────┬───────┘       └──────┬───────┘       └──────┬───────┘
          │                      │                      │
          └──────────────────────┼──────────────────────┘
                                 │
                          ┌──────▼──────┐
                          │    Redis     │
                          │              │
                          │  ┌────────┐  │
                          │  │Locator │  │  Grain 位置映射
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
  GetGrain("role", "100")
    → Redis Locator 查找 "actor:role:100"
    → 找到 → 返回 PID(node1)
    → 未找到 → Service Registry 一致性哈希选节点
    → 向选中节点的 Activator 发送 ActorActive
    → Activator spawn Grain, SETNX 注册到 Locator
    → 返回 PID

续约流程:
  grainActivator (30s tick)
    → Lua 批量 SETEX 续约所有 child grains

注销流程:
  actor.Terminated → Lua 条件删除 (校验值匹配)
```

## 数据持久化模型

```
   ┌─────────────────────────────────────────────────┐
   │              RoleMain Grain                     │
   │                                                 │
   │  ┌─────────────────────────────────────────┐    │
   │  │          5s PersistTick                  │    │
   │  │                                         │    │
   │  │  for each module:                       │    │
   │  │    1. 计算 hash(modState)               │    │
   │  │    2. hash != lastHash ?                │    │
   │  │       YES → UpsertOne                   │    │
   │  │              INSERT ON CONFLICT UPDATE   │    │
   │  │              WHERE role_id AND version   │    │
   │  │              version++                   │    │
   │  │       NO  → skip (无变更)                │    │
   │  └────────────────┬────────────────────────┘    │
   └────────────────────┼────────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────────┐
   │                PostgreSQL                       │
   │                                                 │
   │  ┌──────────────────┐  role_id (unique)         │
   │  │ role_persist_state│  version (乐观锁)        │
   │  │ role_name         │  update_at               │
   │  │ head, login_tm... │  (JSONB columns)         │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────┐  role_id (unique)         │
   │  │ role_sign_state   │  sign_day, sign_time     │
   │  │                   │  accum_draw_stage        │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────┐  role_id (unique)         │
   │  │ role_bag_state    │  items (JSONB map)       │
   │  │                   │  currencies (JSONB map)  │
   │  │                   │  grid_use                │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────┐  role_id (unique)         │
   │  │ role_public_state │  name, head, create_time │
   │  └──────────────────┘                          │
   │                                                 │
   │  ┌──────────────────────┐  role_id (unique)     │
   │  │ role_extra_persist    │  cron_tm              │
   │  │ _state                │                       │
   │  └──────────────────────┘                       │
   │                                                 │
   │  ┌──────────────────────┐  role_id (unique)     │
   │  │ role_activity_persist │  activitys (JSONB)    │
   │  │ _state                │                       │
   │  └──────────────────────┘                       │
   │                                                 │
   │  ┌──────────────────┐  account (unique)         │
   │  │ role_account      │  role_id (unique)        │
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
       ├── pb.ReqSignCheckIn      ← 签到
       ├── pb.RspSignCheckIn
       ├── ... (各业务协议)
       │
       ├── pb.ClientMsg           ← Session → Role 封装
       │     path: string
       │     msg:  anypb.Any
       │
       ├── pb.ServerMsg           ← Role → Session 封装
       │     msg:  anypb.Any
       │
       ├── pb.ActorCallMessage    ← 远程 RPC
       │     msg:  anypb.Any
       │
       ├── pb.PushMessage         ← 远程推送
       │     msgName: string
       │     msgData: []byte
       │
       ├── pb.ActorStop           ← 停止 Actor
       │     reason: string
       │
       └── pb.ActorActive         ← 激活 Grain
             kind: string
             id:   string
```

---
*Last updated: 2026-04-22*
