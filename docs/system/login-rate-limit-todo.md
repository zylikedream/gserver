# Login Rate Limit TODO

## Goal

实现登录入口削峰，防止大量玩家同时登录时冲击 Gateway、Role Actor 激活、PostgreSQL、Redis、Chat/Guild/Friend 初始化流程。

第一版只做 **单 Gateway 节点本地限流**，不做全服排队系统。

## Design

登录限流分两层：

1. 登录 QPS 限制：控制单位时间进入登录流程的请求数。
2. 登录并发闸门：控制同一时间正在执行登录初始化的数量。

建议放在 Gateway 的握手流程中，位置在 `Session.handleHandshake` 里：

```text
ReqHandShake
  -> GetRoleIDByAccount
  -> AcquireLoginPermit
  -> ActivateRole
  -> RspHandShake
```

不要放在 `RoleMain.ReqAccountLogin` 之后，否则 Role Actor 已经激活，削峰太晚。

## Config

建议配置放在 `config/gate.toml`：

```toml
[login_limit]
enabled = true
rate = 200
burst = 400
max_inflight = 100
queue_size = 500
wait_timeout = "3s"
```

配置语义：

- `rate`: 单 Gateway 节点平均每秒放行登录数。
- `burst`: 单 Gateway 节点允许的瞬时登录突发。
- `max_inflight`: 当前节点同时执行登录初始化的最大数量。
- `queue_size`: 并发满时允许等待的最大队列长度。
- `wait_timeout`: 排队等待许可的最长时间。

多 Gateway 部署时：

```text
全服登录 QPS 约等于 gateway_count * rate
全服登录并发约等于 gateway_count * max_inflight
```

## Behavior

- 先检查 QPS 令牌桶。
- 再进入并发闸门。
- 成功拿到许可后执行 `ActivateRole`。
- `ActivateRole` 完成后释放并发许可。

拒绝策略：

- 第一版返回“登录繁忙”并关闭 Session。
- 不静默丢弃。
- 后续可扩展 `RspHandShake`，增加 `code/reason`，让客户端展示明确错误。

建议错误原因：

| 场景 | reason |
|---|---|
| QPS 超限 | `login rate limited` |
| 队列满 | `login queue full` |
| 等待超时 | `login queue timeout` |

## Implementation TODO

- 新增 Gateway 登录限流组件，例如 `LoginLimiter`：
  - token bucket 控制 QPS。
  - semaphore/channel 控制并发。
  - bounded queue 控制等待数量。
- 在 `Session.handleHandshake` 中接入。
- `Acquire(ctx)` 返回许可对象或错误。
- 调用方使用：

```go
permit, err := LoginLimiter().Acquire(ctx)
if err != nil {
    return err
}
defer permit.Release()
```

- `enabled=false` 时直接放行。
- 配置非法时 Gateway 启动失败，避免线上行为不确定。

## Metrics

| 指标 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `login_inflight` | Gauge | - | 当前正在执行登录初始化的数量 |
| `login_queue_length` | Gauge | - | 当前等待登录许可的数量 |
| `login_limit_total` | Counter | `result` | 登录限流结果 |
| `login_wait_duration_seconds` | Histogram | `result` | 等待登录许可耗时 |

`result` 建议值：

- `ok`
- `rate_limited`
- `queue_full`
- `queue_timeout`
- `error`

## Test Plan

- `enabled=false` 时所有请求放行。
- `rate/burst` 超限时返回 `rate_limited`。
- `max_inflight` 满时请求进入等待。
- `queue_size` 满时返回 `queue_full`。
- 等待超过 `wait_timeout` 时返回 `queue_timeout`。
- 正常完成登录后释放并发许可。
- `Acquire` 失败不得泄漏 inflight 计数。

## Assumptions

- 第一版只做 Gateway 节点本地限流。
- 不实现全服排队号、全局令牌或跨 Gateway 协调。
- 拒绝时关闭 Session；协议层错误响应后续再扩展。
- 目标是削峰保护系统，不是严格公平排队。
