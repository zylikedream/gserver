# 日志开发规范

> 索引:AGENTS.md development tips
> 实现:`core/gxylog`(zap 封装,结构化 + JSON + Loki)
> 配合:`docs/development/error-handling.md`(错误规范,日志打错误栈)

## 一句话约定

**统一用 `gxylog`,结构化字段,错误必须 `gxylog.Err(err)`(`%+v` 打完整栈),打印点只在错误最终处理处。**

## 统一入口

```go
import "gserver/core/gxylog"
```

- **禁止** `fmt.Println` / `fmt.Printf` / 标准库 `log` 打印业务日志
- 业务日志一律走 `gxylog.Info/Debug/Warn/Error/Fatal`
- `fmt.Errorf` 是构造错误,不是打日志(见 error 规范)

## 级别使用

| 级别 | 场景 | 示例 |
|---|---|---|
| `Debug` | 诊断信息,默认不关注 | 加物品、扣物品明细 |
| `Info` | 正常业务流程的重要节点 | 模块启动/停止、玩家登录 |
| `Warn` | 可恢复的异常 | 缓存未命中、重试、降级 |
| `Error` | 操作失败,需要关注 | DB 失败、消息处理失败 |
| `Fatal` | 进程无法继续 | 配置缺失、迁移失败 |

## 结构化字段(禁止字符串拼接)

```go
// ✅ 正确: 字段化
gxylog.Info(ctx, "add good", gxylog.Num("roleID", roleID), gxylog.Num("id", op.GoodID))

// ❌ 错误: 字符串拼接(不可查询, 易错)
gxylog.Info(ctx, "add good roleID="+strconv.FormatInt(roleID, 10)+" id="+...)
```

字段 API:`gxylog.Str` / `Num` / `Bool` / `Any` / `Err`。

## 错误日志:必须 `gxylog.Err(err)`

```go
// ✅ 正确: %+v 展开, 含完整错误链 + 栈(file:line)
gxylog.Error(ctx, "load role failed", gxylog.Err(err))

// ❌ 错误: 只打消息, 栈丢失(排查不到出错行)
gxylog.Error(ctx, "load role failed", gxylog.Str("error", err.Error()))
```

- `gxylog.Err` 内部是 `fmt.Sprintf("%+v", err)`——配合 cockroachdb/errors 打全链+栈
- 错误链栈信息由 error 规范保证(产生点/返回处带栈)

## ctx 传递:日志自动带上下文

```go
ctx := gxylog.NewContext(ctx, "role")          // 模块名
ctx = gxylog.WithValue(ctx, gxylog.ContextKeyRoleID, roleID)  // 业务字段
gxylog.Info(ctx, "...")                        // 自动带 mod/roleID/trace_id
```

- 所有日志函数第一个参数是 `ctx`,从请求链一路传
- **不要**在日志字段里重复打 ctx 已有的 key(去重机制:reservedKeys)

## 日志边界

- **打印点 = 错误的最终处理处**(容错吞错 / 边界终结 / actor 停止)
- 不在链路中途重复打印同一错误(打印后上抛 = 重复日志)
- 错误规范:`errors.Wrap` 链上抛,顶层 `gxylog.Error` 一次打全

## 敏感信息

- **禁止**打印密码、token、密钥、完整账号凭据
- 用户输入可打印(便于排查),但脱敏关键字段

## 性能

- 热路径(高频消息处理)用 `Debug` 或不打
- 日志字段值避免昂贵计算(如大对象 `Any`)
- `Fatal` 会退出进程,仅用于启动期致命错误

## Review 检查清单

- [ ] 统一用 `gxylog`,无 `fmt.Println`/`log` 残留?
- [ ] 级别选择正确(Debug/Info/Warn/Error)?
- [ ] 字段化,无字符串拼接?
- [ ] 错误日志用 `gxylog.Err(err)` 而非 `Str("error", err.Error())`?
- [ ] ctx 传递正确(带 mod/roleID/trace_id)?
- [ ] 打印点在最终处理处,无中途重复?
- [ ] 无敏感信息(密码/token)?
