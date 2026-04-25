# role_bag 模块统一实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 `BagGood` 统一替换 `BagItem` + `BagCurrency`，移除 `Grid` 字段和格子检查，实现无限堆叠。

**Architecture:** 背包物品和货币统一用 `Goods map[int]*BagGood` 存储，通过 `Type` 字段区分（0=Item, 1=Currency）。protobuf 消息也统一为 `PBagGoodUpdate`。移除所有格子数相关逻辑。

**Tech Stack:** Go, proto3, pgx/JSONB

---

## 文件变更总览

| 文件 | 操作 |
|------|------|
| `src/apps/role/internal/logic/bag/item.go` | 重写为 `BagGood`、`BagChange` |
| `src/apps/role/internal/logic/bag/currency.go` | 删除 |
| `src/apps/role/internal/logic/role_bag.go` | 修改 `RoleBagState`、简化 `AddSingleItem`/`DecSingleItem` |
| `protocol/bag.proto` | 合并 `PItemUpdate`/`PCurrencyUpdate` 为 `PBagGoodUpdate` |

---

## Task 1: 重写 bag/item.go

**文件:**
- 修改: `src/apps/role/internal/logic/bag/item.go`

删除 `BagItem`、`BagCurrency`、`Item`、`Currency`、`ItemChange`、`CurrencyChange`。
新增统一结构：

```go
package bag

import "time"

// Type constants
const (
    GoodTypeItem     = 0
    GoodTypeCurrency = 1
)

type BagGood struct {
    Type       int       `db:"type"`       // 0=Item, 1=Currency
    PropID     int       `db:"prop_id"`
    Num        uint64    `db:"num"`
    UpdateTime time.Time `db:"update_time"`
}

type BagChange struct {
    PropID  int
    PreNum uint64
    Num    uint64
}

// Item 类型别名，保持向后兼容
type Item struct {
    ID  int `bson:"id" copier:"Id"`
    Num uint64 `bson:"num"`
}

func (g *BagGood) Update(num uint64) *BagChange {
    chg := &BagChange{
        PropID:  g.PropID,
        PreNum: g.Num,
        Num:    num,
    }
    g.Num = num
    g.UpdateTime = time.Now()
    return chg
}
```

- [ ] **Step 1: 备份并重写 `bag/item.go`**

将上述代码写入 `src/apps/role/internal/logic/bag/item.go`

- [ ] **Step 2: 删除 `bag/currency.go`**

```bash
rm src/apps/role/internal/logic/bag/currency.go
```

- [ ] **Step 3: 验证编译**

```bash
go build ./src/apps/role/... 2>&1
```

预期: 失败（`role_bag.go` 还在引用旧的 `bag.BagItem` 等类型），这是正常的。

---

## Task 2: 修改 RoleBagState

**文件:**
- 修改: `src/apps/role/internal/logic/role_bag.go:23-28`

```go
type RoleBagState struct {
    RolePersistState `db:"inline"`
    Goods            map[int]*bag.BagGood `db:"goods"` // key=PropID
}
```

- [ ] **Step 1: 修改 `RoleBagState` 结构体**

将 `Items`、`Currencies`、`GridUse` 替换为 `Goods map[int]*bag.BagGood`

- [ ] **Step 2: 修改 `OnModInit`**

```go
func (r *RoleBag) OnModInit(ctx context.Context) error {
    r.Goods = make(map[int]*bag.BagGood)
    return nil
}
```

---

## Task 3: 简化 AddSingleItem

**文件:**
- 修改: `src/apps/role/internal/logic/role_bag.go:51-67`

移除所有格子检查逻辑。

```go
func (r *RoleBag) AddSingleItem(ctx context.Context, item bag.Item) (*bag.BagChange, error) {
    have := r.Goods[item.ID]
    if have == nil {
        have = &bag.BagGood{
            Type:   bag.GoodTypeItem,
            PropID: item.ID,
        }
        r.Goods[item.ID] = have
    }
    chg := have.Update(have.Num + item.Num)
    return chg, nil
}
```

- [ ] **Step 1: 用简化版本替换 `AddSingleItem`**

删除 `itemTable`、`itemconf`、`MaxOverlap`、`Grid` 计算、`isGridFull` 调用。

- [ ] **Step 2: 验证编译**

```bash
go build ./src/apps/role/... 2>&1
```

---

## Task 4: 简化 DecSingleItem

**文件:**
- 修改: `src/apps/role/internal/logic/role_bag.go:82-99`

```go
func (r *RoleBag) DecSingleItem(ctx context.Context, item bag.Item) (*bag.BagChange, error) {
    have := r.Goods[item.ID]
    if have == nil || item.Num > have.Num {
        return nil, errors.Wrapf(ErrItemDecItemNotEnough, "have:%v, need:%v", have, item)
    }
    chg := have.Update(have.Num - item.Num)
    if have.Num == 0 {
        delete(r.Goods, item.ID)
    }
    return chg, nil
}
```

- [ ] **Step 1: 用简化版本替换 `DecSingleItem`**

删除 `MaxOverlap`、`Grid` 计算、`GridUse` 更新。

- [ ] **Step 2: 验证编译**

```bash
go build ./src/apps/role/... 2>&1
```

---

## Task 5: 简化辅助方法

**文件:**
- 修改: `src/apps/role/internal/logic/role_bag.go`

- [ ] **Step 1: 修改 `GetItem`**

```go
func (r *RoleBag) GetItem(propID int) bag.Item {
    good := r.Goods[propID]
    if good == nil {
        return bag.Item{ID: propID, Num: 0}
    }
    return bag.Item{ID: propID, Num: good.Num}
}
```

- [ ] **Step 2: 修改 `notifyItemUpdate`（改名为 `notifyBagUpdate`）**

```go
func (r *RoleBag) notifyBagUpdate(ctx context.Context, chgs []*bag.BagChange) {
    msg := &pb.NotifyItemUpdate{Items: make([]*pb.PItemUpdate, 0, len(chgs))}
    for _, chg := range chgs {
        msg.Items = append(msg.Items, &pb.PItemUpdate{
            PropId: int32(chg.PropID),
            Num:    int64(chg.Num),
        })
    }
    r.Role.SendClient(ctx, msg)
}
```

- [ ] **Step 3: 修改 `AddItem` 中的通知调用**

把 `r.notifyItemUpdate(ctx, chgs)` 改为 `r.notifyBagUpdate(ctx, chgs)`。

- [ ] **Step 4: 修改 `DecItem` 中的通知调用**

同上。

- [ ] **Step 5: 验证编译**

```bash
go build ./src/apps/role/... 2>&1
```

---

## Task 6: 删除废弃代码

**文件:**
- 修改: `src/apps/role/internal/logic/role_bag.go`

- [ ] **Step 1: 删除 `ErrItemAddNoGrid` 错误变量**

删除 `var ErrItemAddNoGrid = errors.New("bag full no grid to add item")`

- [ ] **Step 2: 删除 `isGridFull` 方法**

删除整个 `func (r *RoleBag) isGridFull(add int) bool` 方法。

- [ ] **Step 3: 删除 `GridUse` 引用**

确保 `RoleBagState` 中无 `GridUse` 字段。

- [ ] **Step 4: 验证编译**

```bash
go build ./src/apps/role/... 2>&1
```

---

## Task 7: 更新 protobuf 定义

**文件:**
- 修改: `protocol/bag.proto`

合并 `PItemUpdate` 和 `PCurrencyUpdate` 为统一的 `PBagGoodUpdate`：

```protobuf
message PBagGoodUpdate {
    int32 prop_id = 1;  // 统一用 prop_id
    int64 pre_num = 2;
    int64 num = 3;
}

message NotifyItemUpdate {
    repeated PBagGoodUpdate items = 1;  // 物品和货币统一
}
```

`NotifyCurrencyUpdate`/`PCurrencyUpdate` 暂保留，不在本次范围内（`RspBagInfo` 仍返回分开的 items/currencys）。

- [ ] **Step 1: 修改 `protocol/bag.proto`**

将 `PItemUpdate.prop_id` 保持不变，`NotifyItemUpdate` 的 item 类型改为 `PBagGoodUpdate`。

- [ ] **Step 2: 重新生成 pb 文件**

```bash
make gen 2>&1
```

- [ ] **Step 3: 验证编译**

```bash
go build ./protocol/... 2>&1
```

---

## Task 8: 验证全量编译

- [ ] **Step 1: 编译整个项目**

```bash
go build ./... 2>&1
```

预期: 全部通过，无错误。

---

## 自查清单

- [ ] `BagGood` 替代了 `BagItem` + `BagCurrency`
- [ ] `BagChange` 替代了 `ItemChange` + `CurrencyChange`
- [ ] `RoleBagState.Goods` 替代了 `Items` + `Currencies`
- [ ] `GridUse` 已删除
- [ ] `isGridFull` 已删除
- [ ] `ErrItemAddNoGrid` 已删除
- [ ] `MaxOverlap` 计算已删除
- [ ] `AddSingleItem` 无格子检查
- [ ] `DecSingleItem` 无格子检查
- [ ] `go build ./...` 通过
