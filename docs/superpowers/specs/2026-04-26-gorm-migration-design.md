# GORM Migration Design

## Summary

Replace the hand-written `gxypgx` CRUD/DDL layer (built on pgx/v5 pool directly) with GORM ORM. GORM's PostgreSQL driver uses pgx/v5 under the hood, so there is no driver change. The goal is simpler code: AutoMigrate replaces hand-written CREATE TABLE SQL, and GORM chain API replaces the custom reflection-based FindOne/UpsertOne.

## Architecture

### gxypgx Package (core/gxypgx/)

The package name and module lifecycle remain unchanged. Internal implementation switches from pgxpool to GORM.

**pgx.go changes:**
- `pool *pgxpool.Pool` → `db *gorm.DB`
- `OnModInit` — read TOML `[postgres].url`, call `gorm.Open(postgres.Open(url), &gorm.Config{})`
- `OnModStart` — ping via `db.Raw("SELECT 1").Rows()`
- `OnModStop` — close underlying `sql.DB`
- New method `DB() *gorm.DB` — primary external access point
- `PGX()` still returns `*PGXApp` (backward compat for node.go registration)
- Remove `GetPool()`

**Deleted files:**
- `queries.go` — hand-written reflection CRUD (FindOne, UpsertOne, InsertOne, Update, Delete, helpers)
- `schema.go` — hand-written DDL (CreateTable, CreateIndex, TableExists, IndexExists)

### Struct Tag Migration

All state structs change from `db:"xxx"` to `gorm:"column:xxx"`.

Embedded `db:"inline"` becomes native Go embedding (GORM auto-flattens embedded struct fields).

JSONB columns use `datatypes.JSON` from `gorm.io/datatypes`:
- Write: `datatypes.JSON` implements `driver.Valuer` automatically
- Read: `datatypes.JSON` implements `sql.Scanner` automatically
- Business layer: `json.Unmarshal` after load, `json.Marshal` before save

`role_sign.accum_draw_stage` (INT[]) uses `pq.Int64Array`.

Each state struct implements `TableName() string` to map to the correct table name, replacing the existing `getColName()` function.

### Schema (AutoMigrate)

`role_schema.go` rewrites from 6 hand-written CREATE TABLE functions (~180 lines of SQL) to a single `AutoMigrate` call:

```go
func InitRoleSchema(ctx context.Context) {
    db := gxypgx.DB()
    db.AutoMigrate(
        &RoleAccount{},
        &RoleBasicState{},
        &RoleBagState{},
        &RoleSignState{},
        &RoleActivityPersistState{},
        &RoleExtraPersistState{},
    )
}
```

Indexes are declared via struct tags (`index`, `uniqueIndex`, `autoUpdateTime`).

`role_app.go` still calls `InitRoleSchema(ctx)` on startup. AutoMigrate is idempotent and safe for future multi-server deployments. A config toggle (`auto_migrate`) can be added later when needed.

### Query Layer

**role_account.go:**
- `FindOne(ctx, "role_account", dest, "account=$1", account)` → `db.Where("account = ?", account).First(&dest).Error`
- `UpsertOne(ctx, "role_account", state, ...)` → `db.Save(&state).Error`
- `sql.ErrNoRows` → `gorm.ErrRecordNotFound`

**role_main.go load/save:**
- `FindOne(ctx, tableName, modState, "role_id=$1", roleID)` → `db.Table(tableName).Where("role_id = ?", roleID).First(modState).Error`
- `UpsertOne(ctx, tableName, modState, ...)` → `db.Save(modState).Error`
- Delete `getColName()` function; table names come from `TableName()` methods

`Save()` is used for upsert. It does SELECT + INSERT/UPDATE (two queries). This is acceptable for now; the call site is a single `db.Save()` so swapping to `Clauses(OnConflict{...}).Create()` later requires no business-layer changes.

## File Changes

### Modified

| File | Change |
|------|--------|
| `core/gxypgx/pgx.go` | pgxpool → gorm.DB init, add DB() method |
| `src/apps/role/internal/logic/role_schema.go` | 6 CREATE TABLE functions → AutoMigrate call |
| `src/apps/role/internal/logic/role_account.go` | FindOne/UpsertOne → GORM First/Save |
| `src/apps/role/internal/logic/role_main.go` | load/save loop → GORM Table/First/Save, delete getColName |
| `src/apps/role/internal/logic/role_basic.go` | db: tags → gorm: tags, add TableName() |
| `src/apps/role/internal/logic/role_bag.go` | db: tags → gorm: tags, JSONB → datatypes.JSON, add TableName() |
| `src/apps/role/internal/logic/role_sign.go` | db: tags → gorm: tags, INT[] → pq.Int64Array, add TableName() |
| `src/apps/role/internal/logic/role_activity.go` | db: tags → gorm: tags, JSONB → datatypes.JSON, add TableName() |
| `src/apps/role/internal/logic/role_extra.go` | db: tags → gorm: tags, add TableName() |
| `src/apps/role/internal/logic/role_public.go` | db: tags → gorm: tags, add TableName() |

### Deleted

| File | Reason |
|------|--------|
| `core/gxypgx/queries.go` | Hand-written CRUD replaced by GORM |
| `core/gxypgx/schema.go` | Hand-written DDL replaced by AutoMigrate |

### New Dependencies

| Package | Purpose |
|---------|---------|
| `gorm.io/gorm` | GORM core |
| `gorm.io/driver/postgres` | PostgreSQL driver (uses pgx/v5) |
| `gorm.io/datatypes` | JSONB type support |

### Unchanged

| File | Reason |
|------|--------|
| `core/gxynode/node.go` | Registration `gxypgx.NewPGXApp()` stays the same |
| `src/apps/role/role_app.go` | Still calls `InitRoleSchema(ctx)` |
| Other apps | No database usage |

## Future Considerations

- **Multi-server AutoMigrate**: Add `auto_migrate` config toggle when deploying multiple servers
- **Save performance**: Swap `db.Save()` to `Clauses(OnConflict{...}).Create()` for single-query upsert if save latency becomes a bottleneck
