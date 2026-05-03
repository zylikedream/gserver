# 鲜花升级系统设计

> 日期：2026-05-03  
> 状态：已批准  
> 基于策划案：`/mnt/d/garden-work/鲜花升级系统策划案.md`

---

## 1. 概述

鲜花升级系统是培育、种植、收获后的长期养成系统。每种花拥有独立等级，升级永久生效，作用于花的品种而非单株植物。

## 2. 数据层

### 2.1 FlowerData 扩展

在 `role_flower.go` 的 `FlowerData` 上新增字段：

```go
type FlowerData struct {
    FlowerID   int32     `json:"flower_id"`
    State      int32     `json:"state"`
    StateTime  time.Time `json:"state_time"`
    Level      int32     `json:"level"`       // 当前等级，默认 1
    BreakStage int32     `json:"break_stage"` // 突破阶段，0=未突破，1=已突破
}
```

- `FlowerMap` 已是 jsonb 存储于 `role_flower.flowers` 列
- 新增字段自动跟随 jsonb 序列化，**不需改数据库表结构**
- 已有花的 jsonb 不包含这两个字段，Go 读取时零值正好为 `Level=1, BreakStage=0`

### 2.2 配置表

| 配置表 | 当前状态 | 所需变更 |
|--------|---------|---------|
| `GardenFlower` | 已含 `essence_item_id`, `essence_drop_rate`, `essence_drop_num` | 新增 `LevelGroup int32` 字段 |
| `GardenFlowerLevel` | 主键为 `FlowerId + Level` | 改为 `LevelGroup + Level` |
| `GardenFlowerBreak` | 主键为 `FlowerId + BreakStage` | 改为 `LevelGroup + BreakStage` |

`GardenFlowerLevel` 核心字段：

| 字段 | 说明 |
|------|------|
| `level_group` | 成长组 ID |
| `level` | 目标等级（累计值，Lv1 行必须有，全 0） |
| `upgrade_coin_cost` | 升级金币消耗 |
| `upgrade_essence_cost` | 升级专属精华消耗 |
| `harvest_num_add` | 累计单次产量加成 |
| `harvest_times_add` | 累计总收获次数加成 |
| `harvest_interval_reduce` | 累计收获间隔缩减值 |
| `essence_drop_rate_add` | 累计专属精华掉率加成 |

`GardenFlowerBreak` 核心字段：

| 字段 | 说明 |
|------|------|
| `level_group` | 成长组 ID |
| `break_stage` | 突破阶段 |
| `need_level` | 触发突破所需等级 |
| `coin_cost` | 突破金币消耗 |
| `essence_cost` | 突破专属精华消耗 |
| `break_item_id` | 突破材料物品 ID |
| `break_item_num` | 突破材料数量 |
| `player_level_limit` | 玩家等级限制 |

## 3. 协议层

### 3.1 新增 Proto 消息

ID 段：`23007~23010`（接在现有培育接口 23006 之后）

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| `ReqUpgradeFlower` / `RspUpgradeFlower` | 23007 / 23008 | C→S / S→C | 升级鲜花 |
| `ReqBreakFlower` / `RspBreakFlower` | 23009 / 23010 | C→S / S→C | 突破 |

### 3.2 现有 Proto 扩展

`RspFlowerInfo` 和 `PFlowerInfo` 新增字段：

- `level` (int32) — 当前等级
- `break_stage` (int32) — 突破阶段

## 4. 逻辑层

### 4.1 RoleFlower 新增方法

#### UpgradeFlower(flowerID)

```
1. 校验花已解锁（在 Flowers map 中）
2. 读取 GardenFlower 配置 → LevelGroup
3. 查找 TbFlowerLevel[LevelGroup][Level+1]：
   - 不存在 → 已达最大等级，返回错误
4. 查找 TbFlowerBreak[LevelGroup][BreakStage+1]：
   - 如果存在 且 Level+1 >= NeedLevel → 必须先突破，返回错误
5. 读取 TbFlowerLevel[LevelGroup][Level+1] 的消耗：
   - UpgradeCoinCost, UpgradeEssenceCost
6. CheckGoods + SaveGoods 扣资源
7. flowerData.Level++
```

#### BreakFlower(flowerID)

```
1. 查找 TbFlowerBreak[LevelGroup][BreakStage+1]：
   - 不存在 → 已达最大突破阶段，返回错误
2. 校验 Level >= NeedLevel
3. 校验玩家等级 >= PlayerLevelLimit
4. 读取消耗：CoinCost, EssenceCost, BreakItemId, BreakItemNum
5. CheckGoods + SaveGoods 扣资源
6. flowerData.BreakStage++
```

### 4.2 HarvestFlower 改造

#### 收获结算对象

```go
type HarvestResult struct {
    FlowerID           int32
    HarvestItemID      int32
    FinalHarvestNum    int32
    RemainingTimes     int32
    NextInterval       int32
    EssenceItemID      int32
    FinalEssenceDropRate int32
    FinalEssenceDropNum  int32
}
```

#### 结算流程

```
1. 读 TbFlower 基础配置
2. 读 RoleFlower 获取该花 Level → LevelGroup
3. 读 TbFlowerLevel[LevelGroup][Level] 累计加成
4. 计算最终值：
   final_harvest_num     = base_harvest_num + harvest_num_add
   final_harvest_times   = base_harvest_times + harvest_times_add
   final_harvest_interval = max(min_interval, base_harvest_interval - harvest_interval_reduce)
   final_essence_drop_rate = base_essence_drop_rate + essence_drop_rate_add
5. 发放花产品（用 final_* 替代 base_*）
6. 按 final_essence_drop_rate 概率判定：
   - 命中 → SaveGoods 发放专属精华
7. 更新地块 harvest_count / 剩余次数
```

### 4.3 服务端配置

访问方式遵循 `gameconfig` 现有模式：

```go
cfg := gameconfig.GetCfg()
flowerCfg := cfg.TbFlower.Get(flowerID)           // TbFlower 按 id
levelCfg := cfg.TbFlowerLevel.Get(flowerCfg.LevelGroup)  // 需按 LevelGroup + Level 查找
breakCfg := cfg.TbFlowerBreak.Get(flowerCfg.LevelGroup)   // 需按 LevelGroup + BreakStage 查找
```

> 注意：由于 `TbFlowerLevel` 和 `TbFlowerBreak` 当前按 `Id` (自增) 索引，而实际需要按 `(LevelGroup, Level)` / `(LevelGroup, BreakStage)` 查找。建议在 `gameconfig` 层封装 `GetByLevelGroupAndLevel(levelGroup, level)` 和 `GetByLevelGroupAndBreakStage(levelGroup, breakStage)` 查询方法。

## 5. 错误码

| 变量 | 说明 |
|------|------|
| `ErrFlowerMaxLevel` | 已达最大等级 |
| `ErrFlowerNeedBreak` | 需先突破才能继续升级 |
| `ErrFlowerBreakMax` | 已达最大突破阶段 |
| `ErrFlowerBreakLevel` | 未达到突破所需等级 |
| `ErrFlowerBreakPlayerLevel` | 玩家等级不足 |

## 6. 实现顺序

1. 配置表生成：更新 `GardenFlower` 加 `LevelGroup`，`FlowerLevel`/`FlowerBreak` 改主键
2. `FlowerData` 新增字段、proto 更新
3. `RoleFlower` 实现 `UpgradeFlower` / `BreakFlower`
4. `RspFlowerInfo` 扩展返回 level / break_stage
5. `RolePlot.HarvestFlower` 改造：两段式结算 + 精华掉落
6. 单元测试
7. 文档更新（`docs/system/flower.md`）

## 7. MVP 边界

明确不做：
- 升级失败率、保底值
- 多次突破（首版仅 1 次）
- 技能树分支、一键升级
- 鲜花互相吞噬或喂养
- 随机词条、外观随等级变化
- 社交偷花相关抗性成长
