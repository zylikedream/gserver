# 偷花系统

## 概述

偷花系统允许好友之间互相偷取已收获的花朵（在收获前）。基于地块系统扩展，好友可查看对方花园并偷取可收获的花朵，每个地块有被偷次数上限，每个玩家对每个好友有每日偷花次数限制。

**流程：** 好友的花处于可收获状态 → 你去偷花（获得奖励）→ 好友收获时产量会因被偷而减产。

## 架构

偷花系统是角色模块的一部分，直接在 Role Actor 内处理，无需独立服务。

主要涉及两个模块：
- `RoleSteal` — 偷花逻辑 + 每日计数
- `RolePlot` — 收获时根据被偷次数扣减产

## 数据结构

### steal_record（PostgreSQL）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 自增主键 |
| owner_id | int64 | 被偷的玩家ID |
| plot_id | int32 | 地块ID |
| stealer_id | int64 | 偷花者ID |
| flower_id | int32 | 花ID |
| steal_time | timestamp | 偷花时间 |

索引：
- `idx_owner_plot(owner_id, plot_id)` — 按地块查被偷次数
- `idx_stealer_owner(stealer_id, owner_id)` — 按偷花者+被偷者查记录

### role_steal（角色持久化模块，PostgreSQL）

| 字段 | 类型 | 说明 |
|------|------|------|
| role_id | int64 PK | 角色ID |
| daily_steals | jsonb | 每日偷花计数 `{friendID: count}` |
| steal_date | string | 当前计数的日期（用于每日重置） |

## 核心逻辑

### ReqPlotFriendInfo(friend_id) — 查看好友花园

1. 校验好友关系
2. 获取好友地块快照
3. 对每个可收获的地块：
   - 检查是否已被偷满、每日是否已达上限、是否已偷过该地块
   - 满足条件则标记 `can_steal = true`
4. 返回地块列表 + `steal_used` / `steal_limit`（整体偷花进度）

### ReqPlotSteal(friend_id, plot_id) — 偷花

1. 校验好友关系
2. 检查每日偷花限制
3. `withPlotLocks`（FOR UPDATE 行锁）锁定目标地块
4. 校验：花存在且可收获、被偷未达上限、未重复偷该地块
5. 插入 `StealRecord`
6. 每日偷花计数 +1
7. 发放偷花奖励（直接从配置生成，不扣减产主）

### 收获减产（在 ReqPlotHarvest 中）

**注意：** 偷花时**不扣减**产主的产量。产量扣减发生在产主收获时：

1. 统计该地块被偷次数
2. 扣除被偷次数对应的产量
3. `OwnerMinKeepNum` 保底（最少保留数量）
4. 清理该地块的偷花记录

```
产量 = max(基础产量 - 被偷次数, OwnerMinKeepNum)
```

## Proto 接口

定义在 `protocol/client/plot.proto`，ID 段 `24013~24016`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqPlotFriendInfo / RspPlotFriendInfo | 24013-24014 | C→S / S→C | 查看好友花园（含偷花信息） |
| ReqPlotSteal / RspPlotSteal | 24015-24016 | C→S / S→C | 偷花（返回成功+奖励） |

`PPlotInfo` 扩展字段：`can_steal bool`（仅该地块是否能偷）。

`RspPlotFriendInfo` 整体字段：`steal_used int32`（今日已偷次数）、`steal_limit int32`（每日上限）。

`RspPlotSteal` 返回：`success bool`、`rewards repeated PGoodInfo`。

## 并发控制

- **Redis 分布式锁**（`gxylock.NewRedisManager` + `withPlotLocks`）
- 锁 key：`plot_lock:{ownerID}:{plotID}`，TTL 3 秒
- `SET NX` 加锁，Lua 脚本释放（仅 token 持有者能解）
- 获取失败重试 3 次，间隔退避
- 不同玩家偷不同地块不冲突；偷同一地块会因锁串行
- 相比 PostgreSQL 行锁：不占用连接池、支持跨节点、内置超时释放

## 配置表

`TbFriendConfig` 相关字段：

| 字段 | 说明 |
|------|------|
| `StealPerFriendDailyLimit` | 每个好友每日偷花上限 |
| `FlowerMaxBeStolenTimes` | 每朵花最多被偷次数 |
| `StealRewardNum` | 每次偷花奖励数量 |
| `OwnerMinKeepNum` | 产主收获时最少保留数量（被偷多次时的保底） |

## 核心文件

| 文件 | 说明 |
|------|------|
| `src/apps/role/internal/logic/role_steal.go` | RoleSteal 模块（偷花逻辑 + 每日计数） |
| `src/apps/role/internal/logic/steal_record.go` | StealRecord 模型 + 查询函数 |
| `src/apps/role/internal/logic/role_plot.go` | 地块系统（收获时扣减产 + 清理偷花记录） |
| `protocol/client/plot.proto` | 协议定义 |
