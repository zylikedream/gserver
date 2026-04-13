# Testing Patterns

## 现状

项目测试覆盖极低，仅有 2 个测试文件：

| 文件 | 测试内容 |
|------|---------|
| `core/gxymongo/mongo_test.go` | MongoDB ReplaceOne + Upsert 功能验证 |
| `apps/role/internal/logic/rolemod_test.go` | 角色模块初始化验证 |

## 测试框架

- **标准库**: `testing` — Go 内置测试框架
- 无第三方测试框架（未使用 testify、mockery 等）
- 无代码覆盖率工具配置
- 无 CI/CD 流水线

## 可测试性分析

### 容易测试的部分

1. **util/ 层** — 纯函数，无外部依赖
   - `util/msg_handler.go` — 消息路由，可通过 mock handler 验证
   - `util/reflect.go` — 反射工具，纯函数
   - `util/ets/ets.go` — 内存表，可独立测试

2. **gameconfig/** — 配置加载和校验

3. **core/gxytimer/** — 定时器逻辑

### 需要集成测试的部分

4. **core/gxymongo/** — 需要 MongoDB 实例
5. **core/gxyredis/** — 需要 Redis 实例
6. **core/gxylocator/** — 需要 Redis 实例

### 难以测试的部分

7. **core/gxyactor/** — 强依赖 protoactor-go ActorSystem，需要进程内启动
8. **apps/role/internal/logic/** — 依赖 Actor 上下文、MongoDB、Redis
9. **apps/gateway/** — 依赖网络层和 Actor 系统

## 测试建议

### 单元测试（优先）

```
util/msg_handler_test.go     — 消息路由正确性
util/ets/ets_test.go         — 内存表 CRUD
util/uid/uid_test.go         — UID 生成
gameconfig/gameconfig_test.go — 配置加载
```

### 接口 Mock 测试

```
// 将 MongoDB/Redis 操作抽象为接口
type IDB interface {
    FindOne(ctx, reply, col, filter) error
    ReplaceOne(ctx, col, filter, update, opts) error
}

// 业务代码依赖接口而非具体实现
// 测试时注入 mock
```

### 集成测试

```
// 使用 testcontainers-go 启动 MongoDB/Redis 容器
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

---
*Last updated: 2026-04-13*
