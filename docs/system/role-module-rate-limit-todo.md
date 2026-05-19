# Role Module Rate Limit TODO

## Goal

第一版在 Role 协议层实现模块级令牌桶限流和模块级降级开关。

模块粒度使用 Go 结构体名，例如 `RoleFriend`、`RoleGuild`、`RoleChat`、`RoleGM`。同一模块下所有协议共享一个限流桶；模块被关闭时，该模块所有协议统一返回 `Ack` 错误。

## Config

建议配置放在 `config/role.toml`：

```toml
[role_limit.default]
enabled = true
rate = 10
burst = 20
reason = "request too frequent"

[role_limit.modules.RoleFriend]
enabled = true
rate = 3
burst = 6

[role_limit.modules.RoleGuild]
enabled = true
rate = 3
burst = 6

[role_limit.modules.RoleChat]
enabled = true
rate = 2
burst = 4

[role_limit.modules.RoleGM]
enabled = true
rate = 1
burst = 2
```

配置语义：

- 未配置模块走 `default`。
- `enabled = false` 表示整个模块降级，所有协议拒绝。
- `rate <= 0` 或 `burst <= 0` 视为配置错误，启动时报错。
- GM 模块和普通模块一样受配置控制。

## Behavior

拦截位置：`RoleMain.HandleClientMsg` 反序列化协议后、调用业务 handler 前。

流程：

1. 根据 `msg_name` 找到对应 handler 所属模块名。
2. 检查模块是否 `enabled = false`。
3. 检查 `role_id + module_name` 对应令牌桶是否允许通过。
4. 被拒绝时返回：

```go
&pb.Ack{
    Code:   1,
    Id:     id,
    Reason: reason,
}
```

超限或降级都不关闭 Session。

降级优先级高于限流：模块关闭时直接返回，不消耗令牌。

## Implementation TODO

- 扩展 `MsgHandler` 或在 `RoleMain.initMsgHandler` 建立 `msg_name -> module_name` 映射。
- 新增 `RoleLimiter`：
  - 每个 Role Actor 内维护自己的模块桶。
  - key 为 `module_name`。
  - Actor 单线程处理消息，不需要锁。
  - Role 停止时自然释放桶状态。
- 令牌桶字段：
  - `rate float64`
  - `burst float64`
  - `tokens float64`
  - `last time.Time`
- 每次请求按 elapsed 补充令牌：
  - `tokens = min(burst, tokens + elapsedSeconds*rate)`
  - `tokens >= 1` 放行并扣 1
  - 否则返回限流 Ack
- 配置加载失败应阻止 role app 启动，避免线上规则不确定。

## Metrics

第一版建议补：

| 指标 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `role_module_limit_total` | Counter | `module`, `result` | `ok` / `limited` / `disabled` |

后续可和 P0 指标联动：

- `client_requests_total{result="limited"}`
- `client_request_duration_seconds`

## Test Plan

- 令牌桶单元测试：
  - 初始 burst 可连续通过 `burst` 次。
  - 超过 burst 后被限流。
  - 时间推进后按 rate 恢复。
  - `enabled = false` 永远拒绝。
- RoleMain 级测试：
  - `ReqFriendSearchPlayer` 命中 `RoleFriend` 规则。
  - `RoleFriend enabled=false` 时返回 Ack，业务 handler 不执行。
  - 未配置模块走 default。
  - `RoleGM` 和普通模块一样受规则控制。
- 配置测试：
  - 缺省配置能启动。
  - 非法 `rate <= 0` 或 `burst <= 0` 返回启动错误。
- 回归：
  - `go test ./core/gxyutil`
  - `go test ./src/apps/role/internal/logic`

## Assumptions

- 第一版只做 Role 协议层，不处理握手前 gateway 包限流。
- 超限和降级都返回 Ack 错误，不断开 Session。
- 模块名使用 Go 结构体名。
- 配置首版启动时加载；热更新后续接入已有 broadcast reload 机制。
