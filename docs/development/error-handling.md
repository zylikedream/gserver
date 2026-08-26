# 错误处理开发规范

> 索引:AGENTS.md development tips
> 决策背景见 `docs/architecture/adr-0002-error-handling.md`(为什么选 cockroachdb/errors)
> 本文档是可操作约定:怎么写,不解释为什么。

## 一句话约定

**统一用 `github.com/cockroachdb/errors`(唯一错误库),错误产生点自动带栈,跨层 `Wrap` 保留因果,日志在最终处理点 `%+v` 打全链。**

## 核心库

```go
import "github.com/cockroachdb/errors"   // 唯一错误库,替代标准库 errors
```

标准库 `errors` 仅在 `errors.Is/As` 判错时可用(兼容,但直接用 cockroachdb 的也行)。

## 场景写法

| 场景 | 写法 | 示例 |
|---|---|---|
| 产生新错误 | `errors.New` / `errors.Newf` | `errors.Newf("role %d not found", id)` |
| 包装已有错误 | `errors.Wrap` / `Wrapf` | `errors.Wrap(err, "load role")` |
| 外部裸错误补栈 | `errors.WithStack` | `errors.WithStack(jsonErr)` |
| 哨兵定义 | `errors.New`(cockroachdb) | `var ErrNotFound = errors.New("not found")` |
| 哨兵使用 | 直接返回,不补栈 | 已带定义处栈 |
| 判错 | `errors.Is` / `errors.As` | `errors.Is(err, ErrNotFound)` |
| 日志 | `gxylog.Err(err)` | `%+v` 自动打全链 + 栈 |

## 硬性规则

1. **禁止 `fmt.Errorf("...: %s", err)` / `%v` 吞错误** —— 因果链断,`errors.Is` 失效
2. **禁止对 cockroachdb 错误再 `WithStack`** —— New/Wrap 已带栈,再包是双层重复栈
3. **`errors.WithStack` 只用于外部错误**(标准库 os/io、第三方库返回的)
4. **不要用 `errors.Newf("...%w...", err)`** —— cockroachdb 遇 `%w` 会走标准库 `fmt.Errorf`,栈丢失;包装一律 `errors.Wrap/Wrapf`
5. **栈只在产生点加一次** —— 中间层 `Wrap` 或 `%w` 不加栈
6. **哨兵错误不额外补栈** —— cockroachdb New 自带定义处栈(诊断价值),`WithStack(sentinel)` 会双层栈

## 日志边界

- 打印点 = 错误的**最终处理处**(容错吞错 / 边界终结 / actor 停止)
- 不在链路中途重复打印同一错误(打印后上抛 = 重复)
- 缓存类容错(Get/Set 失败):打印 + `return nil`,错误不扩散

## Review 检查清单

- [ ] 新错误是否用了 cockroachdb `New/Newf`(带栈)?
- [ ] 包装是否用 `Wrap`/`%w`,没有 `%s`/`%v` 吞错误?
- [ ] 没有对 cockroachdb 错误重复 `WithStack`(含哨兵)?
- [ ] 没有 `Newf` + `%w` 的组合?
- [ ] 打印点是否在最终处理处,没有中途重复打印?

## 迁移记录(2026-08-14 已完成)

- pkg/errors → cockroachdb/errors v1.14.0(全仓唯一错误库)
- 97 处临时 `errors.New` + 57 处哨兵 → cockroachdb(统一,自动带栈)
- 32 处无 `%w` 的 `fmt.Errorf` → `errors.Newf`/`Wrap`
- packet 包重复 `WithStack` 清理
