# 好友系统设计

> 对标《我的花园世界》 | MVP 阶段

## 1. 整体架构

好友系统作为一个**无状态 Web 服务**运行，不嵌入游戏 Actor 进程，不依赖游戏业务逻辑。

```
┌─────────────────┐   HTTP/gRPC    ┌──────────────────────┐
│  Game Server      │ ◄──────────► │  Friend Service       │
│  (Actor 世界)     │              │  (无状态, 可水平扩展)  │
│                   │              │                      │
│ 通知对方在线端     │              │  每次操作：             │
│ 搜索玩家 → 查DB   │              │  BEGIN               │
│                   │              │  FOR UPDATE A        │
│                   │              │  FOR UPDATE B        │
│                   │              │  校验 → 改 JSONB     │
│                   │              │  UPDATE A → UPDATE B │
│                   │              │  COMMIT              │
└─────────────────┘              └──────────────────────┘
```

### 职责划分

| 层 | 职责 |
|----|------|
| Friend Service | 好友关系 CRUD、玩家搜索、数据一致性 |
| Game Server（调用方） | 在线通知、搜索对接 `role_public`、业务编排 |

Friend Service 不做：
- 在线状态推送
- 实时通知
- 玩家详情查询（只存 ID，详情由调用方查 `role_public`）

## 2. 数据模型

### friend_data 表（单表全量 JSONB）

```sql
CREATE TABLE friend_data (
    player_id  BIGINT PRIMARY KEY,
    friends    JSONB NOT NULL DEFAULT '[]',   -- [1002, 1003, ...]
    incoming   JSONB NOT NULL DEFAULT '[]',   -- [2001, 2005, ...] 发申请给我的
    outgoing   JSONB NOT NULL DEFAULT '[]',   -- [3002, ...]        我发出申请的
    cooldowns  JSONB NOT NULL DEFAULT '[]',   -- [{"target_id":4001,"until":1777777777}, ...]
    update_at  TIMESTAMP
);
```

**总量估算：** 1 亿玩家 × 1 行 ≈ 1 亿行（非 200 亿行）

**各行用途：**

| 字段 | 类型 | 说明 |
|------|------|------|
| friends | int64[] | 好友 ID 列表 |
| incoming | int64[] | 收到的好友申请（from_id），待处理 |
| outgoing | int64[] | 发出的好友申请（to_id），待处理 |
| cooldowns | {target_id, until}[] | 删除后的冷却列表 |

### Go 结构体

```go
type Int64List []int64          // 支持 JSONB 序列化
type CooldownEntry struct {
    TargetID int64 `json:"target_id"`
    Until    int64 `json:"until"`
}
type CooldownList []CooldownEntry

type FriendData struct {
    PlayerID  int64         `gorm:"column:player_id;primaryKey"`
    Friends   Int64List     `gorm:"column:friends;type:jsonb;default:'[]'"`
    Incoming  Int64List     `gorm:"column:incoming;type:jsonb;default:'[]'"`
    Outgoing  Int64List     `gorm:"column:outgoing;type:jsonb;default:'[]'"`
    Cooldowns CooldownList  `gorm:"column:cooldowns;type:jsonb;default:'[]'"`
    UpdateAt  time.Time     `gorm:"column:update_at;autoUpdateTime"`
}
```

## 3. 并发控制

### FOR UPDATE + 顺序锁

每次写操作（发申请、同意、拒绝、删除）都按以下模式执行：

```go
tx := openTx(ctx)
defer tx.Rollback()

// 按 player_id 从小到大加锁，防止死锁
a, b, _ := lockBoth(tx, playerID, targetID)

// ... 校验规则、修改 JSONB 数据 ...

saveRow(tx, me)
saveRow(tx, other)
tx.Commit()
```

**锁的顺序：**
- 先锁 `min(player_id, target_id)`
- 再锁 `max(player_id, target_id)`
- 所有事务遵守同一规则，消除死锁可能

## 4. 核心流程

### 4.1 发起申请

```
1. FOR UPDATE A, B (sortedIDs)
2. 校验规则：
   - 不是自己
   - 冷却未过期
   - 不是好友
   - 未重复申请
   - 发出未超限
   - 对方收到未超限
3. A.outgoing += B
   B.incoming += A
4. COMMIT
```

### 4.2 同意申请

```
1. FOR UPDATE B, A (sortedIDs)
2. 校验：
   - A 的申请确实在 B.incoming 中
   - B 的好友未满
   - A 的好友未满
3. B.friends += A
   A.friends += B
   B.incoming -= A
   A.outgoing -= B
4. COMMIT
```

### 4.3 拒绝申请

```
1. FOR UPDATE B, A (sortedIDs)
2. B.incoming -= A
   A.outgoing -= B
3. COMMIT
```

拒绝不建立好友关系，只是清理 pending 记录。

### 4.4 删除好友

```
1. FOR UPDATE A, B
2. 移除对方好友列表
3. 双方增加冷却记录
4. COMMIT
```

### 4.5 读取类接口

读操作（好友列表、申请列表）不走 FOR UPDATE，直接 SELECT，不阻塞写操作。

玩家搜索走 `role_public` 表（通过调用方查询，不是 Friend Service 的职责），Friend Service 只维护好友关系和 ID 列表。

## 5. API 定义

协议文件：`protocol/client/friend.proto`，ID 区间 27001~27099。

```protobuf
// 搜索玩家
message ReqSearchPlayer    { int32 id = 1; string name = 2; }
message RspSearchPlayer    { repeated PPlayerInfo players = 1; }
message PPlayerInfo {
    PRolePublic player_info = 1;
    int32       relation    = 2;  // 0=自己 1=可申请 2=已申请 3=已是好友
}

// 申请
message ReqSendRequest     { int64 target_id = 1; }
message RspSendRequest     {}
message ReqAcceptRequest   { int64 from_id = 1; }
message RspAcceptRequest   {}
message ReqRejectRequest   { int64 from_id = 1; }
message RspRejectRequest   {}

// 好友列表
message ReqFriendList      {}
message RspFriendList {
    int32 total = 1;
    int32 limit = 2;
    repeated PFriendInfo friends = 3;
}
message PFriendInfo {
    PRolePublic player_info = 1;
    int64       friend_since = 2;
}

// 申请列表
message ReqApplyList       {}
message RspApplyList {
    repeated PApplyInfo incoming = 1;
    repeated PApplyInfo outgoing = 2;
}
message PApplyInfo {
    PRolePublic player_info = 1;
    int64       apply_at    = 2;
    int32       status      = 3;  // 0=待处理 1=已同意 2=已拒绝
}

// 删好友
message ReqRemoveFriend    { int64 target_id = 1; }
message RspRemoveFriend    {}
```

所有玩家基本信息统一使用 `PRolePublic`（role_id, name, head, level, last_login_at, is_online）。

## 6. 配置

单例表 `TbFriendConfig`：

| 字段 | 类型 | 说明 | 建议值 |
|------|------|------|--------|
| unlock_level | int | 开启等级 | 6 |
| friend_max_count | int | 好友上限 | 50 |
| apply_send_limit | int | 主动申请上限 | 30 |
| apply_receive_limit | int | 收到申请上限 | 50 |
| apply_expire_seconds | int | 申请过期时间(秒) | 604800 |
| delete_reapply_cd_seconds | int | 删除后冷却(秒) | 86400 |
| search_result_limit | int | 搜索结果上限 | 20 |

## 7. RolePublic 扩展

在原 `RolePublicState` 基础上增加：

```go
type RolePublicState struct {
    // ... 原有字段
    Level       int32     `gorm:"column:level"`
    LastLoginAt time.Time `gorm:"column:last_login_at"`
    IsOnline    bool      `gorm:"column:is_online"`
}
```

IsOnline 由登录/登出流程设置：
- 登录 → `IsOnline = true`，等定时 PublicUpdateTick 同步存盘
- 登出 → `IsOnline = false`，`UpdateRolePublic` 同步

## 8. 包结构

```
service/friend/              ← 独立 Web 服务
├── model.go                   FriendData + DB 操作
├── friend.go                  业务逻辑（SendRequest, AcceptRequest...）
└── server.go                  HTTP/gRPC 服务入口

src/apps/role/internal/logic/
├── role_friend.go             胶水层：调用 friend 服务 + 在线通知
├── role_public.go             扩展：level, last_login_at, is_online
└── role_main.go               登录登出设置 IsOnline + 注册 Handler
```
