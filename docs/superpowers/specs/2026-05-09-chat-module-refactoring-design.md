# Chat Module Refactoring Design

> 将聊天模块从 Redis 驱动 + sidecar 推送的架构，重构为基于 ChannelActor 的统一频道模型。

## 1. 动机

当前聊天系统存在以下问题：

1. **频道类型割裂**：世界频道（lobby）和公会频道各自一套流程（不同的 Redis key、不同的 sidecar 处理分支、不同的 proto 消息），扩展小队频道需要重复类似代码
2. **sidecar 模式维护成本高**：Redis pub/sub + sidecar 转发的推送链路，需要维护 `sync.Map` 注册表，且跨节点推送依赖 pub/sub，不如 protoactor 内置的路由
3. **不必要的 Redis 中间层**：消息先写 Redis 再 pub/sub，对于世界频道（无需持久化）和公会频道（可 actor 内存 + 定时刷 DB），Redis 层可以省略

## 2. 架构

### 2.1 整体结构

```
Client → Proto → RoleActor → Call → ChannelActor(consistent hash)
                                        │
                                    ┌───┴───┐
                                    │ memory │ ring buffer (消息留存)
                                    │  map   │ roleID → PID (频道成员)
                                    └───┬───┘
                                        │ Send(pid, NotifyChannelChat)
                                        ▼
                                  各 RoleActor → SendClient
```

### 2.2 核心组件

**ChannelActor** — 每个频道一个 actor，consistent hash 按 `channel_type:channel_id` 路由

```
channel:world:{lobbyID}    — 世界频道
channel:guild:{guildID}    — 公会频道
channel:team:{teamID}      — 小队频道（未来扩展）
```

职责：
1. 维护频道成员列表 `map[roleID]PID`（通过外部 Register/Unregister 消息管理）
2. 接收聊天消息 → IChannel 校验 → 写内存环形缓冲区 → Send 给所有成员
3. 定时 TickSave（按 IChannel 配置）持久化消息到 DB

**IChannel 接口** — 定义频道的差异化行为

```go
type IChannel interface {
    ChannelType() string           // "world", "guild"
    RingBufferSize() int           // 消息保留上限
    SaveInterval() time.Duration   // >0 启用定时存盘，0 不存盘
    TableName() string             // 存盘表名（SaveInterval>0时需要）
    CanWrite(roleID int64, content string) error  // 频道级校验+限流
    CanJoin(roleID int64) bool     // 是否有权限加入频道
}
```

**消息通知统一化**

原来的 `NotifyWorldChat`、`NotifyGuildChat`、`NotifySystemChat` 用同一个通知消息替代：

```proto
message NotifyChannelChat {
    option (msg_id) = 28020;
    int32 channel_type = 1;    // 1=世界 2=公会 3=小队
    int64 channel_id   = 2;    // lobbyID / guildID / teamID
    int64 sender_id    = 3;
    string content     = 4;
    int64 timestamp    = 5;
}
```

历史消息请求也统一：

```proto
message ReqChannelHistory {
    option (msg_id) = 28021;
    int32 channel_type = 1;
    int64 channel_id   = 2;
    int32 count        = 3;
}
message RspChannelHistory {
    option (msg_id) = 28022;
    repeated PChatMsg messages = 1;
}
```

### 2.3 保留独立的模块

- **私聊**：保持现有 Redis + PostgreSQL 模式不变（一对一，不走 channel 模型）
- **系统消息**：服务端生成，不走 channel 模型

## 3. 频道实现

### 世界频道（WorldChannel）

| 属性 | 值 |
|------|-----|
| ChannelType | `"world"` |
| RingBufferSize | 200 |
| SaveInterval | 0（不持久化，重启清空） |
| TableName | 不需要 |
| CanWrite | 检查世界发言冷却 |
| CanJoin | 登录后自动加入 |

### 公会频道（GuildChannel）

| 属性 | 值 |
|------|-----|
| ChannelType | `"guild"` |
| RingBufferSize | 500 |
| SaveInterval | 600s（定时刷 DB） |
| TableName | `guild_chat_log` 或复用公会 JSONB |
| CanWrite | 仅公会成员 |
| CanJoin | 加入公会时自动注册 |

## 4. 流程

### 4.1 玩家登录→加入世界频道

```
RoleActor.ReqChatInit
  → lobby分配（复用现有 Redis 大厅分配 Lua 脚本）
  → Call(ChannelActor, ChannelRegister{roleID, pid, "world", lobbyID})
  → 返回世界频道历史消息（内存 RingBuffer）
  → 返回系统消息
```

### 4.2 玩家加入公会→注册公会频道

```
GuildActor.addMember
  → Call(ChannelActor, ChannelRegister{roleID, pid, "guild", guildID})
```

### 4.3 发送聊天消息

客户端发送 `ReqSendChannelChat{channel_type, content}`，服务端推断 channel_id：

```
RoleActor:
  → 根据 channel_type 推断 channel_id（世界→lastLobbyID，公会→RoleGuild.GuildID）
  → 校验长度、基础内容
  → Call(ChannelActor, SendChannelChat{channelType, channelID, roleID, content})

ChannelActor:
  → IChannel.CanWrite(roleID, content)  ← 频道级限流+校验
  → 写入内存 RingBuffer
  → 遍历成员 map[roleID]PID → Send(pid, NotifyChannelChat{...})
  → 本节点角色直接 LocalSend，跨节点由 protoactor 自动路由
```

### 4.4 定时存盘（仅需持久化的频道）

```
ChannelActor.TickSave
  → 如果 currentSeq - lastSavedSeq > 0
  → INSERT/UPDATE 到 IChannel.TableName()
```

### 4.5 玩家下线/退出公会

```
RoleActor.Terminate/LeaveGuild:
  → Call(ChannelActor, ChannelUnregister{roleID, channelType, channelID})
```

## 5. 删除/停用的模块

| 模块 | 处理 |
|------|------|
| `chat/redis.go` 世界/公会的存储和 pub/sub | 删除（私聊保留） |
| `chat/sidecar.go` | 删除 |
| `chat/handler.go` 世界/公会的 HTTP handler | 删除 |
| `role_chat.go` 中调用 chat HTTP 的逻辑 | 改为 Call ChannelActor |
| `role_main.go` 中 `NotifyWorldChat`/`NotifyGuildChat` 的直接 `SendClient` | 改为统一的 `NotifyChannelChat` 处理 |

私聊的 Redis 操作（`StorePrivateMsg`、`PublishPrivateChat`、`GetPrivateHistory`）和 HTTP handler 保留。

## 6. Proto 变更

### 新增

| Message | ID | 说明 |
|---------|----|------|
| `ReqSendChannelChat` | 28020 | 统一频道消息发送（channel_type + content） |
| `RspSendChannelChat` | 28021 | 统一频道消息发送响应 |
| `NotifyChannelChat` | 28022 | 统一频道消息通知 |
| `ReqChannelHistory` | 28023 | 统一频道历史请求 |
| `RspChannelHistory` | 28024 | 统一频道历史响应

`ReqSendChannelChat` 由客户端发送，`channel_type` 指定目标频道类型（1=世界 2=公会），`channel_id` **由服务端根据玩家状态推断**（世界→lastLobbyID，公会→RoleGuild.GuildID），客户端不需要管理 channel ID。`

### 替换

旧的世界/公会专属消息被统一的消息替代。兼容期内可保留旧消息定义，客户端逐步迁移：

| 旧消息 | 替换为 | 说明 |
|--------|--------|------|
| `ReqSendWorldChat` / `RspSendWorldChat` | `ReqSendChannelChat` / `RspSendChannelChat` | 统一发送入口 |
| `ReqWorldChatHistory` / `RspWorldChatHistory` | `ReqChannelHistory` / `RspChannelHistory` | 统一历史入口 |
| `NotifyWorldChat` | `NotifyChannelChat` | 统一通知 |
| `ReqSendGuildChat` / `RspSendGuildChat` | 同上 | 同上 |
| `ReqGuildChatHistory` / `RspGuildChatHistory` | 同上 | 同上 |
| `NotifyGuildChat` | 同上 | 同上 |

### 保留

| Message | 说明 |
|---------|------|
| `ReqSendPrivateChat` / `RspSendPrivateChat` | 私聊保持独立 |
| `ReqPrivateChatHistory` / `RspPrivateChatHistory` | 私聊历史保持独立 |
| `ReqSystemChatHistory` / `RspSystemChatHistory` | 系统消息保持独立 |

## 7. 涉及文件

| 文件 | 改动 |
|------|------|
| `protocol/client/chat.proto` | 新增 NotifyChannelChat/ReqChannelHistory/RspChannelHistory，标记待删除消息 |
| `protocol/client/guild.proto` | 删除 NotifyGuildChat |
| `src/apps/chat/channel.go` (新) | IChannel 接口 + WorldChannel/GuildChannel 实现 |
| `src/apps/chat/channel_actor.go` (新) | ChannelActor 实现 |
| `src/apps/chat/redis.go` | 删除世界/公会 Redis 操作，保留私聊 |
| `src/apps/chat/sidecar.go` | 删除 |
| `src/apps/chat/handler.go` | 删除世界/公会 HTTP handler，保留私聊 |
| `src/apps/role/internal/logic/role_chat.go` | 世界/公会改为 Call ChannelActor |
| `src/apps/role/internal/logic/role_main.go` | NotifyChannelChat 处理 |
| `src/lib/actor.go` | 可能需新增 GetChannelActor 辅助方法 |

## 8. 数据流对比

### 重构前（单条世界消息）

```
Client → Proto → RoleActor → HTTP Post → ChatHandler → Redis LPUSH + PUBLISH
                                                          ↓
                                                      Sidecar(Redis SUB)
                                                          ↓
                                                      LocalSend → RoleActor → SendClient
```

### 重构后

```
Client → Proto → RoleActor → Call → ChannelActor(限流+写内存+成员遍历)
                                          ↓
                                      Send → RoleActor → SendClient
```
