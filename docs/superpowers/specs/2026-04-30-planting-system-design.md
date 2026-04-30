# 种植系统技术设计

## 概述

新建 `RolePlot` 模块，管理地块解锁和种植。72 个地块独立存在，视觉上分为 6 组（花圃边框纯客户端展示）。每个地块独立解锁，使用已培育的花进行种植、浇水、收获。与 `RoleFlower`（培育）平级独立模块。

## 数据模型

### PlotData（jsonb map，key = plotID 1-72）

| 字段 | 类型 | 说明 |
|------|------|------|
| plot_id | int32 | 地块全局编号 |
| flower_id | int32 | 种的花ID，0=空 |
| state | int32 | PlotState 枚举 |
| harvest_count | int32 | 已收获次数 |
| state_time | time | 结束时间（浇水后=成熟时间，收获后=下次可收获时间） |

### PlotState 枚举（proto 定义）

| 值 | 名称 | 持久化 | 说明 |
|----|------|--------|------|
| 0 | PLOT_EMPTY | 是 | 空地块（已解锁，可种植） |
| 1 | PLOT_PLANTED | 是 | 已种未浇水 |
| 2 | PLOT_GROWING | 是 | 生长中 |
| 3 | PLOT_HARVESTABLE | 否 | 可收获（仅响应） |

`HARVESTABLE` 不持久化，被动检查：`GROWING && now >= StateTime` 时在响应中返回。

### RolePlotState

```
RolePlotState
  └── Plots  PlotMap (jsonb) — 已解锁地块的种植数据
```

表名 `role_plot`。已解锁的地块在 map 中必有条目（空的为 `PlotData{State: EMPTY}`），未解锁的地块不在 map 中。建号时自动将地块 1-12（初始组）初始化为 EMPTY 写入 map。

### 状态流转

```
(未解锁，不在map中) → UnlockPlot → EMPTY → PlantFlower → PLANTED → WaterFlower → GROWING
    → 被动检查 now >= StateTime → HARVESTABLE(仅响应)
    → HarvestFlower → harvest_count++
        → 未达上限: GROWING + StateTime = now + harvest_interval
        → 达到上限: EMPTY（回到空地块）
    → RemovePlant → EMPTY（PLANTED/GROWING 可移除，不退物品）
```

## 配置表

### TbFlower 新增字段（种植相关）

| 字段 | 类型 | 说明 |
|------|------|------|
| grow_time | int | 首次成熟时间（秒） |
| harvest_interval | int | 收获间隔（秒） |
| harvest_times | int | 可收获总次数 |
| harvest_item_id | int | 收获的花产品ID |
| harvest_num | int | 每次收获数量 |
| water_cost | int | 浇水消耗水滴数 |

### TbGardenPlot（新增，72 条数据）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int | 地块编号(1-72) |
| unlock_level | int | 解锁所需等级 |
| cost | []*GoodStack | 解锁花费（空=免费） |

## Proto 接口

ID 段 `24001~24099`，追加在 `protocol/client/flower.proto`。

```protobuf
enum PlotState {
    PLOT_EMPTY       = 0;
    PLOT_PLANTED     = 1;
    PLOT_GROWING     = 2;
    PLOT_HARVESTABLE = 3;
}

message PPlotInfo {
    int32 plot_id       = 1;
    int32 flower_id     = 2;
    PlotState state     = 3;
    int32 harvest_count = 4;
    int64 state_time    = 5;
}

message ReqPlotInfo {
    option (msg_id) = 24001;
}

message RspPlotInfo {
    option (msg_id) = 24002;
    repeated PPlotInfo plots = 1;
}

message ReqUnlockPlot {
    option (msg_id) = 24003;
    int32 plot_id = 1;
}

message RspUnlockPlot {
    option (msg_id) = 24004;
    PPlotInfo plot = 1;
}

message ReqPlantFlower {
    option (msg_id) = 24005;
    repeated int32 plot_ids = 1;
    int32 flower_id = 2;
}

message RspPlantFlower {
    option (msg_id) = 24006;
    repeated PPlotInfo plots = 1;
}

message ReqWaterFlower {
    option (msg_id) = 24007;
    repeated int32 plot_ids = 1;
}

message RspWaterFlower {
    option (msg_id) = 24008;
    repeated PPlotInfo plots = 1;
}

message ReqHarvestFlower {
    option (msg_id) = 24009;
    repeated int32 plot_ids = 1;
}

message RspHarvestFlower {
    option (msg_id) = 24010;
    repeated PPlotInfo plots = 1;
}

message ReqRemovePlant {
    option (msg_id) = 24011;
    repeated int32 plot_ids = 1;
}

message RspRemovePlant {
    option (msg_id) = 24012;
    repeated PPlotInfo plots = 1;
}
```

## 核心逻辑

### UnlockPlot(plot_id)

1. 校验 plot_id 有效（1-72）且未解锁（不在 Plots map 中）
2. 读 TbGardenPlot 配置，检查等级、扣费
3. 初始化为 EMPTY 写入 Plots

### PlantFlower(plot_ids, flower_id)

1. 校验 flower_id 在 `Role.Flower.Flowers` 中存在（花已培育）
2. 逐个 plot_id：校验地块在 Plots map 中且 state=EMPTY
3. 更新 PlotData：flower_id, state=PLANTED，不扣物品
4. 返回更新的地块列表

### WaterFlower(plot_ids)

1. 逐个 plot_id：校验 state=PLANTED
2. 读 TbFlower 配置获取 water_cost，汇总水滴消耗
3. CheckGoods + SaveGoods 扣水滴
4. 设 state=GROWING，StateTime = now + grow_time
5. 返回更新的地块列表

### HarvestFlower(plot_ids)

1. 逐个 plot_id：校验 state=GROWING 且 now >= StateTime
2. harvest_count++，发花产品进背包（SaveGoods）
3. 如果 harvest_count < harvest_times：state=GROWING，StateTime = now + harvest_interval
4. 如果 harvest_count >= harvest_times：state=EMPTY（地块回到空）
5. 返回更新的地块列表

### RemovePlant(plot_ids)

1. 逐个 plot_id：校验 state=PLANTED 或 GROWING（HARVESTABLE 需先收获）
2. 设 state=EMPTY，不退物品
3. 返回更新的地块列表

## GM 命令

| 命令 | 参数 | 说明 |
|------|------|------|
| unlock_plot | 地块ID | 解锁地块（跳过等级和费用检查） |

## 代码位置

| 文件 | 说明 |
|------|------|
| src/apps/role/internal/logic/role_plot.go | 模块主逻辑 |
| src/apps/role/internal/logic/role_plot_test.go | 单元测试 |
| protocol/client/flower.proto | Proto 定义（追加） |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 模块归属 | 新建 RolePlot | 种植和培育数据模型差异大，独立更清晰 |
| 地块编号 | 全局 1-72 | 客户端操作简单，直接用 plot_id |
| 解锁单位 | 地块（非花圃） | 花圃是纯视觉分组，每个地块独立解锁 |
| 数据存储 | 单表 jsonb map | 同 RoleFlower/RoleBag 模式 |
| 完成检查 | 被动（now >= StateTime） | 同培育系统，不设定时器 |
| StateTime | 存结束时间 | 客户端直接倒计时 |
| 操作接口 | repeated plot_ids | 预留一键操作，客户端传多个 ID |
| 种植消耗 | 无（只需花已培育） | 策划确认不扣种子 |
