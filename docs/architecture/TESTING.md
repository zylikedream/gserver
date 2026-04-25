# Testing Patterns

## 现状

项目测试覆盖极低，仅有 1 个测试文件：

| 文件 | 测试内容 |
|------|---------|
| `src/apps/role/internal/logic/rolemod_test.go` | 角色模块初始化验证 |

## 测试框架

- **标准库**: `testing` — Go 内置测试框架
- 无第三方测试框架（未使用 testify、mockery 等）
- 无代码覆盖率工具配置
- 无 CI/CD 流水线

## 可测试性分析

### 容易测试的部分

1. **src/util/ 层** — 纯函数，无外部依赖
   - `src/util/msg_handler.go` — 消息路由，可通过 mock handler 验证
   - `src/util/reflect.go` — 反射工具，纯函数
   - `src/util/ets/ets.go` — 内存表，可独立测试

2. **gameconfig/** — 配置加载和校验

3. **core/gxytimer/** — 定时器逻辑

### 需要集成测试的部分

4. **core/gxypgx/** — 需要 PostgreSQL 实例（可用 testcontainers-go）
5. **core/gxyredis/** — 需要 Redis 实例
6. **core/gxylocator/** — 需要 Redis 实例，测试 Lua 脚本正确性

### 难以测试的部分

7. **core/gxyactor/** — 强依赖 protoactor-go ActorSystem，需要进程内启动
8. **src/apps/role/internal/logic/** — 依赖 Actor 上下文、PostgreSQL、Redis
9. **src/apps/gateway/** — 依赖网络层和 Actor 系统

## 测试建议

### 单元测试（优先）

```
src/util/msg_handler_test.go     — 消息路由正确性
src/util/ets/ets_test.go         — 内存表 CRUD
src/util/uid/uid_test.go         — UID 生成
gameconfig/gameconfig_test.go — 配置加载
```

### 接口 Mock 测试

```
// 将 PostgreSQL 操作抽象为接口
type IDB interface {
    FindOne(ctx, reply, table, filter) error
    UpsertOne(ctx, table, data, conflictCols) error
}

// 业务代码依赖接口而非具体实现
// 测试时注入 mock
```

### 集成测试

```
// 使用 testcontainers-go 启动 PostgreSQL/Redis 容器
// 或使用 docker-compose 管理测试依赖
```

### Actor 测试

```go
// protoactor-go 提供了测试工具
// 可以在测试中创建 ActorSystem，spawn Actor，发送消息，验证响应
func TestRoleMain_Login(t *testing.T) {
    system := actor.NewActorSystem()
    props := actor.PropsFromProducer(func() actor.Actor {
        return NewRoleMain()
    })
    pid := system.Root.Spawn(props)
    // 发送消息并验证响应
}
```

## 测试优先级建议

按影响面排序：

1. **gxylocator Lua 脚本** — 核心定位逻辑，SETNX 注册和条件注销的正确性
2. **gxyactor grain_manager** — Grain 激活/续约/注销流程
3. **gxypgx UpsertOne** — 乐观锁和 JSONB 映射
4. **src/util/msg_handler** — 消息路由正确性
5. **src/apps/role 业务逻辑** — 角色模块核心逻辑

---
*Last updated: 2026-04-22*
