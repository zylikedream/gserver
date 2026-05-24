# 偷花系统

## 概述

偷花系统允许好友之间互相偷取已收获的花朵。基于地块系统扩展，好友可查看对方花园并偷取可收获的花朵，每个地块有被偷次数上限，每个玩家对每个好友有每日偷花次数限制。

## 架构

偷花系统是角色模块的一部分，直接在 Role Actor 内处理，无需独立服务。并发安全通过 PostgreSQL 行锁保证。

## 数据结构

### steal_record（PostgreSQL）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | uint PK | 自增主键 |
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
| daily_steals | jsonb (DailyStealMap) | 每日偷花计数 `{friendID: count}` |
| steal_date | string | 当前计数的日期（用于每日重置） |

## 并发控制

- `SELECT ... FOR UPDATE`（`clause.Locking{Strength: "UPDATE"}`）锁定目标地块行
- 在事务内完成：检查限制 → 插入偷花记录 → 扣减收获产量 → 发放奖励
- 收获操作（`ReqHarvestFlower`）在地块回归 EMPTY 时清理相关偷花记录

## 核心逻辑

### ReqFriendPlotInfo(friend_id)

1. 查 `friend_relation` 确认好友关系
2. 获取好友地块列表
3. 对每个地块计算：
   - `steal_used` = `countPlotStolen(ownerID, plotID)` — 该地块已被偷次数
   - `can_steal` = 好友关系 && 花朵可收获 && 未达被偷上限 && 今日未达偷花上限 && 该玩家未偷过此地块
4. 返回地块列表 + `steal_used` / `steal_limit`

### ReqStealFlower(friend_id, plot_id)

1. 查 `friend_relation` 确认好友关系
2. 检查每日偷花限制（`DailyStealMap` + `StealDate`）
3. `FOR UPDATE` 锁定目标地块行
4. 校验：花朵存在且可收获、被偷次数未达上限
5. `hasStealRecord` 检查是否重复偷同一地块
6. 插入 `StealRecord`
7. 扣减地块收获产量（`owner_min_keep_num` 保底）
8. 发放奖励物品（`steal_reward_num`）

## Proto 接口

ID 段 `24013~24016`，文件 `protocol/client/flower.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqFriendPlotInfo / RspFriendPlotInfo | 24013-24014 | C→S / S→C | 查看好友花园（含偷花信息） |
| ReqStealFlower / RspStealFlower | 24015-24016 | C→S / S→C | 偷花（返回成功+奖励） |

`PPlotInfo` 扩展字段：`can_steal bool`、`steal_used int32`、`steal_limit int32`。

## 配置表

- `TbGlobalConfig` 相关字段：
  - `steal_per_friend_daily_limit` — 每个好友每日偷花上限
  - `flower_max_be_stolen_times` — 每朵花最多被偷次数
  - `steal_reward_num` — 偷花奖励数量
  - `owner_min_keep_num` — 被偷者最少保留数量

## 核心文件

| 文件 | 说明 |
|------|------|
| src/apps/role/internal/logic/role_steal.go | RoleSteal 模块（偷花逻辑 + 每日计数） |
| src/apps/role/internal/logic/steal_record.go | StealRecord 模型 + 查询函数 |
| src/apps/role/internal/logic/role_plot.go | 地块系统（收获时清理偷花记录） |
