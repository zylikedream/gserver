# Actor 数据分叉防护设计

## 问题

节点抖动（10min 断网后恢复）可能导致 actor 数据分叉：

1. 节点 B 短暂断网，actor 迁移到节点 A
2. A 从 DB 加载旧数据，玩家继续游戏
3. B 恢复后，B 上的旧 actor 还在运行，TickSave 触发时写入旧数据
4. B 的写入可能覆盖 A 新写入的数据

## 防护策略

双层防护：**Redis 归属检查**（主防御）+ **版本号乐观锁**（保底）。

### 第一层：Redis 归属检查

写入前检查该 actor 当前是否归本节点所有。

```
save() 中，每模块写入前:
  → Redis GET gserver:locate:node:actor:role:{roleID}
    → 值 == 本节点 nodeInstanceName → 继续写入
    → 值 != 本节点 nodeInstanceName → skip (warn 日志)
    → key 不存在 → skip (warn 日志)
```

时机：`save()` 中每个模块写入 DB 之前。
成本：每 10min 一次 Redis GET，可以忽略。

### 第二层：版本号乐观锁

版本号不匹配时 DB 拒绝写入，防止极巧合的并发写。

```
UPDATE table SET ..., version = version + 1
WHERE role_id = ? AND version = ?
```

影响行数 = 0 → `ErrVersionConflict` → 打 error 日志，不清 dirty
下次 TickSave 重试（不清 dirty 自然重试，只有下次命中时才可能写入）

## 改动文件

### DB 迁移

每个角色表加一列：

```sql
ALTER TABLE role_basic ADD COLUMN version bigint NOT NULL DEFAULT 0;
ALTER TABLE role_extra ADD COLUMN version bigint NOT NULL DEFAULT 0;
ALTER TABLE role_flower ADD COLUMN version bigint NOT NULL DEFAULT 0;
ALTER TABLE role_plot ADD COLUMN version bigint NOT NULL DEFAULT 0;
```

### core/gxyactor/system.go

`actorApp` 新增公开方法 `NodeInstanceName() string`，供 `save()` 中 Redis 归属检查使用。

### src/apps/role/internal/logic/role_module.go

`RolePersistState` 新增：

```go
type RolePersistState struct {
    RoleID   int64     `gorm:"column:role_id;primaryKey"`
    UpdateAt time.Time `gorm:"column:update_at;autoUpdateTime"`
    Version  int64     `gorm:"column:version"`
    dirty    bool
}
```

`IPersistState` 接口新增 `GetVersion() int64` / `SetVersion(v int64)`：

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

`RolePersistState` 实现：

```go
func (r *RolePersistState) GetVersion() int64  { return r.Version }
func (r *RolePersistState) SetVersion(v int64) { r.Version = v }
```

### src/apps/role/internal/logic/role_main.go

`save()` 方法改为两步：

```go
func (r *RoleMain) save(ctx context.Context) error {
    var errStr string
    for _, mod := range r.Modules() {
        rmod, _ := mod.(IRoleModule)
        if rmod == nil { continue }
        modState := rmod.PersistState()
        if modState == nil || !modState.IsDirty() { continue }

        // 第一层：Redis 归属检查
        key := getRoleLocateKey(r.RoleID)
        owner, err := gxyredis.Redis().Get(ctx, key).Result()
        if err == redis.Nil || owner == "" {
            glog.Warningf(ctx, "actor %d not claimed, skip save", r.RoleID)
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
            glog.Errorf(ctx, "version conflict on %s for role %d, skip", tableName, r.RoleID)
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

### 辅助函数

`getRoleLocateKey` 复用 activator_manager.go 中的 key 格式：

```go
func getRoleLocateKey(roleID int64) string {
    return fmt.Sprintf("gserver:locate:node:actor:role:%d", roleID)
}
```

## 场景验证

| 场景 | 防护 | 结果 |
|------|------|------|
| B 抖动恢复后落地，actor 已迁移到 A | Redis 归属检查 | B 查到 owner=A，skip |
| 正常写入，无冲突 | Redis 检查通过 + 版本号匹配 | 正常写入，version+1 |
| 极端并发写（正常不该发生） | 版本号不匹配 | 0 rows，打 error，不清 dirty |
| Redis 不可用 | Redis 返回 err | 按 err 处理，skip 写入 |

## 不处理的情况

- **B 的旧 actor 仍在处理消息**：不在本问题范围内。旧 actor 因 getActor 路由不到 B 而自然失效
- **B 上旧 actor 的内存状态与 DB 不一致**：skip 写入后旧状态不会污染 DB，下次被重新加载时从 DB 读新数据
