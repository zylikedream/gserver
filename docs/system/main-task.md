# 主线任务系统

## 概述

主线任务系统（`RoleMainTask`）实现线性任务链，每个角色同时只有一个活跃任务。系统通过角色内部事件总线驱动进度更新，支持两种进度模式：事件累加和状态快照。

## 数据结构

### role_main_task（角色持久化模块，PostgreSQL）

| 字段 | 类型 | 说明 |
|------|------|------|
| role_id | int64 PK | 角色ID |
| current_task_id | int32 | 当前任务ID（0=全部完成） |
| progress | int32 | 当前进度 |
| status | int32 | MainTaskStatus 枚举 |

### MainTaskStatus 枚举

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | IN_PROGRESS | 进行中 |
| 1 | CLAIMABLE | 可领奖 |
| 2 | FINISHED | 已完成（任务链结束） |

### 任务配置（TbMainTask）

| 字段 | 说明 |
|------|------|
| id | 任务ID |
| chapter | 章节 |
| sort | 章节内排序 |
| pre_task_id | 前置任务（0=第一个任务） |
| progress_mode | 进度模式（1=事件累加, 2=状态快照） |
| target_type | 目标类型 |
| target_param | 目标参数（0=通配） |
| target_num | 目标数量 |
| reward[] | 奖励列表 |

### 进度模式

| 值 | 名称 | 说明 |
|----|------|------|
| 1 | AFTER_ACCEPT | 接受任务后累加事件计数 |
| 2 | CURRENT_STATE | 检查当前角色状态快照 |

### 目标类型（ETaskTargetType）

| 值 | 名称 | 说明 |
|----|------|------|
| 1 | BREED_START | 开始培育次数 |
| 2 | BREED_FINISH | 完成培育次数 |
| 3 | PLANT_FLOWER | 种花次数 |
| 4 | WATER_FLOWER | 浇水次数 |
| 5 | HARVEST_FLOWER | 收获次数 |
| 6 | GET_ITEM | 获得物品 |
| 7 | PLAYER_LEVEL | 玩家等级 |
| 8 | UNLOCK_PLOT | 解锁地块数 |
| 9 | FLOWER_LEVEL | 鲜花等级 |
| 10 | OWN_ITEM | 拥有物品数 |

## 核心逻辑

### 任务生命周期

```
创建角色 → acceptTask(第一个任务, pre_task_id==0)
    → IN_PROGRESS → 事件驱动进度更新
    → CLAIMABLE → ReqClaimMainTask
    → 发放奖励 → acceptTask(下一个任务)
    → ... → 无下一任务 → FINISHED
```

### 事件驱动

系统订阅以下事件类型：

`EVENT_GOOD_CHANGE`, `EVENT_PLAYER_LEVEL`, `EVENT_BREED_START`, `EVENT_BREED_FINISH`, `EVENT_PLANT_FLOWER`, `EVENT_WATER_FLOWER`, `EVENT_HARVEST_FLOWER`, `EVENT_UNLOCK_PLOT`, `EVENT_FLOWER_LEVEL`

- **AFTER_ACCEPT 模式**：收到匹配事件时累加进度，达到目标数后变为 CLAIMABLE
- **CURRENT_STATE 模式**：收到事件时重新计算当前状态（等级、背包物品数等），满足条件即 CLAIMABLE

### ReqClaimMainTask

1. 校验 status == CLAIMABLE
2. 发放奖励（通过背包系统 SaveGoods）
3. 查找下一个任务（pre_task_id == 当前任务ID）
4. 有下一任务 → acceptTask，推送 NotifyMainTaskUpdate
5. 无下一任务 → FinishAll，标记整个任务系统完成

## Proto 接口

ID 段 `25001~25099`，文件 `protocol/client/maintask.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqMainTaskInfo / RspMainTaskInfo | 25001-25002 | C→S / S→C | 查询当前任务 |
| ReqClaimMainTask / RspClaimMainTask | 25003-25004 | C→S / S→C | 领取任务奖励 |
| NotifyMainTaskUpdate | 25005 | S→C | 任务状态变更推送 |

## 配置表

- `TbMainTask`（`gameconfig/json/garden_tbmaintask.json`）：任务链定义
- 当前配置：13 个任务（ID 1003-1015），分 6 章

## 核心文件

| 文件 | 说明 |
|------|------|
| src/apps/role/internal/logic/role_maintask.go | RoleMainTask 模块 |
| src/apps/role/internal/logic/task_state.go | 任务状态机（Init/ApplyEvent/FinishAll） |
| src/apps/role/internal/logic/task_progress.go | 进度计算逻辑 |
| gameconfig/gosrc/garden.MainTask.go | 任务配置结构 |
| gameconfig/gosrc/garden.TbMainTask.go | 配置表访问器 |
