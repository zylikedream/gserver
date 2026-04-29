# 背包系统 MVP 设计

> 日期: 2026-04-29
> 状态: 已批准

## 目标

在现有背包 CRUD 基础上，补齐配置表加载、堆叠上限校验、初始物品发放。客户端负责所有展示逻辑（标签筛选、排序、分页），服务端只管数据。

## 范围

**做：**
- gameconfig 作为子模块加载配置表
- AddSingleItem 增加 max_stack 堆叠上限校验
- 建号时发放初始物品（GlobalConfig.InitItems）
- ReqBagInfo 返回所有物品，客户端自行筛选

**不做：**
- 格子上限检查
- 物品使用逻辑（宝箱/加能量/加经验/加 Buff）
- 物品出售
- 背包整理排序
- ReqBagFilter 服务端筛选

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 货币存储 | 统一 GoodsMap，不分离 | 货币也是物品，有 ID，map 查询 O(1)，逻辑最简 |
| 背包上限 | MVP 不做 | 暂无必要 |
| 物品使用 | 不做 | 统一使用逻辑后续单独设计 |
| 标签筛选 | 客户端负责 | 服务端返回全量，客户端按配置表筛选 |
| 整理排序 | 客户端负责 | 纯展示逻辑 |

## 改动清单

### 1. gameconfig/gameconfig.go

`OnModInit` 从独立函数改为 `gameConfig` 的方法：

```go
func (c *gameConfig) OnModInit(ctx context.Context) error {
    return c.initTables()
}
```

删除原有的独立 `func OnModInit(ctx context.Context)`。

### 2. src/apps/role/role_app.go

在 `OnModInit` 中将 gameConfig 添加为子模块：

```go
func (r *roleApp) OnModInit(ctx context.Context) error {
    r.AddModule(ctx, gameconfig.NewGameConfig())
    logic.InitRoleSchema(ctx)
    gxyservice.ServiceApp().LoadService(ctx, NewRoleActorService())
    return nil
}
```

### 3. src/apps/role/internal/logic/role_bag.go

`AddSingleItem` 增加配置表校验：

```go
func (r *RoleBag) AddSingleItem(ctx context.Context, item bag.Item) (*bag.BagChange, error) {
    cfg := gameconfig.GetTables().TbItem.Get(int32(item.ID))
    if cfg == nil {
        return nil, errors.New("item config not found")
    }
    have := r.Goods[item.ID]
    if have == nil {
        have = &bag.BagGood{PropID: item.ID}
        r.Goods[item.ID] = have
    }
    newNum := have.Num + item.Num
    if cfg.MaxStack > 0 && newNum > uint64(cfg.MaxStack) {
        return nil, errors.New("exceed max stack")
    }
    chg := have.Update(newNum)
    r.MarkDirty()
    return chg, nil
}
```

### 4. src/apps/role/internal/logic/role_main.go

`OnRoleCreated` 中发放初始物品：

```go
func (r *RoleMain) OnRoleCreated(ctx context.Context) error {
    for _, mod := range r.Modules() {
        rmod := mod.(IRoleModule)
        rmod.OnCreate(ctx)
    }
    r.Public.UpdateRolePublic(ctx)
    // 发放初始物品
    initItems := gameconfig.GetTables().TbGlobalConfig.Get().InitItems
    if len(initItems) > 0 {
        if err := r.Bag.AddItemStack(ctx, initItems); err != nil {
            return err
        }
    }
    if err := r.save(ctx); err != nil {
        return err
    }
    return nil
}
```

## 不改动的文件

- `bag/item.go` — BagGood、BagChange、Item 结构不变
- `role_module.go` — 接口不变
- proto 文件 — 现有 ReqBagInfo/RspBagInfo/NotifyBagUpdate 足够
- 数据库 — 无新增表，无 schema 变更

## Proto 接口（现有，不变）

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqBagInfo | 21001 | C→S | 请求背包所有物品 |
| RspBagInfo | 21001 | S→C | 返回所有物品（含货币） |
| NotifyBagUpdate | - | S→C | 物品变更推送 |
