# Phase 1: PostgreSQL 基础设施 - Context

**Gathered:** 2026-04-13
**Status:** Ready for planning

## Phase Boundary

建立 PostgreSQL 连接和数据库 schema 基础设施，为角色模块数据迁移做准备。本阶段不涉及业务逻辑变更，专注于技术基础设施层。

**包含：**
- 创建 `core/gxypgx` 包（PostgreSQL 连接和操作封装）
- 配置 PostgreSQL 连接池
- 创建 6 个角色模块表的 schema
- 适配 `IPersistState` 接口到 PostgreSQL

**不包含：**
- 数据迁移逻辑（Phase 2）
- MongoDB 代码清理（Phase 3）
- 业务逻辑变更（本阶段）

## Implementation Decisions

### 连接池和配置
- **D-01:** 使用 **Connection URL** 格式配置连接（如 `postgres://user:pass@localhost:5432/dbname?pool_max_conns=20`）
  - *理由：* pgx v5 原生支持，所有配置集中在一处，符合现代最佳实践

- **D-02:** 连接池采用 **固定池模式**（min=5, max=20）
  - *理由：* 保持最小连接数，减少启动时延迟。适合游戏服务器的有规律访问模式。

- **D-03:** TOML 配置节命名为 **`[postgres]`**
  - *理由：* 与现有 `[mongo]` 节保持一致，迁移后直接替换，配置模式统一

### Schema 管理方式
- **D-04:** 使用 **Go 代码内建**表创建（而非 SQL 迁移脚本）
  - *理由：* 启动时自动执行，无需额外工具，与现有代码风格一致（都是 Go）

- **D-05:** 表结构采用 **分列存储**（关系型设计）
  - *理由：* 类型安全，查询高效，符合 PostgreSQL 特性。每张表包含 `role_id` 主键和特定字段。

### IPersistState 接口适配
- **D-06:** `GetIndexes()` 方法返回 **`[]string`**（索引字段名列表）
  - *理由：* 简单，符合 PostgreSQL 索引通常自动生成的惯例

- **D-07:** 结构体字段使用 **`db` 标签**（如 `db:"role_name"`）
  - *理由：* 与标准 `database/sql` 包一致，Go 社区通用，无需学习新标签

### 事务和错误处理
- **D-08:** Phase 1 **暂不支持事务**
  - *理由：* 角色模块是单表操作，暂时不需要事务。Phase 2 根据需要再添加。

- **D-09:** 连接失败采用 **快速失败策略**（panic/fatal）
  - *理由：* 让问题暴露，不进入半死状态。适合生产环境。

- **D-10:** 查询错误 **直接返回 pgx 错误**
  - *理由：* Go 惯用方式，灵活，让上层决定如何重试或降级。

## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### 项目文档
- `CLAUDE.md` — 项目架构说明（Actor 模型、Module 系统、Grain 模式）
- `.planning/REQUIREMENTS.md` — PostgreSQL 迁移需求列表（PG-01 ~ PG-03, SCHEMA-01 ~ SCHEMA-03, INTF-01 ~ INTF-03）
- `.planning/ROADMAP.md` — Phase 1 目标和成功标准

### 现有代码模式
- `core/gxymongo/mongo.go` — MongoDB 连接层实现（参考 gxypgx 的结构）
- `apps/role/internal/logic/role_module.go` — `IPersistState` 接口定义和 `RolePersistState` 结构
- `apps/role/internal/logic/role_basic.go` — 角色模块状态示例（`RoleBasicState`）
- `config/game.toml` — 现有配置结构（MongoDB 配置模式）

### 外部文档
- pgx v5 官方文档: https://github.com/jackc/pgx/wiki/Getting-started-with-pgx
- PostgreSQL Connection Strings: https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING

## Existing Code Insights

### Reusable Assets
- `core/gxymodule/` — Module 生命周期管理系统（`IModule` 接口、`ModuleBase`）
- `util/msg_handler.go` — 反射消息路由机制（处理 protobuf 消息）
- `lib/grain.go` — Grain 虚拟 actor 模式实现

### Established Patterns
- **配置模式：** TOML 格式，分节组织（`[mongo]`, `[redis]`, `[log]` 等）
- **包装层模式：** `core/gxy*` 包作为第三方库的薄包装（`gxymongo`, `gxyredis`）
- **状态嵌入：** 角色模块使用嵌入组合（`RoleBasic` 嵌入 `RoleModule` + `RoleBasicState`）
- **接口隔离：** `IPersistState` 接口抽象持久化行为

### Integration Points
- **App 系统集成：** `role` app 在启动时需要初始化 PostgreSQL 连接
- **Module 注册：** 6 个角色模块（Basic, Bag, Sign, Activity, Account, Extra）注册到角色 grain
- **配置加载：** Node 启动时读取 `[postgres]` 配置节

## Specific Ideas

### 配置示例
```toml
[postgres]
    url = "postgres://gserver:password@localhost:5432/gserver?pool_max_conns=20&pool_min_conns=5"
    # 可选：ssl_mode, connect_timeout 等
```

### 表结构示例（role_basic）
```sql
CREATE TABLE role_basic (
    role_id BIGINT PRIMARY KEY,
    role_name VARCHAR(64) NOT NULL,
    head VARCHAR(128),
    login_tm TIMESTAMPTZ,
    logout_tm TIMESTAMPTZ,
    create_tm TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    vip_lv INT NOT NULL DEFAULT 0,
    update_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_role_basic_update_at ON role_basic(update_at);
```

### gxypgx 包结构
```
core/gxypgx/
  ├── pgx.go          # 连接池管理
  ├── queries.go      # CRUD 操作（InsertOne, FindOne, Update）
  └── schema.go       # 表创建和索引
```

### 结构体标签示例
```go
type RoleBasicState struct {
    RoleID   int64     `db:"role_id"`
    RoleName string    `db:"role_name"`
    Head     string    `db:"head"`
    // ...
}

func (r *RoleBasicState) GetIndexes() []string {
    return []string{"update_at"}
}
```

## Deferred Ideas

None — discussion stayed within phase scope.

---

*Phase: 01-postgresql*
*Context gathered: 2026-04-13*
