# 聊天系统设计

> 全 Redis 驱动的实时聊天系统，基于分线大厅世界频道、好友私聊、系统广播三个频道。

## 1. 概述

聊天系统为游戏提供基础社交沟通能力，首版支持三个频道：

- **世界频道**：分线大厅制公开聊天，非全服广播
- **私聊频道**：好友之间一对一聊天
- **系统频道**：系统公告和事件广播，玩家不可发言

首版不做：敏感词过滤、公会/队伍/附近频道、跨服频道、图片/语音/表情、屏蔽/举报、未读数（客户端自行维护）。

## 2. 架构

### 2.1 整体结构

```
┌─ Chat App (独立 App, 可多实例) ──────────────────┐
│                                                   │
│  ChatHub (单例 actor)                             │
│  - 处理所有聊天请求 (发言/历史/加入/退出)          │
│  - 订阅 Redis PubSub (chat:pub:*)                │
│  - 收到广播 → 推送给本节点在线 role actor          │
│                                                   │
│  Redis 操作:                                      │
│  - 大厅分配 (SortedSet + Lua 原子脚本)            │
│  - 消息存储 (List + LTRIM)                        │
│  - 广播 (PubSub)                                  │
└───────────────────────────────────────────────────┘
         ↑ actor Send                ↑ actor Send
┌─ Role Node A ──┐      ┌─ Role Node B ──┐
│  Role-1  Role-2 │      │  Role-3  Role-4 │
└─────────────────┘      └─────────────────┘
```

### 2.2 设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| Chat 部署方式 | 独立 App | 职责清晰，可独立扩容 |
| 大厅分配 | Redis SortedSet + Lua 原子脚本 | 无状态，多实例安全 |
| 大厅策略 | 填满优先 | 大厅少但热闹，适合休闲游戏 |
| 消息存储 | Redis List + LTRIM | 不需要持久化，自动保留近期消息 |
| 广播机制 | Redis PubSub | 天然跨节点，频道模型 |
| 冷却控制 | Role 内存临时变量 | 短时效，不需持久化 |
| 未读数 | 客户端自行维护 | 减轻服务端负担 |
| 敏感词 | 首版不实现，留接口 | 加快交付 |
| 私聊权限 | 好友关系（查 friend_relation 表） | 复用现有基础设施 |

### 2.3 Role 侧职责

Role 模块需要维护的内存数据：

- `lastLobbyID int64` — 当前所在大厅 ID（加入时返回，退出时传递）
- `lastWorldChatTime time.Time` — 世界频道发言冷却（纯内存，不存 DB）

## 3. Redis 数据结构

| Key | 类型 | 用途 | 过期策略 |
|-----|------|------|---------|
| `chat:lobby:{id}` | Set\<roleID\> | 大厅玩家集合 | 空后 3 天自动删除 |
| `chat:lobby:sizes` | SortedSet (score=人数) | 大厅分配 | lobby 空后由 Lua 管理 |
| `chat:msg:lobby:{id}` | List\<JSON\> | 世界消息，LTRIM 100 | 随 lobby 生命周期 |
| `chat:msg:private:{min}:{max}` | List\<JSON\> | 私聊消息，LTRIM 50 | 7 天 TTL |
| `chat:msg:system` | List\<JSON\> | 系统消息，LTRIM 50 | 不过期 |
| `chat:lobby:counter` | String (int) | 自增，生成新 lobby ID | 不过期 |

PubSub channels：

| Channel | 用途 |
|---------|------|
| `chat:pub:lobby:{id}` | 世界频道实时广播 |
| `chat:pub:system` | 系统消息实时广播 |

私聊 key 中的 `{min}:{max}` 取两个 roleID 中较小和较大的值，确保两个玩家的私聊只对应一个 key。

## 4. 大厅管理

### 4.1 加入大厅（Lua 原子脚本）

```lua
-- chat_join_lobby.lua
-- KEYS[1] = chat:lobby:sizes
-- ARGV[1] = maxSize (单个大厅最大人数)
-- ARGV[2] = roleID
local maxSize = tonumber(ARGV[1])
local roleID = ARGV[2]

-- 填满优先：从最满的未满 lobby 开始找
local available = redis.call('ZREVRANGEBYSCORE', KEYS[1], maxSize - 1, 0, 'LIMIT', 0, 1)
local lobbyID
if #available == 0 then
    lobbyID = tostring(redis.call('INCR', 'chat:lobby:counter'))
else
    lobbyID = available[1]
end

-- 加入大厅
redis.call('SADD', 'chat:lobby:' .. lobbyID, roleID)
redis.call('ZINCRBY', KEYS[1], 1, lobbyID)
-- 如果 key 有 TTL（之前空了 3 天没被清理），移除 TTL
redis.call('PERSIST', 'chat:lobby:' .. lobbyID)
redis.call('PERSIST', 'chat:msg:lobby:' .. lobbyID)

return lobbyID
```

### 4.2 退出大厅（Lua 原子脚本）

```lua
-- chat_leave_lobby.lua
-- ARGV[1] = roleID
-- ARGV[2] = lobbyID
local roleID = ARGV[1]
local lobbyID = ARGV[2]

redis.call('SREM', 'chat:lobby:' .. lobbyID, roleID)
redis.call('ZINCRBY', 'chat:lobby:sizes', -1, lobbyID)

-- 大厅空了，设 3 天 TTL 延迟清理
local size = redis.call('SCARD', 'chat:lobby:' .. lobbyID)
if size == 0 then
    redis.call('EXPIRE', 'chat:lobby:' .. lobbyID, 259200)
    redis.call('EXPIRE', 'chat:msg:lobby:' .. lobbyID, 259200)
end
return 1
```

### 4.3 分配策略

填满优先：`ZREVRANGEBYSCORE` 取人数最多的未满 lobby。新玩家被分配到最热闹的大厅，lobby-1 满了再开 lobby-2，大部分时候只有 1-2 个活跃大厅。

## 5. 频道流程

### 5.1 玩家登录

```
RoleActor.OnLogin → Send(ChatHub, ChatJoin{roleID})
ChatHub:
  1. EVAL chat_join_lobby.lua → lobbyID
  2. LRANGE chat:msg:lobby:{lobbyID} 0 -1 → 世界历史消息
  3. LRANGE chat:msg:system 0 -1 → 系统消息
  4. 返回 RspChatInit{lobbyID, worldMessages, systemMessages}
RoleActor:
  - 存 lastLobbyID = lobbyID
  - SendClient 下发给客户端
```

### 5.2 玩家下线

```
RoleActor.OnLogout → Send(ChatHub, ChatLeave{roleID, lobbyID})
ChatHub:
  1. EVAL chat_leave_lobby.lua
```

### 5.3 世界频道发言

```
RoleActor:
  1. 检查冷却: time.Since(lastWorldChatTime) < cooldown → 拒绝
  2. 检查字数 + 非空
  3. 更新 lastWorldChatTime = now
  4. Send(ChatHub, WorldChat{roleID, name, content, lobbyID})

ChatHub:
  1. LPUSH chat:msg:lobby:{lobbyID} {JSON}
  2. LTRIM chat:msg:lobby:{lobbyID} 0 99
  3. PUBLISH chat:pub:lobby:{lobbyID} {JSON}

每个节点的 ChatHub PubSub 订阅回调:
  1. SMEMBERS chat:lobby:{lobbyID}
  2. 遍历: GetRoleActor(roleID, false) → Send(NotifyWorldChat{...})
```

### 5.4 私聊发言

```
RoleActor:
  1. 查 friend_relation 确认好友关系
  2. 检查字数 + 非空
  3. Send(ChatHub, PrivateChat{senderID, targetID, content})

ChatHub:
  1. minID, maxID :=排序 senderID, targetID
  2. LPUSH chat:msg:private:{minID}:{maxID} {JSON}
  3. LTRIM ... 0 49
  4. EXPIRE ... 604800 (7 天)
  5. 目标在线? GetRoleActor(targetID, false) → Send(NotifyPrivateChat{...})
```

### 5.5 拉取历史消息

```
世界历史: LRANGE chat:msg:lobby:{lobbyID} 0 {count-1}
私聊历史: LRANGE chat:msg:private:{min}:{max} 0 {count-1}
系统历史: LRANGE chat:msg:system 0 {count-1}
```

### 5.6 系统消息

```
运营/GM 调用:
  ChatHub.SendSystemMsg{type, content}
ChatHub:
  1. LPUSH chat:msg:system {JSON}
  2. LTRIM chat:msg:system 0 49
  3. PUBLISH chat:pub:system {JSON}

每个节点的 ChatHub 收到:
  遍历本节点所有在线 role → GetRoleActor → Send(NotifySystemChat{...})
```

## 6. 发言限制

### 6.1 世界频道冷却

Role 内存维护 `lastWorldChatTime`，发言前检查 `time.Since(lastWorldChatTime) < cooldown`。冷却时间从配置读取（如 5 秒）。

### 6.2 字数限制

所有消息统一字数上限，从配置读取（如 200 字）。空消息和纯空格消息拒绝发送。

### 6.3 敏感词

首版不实现。预留 `filterContent(content string) string` 接口，后续接入。

## 7. Proto 设计 (28001-28099)

```proto
// === 通用 ===

message PChatMsg {
    int64 sender_id = 1;
    string sender_name = 2;
    string content = 3;
    int64 timestamp = 4;
}

// === 登录初始化 ===

// 聊天初始化 (28001) - 登录时自动调用
message ReqChatInit {
    option (msg_id) = 28001;
}
message RspChatInit {
    option (msg_id) = 28002;
    int32 lobby_id = 1;
    repeated PChatMsg world_messages = 2;
    repeated PChatMsg system_messages = 3;
}

// === 世界频道 ===

// 发送世界消息 (28003)
message ReqSendWorldChat {
    option (msg_id) = 28003;
    string content = 1;
}
message RspSendWorldChat {
    option (msg_id) = 28004;
}

// 拉取世界频道历史 (28005)
message ReqWorldChatHistory {
    option (msg_id) = 28005;
    int32 count = 1;
}
message RspWorldChatHistory {
    option (msg_id) = 28006;
    repeated PChatMsg messages = 1;
}

// 世界频道消息通知 (28007) - 服务端推送
message NotifyWorldChat {
    option (msg_id) = 28007;
    PChatMsg message = 1;
}

// === 私聊 ===

// 发送私聊 (28008)
message ReqSendPrivateChat {
    option (msg_id) = 28008;
    int64 target_id = 1;
    string content = 2;
}
message RspSendPrivateChat {
    option (msg_id) = 28009;
}

// 拉取私聊历史 (28010)
message ReqPrivateChatHistory {
    option (msg_id) = 28010;
    int64 friend_id = 1;
    int32 count = 2;
}
message RspPrivateChatHistory {
    option (msg_id) = 28011;
    repeated PChatMsg messages = 1;
}

// 私聊消息通知 (28012) - 服务端推送
message NotifyPrivateChat {
    option (msg_id) = 28012;
    int64 sender_id = 1;
    string sender_name = 2;
    string content = 3;
    int64 timestamp = 4;
}

// === 系统频道 ===

// 拉取系统消息 (28013)
message ReqSystemChatHistory {
    option (msg_id) = 28013;
    int32 count = 1;
}
message RspSystemChatHistory {
    option (msg_id) = 28014;
    repeated PChatMsg messages = 1;
}

// 系统消息通知 (28015) - 服务端推送
message NotifySystemChat {
    option (msg_id) = 28015;
    PChatMsg message = 1;
}
```

## 8. 配置

在 `TbFriendConfig` 或新建 `TbChatConfig` 中添加：

| 字段 | 类型 | 建议值 | 说明 |
|------|------|--------|------|
| chat_lobby_max_capacity | int | 100 | 单个大厅最大人数 |
| chat_world_cooldown | int | 5 | 世界频道发言冷却（秒） |
| chat_msg_max_length | int | 200 | 单条消息最大字数 |
| chat_world_msg_keep | int | 100 | 世界频道保留消息条数 |
| chat_private_msg_keep | int | 50 | 私聊保留消息条数 |
| chat_system_msg_keep | int | 50 | 系统频道保留消息条数 |

## 9. 涉及文件

| 文件 | 改动 |
|------|------|
| protocol/client/chat.proto (新) | 聊天相关 proto 定义 |
| src/apps/chat/ (新) | Chat App：ChatHub actor、Lua 脚本、Redis 操作 |
| src/apps/role/internal/logic/role_chat.go (新) | Role 侧聊天模块：冷却、proto handler、登录/登出联动 |
| src/apps/role/internal/logic/role_main.go | roleModules 新增 Chat 字段 |
| gameconfig/ | 聊天配置表 |

## 10. 后续扩展

| 扩展 | 实现方式 |
|------|---------|
| 公会频道 | 新 PubSub channel `chat:pub:guild:{guildID}`，新建 GuildActor 管成员 |
| 敏感词 | 实现 `filterContent` 接口，接入 DFA 或第三方服务 |
| 屏蔽/举报 | 新增屏蔽列表（Redis Set 或 DB），消息推送前检查 |
| 花贸市场喊话 | 复用世界频道，消息加 type 字段区分 |
| 物品/花朵链接 | 扩展 PChatMsg 增加附件字段 |
