# Codebase Concerns

## 架构层面

### 1. Grain 定位依赖 Redis TTL
- **位置**: `core/gxyactor/grain_manager.go`
- **问题**: Grain 在 Redis 中的注册使用 40s TTL + 30s 刷新。如果 Redis 不可用超过 40s，Grain 位置会丢失，导致重复创建
- **风险**: 中等 — Redis 通常高可用，但 TTL 机制存在窗口期
- **建议**: 考虑本地缓存作为 fallback，或缩短 TTL/刷新间隔

### 2. Supervisor 策略过于简单
- **位置**: `core/gxyactor/system.go:157-164`
- **问题**: 所有 Actor 使用相同的 OneForOne 策略（10 次/3 秒 → Stop）。对于 RoleMain 这种有状态的 Grain，重启后状态可能不一致
- **现状**: 代码中 decider 直接返回 StopDirective，实际不会重启，只是停止
- **风险**: 低 — 当前策略是停止而非重启，符合游戏服务器的"保存后停止"语义

### 3. 无消息重试机制
- **位置**: `apps/gateway/internal/logic/session.go:199`
- **问题**: Session 向 RoleGrain 发送消息使用 `CallSync`（fire-and-forget），如果 RoleGrain 暂时不可用（正在迁移、重启），消息会丢失
- **风险**: 中等 — 客户端请求可能丢失，需要客户端重试或服务端消息队列

## 数据层面

### 4. 乐观锁冲突处理不完善
- **位置**: `apps/role/internal/logic/role_main.go:296-348`
- **问题**: 乐观锁冲突时直接返回 `ErrVersionConflict` 并停止 Actor，没有重试或合并逻辑。在跨节点迁移场景下可能出现
- **风险**: 低 — 单节点内 Actor 串行处理不会冲突，跨节点场景才有风险

### 5. 保存失败直接停止 Actor
- **位置**: `apps/role/internal/logic/role_main.go:283-289`
- **问题**: 定时保存失败时直接调用 `r.Stop(err)` 终止 Actor。如果是临时性数据库故障，会导致大量角色被踢下线
- **风险**: 中等 — 应考虑重试机制或降级策略

### 6. 脏检查 hash 碰撞风险
- **位置**: `util/reflect.go` → `GetObjectHash`
- **问题**: 使用对象 hash 判断是否需要保存。理论上存在 hash 碰撞可能（虽然极低），且 hash 计算性能取决于结构体大小
- **风险**: 极低 — 实际碰撞概率可忽略

## 并发安全

### 7. SessionMgr 非线程安全
- **位置**: `apps/gateway/internal/logic/session_mgr.go`
- **问题**: SessionMgr 管理 roleID → sessionPID 映射，需要确认 Add/Remove/All 操作是否在 Actor 上下文中调用（Actor 串行处理则安全）
- **风险**: 低 — 如果仅在 Actor 内调用则安全

### 8. grainActivator.childs 并发访问
- **位置**: `core/gxyactor/grain_manager.go:40,113,128`
- **问题**: `childs map[PID]string` 在 grainActivator 中被读写。由于 Actor 串行处理消息，这实际上是安全的
- **风险**: 无 — Actor 模型保证串行访问

## 可观测性

### 9. 缺乏监控指标
- **问题**: 没有 Prometheus/metrics 暴露，无法监控在线人数、消息延迟、Grain 分布等关键指标
- **风险**: 中等 — 生产环境需要这些指标进行运维

### 10. 缺乏分布式追踪
- **问题**: 消息从 Client → Session → RoleMain 跨越多层 Actor，没有 traceID 串联
- **风险**: 低 — 开发阶段可通过日志定位，生产环境需要追踪

## 代码质量

### 11. role_account.go 中 FindOne 的使用方式
- **位置**: `apps/role/internal/logic/role_account.go:22`
- **问题**: 先创建空 `RoleAccount` 对象，再 `FindOne` 填充。如果 MongoDB 查询无结果，对象保持零值，需要额外检查字段是否被填充
- **风险**: 低 — 当前逻辑正确，但模式不够直观

### 12. index.go 中创建 RoleMain 实例仅为获取模块列表
- **位置**: `apps/role/internal/logic/index.go:17`
- **问题**: `NewRoleDBIndex()` 创建了一个 `RoleMain` 实例只为调用 `initRoleModules`，这个 RoleMain 不会被使用，浪费资源
- **风险**: 极低 — 仅在启动时执行一次

### 13. bson tag 拼写错误
- **位置**: `apps/role/internal/logic/role_sign.go` — `Patch` 字段
- **问题**: 部分字段可能存在 bson tag 拼写不一致（如 `bason` 应为 `bson`，需确认）
- **风险**: 低 — 编译不会报错，但数据序列化可能出问题

## 测试

### 14. 测试覆盖不足
- **位置**: 项目整体
- **问题**: 仅有 `gxymongo/mongo_test.go` 和 `rolemod_test.go` 两个测试文件，核心业务逻辑缺乏单元测试
- **风险**: 中等 — 重构或修改时容易引入回归 bug

---
*Last updated: 2026-04-13*
