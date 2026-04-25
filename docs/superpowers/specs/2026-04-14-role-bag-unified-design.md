# role_bag 模块统一设计

## 概述

统一 `BagItem` 和 `BagCurrency` 为单一 `BagGood` 结构，移除 `Grid` 字段和格子数检查，实现无限堆叠。

## 改动范围

- `src/apps/role/internal/logic/bag/item.go` — 删除，重写为 `BagGood`
- `src/apps/role/internal/logic/bag/currency.go` — 删除
- `src/apps/role/internal/logic/role_bag.go` — 简化状态和逻辑
- `src/apps/role/internal/event/bag.go` — 更新事件结构
- protobuf 消息定义 — 检查并更新 `BagChange` 相关协议

---

## 新数据结构

### BagGood（替换 BagItem + BagCurrency）

```go
type BagGood struct {
    Type       int       `db:"type"`        // 0=Item, 1=Currency, extensible int
    PropID     int       `db:"prop_id"`    // 配置表ID，作为map的key
    Num        uint64    `db:"num"`        // 无限堆叠
    UpdateTime time.Time `db:"update_time"`
}
```

- `Type` 用 int 可扩展，未来可加 `Material(2)`、`Ticket(3)` 等
- `PropID` 作为 map key 唯一确定物品类型，不需要在变更记录里存 Type

### BagChange（替换 ItemChange + CurrencyChange）

```go
type BagChange struct {
    PropID  int
    PreNum uint64
    Num    uint64
}
```

- 不需要 `Type` 字段，`PropID` 唯一确定类型

---

## RoleBagState 简化

```go
type RoleBagState struct {
    RolePersistState `db:"inline"`
    Goods           map[int]*BagGood  `db:"goods"`  // key=PropID
}
```

**移除：**
- `Items map[int]*BagItem`
- `Currencies map[int]*BagCurrency`
- `GridUse int`

---

## 逻辑变更

### OnModInit

```go
func (r *RoleBag) OnModInit(ctx context.Context) error {
    r.Goods = make(map[int]*BagGood)
    return nil
}
```

初始化单个 map，不再分别初始化 Items 和 Currencies。

### AddSingleItem

- 移除 `isGridFull` 调用
- 移除 `MaxOverlap` 计算和 `Grid` 字段更新
- 移除 `GridUse` 计数更新
- 直接操作 `Goods[PropID]`，无限堆叠

### DecSingleItem

- 同样移除格子相关逻辑
- 直接操作 `Goods[PropID]`

### GetItem / GetCurrency

- 统一为 `GetGood(propID int) *BagGood`
- 或保留两个方法，内部都读 `Goods`

### 格子检查相关

**删除：**
- `ErrItemAddNoGrid` 错误
- `isGridFull` 方法
- `MaxGrid` 配置引用（如果仅用于背包格子检查）

---

## 事件广播

`BagChange` 广播时，接收方通过 `PropID` 查配置表 `TbItem` 判断类型，不依赖 `Type` 字段。

如果 protobuf 协议中 `ItemChange` 和 `CurrencyChange` 是分开的，合并为统一的 `BagChange`。

---

## 数据库兼容

无迁移需求。项目处于开发阶段，数据随时可清空重置。

---

## 实现顺序

1. 新增 `BagGood`、`BagChange` 结构
2. 修改 `RoleBagState`，移除旧字段（`Items`、`Currencies`、`GridUse`）
3. 简化 `AddSingleItem` / `DecSingleItem` 逻辑（移除格子检查）
4. 更新 `RoleBag` 的 `OnModInit` 初始化
5. 更新事件结构
6. 检查并更新 protobuf 定义
7. 删除废弃的 `bag/item.go`、`bag/currency.go`
8. 验证 `go build ./...` 通过
