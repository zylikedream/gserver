# MongoDB → PostgreSQL Migration

## What This Is

将 gserver 游戏服务器的持久化层从 MongoDB 迁移到 PostgreSQL，使用 pgx v5 原生驱动 + JSONB 字段存储 map 类型数据，保持现有业务逻辑和 API 封装层不变。

## Core Value

数据层迁移完成后，项目获得 PostgreSQL 的强类型查询、事务支持和更好的运维生态，同时保持现有的脏检查 + 乐观锁保存策略不变。

## Requirements

### Validated

(None yet — ship to validate)

### Active

- [ ] 实现 `core/gxypgx` 模块，替代 `core/gxymongo`，API 兼容（FindOne/ReplaceOne/InsertOne/EnsureIndexes）
- [ ] 所有持久化状态结构体的标签从 `bson` 迁移到 `db`/`json`
- [ ] 使用 JSONB 存储 map 类型字段（BagItem、Currency、ActivityData）
- [ ] 创建 PostgreSQL schema DDL（7 张表 + 索引）
- [ ] 修改 `gxynode` 注册 `gxypgx` 替代 `gxymongo`
- [ ] 修改所有引用 `gxymongo` 的业务代码
- [ ] 确保乐观锁（version 字段）和脏检查逻辑正常工作
- [ ] 确保配置文件支持 PostgreSQL 连接参数

### Out of Scope

- 角色模块系统简化重构（独立任务，已在 todo 中）
- GORM/ORM 引入
- 数据迁移工具（从 MongoDB 导入现有数据到 PostgreSQL）
- 查询模式优化（迁移完成后可利用 PostgreSQL 的复杂查询能力）

## Context

- 项目是基于 Actor 模型的分布式游戏服务器，使用 protoactor-go
- 当前持久化通过 `core/gxymongo` 封装层访问 MongoDB，业务代码不直接操作 MongoDB
- 每个角色有 6-7 个数据模块，每个模块对应一个 MongoDB collection
- 保存策略：5s 定时 Tick + 脏检查（对象 hash）+ 乐观锁（version 字段）+ upsert
- 已有完整代码库映射文档：`.planning/codebase/`

## Constraints

- **API 兼容**: gxypgx 的 API 必须与 gxymongo 兼容，业务代码改动最小化
- **性能**: save() 是热路径（5s 间隔 × 所有在线角色），必须高效
- **JSONB**: map 类型字段用 JSONB 存储，不需要拆独立表
- **pgx v5**: 使用原生驱动，不引入 ORM

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| pgx v5 而非 GORM | 游戏服务器性能敏感，GORM 反射开销不可接受；查询模式简单不需要 ORM | — Pending |
| JSONB 而非拆表 | 最平滑迁移路径，改动量最小；背包/活动数据不需要跨表查询 | — Pending |
| 封装层保持 API 不变 | 业务代码零改动，只改底层实现 | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-04-13 after initialization*
