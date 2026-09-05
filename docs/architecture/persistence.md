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

Role Schema 当前通过 `AutoMigrate` 管理以下模型：

| 表名 | 模型 | 说明 |
|------|------|------|
| `role_basic` / `role_public` / `role_extra` | 对应 Role 状态 | 基础、公开与扩展状态 |
| `role_bag` | `RoleBagState` | 背包（goods 为 JSONB） |
| `role_flower` / `role_plot` / `role_steal` | 对应 Role 状态 | 鲜花、种植与偷花 |
| `role_main_task` / `role_resident_order` | 对应 Role 状态 | 主线任务与居民订单 |
| `role_chat` | `RoleChatState` | 聊天状态 |
| `role_mail_state` | `RoleMailState` | 玩家邮件游标与红点状态 |
| `steal_record` / `personal_mail` / `sys_mail` | 独立记录模型 | 偷花记录、个人邮件与系统邮件 |

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

Role Actor 的 mailbox 串行处理正常保存。每次 `save()` 事务开始时，先在 PostgreSQL 中锁定完全匹配的 `role_actor_fence(role_id, node_id, epoch)`；锁定失败则拒绝整次保存。fence 校验通过后，各模块按 `role_id` 使用 GORM `Save` 写入，全部成功后再清除 dirty 状态。

`role_actor_fence` 防止旧 owner 在 ownership 转移后写入业务表；同一 `role_id` 的重复 activation 必须由 Activator fail closed。已部署数据库中可能保留未使用的历史 `version` 列，`AutoMigrate` 不会自动删除它。

## 源码位置

| 文件 | 说明 |
|------|------|
| `core/gxypgx/pgx.go` | PGX App、DB() 入口 |
| `src/apps/role/internal/logic/role_schema.go` | AutoMigrate 初始化 |
| `src/apps/role/internal/logic/role_main.go` | save() 实现 |
| `src/apps/role/internal/logic/role_module.go` | IPersistState 接口 |
