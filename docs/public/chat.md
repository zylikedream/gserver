# 聊天系统

## 概述

聊天系统采用 **Channel Actor** 架构。每个频道（世界频道、公会频道）对应一个 Actor，按 `channelType_channelID` 命名并通过 consistent-hash 路由到固定节点。Actor 维护成员列表和环形缓冲区消息缓存，定时持久化到 PostgreSQL。

## 架构

```
┌─────────────────────────────────────────────────────────┐
│  Chat HTTP 服务 (chat_http_service.go)                   │
│  - lobby join/leave（大厅分配）                          │
│  - 私聊存储 / 查询                                      │
│  - 系统消息存储 / 查询                                   │
└──────────────────────┬──────────────────────────────────┘
                       │ HTTP (PostService)
         ┌─────────────┴─────────────┐
         │  Role Chat 模块           │
         │  (role_chat.go)          │
         │  - 登录初始化 JoinChannel │
         │  - 发送/查询消息          │
         └─────────────┬─────────────┘
                       │ Actor Call/Send
         ┌─────────────┴──────────────────────┐
         │  ChannelActor (consistent-hash)    │
         │  - 成员管理 (map[int64]*actor.PID) │
         │  - ringBuffer 消息缓存             │
         │  - 定时 save → PostgreSQL          │
         │  - 空闲 30min → Stop               │
         └────────────────────────────────────┘
```

- **Chat HTTP 服务**（`chat_http_service.go`）：独立 HTTP 服务，处理大厅分配、私聊/系统消息存储查询
- **ChannelActor**（`channel_actor.go`）：按 `ChannelType` 区分行为（世界/公会），consistent-hash 路由确保同频道消息落在同一节点
- **Role Chat 模块**（`role_chat.go`）：角色子模块，通过 HTTP 调用 Chat 服务 + Actor Call 给 ChannelActor

## 频道类型

```protobuf
enum ChannelType {
    CHANNEL_TYPE_UNKNOWN = 0;
    CHANNEL_TYPE_WORLD  = 1;  // 世界频道
    CHANNEL_TYPE_GUILD  = 2;  // 公会频道
}
```

定义在 `protocol/client/chat.proto`，Go 代码中使用 `pb.ChannelType_CHANNEL_TYPE_*` 常量。

## 数据结构

### PostgreSQL

**chat_private_message**：私聊持久化

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 自增主键 |
| min_role_id | int64 | 较小角色ID |
| max_role_id | int64 | 较大角色ID |
| sender_id | int64 | 发送者ID |
| content | text | 消息内容 |
| created_at | timestamp | 创建时间 |

复合索引：`(min_role_id, max_role_id, created_at)`

**chat_system_message**：系统消息持久化

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 自增主键 |
| content | text | 消息内容 |
| created_at | timestamp | 创建时间 |

**频道消息表**：每个频道的持久化表，由 `IChannel.TableName()` 指定：

| 字段 | 类型 | 说明 |
|------|------|------|
| channel_type | int32 | 频道类型 |
| channel_id | int64 | 频道ID |
| sender_id | int64 | 发送者ID |
| content | text | 消息内容 |
| timestamp | bigint | 时间戳 |

## 消息流

### 频道消息（世界/公会）

1. Client → `ReqSendChannelChat` (28020) → Role Chat 模块
2. Role 通过 `switch req.ChannelType` 确定 `channelID`（世界=lastLobbyID，公会=Role.GuildID）
3. `lib.GetChannelActor(channelType, channelID)` 获取或创建 ChannelActor PID
4. Role `Call` ChannelActor `ReqChannelSend`
5. ChannelActor 校验 `CanWrite` → push `ringBuffer` → 广播 `NotifyChannelChat` 给所有成员 → `Respond(nil)`

### 私聊消息

1. Client → `ReqSendPrivateChat` (28008) → Role Chat 模块
2. HTTP 调用 Chat 服务 `store_private` → INSERT 到 PostgreSQL
3. 通过 `lib.GetRoleActor(targetID)` 获取目标角色 PID
4. 发送 `NotifyPrivateChat` 给目标角色

### 系统消息

1. 服务端调用 `lib.PublishToAll` 广播到所有在线玩家
2. Role 收到后通过 HTTP 调用 Chat 服务 `store_system` → INSERT 到 PostgreSQL
3. Role 回复 `NotifySystemChat` 给客户端

## 频道 Actor 生命周期

```
                              GetChannelActor
                                    │
                            Activator 创建 Actor
                                    │
                        ┌───────────┴───────────┐
                        │     Init              │
                        │  解析 id → ChannelType│
                        │  ChannelID            │
                        │  创建 ringBuffer      │
                        └───────────┬───────────┘
                                    │
                        ┌───────────┴───────────┐
                        │  DelayInit            │
                        │  注册 TickSave 定时器 │
                        └───────────┬───────────┘
                                    │
                        ┌───────────┴───────────┐
                        │  HandleMessage        │
                        │  RegisterMsg → 加成员  │
                        │  取消 stop 定时器      │
                        │  UnregisterMsg → 删成员│
                        │  空 → save + 30min停止 │
                        │  ReqChannelSend →     │
                        │  写入 buffer + 广播    │
                        │  ReqChannelHistory →  │
                        │  返回 Recent          │
                        └───────────┬───────────┘
                                    │
                     ┌──────────────┴──────────────┐
                     │    空频道 30min 未恢复       │
                     │  → Terminate                │
                     │  → save + StopModule         │
                     │  → 清理 Redis locate         │
                     └─────────────────────────────┘
```

空频道停用：成员数为 0 时触发 30min 定时器，期间有成员加入则取消。定时器到期后再次检查，真空中才 `Stop`。

## Proto 接口

ID 段 `28001~28099`，文件 `protocol/client/chat.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqChatInit / RspChatInit | 28001-28002 | C→S / S→C | 登录初始化（JoinChannel + 拉历史） |
| ReqSendPrivateChat / RspSendPrivateChat | 28008-28009 | C→S / S→C | 发送私聊 |
| ReqPrivateChatHistory / RspPrivateChatHistory | 28010-28011 | C→S / S→C | 拉取私聊历史 |
| NotifyPrivateChat | 28012 | S→C | 私聊推送 |
| ReqSystemChatHistory / RspSystemChatHistory | 28013-28014 | C→S / S→C | 拉取系统历史 |
| NotifySystemChat | 28015 | S→C | 系统消息推送 |
| ReqSendChannelChat / RspSendChannelChat | 28020-28021 | C→S / S→C | 发送频道消息 |
| NotifyChannelChat | 28022 | S→C | 频道消息推送 |
| ReqChannelHistory / RspChannelHistory | 28023-28024 | C→S / S→C | 拉取频道历史 |

### 移除的旧协议

移除的旧协议（已被统一频道消息取代）：

- `ReqSendWorldChat / RspSendWorldChat` (28003-28004)
- `ReqWorldChatHistory / RspWorldChatHistory` (28005-28006)
- `NotifyWorldChat` (28007)
- `ReqSendGuildChat / RspSendGuildChat` (28016-28017)
- `ReqGuildChatHistory / RspGuildChatHistory` (28018-28019)

## HTTP 接口

Chat HTTP 服务（`chat-http`），所有 POST。

| 路径 | 参数 | 说明 |
|------|------|------|
| /join_lobby | role_id | 加入大厅，返回 lobby_id |
| /leave_lobby | role_id, lobby_id | 离开大厅 |
| /store_private | sender_id, target_id, sender_name, content | 存储私聊 |
| /private_history | role_id, friend_id, count | 私聊历史 (POST) |
| /store_system | content | 存储系统消息 |
| /system_history | count | 系统消息历史 |

## 配置

| 参数 | 默认值 | 说明 |
|------|--------|------|
| LobbyMaxCapacity | 100 | 每个大厅最大人数 |
| WorldCooldown | 5s | 世界频道发言冷却 |
| MsgMaxLength | 200 | 消息最大字数 |
| WorldMsgKeep | 100 | 世界频道缓冲区保留条数 |
| SystemMsgKeep | 50 | 系统频道保留条数 |
| PrivateKeepDays | 30 | 私聊保留天数 |

## 核心文件

| 文件 | 说明 |
|------|------|
| src/apps/chat/chat_app.go | App 注册 + schema 初始化 |
| src/apps/chat/chat_service.go | 频道 Actor 注册（`chat_channel` kind）|
| src/apps/chat/chat_http_service.go | HTTP 服务生命周期 |
| src/apps/chat/handler.go | HTTP 路由处理 |
| src/apps/chat/channel.go | IChannel 接口 + WorldChannel/GuildChannel 实现 |
| src/apps/chat/channel_actor.go | ChannelActor（成员管理、ringBuffer、持久化） |
| src/apps/chat/redis.go | 大厅 Lua 操作 + PG 私聊/系统消息 |
| src/apps/chat/model.go | ChatPrivateMessage、ChatSystemMessage 模型 |
| src/apps/chat/config.go | 配置 |
| src/apps/role/internal/logic/role_chat.go | Role Chat 子模块 |

## 服务注册

- `chatService` (`chat_service.go`): `ServiceName()` = `"chat_channel"`，注册 `ChannelActor` kind
- `chatHttpService` (`chat_http_service.go`): `ServiceName()` = `"chat-http"`，启动 HTTP 服务
