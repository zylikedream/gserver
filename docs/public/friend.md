# 好友系统

## 概述

好友系统采用微服务架构，独立 HTTP 服务（`friend_app`）管理好友关系数据，角色 Actor 通过 `gxyhttp.PostService` 调用。跨角色通知（好友请求、新增好友）通过 Actor 消息投递。

## 架构

```
┌─────────────┐   HTTP    ┌───────────────┐
│  Role Actor │──────────→│  Friend App   │
│ (role_friend)│←──────────│ (HTTP Service) │
└──────┬──────┘           └───────────────┘
       │ gxyactor.Send
       ↓
┌──────────────┐
│ Target Actor │
└──────────────┘
```

- **Friend App**（`src/apps/friend/`）：独立 HTTP 服务，拥有 `friend_data` + `friend_relation` 两张表
- **Role 桥接层**（`src/apps/role/internal/logic/role_friend.go`）：接收客户端 Proto 请求，调 Friend HTTP，补充 PRolePublic 信息，推送跨角色通知

## 数据结构

### friend_data（单行聚合，PostgreSQL）

| 字段 | 类型 | 说明 |
|------|------|------|
| player_id | int64 PK | 玩家ID |
| friends | jsonb (FriendList) | 好友列表 `[{player_id, added_at}]` |
| incoming | jsonb (ApplyList) | 收到的申请 `[{player_id, apply_at}]` |
| outgoing | jsonb (ApplyList) | 发出的申请 |
| cooldowns | jsonb (CooldownList) | 删除冷却 `[{target_id, until}]` |

### friend_relation（双向关系表，PostgreSQL）

| 字段 | 类型 | 说明 |
|------|------|------|
| player_id | int64 PK | 较小ID |
| friend_id | int64 PK | 较大ID |
| added_at | int64 | 添加时间 |

复合索引用于快速查询 `isFriend`。

## 并发控制

- `SELECT ... FOR UPDATE` 行锁，按 player_id 排序加锁防止死锁
- 所有写操作（申请/接受/拒绝/删除）在同一事务内完成

## HTTP 接口

基础路径 `/friend`，所有 POST。

| 路径 | 参数 | 说明 |
|------|------|------|
| /send_request | a, bs[] | 批量发送好友请求 |
| /accept_request | a, bs[] | 批量接受请求 |
| /reject_request | a, bs[] | 批量拒绝请求 |
| /remove_friend | a, b | 删除好友 |
| /list | player_id | 查询完整 FriendData |

返回格式：`{code, message, data}`，批量操作返回 `FriendBatchItem[]`。

## Proto 接口

ID 段 `27001~27099`，文件 `protocol/client/friend.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqSearchPlayer / RspSearchPlayer | 27001-27002 | C→S / S→C | 按名称搜索玩家 |
| ReqSendRequest / RspSendRequest | 27003-27004 | C→S / S→C | 发送好友请求（支持批量） |
| ReqAcceptRequest / RspAcceptRequest | 27005-27006 | C→S / S→C | 接受请求 |
| ReqRejectRequest / RspRejectRequest | 27007-27008 | C→S / S→C | 拒绝请求 |
| ReqFriendList / RspFriendList | 27009-27010 | C→S / S→C | 好友列表 |
| ReqApplyList / RspApplyList | 27011-27012 | C→S / S→C | 申请列表 |
| ReqRemoveFriend / RspRemoveFriend | 27013-27014 | C→S / S→C | 删除好友 |
| NotifyNewRequest | 27015 | S→C | 收到好友申请推送 |
| NotifyNewFriend | 27016 | S→C | 新好友推送 |

## 配置表

- `TbFriendConfig`：好友上限、申请收发上限、删除重加冷却时间

## 核心文件

| 文件 | 说明 |
|------|------|
| src/apps/friend/friend_app.go | App 注册 + schema 初始化 |
| src/apps/friend/friend_service.go | HTTP 服务生命周期 |
| src/apps/friend/logic/handler.go | HTTP 路由处理 |
| src/apps/friend/logic/friend.go | 业务逻辑（事务+锁） |
| src/apps/friend/logic/model.go | FriendData/FriendRelation 模型 |
| src/apps/role/internal/logic/role_friend.go | Role 桥接层 |
