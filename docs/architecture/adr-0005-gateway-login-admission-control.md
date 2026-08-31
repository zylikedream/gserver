# ADR 0005:Gateway 登录准入控制

- 日期:2026-08-31
- 状态:**Accepted**
- 关联:Issue #34

## 背景

`Session.handleHandshake` 在 GateToken 验证后直接调用 `ActivateRole`。登录洪峰会同时触发 Role Actor 激活及 PostgreSQL、Redis、Chat、Guild、Friend 初始化,单纯限制客户端连接数无法约束这些下游操作的启动速率和并发量。

本决策只处理**单个 Gateway 节点的登录准入**。多 Gateway 部署时,全服近似容量为节点数乘以单节点配置;第一版不增加 Redis 协调、全服排队号或跨节点公平性。

## 决策

### 两级准入

每个 Gateway 节点持有一个、由全部 Session 共享的 `LoginLimiter`:

1. 复用 `core/gxylimit.Bucket` 控制进入登录准入流程的平均 QPS 和瞬时 burst。
2. 使用有界并发闸门控制正在执行 `ActivateRole` 的数量;满载时仅允许有限数量的 Session 等待。

组合逻辑放在 `src/apps/gateway/internal/logic`,不扩展 `core/gxylimit`。共享基础设施层继续只负责 Token Bucket;排队、超时、许可生命周期和登录指标属于 Gateway 业务边界。

### 配置注入与生命周期

`gateApp.OnModInit` 在创建网络模块前严格读取 `[login_limit]`,构造单节点 `LoginLimiter`,并通过包级 setter 注入。该方式与现有 `SetGateTokenVerifier` 一致:

```text
gateApp.OnModInit
  -> LoadLoginLimitConfig
  -> NewLoginLimiter
  -> SetLoginLimiter
  -> 创建网络模块
  -> Session 开始生成
```

配置只在启动阶段写入、运行期只读;修改后通过重启 Gateway 生效。测试使用同包 swap helper 临时替换并恢复 limiter,避免全局状态污染。

### 配置契约

```toml
[login_limit]
enabled = true
rate = 200
burst = 400
max_inflight = 100
queue_size = 500
wait_timeout = "3s"
```

字段约束:

- `rate` 是每秒补充令牌数,必须为有限正数。
- `burst` 必须为正整数。
- `max_inflight` 必须为正整数。
- `queue_size` 必须为非负整数。
- `wait_timeout` 必须为正 duration。
- `queue_size = 0` 表示不排队;并发满时立即返回 `queue_full`。零值不表示无限队列。
- `enabled = false` 绕过令牌桶、并发和等待,但其余字段仍必须完整且合法。

配置使用 exact decode。缺失配置节、缺失字段、未知字段、非有限 rate 和非法边界均阻止 Gateway 启动。模板提供显式默认值;不做隐式运行时默认。

### 请求数据流

`Session.handleHandshake` 的顺序为:

```text
ReqHandShake 类型检查
  -> maintenance 检查
  -> GateToken 验证并解析 account_id / role_id
  -> 在局部调用域中执行:
       -> LoginLimiter.Acquire(ctx)
            -> enabled=false:直接返回 no-op permit
            -> Token Bucket.Allow
            -> 尝试获取并发许可
            -> 满载时登记 waiter 或拒绝 queue_full
            -> 等待许可 / wait_timeout / context cancellation
       -> defer permit.Release()
       -> ActivateRole
  -> Watch Role Actor
  -> RspHandShake
```

准入发生在身份验证之后,避免未认证请求消耗真实登录容量;GateToken 验证是本地计算,不触发本决策要保护的下游初始化。

QPS 令牌一旦消费便不退款。后续 `queue_full`、等待超时、context cancellation 或 `ActivateRole` 失败都表示一次真实登录尝试,退款会让重试洪峰绕过 QPS 预算。

许可覆盖且只覆盖 `ActivateRole`。调用方使用局部闭包或私有 helper 包围 Acquire、`defer permit.Release()` 和 `ActivateRole`,使许可在激活返回或 panic unwind 时立即释放;发送握手响应、SessionMgr 登记等轻量后续步骤不占用下游初始化并发额度。

### 并发闸门不变量

- buffered channel 容量等于 `max_inflight`;成功占位表示一个 inflight 登录。
- `queue_size` 只计算正在等待许可的请求,不包含 inflight。
- waiter 登记、撤销和 queue gauge 更新在同一互斥区内完成。
- 获取许可后先撤销 waiter,再增加 inflight。
- timeout 和 context cancellation 只撤销 waiter,不获得或释放不存在的许可。
- permit 的 `Release` 幂等,恰好归还一个许可并减少一个 inflight。
- disabled permit 的 `Release` 为 no-op。
- 第一版不承诺严格 FIFO;目标是有界削峰,不是公平排队。

### 拒绝语义

预期拒绝使用稳定的包级 sentinel error,不逐请求创建错误栈:

| 场景 | error text |
|---|---|
| QPS 超限 | `login rate limited` |
| 队列满 | `login queue full` |
| 等待超时 | `login queue timeout` |

`OnHandleClientMessage` 识别这些预期错误后调用现有 Session 停止路径并返回 nil,避免把容量拒绝作为 Actor 处理故障逐层包装和打印。第一版关闭 Session,不修改 `RspHandShake` 协议。`session_disconnects_total` 使用固定的 `login_rate_limited`、`login_queue_full`、`login_queue_timeout` 原因,不得使用原始错误文本作为标签。

context cancellation 计为 limiter `error`,撤销 waiter 后沿现有错误路径结束;`ActivateRole` 的业务错误不属于 limiter 结果。

### 可观测性

增加低基数指标:

| 指标 | 类型 | 标签 | 语义 |
|---|---|---|---|
| `login_inflight` | Gauge | 无 | 当前持有并发许可的登录数 |
| `login_queue_length` | Gauge | 无 | 当前等待许可的登录数 |
| `login_limit_total` | Counter | `result` | 每次 Acquire 的终态 |
| `login_wait_duration_seconds` | Histogram | `result` | 进入并发闸门后的等待时间 |

`login_limit_total.result` 固定为 `ok`、`rate_limited`、`queue_full`、`queue_timeout`、`error`。disabled bypass 计为 `ok`。`login_wait_duration_seconds` 只记录通过 QPS 检查并尝试并发闸门的请求,结果为 `ok`、`queue_full`、`queue_timeout` 或 `error`;QPS 拒绝没有等待阶段,不记录 histogram。

不增加 `role_id`、account、IP、协议名、错误文本等高基数标签。Gauge 只由 limiter 状态转换更新,不从 Session 侧重复推算。

## 验证

### Limiter 单元测试

- disabled 时返回 no-op permit,不消费 bucket、不改变 queue/inflight。
- 初始 burst 耗尽后稳定返回 `rate_limited`。
- inflight 未满时立即获得许可;Release 恰好归还一次。
- inflight 满时进入等待;已有 permit 释放后等待者获得许可。
- `queue_size = 0` 时立即 `queue_full`。
- waiter 达到 `queue_size` 后新增请求返回 `queue_full`。
- 可控 timer 触发后返回 `queue_timeout`,queue/inflight 不泄漏。
- context cancellation 返回 `error`,queue/inflight 不泄漏。
- 并发测试和 race detector 验证 inflight 从不超过 `max_inflight`。

测试注入 fake bucket 和可控 timer channel,禁止依赖 `time.Sleep`。

### 配置与 Gateway 集成测试

- exact decode 覆盖合法配置、disabled、零队列、缺失字段、未知字段及全部非法数值。
- limiter 必须在网络模块和 Session 生成前完成注入。
- token 验证失败不调用 limiter。
- limiter 拒绝不调用 `ActivateRole`,使用固定原因关闭 Session。
- Acquire 成功后,`ActivateRole` 成功和失败均释放 permit。
- Counter、Histogram 和两个 Gauge 的每条路径增量准确且无泄漏。

## 后果

正面影响:

- 单节点登录启动速率、并发和等待内存均有明确上限。
- QPS 与并发保护互补:前者削峰,后者直接约束昂贵初始化。
- 复用 ADR-0004 已交付的并发安全 Token Bucket,不维护第二套速率算法。
- 队列、许可和指标封装在一个 Gateway 深模块中,Session 只持有 Acquire/Release 契约。

代价与风险:

- 等待期间 Session Actor 会阻塞到许可、超时或 context cancellation;队列上限同时限制该数量。
- 容量按 Gateway 节点独立计算,扩缩容会改变全服近似容量。
- 第一版拒绝后直接断开,客户端只能通过断线和服务端稳定原因识别繁忙。
- channel 等待顺序不作为公平性契约。

## 否决方案

1. **把组合 limiter 放入 `core/gxylimit`**:当前只有 Gateway 需要并发队列语义,提前泛化会把业务错误、指标和许可生命周期泄漏到基础设施层。
2. **每 Session 独立 limiter**:无法限制节点总 QPS或总并发,不保护共享下游。
3. **限流放在 Role 登录 Handler 后**:Role Actor 和依赖初始化已经发生,削峰过晚。
4. **身份验证前消费 QPS**:未认证请求可耗尽真实玩家容量;当前本地 token 验证成本低且不触发下游初始化。
5. **无限队列或 `queue_size = 0` 表示无限**:登录洪峰会转化为不可控的内存和等待积压。
6. **退款 QPS 令牌**:队列拒绝或下游错误的快速重试可绕过速率预算。

## 非目标

- Redis 或其他跨 Gateway 协调
- 全服排队号、预计等待时间和严格 FIFO
- 运行时热加载及迁移已有等待者
- 修改握手协议返回结构化繁忙错误
- 按账号、角色、IP 或设备维度限流
- 自动容量调优、重试或客户端退避策略
