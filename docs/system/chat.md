# 聊天系统

## 概述

聊天系统采用三层架构：Chat HTTP 服务处理业务逻辑，Redis PubSub 跨节点广播，每个角色节点运行 Sidecar 订阅 PubSub 并投递给本地角色。

## 架构

```
┌─────────────────────────────────────────────────────┐
│  Chat App (独立 HTTP 服务)                            │
│  - lobby join/leave (Lua 原子操作)                    │
│  - 消息存储 (Redis List / PostgreSQL)                 │
│  - 发布到 PubSub                                      │
│  - 历史查询                                          │
└─────────────────────────────────────────────────────┘
          ▲ HTTP                   ▲ PubSub
          │                        │
┌─────────┴─────────┐   ┌─────────┴──────────┐
│  Role Node A       │   │  Role Node B        │
│  ┌──────────────┐  │   │  ┌──────────────┐   │
│  │ Chat Sidecar │◄─┼───┼─►│ Chat Sidecar │   │
│  │ (PubSub 订阅) │  │   │  │ (PubSub 订阅) │   │
│  │ 本地角色注册表 │  │   │  │ 本地角色注册表 │   │
│  └──────┬───────┘  │   │  └──────┬───────┘   │
│         │LocalSend  │   │         │LocalSend   │
│  ┌──────┴───────┐  │   │  ┌──────┴───────┐   │
│  │ Role Actors  │  │   │  │ Role Actors  │   │
│  └──────────────┘  │   │  └──────────────┘   │
└────────────────────┘   └─────────────────────┘
```

- **Chat App**（`src/apps/chat/`）：独立 HTTP 服务，处理大厅分配、消息存储、PubSub 发布
- **Chat Sidecar**（`sidecar.go`）：每个角色节点一个，订阅 `chat:pub:*`，维护 `sync.Map` 本地注册表，`LocalSend` 投递
- **Role Chat 模块**（`role_chat.go`）：通过 HTTP 调 Chat 服务，注册/注销本地 Sidecar

## 数据结构

### Redis

| Key | 类型 | 说明 |
|-----|------|------|
| chat:lobby:sizes | SortedSet | 大厅人数 (score=人数, member=lobbyID) |
| chat:lobby:{id} | Set | 大厅成员 roleID 集合 |
| chat:lobby:counter | String (int) | 大厅ID自增计数器 |
| chat:msg:lobby:{id} | List | 世界频道消息 (LPUSH + LTRIM) |
| chat:msg:system | List | 系统频道消息 |
| chat:pub:lobby:{id} | PubSub | 世界频道广播 |
| chat:pub:system | PubSub | 系统频道广播 |
| chat:pub:private:{targetID} | PubSub | 私聊通知 |

### PostgreSQL

**chat_private_message**：私聊持久化

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint PK | 自增主键 |
| min_role_id | int64 | 较小角色ID |
| max_role_id | int64 | 较大角色ID |
| sender_id | int64 | 发送者ID |
| content | text | 消息内容 |
| created_at | timestamp | 创建时间 |

复合索引：`(min_role_id, max_role_id, created_at)`

## 大厅分配（Lua 原子操作）

- **填满优先**：`ZREVRANGEBYSCORE` 找未满大厅，找不到则新建
- **空大厅**：最后一人离开后设置 3 天 TTL，不立即删除
- Lua 脚本保证 join/leave 原子性

## 消息流

### 世界/系统消息

1. Role → HTTP 调 Chat 服务 `send_world` / `send_system`
2. Chat 服务 LPUSH + LTRIM 存储，PUBLISH 广播
3. 各节点 Sidecar 收到 PubSub → 查本地注册表 → LocalSend 投递

### 私聊消息

1. Role → HTTP 调 Chat 服务 `store_private`
2. Chat 服务 INSERT 到 PostgreSQL，PUBLISH 到 `chat:pub:private:{targetID}`
3. 目标节点 Sidecar 收到 → 查本地注册表 → LocalSend 投递

## HTTP 接口

基础路径 `/chat`，所有 POST。

| 路径 | 参数 | 说明 |
|------|------|------|
| /join_lobby | role_id | 加入大厅，返回 lobby_id |
| /leave_lobby | role_id, lobby_id | 离开大厅 |
| /send_world | sender_id, sender_name, content, lobby_id | 发送世界消息 |
| /send_system | sender_id, sender_name, content | 发送系统消息 |
| /store_private | sender_id, target_id, sender_name, content | 存储私聊 |
| /world_history | lobby_id, count | 世界频道历史 |
| /private_history | role_id, friend_id, count | 私聊历史 |
| /system_history | count | 系统频道历史 |

## Proto 接口

ID 段 `28001~28099`，文件 `protocol/client/chat.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqChatInit / RspChatInit | 28001-28002 | C→S / S→C | 登录初始化（加入大厅+拉历史） |
| ReqSendWorldChat / RspSendWorldChat | 28003-28004 | C→S / S→C | 发送世界消息 |
| ReqWorldChatHistory / RspWorldChatHistory | 28005-28006 | C→S / S→C | 拉取世界历史 |
| NotifyWorldChat | 28007 | S→C | 世界消息推送 |
| ReqSendPrivateChat / RspSendPrivateChat | 28008-28009 | C→S / S→C | 发送私聊 |
| ReqPrivateChatHistory / RspPrivateChatHistory | 28010-28011 | C→S / S→C | 拉取私聊历史 |
| NotifyPrivateChat | 28012 | S→C | 私聊推送 |
| ReqSystemChatHistory / RspSystemChatHistory | 28013-28014 | C→S / S→C | 拉取系统历史 |
| NotifySystemChat | 28015 | S→C | 系统消息推送 |

## 配置

| 参数 | 默认值 | 说明 |
|------|--------|------|
| LobbyMaxCapacity | 100 | 每个大厅最大人数 |
| WorldCooldown | 5s | 世界频道发言冷却 |
| MsgMaxLength | 200 | 消息最大字数 |
| WorldMsgKeep | 100 | 世界频道保留条数 |
| SystemMsgKeep | 50 | 系统频道保留条数 |
| PrivateKeepDays | 30 | 私聊保留天数 |

## 核心文件

| 文件 | 说明 |
|------|------|
| src/apps/chat/chat_app.go | App 注册 + schema 初始化 |
| src/apps/chat/chat_service.go | HTTP 服务生命周期 |
| src/apps/chat/handler.go | HTTP 路由处理 |
| src/apps/chat/redis.go | Lua 脚本 + Redis 操作 + PG 私聊 |
| src/apps/chat/sidecar.go | Sidecar（PubSub 订阅 + 本地投递） |
| src/apps/chat/model.go | ChatPrivateMessage 模型 |
| src/apps/chat/config.go | 配置 |
| src/apps/role/internal/logic/role_chat.go | Role Chat 模块 |
