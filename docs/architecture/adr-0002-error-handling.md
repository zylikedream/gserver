# ADR 0002:错误处理规范(cockroachdb/errors)

- 日期:2026-08-14
- 状态:**Accepted**

## 背景

全仓 error 风格不一,导致**线上报错无法定位真正出错的行**:

- 98 处 `errors.New` 无栈,`gxylog.Err`(`%+v`)打不出行号
- `errors.WithStack` 只集中在 packet 包(23 处)
- 32 处 `fmt.Errorf` 无 `%w`,因果链断,`errors.Is` 失效
- 日志打印点(65 处)分散,打印点 ≠ 出错点

## 决策

1. **错误库**:`github.com/pkg/errors` → `github.com/cockroachdb/errors v1.14.0`(活跃维护,pkg/errors 继任)
2. **栈策略**:栈只在**错误产生点**加一次,中间层不加(避免重复栈)
   - cockroachdb 的 `errors.New`/`Newf`/`Wrap` **自动带栈**,无需显式 `WithStack`
   - `errors.WithStack` 仅用于**外部裸错误补栈**(标准库 os/io/第三方返回)
3. **哨兵错误**:`var ErrXxx = errors.New(...)`(cockroachdb,带定义处栈)——`errors.Is` 比较不受影响;不额外 `WithStack`(避免双层栈)
4. **包装**:`errors.Wrap`/`Wrapf`(消息+栈)或 `fmt.Errorf("%w")`(中间层不加栈);禁止 `%s`/`%v` 吞错误
5. **日志**:`gxylog.Err(err)`(`%+v`)在**最终处理点**打印完整链+栈;打印点即错误归宿(容错吞错或边界终结),不在链路中途重复打印

## 使用规范

| 场景 | 写法 | 栈 |
|---|---|---|
| 产生新错误 | `errors.New` / `errors.Newf`(cockroachdb) | ✅ 自动 |
| 包装已有错误 | `errors.Wrap` / `Wrapf` | ✅ 消息+栈 |
| 外部裸错误 | `errors.WithStack(err)` | ✅ 补栈 |
| 哨兵定义 | `errors.New`(cockroachdb) | ✅ 定义处栈 |
| 哨兵使用 | 直接返回 | ✅ |
| 日志 | `gxylog.Err(err)` | ✅ 全链+栈 |

## 注意

- cockroachdb `errors.Newf` 遇 `%w` 会 shortcut 到标准库 `fmt.Errorf`(**无栈**)——包装场景必须用 `errors.Wrap`/`Wrapf`,勿用 `Newf` + `%w`
- `errors.Is`/`As` 兼容(所有错误实现 `Unwrap`)

## 迁移记录

- pkg/errors → cockroachdb/errors:import 路径替换(包名同为 errors,调用点零改动)
- 97 处临时 `errors.New` + 57 处哨兵 → cockroachdb(统一唯一错误库)
- 32 处无 `%w` 的 `fmt.Errorf` → `errors.Newf`/`Wrap`
- packet 包重复 `WithStack` 清理
- 后续更新:哨兵从 stderrors 改回 cockroachdb(全仓统一单库,接受定义处栈噪音)

## 待办

- 34 处 `fmt.Errorf("%w")` 中间层包装:符合规范(不加栈),如需栈可改 `Wrapf`
- 日志打印点 65 处:评估收敛(当前多在合理边界,不机械删)
