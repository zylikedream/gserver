# 鲜花系统

## 概述

鲜花系统（`RoleFlower`）管理玩家的鲜花数据，培育是首个子功能。模块以"花"为核心实体设计，后续升级、种花等功能在 `FlowerData` 上扩展字段。

## 数据结构

### FlowerData（jsonb map，key = flowerID）

| 字段 | 类型 | 说明 |
|------|------|------|
| flower_id | int32 | 花ID |
| state | int32 | FlowerState 枚举 |
| state_time | time | BREEDING 时为培育完成时间 |

### 状态枚举

| 值 | 名称 | 持久化 | 说明 |
|----|------|--------|------|
| 0 | FLOWER_UNLOCKED | 是 | 已解锁，可培育 |
| 1 | FLOWER_BREEDING | 是 | 培育中 |
| 2 | FLOWER_BREED_DONE | 否 | 培育完成，待收获（仅响应） |
| 3 | FLOWER_HARVESTED | 是 | 已收获，可种植 |

状态流转：`(不存在) → GM解锁 → UNLOCKED → StartBreed → BREEDING → FinishBreed → HARVESTED`

`BREED_DONE` 不持久化，`ReqFlowerInfo` 发现 `BREEDING && now >= StateTime` 时在响应中返回。

## 配置表

- `TbFlower`：花配置（breed_time, breed_cost）
- `TbItem`：材料和种子物品属性

## Proto 接口

ID 段 `23001~23099`，文件 `protocol/client/flower.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqFlowerInfo / RspFlowerInfo | 23001-23002 | C→S / S→C | 查询鲜花状态 |
| ReqStartBreed / RspStartBreed | 23003-23004 | C→S / S→C | 开始培育 |
| ReqFinishBreed / RspFinishBreed | 23005-23006 | C→S / S→C | 收获成果 |

## 核心逻辑

### StartBreed(flower_id)

1. 校验花已解锁（在 Flowers map 中）
2. 校验没有正在培育的花（FindBreeding）
3. 读 TbFlower 配置，CheckGoods 检查材料
4. SaveGoods 扣材料
5. 设 State=BREEDING, StateTime=now+BreedTime

### FinishBreed(flower_id)

1. 校验花存在且 State=BREEDING
2. 校验 now >= StateTime（被动检查）
3. 设 State=HARVESTED

## GM 命令

| 命令 | 参数 | 说明 |
|------|------|------|
| add_flower | 花ID | 解锁花 |
| add_flower_breed_goods | 花ID | 一键添加培育所需材料 |
| finish_breed | - | 立即完成当前培育（将 StateTime 设为过去） |

## 错误码

| 变量 | 说明 |
|------|------|
| ErrFlowerLocked | 花未解锁 |
| ErrFlowerBreedBusy | 已有花在培育中 |
| ErrFlowerNotBreeding | 花未在培育中 |
| ErrFlowerNotBreedDone | 培育尚未完成 |

## 代码位置

| 文件 | 说明 |
|------|------|
| src/apps/role/internal/logic/role_flower.go | 模块主逻辑 |
| src/apps/role/internal/logic/role_flower_test.go | 单元测试 |
| protocol/client/flower.proto | Proto 定义 |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 模块命名 | RoleFlower（非 RoleBreed） | 培育是子功能，后续升级/种花挂在花上 |
| 数据存储 | 单表 jsonb map | 同 RoleBag 模式，单行持久化 |
| 完成检查 | 被动（now >= StateTime） | 不设定时器，客户端对比时间 |
| StateTime | 存完成时间 | 客户端直接做倒计时 |

---

## 鲜花升级系统

### 数据模型扩展

`FlowerData` 新增字段（自动跟随 jsonb 序列化，不需改数据库表结构）：

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| level | int32 | 1 | 当前等级 |
| break_stage | int32 | 0 | 突破阶段，0=未突破 |

### Proto 接口扩展

| 消息 | ID | 说明 |
|------|----|------|
| ReqUpgradeFlower / RspUpgradeFlower | 23007-23008 | 升级鲜花 |
| ReqBreakFlower / RspBreakFlower | 23009-23010 | 突破 |

`PFlowerInfo` 扩展 `level`、`break_stage` 字段。

### 升级流程

1. 校验花已解锁
2. 通过 TbFlower.level_group 查找升级配置
3. 校验是否被突破门槛拦住（查找下一级突破配置，若存在且 level >= need_level 则需先突破）
4. 扣金币 + 专属精华 → level++

### 突破流程

1. 查找 TbFlowerBreak 下一阶段配置
2. 校验等级（和预留玩家等级门槛，待玩家等级系统上线）
3. 扣金币 + 专属精华 + 突破材料 → break_stage++

### 收获结算变化

- 花产品数量 = 基础 + 等级加成
- 收获次数 = 基础 + 等级加成
- 收获间隔 = 基础 - 等级缩减（最小 1s）
- 额外按万分比概率掉落专属精华（含等级掉率加成）

### 配置表

| 表 | 主键 | 说明 |
|----|------|------|
| TbFlower | id | 花基础配置，新增 level_group、essence_item_id 等字段 |
| TbFlowerLevel | level_group + level | 每级升级消耗和累计加成 |
| TbFlowerBreak | level_group + break_stage | 突破条件和消耗 |

### GM 命令

| 命令 | 参数 | 说明 |
|------|------|------|
| add_flower_level | 花ID 等级 | 设置鲜花等级（用于测试） |

### 错误码

| 变量 | 说明 |
|------|------|
| ErrFlowerMaxLevel | 已达最大等级 |
| ErrFlowerNeedBreak | 需先突破才能继续升级 |
| ErrFlowerBreakMax | 已达最大突破阶段 |
| ErrFlowerBreakLevel | 未达到突破所需等级 |
| ErrFlowerBreakPlayerLevel | 玩家等级不足（预留） |

### MVP 边界

- 升级失败率、保底值 — 不做
- 多次突破（首版仅 1 次）— 不做
- 技能树分支、一键升级 — 不做
- 鲜花互相吞噬或喂养 — 不做
- 随机词条、外观随等级变化 — 不做
