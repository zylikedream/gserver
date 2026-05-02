# Actor 数据分叉防护实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 防止节点抖动后过期 actor 的脏数据覆盖新 actor 的写入。

**Architecture:** `save()` 中写入 DB 前先查 Redis 确认 actor 归属本节点（主防御），然后以版本号乐观锁写入 DB（保底防御）。GORM AutoMigrate 自动为所有角色表加 `version` 列。

**Tech Stack:** GORM, Redis, protoactor-go

---

### Task 1: 添加 NodeInstanceName() 方法

**Files:**
- Modify: `core/gxyactor/system.go:145-148`

- [ ] **Step 1: 在 actorApp 上添加 NodeInstanceName() 方法**

在 `Host()` 方法之后添加：

```go
func (a *actorApp) NodeInstanceName() string {
	return a.nodeInstanceName
}
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```
Expected: no output (success)

- [ ] **Step 3: 提交**

```bash
git add core/gxyactor/system.go
git commit -m "feat: add NodeInstanceName() method to actorApp"
```

---

### Task 2: RolePersistState 增加 Version 字段

**Files:**
- Modify: `src/apps/role/internal/logic/role_module.go:26-30`
- Modify: `src/apps/role/internal/logic/role_module.go:16-24`
- Modify: `src/apps/role/internal/logic/const.go`（已有 ErrVersionConflict，无需改）

- [ ] **Step 1: RolePersistState 添加 Version 字段**

```go
type RolePersistState struct {
	RoleID   int64     `gorm:"column:role_id;primaryKey"`
	UpdateAt time.Time `gorm:"column:update_at;autoUpdateTime"`
	Version  int64     `gorm:"column:version"`
	dirty    bool
}
```

- [ ] **Step 2: IPersistState 接口添加 GetVersion/SetVersion**

```go
type IPersistState interface {
	SetRoleID(roleID int64)
	GetUpdateAt() time.Time
	SetUpdateAt(updateAt time.Time)
	GetIndexes() []string
	MarkDirty()
	IsDirty() bool
	ClearDirty()
	GetVersion() int64
	SetVersion(v int64)
}
```

- [ ] **Step 3: RolePersistState 实现 GetVersion/SetVersion**

```go
func (r *RolePersistState) GetVersion() int64  { return r.Version }
func (r *RolePersistState) SetVersion(v int64) { r.Version = v }
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```
Expected: no output (success)。注意所有嵌入 `RolePersistState` 的模块（RoleBasicState、RoleBagState、RoleExtraPersistState、RolePublicState、RoleFlowerState、RolePlotState）都自动获得 Version 字段。GORM AutoMigrate 会在下次启动时自动为对应的 6 张表加 `version bigint` 列。

- [ ] **Step 5: 提交**

```bash
git add src/apps/role/internal/logic/role_module.go
git commit -m "feat: add version field to RolePersistState for optimistic locking"
```

---

### Task 3: save() 加入 Redis 归属检查 + 版本号写入

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go` — `save()` 方法（约 250-280 行）
- Check: `src/apps/role/internal/logic/role_main.go` imports 是否需要补充

- [ ] **Step 1: 确认当前 save() 代码和 import**

当前 `save()` 在 role_main.go：

```go
func (r *RoleMain) save(_ context.Context) error {
	var errStr string
	for _, mod := range r.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil {
			continue
		}
		modState := rmod.PersistState()
		if modState == nil || !modState.IsDirty() {
			continue
		}
		modState.SetUpdateAt(time.Now())

		if err := gxypgx.DB().Save(modState).Error; err != nil {
			tableName := modState.(tabler).TableName()
			errStr += fmt.Sprintf("save mod %s failed: %s", tableName, err)
			continue
		}
		modState.ClearDirty()
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}
```

当前 imports：

```go
import (
	"context"
	"errors"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/core/gxytimer"
	"gserver/core/gxyutil"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"
	"reflect"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"gorm.io/gorm"
)
```

需要新增的 import：`"gserver/core/gxyredis"`、`"github.com/redis/go-redis/v9"`

- [ ] **Step 2: 重写 save()**

```go
func (r *RoleMain) save(ctx context.Context) error {
	var errStr string
	for _, mod := range r.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil {
			continue
		}
		modState := rmod.PersistState()
		if modState == nil || !modState.IsDirty() {
			continue
		}

		// 第一层：Redis 归属检查
		key := getRoleLocateKey(r.RoleID)
		owner, err := gxyredis.Redis().Get(ctx, key).Result()
		if err == redis.Nil || owner == "" {
			glog.Warningf(ctx, "actor %d not claimed in redis, skip save", r.RoleID)
			continue
		}
		if err != nil {
			glog.Errorf(ctx, "redis get failed for role %d: %v, skip save", r.RoleID, err)
			continue
		}
		if owner != gxyactor.ActorApp().NodeInstanceName() {
			glog.Warningf(ctx, "actor %d claimed by %s, skip save", r.RoleID, owner)
			continue
		}

		// 第二层：版本号乐观锁写入
		oldVersion := modState.GetVersion()
		modState.SetVersion(oldVersion + 1)
		modState.SetUpdateAt(time.Now())

		result := gxypgx.DB().Model(modState).
			Select("*").
			Where("role_id = ? AND version = ?", r.RoleID, oldVersion).
			Updates(modState)
		if result.Error != nil {
			tableName := modState.(tabler).TableName()
			errStr += fmt.Sprintf("save mod %s failed: %s", tableName, result.Error)
			continue
		}
		if result.RowsAffected == 0 {
			tableName := modState.(tabler).TableName()
			glog.Errorf(ctx, "version conflict on %s for role %d, oldVersion=%d, skip", tableName, r.RoleID, oldVersion)
			// 不清 dirty，下次重试
			continue
		}
		modState.ClearDirty()
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}
```

同时添加辅助函数（放在 save 前后即可）：

```go
func getRoleLocateKey(roleID int64) string {
	return fmt.Sprintf("gserver:locate:node:actor:role:%d", roleID)
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```
Expected: no output (success)

- [ ] **Step 4: 测试——确认旧 save 行为被替换**

手动跑 role 模块测试，确认 GORM 的 Save() 不再被使用：

```bash
go test ./src/apps/role/... -run TestRole -v -count=1 2>&1 | grep -E "PASS|FAIL|Error|save|version"
```
Expected: 测试通过，且日志中可以看到 version 相关信息

- [ ] **Step 5: 提交**

```bash
git add src/apps/role/internal/logic/role_main.go
git commit -m "feat: add redis ownership check and version lock to role save"
```

---

### Task 4: 验证完整编译和 AutoMigrate

- [ ] **Step 1: 完整编译确认**

```bash
go build ./...
```
Expected: clean build, no output

- [ ] **Step 2: 确认 AutoMigrate 覆盖所有角色表**

检查 role_schema.go 中 `AutoMigrate` 调用——所有传入的 struct 都嵌入了 `RolePersistState`，因此 `Version` 会被 GORM 自动加到对应表。

```bash
grep -A 10 "AutoMigrate" src/apps/role/internal/logic/role_schema.go
```
Expected: RoleAccount（不嵌 RolePersistState，无影响）+ 6 个角色状态 struct（都嵌了 RolePersistState → 自动加 version 列）

- [ ] **Step 3: 最终提交**

```bash
git add .
git commit -m "docs: add data fork protection design doc and plan"
```

---

## Self-Review

1. **Spec 覆盖**:
   - Redis 归属检查 → Task 3 (save 中的第一层检查)
   - 版本号乐观锁 → Task 2 + Task 3 (Version 字段 + 写入逻辑)
   - NodeInstanceName() → Task 1
   - DB 迁移 → Task 2 (GORM AutoMigrate 自动处理，无需手动 SQL)
   - 所有 spec 中的设计决策都有对应的实现任务

2. **占位符检查**: 无 TBD/TODO/半成品

3. **类型一致性**: GetVersion/SetVersion 在 Task 2 定义，在 Task 3 使用，签名一致
