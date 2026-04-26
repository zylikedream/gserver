# GORM Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hand-written gxypgx CRUD/DDL layer with GORM ORM, keeping the same module lifecycle and package structure.

**Architecture:** gxypgx package switches from pgxpool to GORM `*gorm.DB` internally. All state structs migrate from `db:` tags to `gorm:` tags. AutoMigrate replaces hand-written CREATE TABLE SQL. GORM `First()`/`Save()` replace custom `FindOne()`/`UpsertOne()`.

**Tech Stack:** Go 1.25.1, gorm.io/gorm, gorm.io/driver/postgres (pgx/v5), gorm.io/datatypes

---

### Task 1: Add GORM Dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Install GORM packages**

```bash
cd /home/zyr/workspace/gserver && go get gorm.io/gorm gorm.io/driver/postgres gorm.io/datatypes
```

- [ ] **Step 2: Verify go.mod updated**

Run: `grep -E "gorm.io" go.mod`
Expected: Three new lines with gorm.io/gorm, gorm.io/driver/postgres, gorm.io/datatypes

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add GORM dependencies (gorm, postgres driver, datatypes)"
```

---

### Task 2: Rewrite gxypgx/pgx.go — GORM Initialization

**Files:**
- Modify: `core/gxypgx/pgx.go`

- [ ] **Step 1: Rewrite pgx.go to use GORM instead of pgxpool**

Replace the entire file content with:

```go
package gxypgx

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type PGXApp struct {
	gxyapp.App
	db   *gorm.DB
	conf *pgxConfig
}

type pgxConfig struct {
	URL string `toml:"url"`
}

var pgxAppInstance *PGXApp

func PGX() *PGXApp {
	if pgxAppInstance.db == nil {
		glog.Error(context.Background(), "pgx not init, miss config")
	}
	return pgxAppInstance
}

func DB() *gorm.DB {
	return pgxAppInstance.db
}

func NewPGXApp() *PGXApp {
	pgxAppInstance = &PGXApp{}
	return pgxAppInstance
}

func (p *PGXApp) OnModInit(ctx context.Context) error {
	conf := &pgxConfig{}
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "postgres", conf); err != nil {
		return err
	}
	if conf.URL == "" {
		return nil
	}
	glog.Debugf(ctx, "conf = %v", conf)
	p.conf = conf

	db, err := gorm.Open(postgres.Open(conf.URL), &gorm.Config{})
	if err != nil {
		return err
	}
	p.db = db
	return nil
}

func (p *PGXApp) OnModStart(ctx context.Context) error {
	if p.db == nil {
		return nil
	}
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Infof(ctx, "[module]postgres(gorm) start success: %s", p.conf.URL)
	return nil
}

func (p *PGXApp) OnModStop(ctx context.Context) error {
	if p.db != nil {
		sqlDB, err := p.db.DB()
		if err == nil {
			sqlDB.Close()
		}
	}
	glog.Info(ctx, "[module]postgres stop success")
	return nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /home/zyr/workspace/gserver && go build ./core/gxypgx/...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add core/gxypgx/pgx.go
git commit -m "refactor(gxypgx): switch from pgxpool to GORM initialization"
```

---

### Task 3: Delete Old gxypgx Files

**Files:**
- Delete: `core/gxypgx/queries.go`
- Delete: `core/gxypgx/schema.go`

- [ ] **Step 1: Delete the files**

```bash
rm core/gxypgx/queries.go core/gxypgx/schema.go
```

- [ ] **Step 2: Commit**

```bash
git add -u core/gxypgx/
git commit -m "refactor(gxypgx): remove hand-written CRUD and DDL files"
```

---

### Task 4: Migrate State Structs — Tags and TableName

**Files:**
- Modify: `src/apps/role/internal/logic/role_module.go`
- Modify: `src/apps/role/internal/logic/role_basic.go`
- Modify: `src/apps/role/internal/logic/role_bag.go`
- Modify: `src/apps/role/internal/logic/role_sign.go`
- Modify: `src/apps/role/internal/logic/role_activity.go`
- Modify: `src/apps/role/internal/logic/role_extra.go`
- Modify: `src/apps/role/internal/logic/role_public.go`

- [ ] **Step 1: Migrate role_module.go — RolePersistState and RoleModule tags**

In `src/apps/role/internal/logic/role_module.go`, change the `RolePersistState` struct and remove `getColName`:

```go
type RolePersistState struct {
	RoleID   int64     `gorm:"column:role_id;primaryKey"`
	UpdateAt time.Time `gorm:"column:update_at;autoUpdateTime"`
}
```

Change `RoleModule` struct tags — remove `db:"-"` tags (GORM ignores unexported/non-tagged fields by default, but these are on the module struct not the state struct, so no tag needed):

```go
type RoleModule struct {
	gxymodule.ModuleBase
	RoleID int64
	Role   *RoleMain
}
```

Remove the `getColName` function entirely (lines 47-49).

Remove `import "github.com/gogf/gf/v2/text/gstr"` since `getColName` was the only user.

- [ ] **Step 2: Migrate role_basic.go — RoleBasicState**

```go
type RoleBasicState struct {
	RolePersistState
	RoleName   string    `gorm:"column:role_name"`
	Head       string    `gorm:"column:head"`
	LoginTm    time.Time `gorm:"column:login_tm"`
	LogoutTm   time.Time `gorm:"column:logout_tm"`
	CreateTm   time.Time `gorm:"column:create_tm"`
	VipLv      int       `gorm:"column:vip_lv"`
}

func (RoleBasicState) TableName() string { return "role_basic" }
```

- [ ] **Step 3: Migrate role_bag.go — RoleBagState with JSONB**

Add import `"gorm.io/datatypes"` and change:

```go
type RoleBagState struct {
	RolePersistState
	Goods datatypes.JSON `gorm:"column:goods;type:jsonb"`
}

func (RoleBagState) TableName() string { return "role_bag" }
```

Note: The old DDL used `items` and `currencies` columns, but the current struct only has `Goods`. AutoMigrate will create/update to match this struct.

- [ ] **Step 4: Migrate role_sign.go — RoleSignState with INT[]**

The `AccumDrawStage []int` field needs a PostgreSQL INT[] compatible type. Use `pq.Int64Array`:

Add import `"github.com/lib/pq"` and change:

```go
type RoleSignState struct {
	RolePersistState
	DrawTime       time.Time    `gorm:"column:draw_time"`
	SignDay        int          `gorm:"column:sign_day"`
	AccumDrawStage pq.Int64Array `gorm:"column:accum_draw_stage;type:int[]"`
	DrawDay        int          `gorm:"column:draw_day"`
	Patch          int          `gorm:"column:patch"`
}

func (RoleSignState) TableName() string { return "role_sign" }
```

Note: The column name changes from `sign_time` to `draw_time` to match the DDL column name `draw_time`. Actually, checking the DDL: the DDL has `draw_time TIMESTAMPTZ`, so use `draw_time`. The DDL also has `accum_draw_stage INT[]`, so pq.Int64Array maps correctly.

- [ ] **Step 5: Migrate role_activity.go — RoleActivityPersistState with JSONB**

Add import `"gorm.io/datatypes"` and change:

```go
type RoleActivityPersistState struct {
	RolePersistState
	Activitys datatypes.JSON `gorm:"column:activitys;type:jsonb"`
}

func (RoleActivityPersistState) TableName() string { return "role_activity" }
```

Change `RoleActivity` struct to remove `db:"inline"` tag:

```go
type RoleActivity struct {
	RoleModule
	RoleActivityPersistState
}
```

- [ ] **Step 6: Migrate role_extra.go — RoleExtraPersistState**

```go
type RoleExtraPersistState struct {
	RolePersistState
	CronTm time.Time `gorm:"column:cron_tm"`
}

func (RoleExtraPersistState) TableName() string { return "role_extra" }
```

Change `RoleExtra` struct to remove `db:"inline"` tag:

```go
type RoleExtra struct {
	RoleModule
	RoleExtraPersistState
}
```

- [ ] **Step 7: Migrate role_public.go — RolePublicState**

```go
type RolePublicState struct {
	RolePersistState
	Name       string    `gorm:"column:name"`
	Head       string    `gorm:"column:head"`
	CreateTime time.Time `gorm:"column:create_time"`
}

func (RolePublicState) TableName() string { return "role_public" }
```

- [ ] **Step 8: Verify compilation**

Run: `cd /home/zyr/workspace/gserver && go build ./src/apps/role/...`
Expected: Compilation errors about missing FindOne/UpsertOne — that's expected, fixed in Task 5 and 6.

- [ ] **Step 9: Commit**

```bash
git add src/apps/role/internal/logic/
git commit -m "refactor(role): migrate state structs from db: tags to gorm: tags with TableName()"
```

---

### Task 5: Migrate role_account.go to GORM

**Files:**
- Modify: `src/apps/role/internal/logic/role_account.go`

- [ ] **Step 1: Rewrite role_account.go**

```go
package logic

import (
	"context"
	"errors"

	"gserver/core/gxypgx"
	"gserver/src/util/uid"

	"github.com/gogf/gf/v2/os/glog"
	"gorm.io/gorm"
)

type RoleAccount struct {
	RoleID  int64  `gorm:"column:role_id;uniqueIndex"`
	Account string `gorm:"column:account;primaryKey"`
}

func (RoleAccount) TableName() string { return "role_account" }

func GetRoleIDByAccount(account string) (int64, error) {
	roleAccount := &RoleAccount{}
	err := gxypgx.DB().Where("account = ?", account).First(roleAccount).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		roleID, err := genRoleID()
		if err != nil {
			return 0, err
		}
		roleAccount.RoleID = roleID
		roleAccount.Account = account
		if err := gxypgx.DB().Save(roleAccount).Error; err != nil {
			return 0, err
		}
	}
	return roleAccount.RoleID, nil
}

func GetAccountByRoleID(roleID int64) string {
	roleAccount := &RoleAccount{}
	err := gxypgx.DB().Where("role_id = ?", roleID).First(roleAccount).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			glog.Errorf(context.Background(), "check role exist error, roleID: %d, err: %v", roleID, err)
		}
		return ""
	}
	return roleAccount.Account
}

func genRoleID() (int64, error) {
	var offset int64 = 100000
	uid, err := uid.UidGen().GenAutoIncID("role")
	if err != nil {
		return 0, err
	}
	return uid + offset, nil
}
```

- [ ] **Step 2: Verify compilation of this file**

Run: `cd /home/zyr/workspace/gserver && go vet ./src/apps/role/internal/logic/role_account.go 2>&1 || true`

- [ ] **Step 3: Commit**

```bash
git add src/apps/role/internal/logic/role_account.go
git commit -m "refactor(role): migrate role_account.go to GORM First/Save"
```

---

### Task 6: Migrate role_main.go — load/save to GORM

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go`

- [ ] **Step 1: Update imports**

Remove `"database/sql"` from imports (no longer using `sql.ErrNoRows`). Add `"gorm.io/gorm"`.

- [ ] **Step 2: Rewrite loadModuleState function**

Replace the `loadModuleState` function (lines 191-199) with:

```go
func loadModuleState(ctx context.Context, roleID int64, modState IPersistState) error {
	tableName := modState.(interface{ TableName() string }).TableName()
	err := gxypgx.DB().Table(tableName).Where("role_id = ?", roleID).First(modState).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		modState.SetRoleID(roleID)
		return nil
	}
	return err
}
```

- [ ] **Step 3: Rewrite save function**

Replace the `save` function (lines 293-318). Remove the `getColName` usage and use GORM Save:

```go
func (r *RoleMain) save(ctx context.Context) error {
	var errStr string
	for _, mod := range r.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil {
			continue
		}
		modState := rmod.PersistState()
		if modState == nil {
			continue
		}
		modState.SetUpdateAt(time.Now())

		if err := gxypgx.DB().Save(modState).Error; err != nil {
			tableName := modState.(interface{ TableName() string }).TableName()
			errStr += fmt.Sprintf("save mod %s failed: %s", tableName, err)
			continue
		}
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}
```

- [ ] **Step 4: Remove unused import**

Remove `"database/sql"` from the import block since `sql.ErrNoRows` is no longer used.

- [ ] **Step 5: Verify full compilation**

Run: `cd /home/zyr/workspace/gserver && go build ./src/apps/role/...`
Expected: Success — no errors

- [ ] **Step 6: Commit**

```bash
git add src/apps/role/internal/logic/role_main.go
git commit -m "refactor(role): migrate load/save to GORM First/Save"
```

---

### Task 7: Rewrite role_schema.go — AutoMigrate

**Files:**
- Modify: `src/apps/role/internal/logic/role_schema.go`

- [ ] **Step 1: Rewrite role_schema.go**

Replace the entire file with:

```go
package logic

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

func InitRoleSchema(ctx context.Context) {
	db := gxypgx.DB()

	if err := db.AutoMigrate(
		&RoleAccount{},
		&RoleBasicState{},
		&RoleBagState{},
		&RoleSignState{},
		&RoleActivityPersistState{},
		&RoleExtraPersistState{},
	); err != nil {
		glog.Fatal(ctx, err)
	}

	glog.Info(ctx, "[schema] all role tables migrated successfully")
}
```

- [ ] **Step 2: Verify full build**

Run: `cd /home/zyr/workspace/gserver && go build ./...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add src/apps/role/internal/logic/role_schema.go
git commit -m "refactor(role): replace hand-written DDL with GORM AutoMigrate"
```

---

### Task 8: Handle JSONB Business Logic — Bag and Activity

**Files:**
- Modify: `src/apps/role/internal/logic/role_bag.go`
- Modify: `src/apps/role/internal/logic/role_activity.go`

The JSONB columns now use `datatypes.JSON` (raw bytes). Business code that accesses `r.Goods` (a `map[int]*bag.BagGood`) needs helper methods to marshal/unmarshal.

- [ ] **Step 1: Add JSONB helper to role_bag.go**

In `role_bag.go`, the `RoleBagState.Goods` field is now `datatypes.JSON`. The business code directly accesses `r.Goods` as `map[int]*bag.BagGood`. Add helper methods:

```go
func (r *RoleBagState) GetGoods() map[int]*bag.BagGood {
	if r.Goods == nil {
		return nil
	}
	goods := make(map[int]*bag.BagGood)
	json.Unmarshal(r.Goods, &goods)
	return goods
}

func (r *RoleBagState) SetGoods(goods map[int]*bag.BagGood) {
	if goods == nil {
		r.Goods = nil
		return
	}
	data, _ := json.Marshal(goods)
	r.Goods = data
}
```

Then update `RoleBag.OnModInit` to initialize via the helper:

```go
func (r *RoleBag) OnModInit(ctx context.Context) error {
	r.SetGoods(make(map[int]*bag.BagGood))
	return nil
}
```

And update all `r.Goods[...]` accesses in `RoleBag` methods to use an internal `goods` field. Actually, a cleaner approach: keep a `goodsMap` field separate from the persist state, and sync before save / after load.

**Cleaner approach — add a non-persisted goodsMap field to RoleBag:**

In `RoleBag` struct, add an internal map that is not persisted:

```go
type RoleBag struct {
	RoleModule
	RoleBagState
	goodsMap map[int]*bag.BagGood // business data, synced to/from RoleBagState.Goods (JSONB)
}
```

Add sync methods:

```go
func (r *RoleBag) OnModInit(ctx context.Context) error {
	r.goodsMap = make(map[int]*bag.BagGood)
	return nil
}

// After loading from DB, deserialize JSONB to map
func (r *RoleBag) OnModStart(ctx context.Context) error {
	if r.Goods != nil {
		json.Unmarshal(r.Goods, &r.goodsMap)
	}
	return nil
}

// Before saving to DB, serialize map to JSONB
func (r *RoleBag) beforeSave() {
	data, _ := json.Marshal(r.goodsMap)
	r.Goods = data
}
```

Replace all `r.Goods[...]` references with `r.goodsMap[...]` throughout the business methods in `role_bag.go`. Then in the save cycle, call `beforeSave()` before `db.Save()`.

- [ ] **Step 2: Similarly add JSONB helper to role_activity.go**

The `Activitys` field is `map[int32]server.ActivityData` in business code. Add:

```go
type RoleActivity struct {
	RoleModule
	RoleActivityPersistState
	activitysMap map[int32]server.ActivityData
}

func (r *RoleActivity) OnModStart(ctx context.Context) error {
	if r.Activitys != nil {
		json.Unmarshal(r.Activitys, &r.activitysMap)
	}
	if r.activitysMap == nil {
		r.activitysMap = make(map[int32]server.ActivityData)
	}
	r.updateActivity(ctx)
	return nil
}

func (r *RoleActivity) beforeSave() {
	data, _ := json.Marshal(r.activitysMap)
	r.Activitys = data
}
```

Replace all `r.Activitys[...]` references with `r.activitysMap[...]`.

- [ ] **Step 3: Update save() in role_main.go to call beforeSave**

In `role_main.go`'s `save` function, add a hook before `db.Save()`:

```go
// In the save loop, before Save:
if hook, ok := modState.(interface{ BeforeSave() }); ok {
    hook.BeforeSave()
}
```

Then have `RoleBag` and `RoleActivity` implement `BeforeSave()` on their state structs, which calls `beforeSave()`.

- [ ] **Step 4: Update loadModuleState to call after-load hook**

In `loadModuleState`, after `First()`, add:

```go
if hook, ok := modState.(interface{ AfterLoad() }); ok {
    hook.AfterLoad()
}
```

- [ ] **Step 5: Verify full build**

Run: `cd /home/zyr/workspace/gserver && go build ./...`
Expected: Success

- [ ] **Step 6: Commit**

```bash
git add src/apps/role/internal/logic/
git commit -m "refactor(role): add JSONB marshal/unmarshal helpers for bag and activity"
```

---

### Task 9: Full Build Verification and Cleanup

**Files:**
- All modified files

- [ ] **Step 1: Full build**

Run: `cd /home/zyr/workspace/gserver && go build ./...`
Expected: Success with no errors

- [ ] **Step 2: Vet check**

Run: `cd /home/zyr/workspace/gserver && go vet ./...`
Expected: No issues

- [ ] **Step 3: Verify no old gxypgx references remain**

Run: `grep -rn "pool\.Exec\|pool\.QueryRow\|pgxpool\|FindOne\|UpsertOne\|CreateTable\b\|CreateIndex\b\|GetPool" src/ core/ 2>/dev/null | grep -v "_test.go" | grep -v ".pb.go"`
Expected: No matches (all old API calls removed)

- [ ] **Step 4: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: cleanup after GORM migration"
```

---

## Self-Review

**Spec coverage:**
- gxypgx/pgx.go rewrite → Task 2 ✓
- Delete queries.go, schema.go → Task 3 ✓
- Struct tag migration (db: → gorm:) → Task 4 ✓
- JSONB handling → Task 4 (tags) + Task 8 (business logic) ✓
- TableName() methods → Task 4 ✓
- role_account.go migration → Task 5 ✓
- role_main.go load/save → Task 6 ✓
- role_schema.go AutoMigrate → Task 7 ✓
- GORM dependencies → Task 1 ✓

**Placeholder scan:** No TBD/TODO found. All code blocks contain actual implementation.

**Type consistency:** `datatypes.JSON` used consistently for JSONB fields. `pq.Int64Array` for INT[]. `gorm.ErrRecordNotFound` used for empty result checks.
