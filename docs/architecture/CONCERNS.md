# Codebase Concerns

## 架构层面

### 1. Grain 定位依赖 Redis TTL
- **位置**: `core/gxyactor/grain_manager.go`
- **问题**: Grain 在 Redis 中的注册使用 40s TTL + 30s 刷新，余量仅 10s。如果 Redis 不可用超过 40s，Grain 位置会丢失，导致重复创建
- **风险**: 中等 — Redis 通常高可用，但 TTL 机制存在窗口期
- **建议**: 考虑增大 TTL/刷新间隔比例（如 TTL=90s, 刷新=30s），或本地缓存作为 fallback，增加续约重试机制

### 2. Supervisor 策略过于简单
- **位置**: `core/gxyactor/system.go:157-164`
- **问题**: 所有 Actor 使用相同的 OneForOne 策略，decider 直接返回 `StopDirective`，无论错误类型。临时性故障（Redis 超时、数据库连接中断）也会永久停止 Actor
- **现状**: 没有按错误类型区分（ResumeDirective / RestartDirective / StopDirective）
- **风险**: 中等 — 应区分可恢复错误与致命错误，对有状态 Grain 的重启策略需特殊处理

### 3. 无消息重试机制
- **位置**: `apps/gateway/internal/logic/session.go:199`
- **问题**: Session 向 RoleGrain 发送消息使用 `CallSync`（fire-and-forget），如果 RoleGrain 暂时不可用（正在迁移、重启），消息会丢失
- **风险**: 中等 — 客户端请求可能丢失，需要客户端重试或服务端消息队列

### 4. 批量续约不校验所有权
- **位置**: `core/gxylocator/script.go:26-33`
- **问题**: 批量续约 Lua 脚本使用 `SETEX` 无条件覆盖，不检查当前值是否匹配。如果 Actor 已停止但尚未从 childs 移除，续约可能覆盖其他节点已注册的新 PID
- **风险**: 中等 — 存在时间窗口内的 key 竞争，需在 Lua 脚本中加入值校验

### 5. 无 Dead Letter 处理
- **位置**: 全局
- **问题**: 没有订阅 protoactor-go 的 DeadLetter 事件，发送到已停止 Actor 的消息被静默丢弃。无日志、无指标、无告警
- **风险**: 中等 — 生产环境中丢消息难以排查，玩家操作可能静默丢失

### 6. 无流控/过载保护
- **位置**: 全局
- **问题**: 没有 mailbox 深度限制、没有背压机制、没有 per-session 限流。大量消息涌入时 Actor 可能积压
- **风险**: 中等 — 高并发场景下可能导致内存问题

## 数据层面

### 7. 保存失败直接停止 Actor
- **位置**: `apps/role/internal/logic/role_main.go`
- **问题**: 定时保存失败时直接调用 `r.Stop(err)` 终止 Actor。如果是临时性数据库故障，会导致大量角色被踢下线
- **风险**: 中等 — 应考虑重试机制或降级策略

### 8. 脏检查 hash 碰撞风险
- **位置**: `util/reflect.go` → `GetObjectHash`
- **问题**: 使用对象 hash 判断是否需要保存。理论上存在 hash 碰撞可能（虽然极低），且 hash 计算性能取决于结构体大小
- **风险**: 极低 — 实际碰撞概率可忽略

## 并发安全

### 9. SessionMgr 非线程安全
- **位置**: `apps/gateway/internal/logic/session_mgr.go`
- **问题**: SessionMgr 管理 roleID → sessionPID 映射，需要确认 Add/Remove/All 操作是否在 Actor 上下文中调用（Actor 串行处理则安全）
- **风险**: 低 — 如果仅在 Actor 内调用则安全

### 10. grainActivator.childs 并发访问
- **位置**: `core/gxyactor/grain_manager.go`
- **问题**: `childs map[PID]string` 在 grainActivator 中被读写。由于 Actor 串行处理消息，这实际上是安全的
- **风险**: 无 — Actor 模型保证串行访问

### 11. GrainBase.Receive 潜在 nil panic
- **位置**: `core/gxyactor/actor.go:244`
- **问题**: `actorCtx.InitArgs[0]` 没有 nil 检查，如果缺少 ContextDecorator 包装，`InitArgs` 为 nil 会导致 panic
- **风险**: 低 — 正常流程总有 ContextDecorator，但缺少防御性编程

## 可观测性

### 12. 缺乏监控指标
- **问题**: 没有 Prometheus/metrics 暴露，无法监控在线人数、消息延迟、Grain 分布等关键指标
- **风险**: 中等 — 生产环境需要这些指标进行运维

### 13. 缺乏分布式追踪
- **问题**: 消息从 Client → Session → RoleMain 跨越多层 Actor，没有 traceID 串联
- **风险**: 低 — 开发阶段可通过日志定位，生产环境需要追踪

### 14. Logger WithAttrs/WithGroup 丢失字段
- **位置**: `core/gxyactor/logger.go:68-74`
- **问题**: `actorLogAdapter` 的 `WithAttrs` 和 `WithGroup` 返回 `self` 而非新实例，protoactor-go 通过 `With()` 附加的系统字段被静默丢弃
- **风险**: 低 — 影响 Actor 系统内部日志的结构化字段

## 代码质量

### 15. protojson.Marshal 错误被忽略
- **位置**: `core/gxyactor/grain_manager.go:146,155,276`
- **问题**: 三处 `protojson.Marshal` 的错误返回用 `_` 丢弃。如果序列化失败，空字符串会被注册为 Grain PID，导致 Grain 不可定位
- **风险**: 低 — proto 消息结构稳定不太可能失败，但应处理错误

### 16. Timer 回调 map 无清理
- **位置**: `core/gxyactor/actor_timer.go`
- **问题**: `callbackFuncs` map 只增不减，`Once` 类型的定时器触发后回调条目不会被清理，长期运行存在内存泄漏
- **风险**: 低 — 实际回调数量有限，但应定期清理

### 17. ActorCheckResult 死代码
- **位置**: `core/gxyactor/grain_manager.go:43-48`
- **问题**: `ActorCheckResult` 类型已定义且在 HandleMessage 中处理，但从未被构造或发送。属于旧版 Touch+Check 流程的残留
- **风险**: 极低 — 不影响功能，但增加代码理解成本

### 18. 全局 actorApp 单例无保护
- **位置**: `core/gxyactor/system.go:31`
- **问题**: `var app *actorApp` 被写入后无二次检查，多次调用 `NewActorApp` 会静默覆盖
- **风险**: 极低 — 正常启动只调用一次，但测试中可能出错

## 测试

### 19. 测试覆盖不足
- **位置**: 项目整体
- **问题**: 核心业务逻辑缺乏单元测试，仅有 1 个测试文件
- **风险**: 中等 — 重构或修改时容易引入回归 bug

---
*Last updated: 2026-04-22*
