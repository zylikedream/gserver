# GServer Roadmap

## Milestone v1.0: MongoDB → PostgreSQL Migration

### Phase 1: PostgreSQL 基础设施

**目标:** 建立 PostgreSQL 连接和数据库 schema

**需求:** PG-01, PG-02, PG-03, SCHEMA-01, SCHEMA-02, SCHEMA-03, INTF-01, INTF-02, INTF-03

**成功标准:**
- `core/gxypgx` 包创建完成，能连接 PostgreSQL
- 所有角色模块表创建完成，包含正确的索引
- `IPersistState` 接口适配 PostgreSQL 标签

**交付物:**
- pgx v5 连接池实现
- DDL 脚本（创建表和索引）
- 适配后的 `IPersistState` 接口

**Plans:**
- [x] 01-01-PLAN.md — 创建 PostgreSQL 连接基础设施（core/gxypgx 包、连接池、配置）
- [ ] 01-02-PLAN.md — 创建 6 个角色模块的 PostgreSQL 表结构
- [x] 01-03-PLAN.md — 适配 IPersistState 接口到 PostgreSQL

---

### Phase 2: 持久层实现

**目标:** 实现所有数据操作接口，适配角色模块状态

**需求:** DATA-01, DATA-02, DATA-03, DATA-04, ADAPT-01 ~ ADAPT-07

**成功标准:**
- 所有角色模块能从 PostgreSQL 加载数据
- 所有角色模块能保存数据到 PostgreSQL
- 乐观锁和 hash 检测代码已移除

**交付物:**
- InsertOne, FindOne, Update 实现
- 6 个角色模块的状态结构体（移除 bson/hash 标签）
- 单元测试覆盖数据操作

**Plans:**
- [ ] 02-01-PLAN.md — 实现 InsertOne 操作
- [ ] 02-02-PLAN.md — 实现 FindOne 操作
- [ ] 02-03-PLAN.md — 实现 Update 操作
- [ ] 02-04-PLAN.md — 适配 6 个角色模块的持久化逻辑

---

### Phase 3: 清理和集成

**目标:** 移除所有 MongoDB 相关代码，确保系统编译通过

**需求:** CLEAN-01 ~ CLEAN-04

**成功标准:**
- `core/gxymongo` 包已移除
- 所有 import 更新为 `gxypgx`
- 配置文件移除 MongoDB 配置
- 项目编译无错误

**交付物:**
- 清理后的代码库
- 更新后的配置文件
- 编译验证报告

**Plans:**
- [x] 03-01-PLAN.md — 移除 MongoDB 依赖和导入
- [x] 03-02-PLAN.md — 清理 MongoDB 配置
- [x] 03-03-PLAN.md — 编译验证和修复

---

### Phase 4: 测试和验证

**目标:** 端到端测试确保迁移成功

**需求:** TEST-01 ~ TEST-04

**成功标准:**
- 角色创建流程正常
- 角色登录/登出正常
- 数据持久化和加载正常
- 所有测试用例通过

**交付物:**
- 测试报告
- 验证清单
- 已迁移并运行的系统

**Plans:**
- [ ] 04-01-PLAN.md — 编写单元测试
- [ ] 04-02-PLAN.md — 编写集成测试
- [ ] 04-03-PLAN.md — 端到端验证

---

## Progress

| Phase | Status | Plans | Summaries | Date |
|-------|--------|-------|-----------|------|
| Phase 1 | Pending | 3 | 0 | - |
| Phase 2 | Pending | 0 | 0 | - |
| Phase 3 | Pending | 0 | 0 | - |
| Phase 4 | Pending | 0 | 0 | - |

---
*Roadmap created: 2026-04-13*
*Last updated: 2026-04-13*
