# 背包系统 MVP 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有背包 CRUD 基础上，补齐配置表加载、堆叠上限校验、初始物品发放。

**Architecture:** gameconfig 作为子模块挂到 roleApp 下，启动时加载 JSON 配置表。背包 AddSingleItem 增加配置表校验（物品存在性 + max_stack）。建号时从 GlobalConfig.InitItems 发放初始物品。

**Tech Stack:** Go 1.25.1, GORM, protoactor-go, gameconfig (auto-generated from Excel)

---

### Task 1: 修复 gameconfig 模块生命周期

**Files:**
- Modify: `gameconfig/gameconfig.go`

当前 `OnModInit` 是独立函数（第 18-22 行），不会被 `ModuleBase.AddModule` 调用。需要改为 `gameConfig` 的方法，同时删除多余的 `OnInit` 方法。

- [ ] **Step 1: 将 OnModInit 改为方法，删除独立的 OnModInit 函数和 OnInit 方法**

将 `gameconfig/gameconfig.go` 全文替换为：

```go
package gameconfig

import (
	"context"
	"encoding/json"
	"gserver/core/gxymodule"
	gamecfg "gserver/gameconfig/gosrc"
	"os"
)

type gameConfig struct {
	gxymodule.ModuleBase
	*gamecfg.Tables
}

var gameCfg *gameConfig

func GameConfig() *gameConfig {
	return gameCfg
}

func NewGameConfig() *gameConfig {
	gameCfg = &gameConfig{}
	return gameCfg
}

func (c *gameConfig) OnModInit(ctx context.Context) error {
	return c.initTables()
}

func (gc *gameConfig) initTables() error {
	tables, err := gamecfg.NewTables(loader)
	if err != nil {
		return err
	}
	gc.Tables = tables
	return nil
}

func loader(file string) ([]map[string]interface{}, error) {
	if bytes, err := os.ReadFile("gameconfig/json/" + file + ".json"); err != nil {
		return nil, err
	} else {
		jsonData := make([]map[string]interface{}, 0)
		if err = json.Unmarshal(bytes, &jsonData); err != nil {
			return nil, err
		}
		return jsonData, nil
	}
}
```

- [ ] **Step 2: 验证编译通过**

Run: `go build ./...`
Expected: 无输出（编译成功）

- [ ] **Step 3: 提交**

```bash
git add gameconfig/gameconfig.go
git commit -m "fix(gameconfig): 将 OnModInit 改为方法以适配模块生命周期"
```

---

### Task 2: 将 gameConfig 注册为 roleApp 子模块

**Files:**
- Modify: `src/apps/role/role_app.go:1-10` (imports)
- Modify: `src/apps/role/role_app.go:28-33` (OnModInit)

- [ ] **Step 1: 在 role_app.go 中添加 import 并修改 OnModInit**

将 `src/apps/role/role_app.go` 全文替换为：

```go
package role

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic"
)

type roleApp struct {
	gxyapp.App
}

func NewRoleApp() *roleApp {
	return &roleApp{}
}

func (r *roleApp) ServiceName() string {
	return ROLE_SERVICE
}

func (r *roleApp) Weight() int {
	return gxyactor.GetActorCount(r.ServiceName())
}

func (r *roleApp) OnModInit(ctx context.Context) error {
	r.AddModule(ctx, gameconfig.NewGameConfig())
	logic.InitRoleSchema(ctx)
	gxyservice.ServiceApp().LoadService(ctx, NewRoleActorService())
	return nil
}

func GetRolePublic(ctx context.Context, roleID int64) *pb.PRolePublic {
	return logic.GetRolePublic(ctx, roleID)
}

func GetRoleIDByAccount(account string) (int64, error) {
	return logic.GetRoleIDByAccount(account)
}
```

- [ ] **Step 2: 验证编译通过**

Run: `go build ./...`
Expected: 无输出（编译成功）

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/role_app.go
git commit -m "feat(role): 将 gameConfig 注册为 roleApp 子模块，启动时加载配置表"
```

---

### Task 3: AddSingleItem 增加配置表校验

**Files:**
- Modify: `src/apps/role/internal/logic/role_bag.go:3-17` (imports)
- Modify: `src/apps/role/internal/logic/role_bag.go:19-21` (errors)
- Modify: `src/apps/role/internal/logic/role_bag.go:80-91` (AddSingleItem)

- [ ] **Step 1: 添加 import 和错误变量，修改 AddSingleItem**

在 `role_bag.go` 中：

1. 在 imports 中添加 `"gserver/gameconfig"`

2. 在 `var` 块中添加错误变量：

```go
var (
	ErrItemDecItemNotEnough    = errors.New("dec item not enough")
	ErrItemConfigNotFound      = errors.New("item config not found")
	ErrItemExceedMaxStack      = errors.New("exceed max stack")
)
```

3. 替换 `AddSingleItem` 方法（第 80-91 行）为：

```go
func (r *RoleBag) AddSingleItem(ctx context.Context, item bag.Item) (*bag.BagChange, error) {
	cfg := gameconfig.GameConfig().TbItem.Get(int32(item.ID))
	if cfg == nil {
		return nil, ErrItemConfigNotFound
	}
	have := r.Goods[item.ID]
	if have == nil {
		have = &bag.BagGood{
			PropID: item.ID,
		}
		r.Goods[item.ID] = have
	}
	newNum := have.Num + item.Num
	if cfg.MaxStack > 0 && newNum > uint64(cfg.MaxStack) {
		return nil, ErrItemExceedMaxStack
	}
	chg := have.Update(newNum)
	r.MarkDirty()
	return chg, nil
}
```

- [ ] **Step 2: 验证编译通过**

Run: `go build ./...`
Expected: 无输出（编译成功）

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_bag.go
git commit -m "feat(bag): AddSingleItem 增加配置表校验（物品存在性 + max_stack 上限）"
```

---

### Task 4: 建号时发放初始物品

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go:3-24` (imports)
- Modify: `src/apps/role/internal/logic/role_main.go:368-379` (OnRoleCreated)

- [ ] **Step 1: 添加 import 并修改 OnRoleCreated**

1. 在 `role_main.go` 的 imports 中添加 `"gserver/gameconfig"`

2. 替换 `OnRoleCreated` 方法（第 368-379 行）为：

```go
func (r *RoleMain) OnRoleCreated(ctx context.Context) error {
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		rmod.OnCreate(ctx)
	}
	r.Public.UpdateRolePublic(ctx)
	// 发放初始物品
	initItems := gameconfig.GameConfig().TbGlobalConfig.Get().InitItems
	if len(initItems) > 0 {
		if err := r.Bag.AddItemStack(ctx, initItems); err != nil {
			return err
		}
	}
	// 建号强制保存一次
	if err := r.save(ctx); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 2: 验证编译通过**

Run: `go build ./...`
Expected: 无输出（编译成功）

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_main.go
git commit -m "feat(role): 建号时发放初始物品（GlobalConfig.InitItems）"
```

---

## 自查

**规格覆盖率：**
- 配置表加载 → Task 1 + Task 2
- max_stack 校验 → Task 3
- 初始物品发放 → Task 4
- ReqBagInfo 现有不变 → 无需改动
- 所有“不做”项 → 确认不涉及

**占位符扫描：** 无 TBD/TODO/待实现

**类型一致性：**
- `gameconfig.GameConfig()` 返回 `*gameConfig`，内嵌 `*gamecfg.Tables`
- `GameConfig().TbItem.Get(int32)` 返回 `*GardenItem`，字段 `MaxStack int32`
- `GameConfig().TbGlobalConfig.Get()` 返回 `*GardenGlobalConfig`，字段 `InitItems []*GardenItemStack`
- `AddItemStack` 接收 `[]*gamecfg.GardenItemStack`，类型匹配
