# 偷花系统设计

> 对标《我的花园世界》MVP 社交互动玩法，建立在好友系统之上。

## 1. 概述

玩家进入好友花园后，可对好友已成熟的鲜花进行摘取，获得花产物。轻度竞争，不强对抗。

本阶段范围：
- 查看好友花园（ReqFriendPlotInfo）
- 偷花（ReqStealFlower）
- 修改现有收获逻辑支持被偷扣减

不做：反偷、护盾、复仇、排行榜、日志、黑名单、失败概率、按比例偷取。

## 2. 数据模型

### 2.1 steal_record 表（与 role_plot 同库）

```sql
CREATE TABLE steal_record (
    id          BIGSERIAL PRIMARY KEY,
    owner_id    BIGINT NOT NULL,    -- 被偷者
    plot_id     INT NOT NULL,       -- 地块 ID
    stealer_id  BIGINT NOT NULL,    -- 偷花者
    flower_id   INT NOT NULL,       -- 花配置 ID（用于给奖励）
    steal_time  TIMESTAMPTZ NOT NULL -- 偷取时间
);
CREATE INDEX idx_steal_record_owner_plot ON steal_record (owner_id, plot_id);
CREATE INDEX idx_steal_record_stealer_owner ON steal_record (stealer_id, owner_id);
```

**不修改 PlotData 结构**，偷取次数全从 steal_record 表 COUNT 查询。

**清理策略**：地块回到 EMPTY 时 `DELETE FROM steal_record WHERE owner_id=? AND plot_id=?`。

### 2.2 查询模式

| 用途 | SQL |
|------|-----|
| 单花被偷次数 | `COUNT(*) WHERE owner_id=B AND plot_id=X` |
| A 对 B 今日偷取次数 | `COUNT(*) WHERE stealer_id=A AND owner_id=B AND steal_time >= today_start` |

量极小（单花上限 3 条，每好友每日上限 10 条），走索引无性能问题。

## 3. Proto（flower.proto，24013 起）

```proto
// 查看好友花园 (24013)
message ReqFriendPlotInfo {
    option (msg_id) = 24013;
    int64 friend_id = 1;
}
message RspFriendPlotInfo {
    option (msg_id) = 24014;
    repeated PPlotInfo plots = 1;
    int32 steal_used  = 2;  // 今日对该好友已偷次数
    int32 steal_limit = 3;  // 每日上限
}

// PPlotInfo 新增字段
// bool can_steal = N;  // 是否可被偷取（查看自己花园时为 false）

// 偷花 (24015)
message ReqStealFlower {
    option (msg_id) = 24015;
    int64 friend_id = 1;
    int32 plot_id   = 2;
}
message RspStealFlower {
    option (msg_id) = 24016;
    bool              success = 1;
    string            error   = 2;
    repeated PGoodInfo rewards = 3;  // 偷到的物品
}
```

## 4. 偷花逻辑（A 的 actor 执行）

```
ReqStealFlower(friend_id=B, plot_id=X):
  1. 检查好友关系（查 friend_relation 表）
  2. BEGIN
  3. SELECT * FROM role_plot WHERE player_id=B FOR UPDATE  -- 排他锁
  4. 解析 PlotMap，判断地块 X 可收获: state=GROWING && now >= StateTime
  5. SELECT COUNT(*) FROM steal_record WHERE owner_id=B AND plot_id=X
     → 若 >= flower_max_be_stolen_times，拒绝
  6. SELECT COUNT(*) FROM steal_record WHERE stealer_id=A AND owner_id=B AND steal_time >= today_start
     → 若 >= steal_per_friend_daily_limit，拒绝
  7. INSERT steal_record (owner_id=B, plot_id=X, stealer_id=A, flower_id=配置ID, steal_time=now)
  8. COMMIT
  9. 给 A 发奖励: AddGood(harvest_item_id, steal_reward_num)
  10. 返回 RspStealFlower{success=true, rewards=[{prop_id, num}]}
```

### 4.1 并发控制

- FOR UPDATE 锁 role_plot 行，串行化同一目标的偷花操作
- 锁粒度：玩家整行（一行 role_plot），持有时间极短（检查+插入）

## 5. 查看好友花园（A 的 actor 执行）

```
ReqFriendPlotInfo(friend_id=B):
  1. 检查好友关系
  2. SELECT * FROM role_plot WHERE player_id=B  -- 无需 FOR UPDATE，只读
  3. 解析 PlotMap，计算每个地块的派生状态（可收获判断）
  4. SELECT COUNT(*) FROM steal_record WHERE stealer_id=A AND owner_id=B AND steal_time >= today_start
  5. 对每个地块计算 can_steal:
     - 地块可收获（GROWING + now >= StateTime）
     - 且被偷次数 < flower_max_be_stolen_times
     - 且 A 对 B 今日偷取次数 < steal_per_friend_daily_limit
  6. 返回 RspFriendPlotInfo{plots(can_steal), steal_used, steal_limit}
```

## 6. 收获逻辑修改（B 的 actor 执行）

现有 `ReqHarvestFlower` 增加偷花扣减和并发保护：

```
HarvestFlower(plot_id=X):
  1. BEGIN
  2. SELECT * FROM role_plot WHERE player_id=B FOR UPDATE  -- 排他锁，串行化偷花
  3. SELECT COUNT(*) FROM steal_record WHERE owner_id=B AND plot_id=X
  4. yield = max(base_yield - stolen_count, owner_min_keep_num)
  5. 给 B 发 yield 个物品
  6. harvest_count++
  7. 若 harvest_count >= harvest_times → plot EMPTY
     → DELETE FROM steal_record WHERE owner_id=B AND plot_id=X
  8. UPDATE role_plot (force-save)
  9. COMMIT
```

关键改动：
- 收获时加 FOR UPDATE 锁，与偷花串行化，避免交错读取 steal_record
- yield 计算扣减被偷次数
- 地块归空时清理 steal_record

## 7. 地块保存修改

`ReqPlantFlower`、`ReqWaterFlower`、`ReqHarvestFlower` 操作后立即 save 到 DB，不等 600s flush 周期。确保偷花读 DB 时状态准确。

## 8. 配置（TbFriendConfig 新增字段）

| 字段 | 类型 | 建议值 | 说明 |
|------|------|--------|------|
| steal_unlock_level | int | 6 | 偷花功能开启等级 |
| steal_per_friend_daily_limit | int | 10 | 每好友每日摘取上限 |
| steal_reward_num | int | 1 | 每次偷取获得数量 |
| flower_max_be_stolen_times | int | 3 | 单花单轮被偷上限 |
| owner_min_keep_num | int | 1 | 主人保底数量 |

## 9. 错误处理

| 场景 | 处理 |
|------|------|
| 非好友 | "对方不是你的好友" |
| 花未成熟 | "该鲜花尚未成熟" |
| 花本轮被偷满 | "该鲜花已被摘取完毕" |
| 今日次数已满 | "今日对该好友的摘取次数已用完" |
| 花已被收获 | "该鲜花已无法摘取" |
| 等级未解锁 | "偷花功能尚未解锁" |

## 10. 涉及文件

| 文件 | 改动 |
|------|------|
| protocol/client/flower.proto | 新增 ReqFriendPlotInfo / RspFriendPlotInfo / ReqStealFlower / RspStealFlower |
| src/apps/role/internal/logic/role_plot.go | 收获加 FOR UPDATE + yield 扣减 + steal_record 清理 + 所有操作立即 save |
| src/apps/role/internal/logic/role_steal.go (新) | 偷花逻辑 + 查看好友花园 |
| src/apps/role/internal/logic/model/steal.go (新) | steal_record GORM 模型 |
