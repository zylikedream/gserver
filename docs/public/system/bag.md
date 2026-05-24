# 背包系统

## 概述

背包系统管理玩家的所有物品（good），包括货币和普通物品。统一使用 `GoodsMap` 存储，通过配置表中的 `major_type` 区分货币和物品。核心操作为 `SaveGoods`，原子性地完成扣除和添加。

## 数据结构

### Good（物品）

```go
// src/apps/role/internal/logic/bag/good.go
type Good struct {
    GoodID int
    Num    uint64
}
```

### BagGood（背包物品）

```go
type BagGood struct {
    GoodID     int       `json:"good_id"`
    Num        uint64
    UpdateTime time.Time `json:"update_time"`
}
```

`BagGood.Update(num)` 返回 `GoodOp`，记录变更前后数量。

### GoodsMap

```go
// src/apps/role/internal/logic/role_bag.go
type GoodsMap map[int]bag.BagGood  // key = goodID
```

存储在 PostgreSQL `role_bag` 表的 `goods` JSONB 列中。值类型为 `BagGood`（非指针）。

### 操作记录

```go
// GoodOp 单次操作记录
type GoodOp struct {
    GoodID int
    PreNum uint64
    Num    uint64
    Reson  string
}

// GoodUpdate 合并后的变更（同一 goodID 的扣+加合并为一条）
type GoodUpdate struct {
    GoodID    int
    PreNum    uint64
    Num       uint64
    RemoveNum uint64
    AddNum    uint64
    Reason    string
}
```

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
| `init_items` | 新玩家初始物品 `[]*GardenGoodStack` |
| `bag_max_cells` | 背包格子上限（MVP 未启用） |

### TbItemTag（标签表）

标签筛选由客户端负责，服务端返回全量物品。

## 核心逻辑

### SaveGoods（原子操作）

```go
func (r *RoleBag) SaveGoods(ctx context.Context, removeGoods []*gamecfg.GardenGoodStack,
    addGoods []*gamecfg.GardenGoodStack, reason string) error
```

流程：
1. `classifyGoods` 将 `[]*GardenGoodStack` 按ID分组累加数量，转为 `[]Good`
2. `cloneGoodsMap` 复制当前背包（clone-and-modify 模式）
3. `decGoods` 在副本上执行扣除，每个物品生成 `GoodOp`
4. `addGoods` 在副本上执行添加，校验 `MaxStack`，生成 `GoodOp`
5. 成功后 `saveGoodsMap` 替换原 map 并标记脏
6. `onBagChange` 合并 GoodOps → GoodUpdates，通知客户端
7. `saveGoodOps` 记录日志（预留同步到日志库）

扣除和添加在同一副本上执行，失败不影响原数据。

### 合并变更

`onBagChange` 将同一 goodID 的多次操作合并为一条 `GoodUpdate`：
- `PreNum` 取第一次操作的变更前数量
- `Num` 取最后一次操作的变更后数量
- `AddNum` / `RemoveNum` 累加净变化

### 查询

- `GetGood(goodID)` → 按 ID 直接查 map，O(1)
- `CheckGoods(goodsStack)` → 检查一组物品是否足够

### 初始物品发放

建号时 `RoleMain.OnRoleCreated` 读取 `GlobalConfig.InitItems` 调用 `SaveGoods(nil, initItems, "")` 发放。

## Proto 接口

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| `ReqBagInfo` | 21001 | C→S | 请求背包所有物品 |
| `RspBagInfo` | 21001 | S→C | 返回所有物品（含货币） |
| `NotifyBagUpdate` | - | S→C | 物品增减时推送变更 |

### RspBagInfo

```proto
message RspBagInfo {
    repeated PGoodInfo goods = 1;
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
    int64 pre_num = 2;
    int64 num     = 3;
}
```

## 代码位置

| 文件 | 说明 |
|------|------|
| `src/apps/role/internal/logic/role_bag.go` | 背包模块主逻辑 |
| `src/apps/role/internal/logic/bag/good.go` | Good、BagGood、GoodOp、GoodUpdate 类型 |
| `gameconfig/gameconfig.go` | 配置表加载模块 |

## 预留接口

| 方法 | 说明 | 当前实现 |
|------|------|----------|
| `onGoodUpdateEvent` | 物品变更事件分发 | 空，预留给任务/成就等模块 |
| `saveGoodOps` | 操作日志持久化 | MVP 仅打 debug log |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 命名 | 统一 good（= item + currency） | 消除 item/good 混用 |
| 核心操作 | SaveGoods 原子接口 | 实际业务（抽卡/合成）需要同时扣+加 |
| 安全模式 | clone-and-modify | 操作失败不影响原数据，无需回滚 |
| 变更合并 | GoodUpdate 合并 | 同一物品的扣+加合并为一条通知 |
| 货币存储 | 统一 GoodsMap | 货币也是物品，有唯一 ID，map 查询 O(1) |
| 标签筛选 | 客户端负责 | 服务端返回全量，客户端按配置表筛选 |

## 错误码

| 变量 | 说明 |
|------|------|
| `ErrGoodNotEnough` | 物品数量不足 |
| `ErrGoodConfigNotFound` | 配置表未找到该物品 |
| `ErrGoodExceedMaxStack` | 超出最大堆叠数 |
