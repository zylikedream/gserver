# 背包系统

## 概述

背包系统管理玩家的所有物品，包括货币和普通物品。统一使用 `GoodsMap` 存储，通过配置表中的 `major_type` 区分货币和物品。

## 数据结构

### GoodsMap

```go
// src/apps/role/internal/logic/role_bag.go
type GoodsMap map[int]*bag.BagGood  // key = itemID

type BagGood struct {
    PropID     int       `json:"prop_id"`
    Num        uint64
    UpdateTime time.Time `json:"update_time"`
}
```

存储在 PostgreSQL `role_bag` 表的 `goods` JSONB 列中。

### 数据库

| 表名 | 结构体 | 说明 |
|------|--------|------|
| `role_bag` | `RoleBagState` | 背包数据，goods 字段存 JSONB |

## 配置表依赖

### TbItem（物品表）

每个物品ID对应一条配置，核心字段：

| 字段 | 说明 |
|------|------|
| `major_type` | 主分类：CURRENCY(1)=货币, ITEM(2)=物品 |
| `sub_type` | 子分类：GOLD/DIAMOND/WATER_DROP/PREMIUM/SEED/MATERIAL 等 |
| `max_stack` | 最大堆叠数，0=无限制 |
| `can_sell` | 是否可出售 |
| `use_type` | 使用效果类型 |
| `quality` | 品质：1-5 |

### TbGlobalConfig（全局配置）

| 字段 | 说明 |
|------|------|
| `init_items` | 新玩家初始物品 `[]*GardenItemStack` |
| `bag_max_cells` | 背包格子上限（MVP 未启用） |

### TbItemTag（标签表）

标签筛选由客户端负责，服务端返回全量物品。

## 核心逻辑

### 添加物品

`AddItemStack` → `AddItem` → `AddSingleItem`

AddSingleItem 校验流程：
1. 查配置表 `TbItem.Get(itemID)`，不存在返回 `ErrItemConfigNotFound`
2. 计算新数量 `newNum = have.Num + item.Num`
3. 检查堆叠上限 `cfg.MaxStack > 0 && newNum > MaxStack` → `ErrItemExceedMaxStack`
4. 更新并标记脏 `MarkDirty()`
5. 通知客户端 `NotifyBagUpdate`

### 扣除物品

`DecItemStack` → `DecItem` → `DecSingleItem`

- 数量不足返回 `ErrItemDecItemNotEnough`
- 扣到 0 自动从 GoodsMap 删除

### 查询物品

`GetItem(propID)` → 按 ID 直接查 map，O(1)

### 初始物品发放

建号时 `RoleMain.OnRoleCreated` 读取 `GlobalConfig.InitItems` 调用 `AddItemStack` 发放。

## Proto 接口

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| `ReqBagInfo` | 21001 | C→S | 请求背包所有物品 |
| `RspBagInfo` | 21001 | S→C | 返回所有物品（含货币） |
| `NotifyBagUpdate` | - | S→C | 物品增减时推送变更 |

### RspBagInfo

```proto
message RspBagInfo {
    repeated PGoodInfo goods = 1;  // 所有物品（含货币）
}
message PGoodInfo {
    int32 prop_id = 1;
    int64 num     = 2;
}
```

### NotifyBagUpdate

```proto
message NotifyBagUpdate {
    repeated PBagGoodUpdate goods = 1;
}
message PBagGoodUpdate {
    int32 prop_id = 1;
    int64 pre_num = 2;  // 变更前数量
    int64 num     = 3;  // 变更后数量
}
```

## 代码位置

| 文件 | 说明 |
|------|------|
| `src/apps/role/internal/logic/role_bag.go` | 背包模块主逻辑 |
| `src/apps/role/internal/logic/bag/item.go` | BagGood、BagChange、Item 类型 |
| `gameconfig/gameconfig.go` | 配置表加载模块 |
| `gameconfig/gosrc/garden.Item.go` | 物品配置结构体 |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 货币存储 | 统一 GoodsMap | 货币也是物品，有唯一 ID，map 查询 O(1) |
| 标签筛选 | 客户端负责 | 服务端返回全量，客户端按配置表筛选 |
| 整理排序 | 客户端负责 | 纯展示逻辑 |
| 背包上限 | MVP 未启用 | 暂无必要 |

## MVP 未实现

- 物品使用逻辑（宝箱/加能量/加经验/加 Buff）
- 物品出售
- 格子上限检查
- 批量使用
