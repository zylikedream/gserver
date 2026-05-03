# 持久化层

GServer 使用 **PostgreSQL** 作为主存储，通过 **GORM** 进行 ORM 映射。

## 架构

```
┌────────────────────────────────────────────┐
│           gxypgx.PGXApp                    │
│   全局单例 DB()，AutoMigrate 入口            │
├────────────────────────────────────────────┤
│  ┌──────────────────────────────────────┐  │
│  │     GORM v1.31.1 + PG Driver v1.6.0  │  │
│  └──────────────────────────────────────┘  │
├────────────────────────────────────────────┤
│  Role 层                                    │
│  ┌──────────┐ ┌──────────┐ ┌────────────┐ │
│  │ role_basic│ │ role_bag │ │ role_flower │ │
│  │ (单行)    │ │(JSONB)   │ │ ...         │ │
│  └──────────┘ └──────────┘ └────────────┘ │
└────────────────────────────────────────────┘
```

## GORM 配置

- **版本**: GORM v1.31.1、PostgreSQL Driver v1.6.0
- **表映射**: 使用 `gorm:"column:name"` 标签
- **迁移**: 应用启动时通过 `AutoMigrate` 自动建表

## Role 持久化模型

每个 Role 子模块对应一张数据库表和一个状态结构体。

### 表清单

| 表名 | 模型 | 说明 |
|------|------|------|
| `role_account` | `RoleAccount` | 账号绑定 |
| `role_basic` | `RoleBasicState` | 基础信息 |
| `role_bag` | `RoleBagState` | 背包（goods 为 JSONB） |
| `role_extra` | `RoleExtraPersistState` | 扩展数据 |
| `role_public` | `RolePublicState` | 公开信息 |
| `role_flower` | `RoleFlowerState` | 鲜花系统 |
| `role_plot` | `RolePlotState` | 种植系统 |

### 持久化状态接口

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

### 脏跟踪机制

1. 子模块数据变更 → 调用 `MarkDirty()`
2. `RoleMain.TickSave` 每隔 600 秒检查所有模块
3. 脏数据通过 `save()` 写入数据库
4. 写入成功后调用 `ClearDirty()`

### JSONB 字段

`role_bag.goods` 字段使用 Go 的 `driver.Valuer`/`Scanner` 接口：

```go
type GoodsMap map[int]bag.BagGood

func (m GoodsMap) Value() (driver.Value, error)  // → JSON 序列化
func (m *GoodsMap) Scan(value interface{}) error  // ← JSON 反序列化
```

## save() 并发安全

`RoleMain.save()` 实现两层保护：

### 第一层：Redis 所有权检查

写入前校验 Redis 中该 Actor 的 locate key 是否指向本节点：

```go
owner, _ := gxyredis.Redis().Get(ctx, key).Result()
if owner != nodeInstanceName {
    // Actor 已迁移到其他节点，拒绝写入
    return
}
```

防止 Actor 因负载均衡迁移后，旧节点仍在回写数据。

### 第二层：版本乐观锁

```go
result := db.Model(state).Where("role_id = ? AND version = ?", roleID, oldVersion).
    Save(state).Limit(1)
if result.RowsAffected == 0 {
    // 版本冲突或行不存在
    // 尝试创建新行（首次写入场景）
}
```

- 每次成功写入后 `version++`
- 版本冲突表示有其他写入者，拒绝本次写入
- 首次写入（`RowsAffected == 0`）时自动创建新行

## 源码位置

| 文件 | 说明 |
|------|------|
| `core/gxypgx/pgx.go` | PGX App、DB() 入口 |
| `src/apps/role/internal/logic/role_schema.go` | AutoMigrate 初始化 |
| `src/apps/role/internal/logic/role_main.go` | save() 实现 |
| `src/apps/role/internal/logic/role_module.go` | IPersistState 接口 |
