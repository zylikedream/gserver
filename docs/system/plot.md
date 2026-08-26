# 种植系统

## 概述

种植系统（`RolePlot`）管理玩家的 72 个地块，支持解锁、种植、浇水、收获、移除功能。模块以"地块"为核心实体，每个地块独立维护状态和种植数据。

## 数据结构

### PlotData（jsonb map，key = plotID）

| 字段 | 类型 | 说明 |
|------|------|------|
| plot_id | int32 | 地块ID（1-72） |
| flower_id | int32 | 种植的花ID |
| state | int32 | PlotState 枚举 |
| harvest_count | int32 | 已收获次数 |
| state_time | time | 生长完成/下次收获时间 |

### 状态枚举

| 值 | 名称 | 持久化 | 说明 |
|----|------|--------|------|
| 0 | PLOT_EMPTY | 是 | 空地，可种植 |
| 1 | PLOT_PLANTED | 是 | 已种植，待浇水 |
| 2 | PLOT_GROWING | 是 | 生长中 |
| 3 | PLOT_HARVESTABLE | 否 | 可收获（仅响应，由 now >= state_time 推导） |

状态流转：

`(未解锁) → UnlockPlot → PLOT_EMPTY → PlantFlower → PLOT_PLANTED → WaterFlower → PLOT_GROWING → (时间到) → HarvestFlower → PLOT_GROWING（反复直到 harvest_times 上限）→ PLOT_EMPTY`

`PLOT_HARVESTABLE` 不持久化，`ReqPlotInfo` / `getPlotState` 发现 `GROWING && now >= StateTime` 时在响应中返回。

## 配置表

- `TbGardenPlot`：地块配置（unlock_level, cost），72 条，地块 1-12 免费，13+ 需要等级和消耗
- `TbFlower`：花配置（grow_time, harvest_interval, harvest_times, harvest_item_id, harvest_num, water_cost）
- `TbItem`：水滴和收获物品属性

## Proto 接口

ID 段 `24001~24012`，文件 `protocol/client/flower.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqPlotInfo / RspPlotInfo | 24001-24002 | C→S / S→C | 查询地块状态 |
| ReqUnlockPlot / RspUnlockPlot | 24003-24004 | C→S / S→C | 解锁地块 |
| ReqPlantFlower / RspPlantFlower | 24005-24006 | C→S / S→C | 种植 |
| ReqWaterFlower / RspWaterFlower | 24007-24008 | C→S / S→C | 浇水 |
| ReqHarvestFlower / RspHarvestFlower | 24009-24010 | C→S / S→C | 收获 |
| ReqRemovePlant / RspRemovePlant | 24011-24012 | C→S / S→C | 移除植物 |

## 核心逻辑

### UnlockPlot(plot_id)

1. 读 TbGardenPlot 配置，校验不为 nil
2. 校验地块未解锁（不在 Plots map 中）
3. 读配置 cost，CheckGoods + SaveGoods 扣消耗
4. 创建 PlotData 加入 Plots map，初始状态 EMPTY

### PlantFlower(plot_ids, flower_id)

1. 校验花已解锁（在 Flowers map 中）
2. 校验所有地块已解锁且状态为 EMPTY
3. 设置 flower_id，状态为 PLANTED

### WaterFlower(plot_ids)

1. 校验所有地块状态为 PLANTED
2. 汇总各花配置的水滴消耗（water_cost × 地块数）
3. 先 CheckGoods 再统一扣水滴
4. 设置状态为 GROWING，StateTime = now + grow_time

### HarvestFlower(plot_ids)

1. 校验所有地块状态为 HARVESTABLE（getPlotState 推导）
2. 读 TbFlower 配置，统计收获物品
3. SaveGoods 发放收获物品
4. harvest_count++，判断是否达到 harvest_times：
   - 未达到：保留 GROWING，StateTime = now + harvest_interval（再次生长）
   - 已达到：重置为 EMPTY（flower_id=0, harvest_count=0）

### RemovePlant(plot_ids)

1. 校验所有地块状态为 PLANTED 或 GROWING（非可收获状态）
2. 可收获状态不可移除，必须先收获
3. 重置为 EMPTY

### 被动状态检查（getPlotState）

`ReqPlotInfo` 和其他查询接口通过 `getPlotState` 判断时效状态：`GROWING && now >= StateTime` 时视为 `HARVESTABLE`。客户端可通过差值做倒计时。

## GM 命令

| 命令 | 参数 | 说明 |
|------|------|------|
| unlock_plot | 地块ID | 解锁地块 |

## 错误码

| 变量 | 说明 |
|------|------|
| ErrPlotLocked | 地块未解锁 |
| ErrPlotNotEmpty | 地块非空地 |
| ErrPlotNotPlanted | 地块未种植 |
| ErrPlotNotGrowing | 地块未在生长 |
| ErrPlotNotReady | 收获未就绪 |
| ErrPlotHarvestable | 地块可收获，需先收获 |

## 代码位置

| 文件 | 说明 |
|------|------|
| src/apps/role/internal/logic/role_plot.go | 模块主逻辑 |
| src/apps/role/internal/logic/role_plot_test.go | 单元测试 |
| src/apps/role/internal/logic/role_gm.go | GM 命令 |
| gameconfig/gosrc/garden.GardenPlot.go | 地块配置结构体 |
| gameconfig/gosrc/garden.TbGardenPlot.go | 地块配置表 |
| gameconfig/gosrc/garden.Flower.go | 花配置（含种植字段） |
| gameconfig/json/garden_tbgardenplot.json | 地块配置数据（72条） |
| protocol/client/flower.proto | Proto 定义 |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 数据存储 | 单表 jsonb map | 同 RoleBag/RoleFlower 模式，单行持久化 |
| 状态推导 | getPlotState 被动检查 | 不设定时器，客户端对比时间 |
| 多地块批量操作 | 先校验全部再统一执行 | 避免部分成功导致数据不一致 |
| Harvestable 非持久化 | GROWING + now >= StateTime 推导 | 减少写入，时间敏感状态由运行时决定 |
| 浇水消耗 | 按花独立配置汇总再扣除 | 支持不同花不同水滴消耗，原子扣除 |
