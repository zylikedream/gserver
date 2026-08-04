# 日志使用规范

本项目日志统一走 `core/gxylog`(zap 后端)。本文档约定**怎么用、字段怎么命名、哪些不能做**。

## 架构总览

```
业务代码 → gxylog.Info/Debug/... → zap
                                  ├── JSON 文件 log/<app>/<app>.log → promtail → Loki
                                  └── console stdout → systemd journald
```

- **文件输出**:单行 JSON(机器可读,Loki 检索),lumberjack 轮转(100MB × 7 份,压缩)
- **终端输出**:console 格式(人读,`journalctl --user -u gserver@<app> -f`)
- 每条日志**自动注入**上下文:见「自动注入字段」
- 配置:TOML 的 `[log]` 段(`level`/`path`),由 `gxylog.InitLog(ctx, appName)` 在 Node 启动时初始化

## 基本用法

```go
import "gserver/core/gxylog"

gxylog.Info(ctx, "player login",
    gxylog.Num("role_id", rid),
    gxylog.Str("account", acc),
)
gxylog.Debug(ctx, "handle msg start", gxylog.Str("payload", gxyutil.FormatObject(msg)))
gxylog.Warn(ctx, "slow client request", gxylog.Num("cost_ms", cost.Milliseconds()))
gxylog.Error(ctx, "db save failed", gxylog.Err(err))
```

接口:`Info / Debug / Warn / Error / Fatal(ctx, msg, fields...)`,`ctx` 必须传(自动注入依赖它)。

## 日志级别

| 级别 | 语义 | 举例 |
|------|------|------|
| `debug` | 开发调试,生产可关 | 协议收发、字段明细 |
| `info` | 常规事件 | 登录/登出、服务注册变更 |
| `warn` | 异常但不影响主流程 | 慢请求、非法状态忽略 |
| `error` | 出错了,但进程可继续 | DB 失败、处理失败 |
| `fatal` | 致命,打印后 `os.Exit(1)` | 启动失败、配置错误 |

- 默认级别由 `[log] level` 配置(`all`/`debug`/`info`/`warn`/`error`/`fatal`)
- 运行时可动态调:`gxylog.SetLevel("info")`(如 `--pressure` 模式)

## 字段命名规范(强制)

**禁止使用内置 key**,否则产生 JSON 重复键或字段被静默拦截:

```
msg  ts  level  caller  stacktrace  trace_id  ctx_id
```

| 想打什么 | 用 |
|---------|-----|
| 消息/请求载荷 | `payload` |
| 请求/消息 ID | `msg_id` |
| 业务标识 | 语义化名字(`role_id`、`account`、`state`) |
| 错误 | `gxylog.Err(err)` → 自动 `error` 字段 |
| 耗时 | `cost_ms`(统一毫秒) |

字段构造器:`gxylog.Str / Num / Bool / Err / Any`。大对象(proto 消息)先用 `gxyutil.FormatObject` 序列化成字符串再传,禁止直接打二进制/结构体。

## 自动注入字段

| 字段 | 来源 | 说明 |
|------|------|------|
| `mod` | ctx(`gxylog.NewContext` / `WithValue(ContextKeyModType)`) | 模块名(role/chat/gate...) |
| `roleID` | ctx(`SetLogValue(gxylog.ContextKeyRoleID, rid)`) | 角色 ID,Actor 内自动带 |
| `trace_id` | ctx 中的 otel span | 与 Tempo 追踪关联;无 span 时兜底 `ctx_id` |

**不要手动传 `trace_id`/`ctx_id`**——自动注入已覆盖,显式传会与自动字段重复。

在 Actor 内(协议处理方法),`ctx` 已经带好 `mod`/`roleID`/`trace_id`,直接用即可。日志的 `trace_id` 与 Tempo 中该请求的 trace 一致,可在 Grafana 日志 ↔ 链路间跳转排查。

## 第三方库日志

- protoactor / gnet 已通过 `gxylog.LogAdapter` 自动接入,进同一个 JSON 文件与 Loki(阈值 error)
- **不要**直接使用 goframe `glog`(默认 logger 已静音;http 访问日志例外,走独立 glog 到 stdout)

## 反模式

| ❌ 不要 | ✅ 应该 | 原因 |
|--------|--------|------|
| `fmt.Sprintf("role %d login", rid)` 拼进 msg | 消息文本固定,数据进字段 | 字段可被 Loki `\| json` 检索,拼接文本只能全文搜 |
| `gxylog.Str("msg", payload)` | `gxylog.Str("payload", ...)` | `msg` 是内置消息主体 key |
| 日志打密码/token | 脱敏 | 日志进 Loki/Grafana,敏感信息等于泄露 |
| `InitLog` 前调 `Fatal` 之外的需求级日志 | 依赖启动阶段再打 | 兜底走 stderr,不影响功能 |

## 快速排查

```bash
# 终端看日志(console 格式)
journalctl --user -u gserver@role -f

# Loki 查询(JSON 字段检索)
curl -sG 'http://localhost:3100/loki/api/v1/query_range' \
  --data-urlencode 'query={job="gserver"} | json | level="error" | mod="role"' \
  | jq -r '.data.result[0].values[-1][1]'

# 日志文件(JSON 行)
tail -f log/role/role.log | jq
```

Grafana(`localhost:3000`):Explore → Loki 数据源,可过滤 `{job="gserver"}`,对 `trace_id` 关联 Tempo。

### 联动使用提醒(时间窗口)

Loki 日志行点 TraceID 跳转 Tempo 时,**时间范围继承当前 Explore 的窗口**——日志/trace 在窗口外会显示无数据(404)。排查历史日志前,先把时间范围切到覆盖目标时间(如 Last 6 hours),再点跳转。

验证链路:`log/role/role.log` 业务行(带 `trace_id` 且非启动日志)→ Loki 查 `{job="gserver"} | json | trace_id!=""` → 展开行 → Links → Tempo → trace 详情(span 列表)。启动日志的 trace_id 是孤儿(span 不导出 Tempo),跳转会 404,属已知限制。
