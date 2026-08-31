# ADR 0004:Role 业务模块限流与降级

- 日期:2026-08-31
- 状态:**Accepted**
- 关联:Issue #35

## 背景

`RoleMain.HandleClientMsg` 在 protobuf 解码和角色状态检查后直接调用业务 Handler。单个客户端可以持续请求 `RoleFlower`、`RoleBasic`、`RoleGuild` 等模块,占用 Role Actor 和下游 PostgreSQL、Redis 及其他服务资源。当前没有按玩家、按业务模块隔离的请求预算,也没有启动配置控制的模块降级开关。

本决策只处理**单个玩家对单个业务模块的滥用**。跨玩家登录洪峰由 Issue #34 的 Gateway 登录限流负责;本决策不增加进程级或集群级业务限流。

## 决策

### 限流层级

每个 `RoleMain` Actor 为每个业务模块持有独立 Token Bucket:

```text
Role 1001 / RoleFlower -> 独立桶
Role 1001 / RoleBasic  -> 独立桶
Role 1002 / RoleFlower -> 独立桶
```

同一 Role 的不同模块不共享额度,不同 Role 也不共享额度。所有业务请求固定消耗一个令牌;第一版不支持按协议设置权重。

`ReqAccountLogin`、`ReqAccountLogout` 由 `RoleMain` 自身处理并绕过业务模块 guard。当前 `roleModules` 中注册的 `Bag`、`Basic`、`Public`、`Extra`、`Flower`、`Plot`、`Steal`、`MainTask`、`ResidentOrder`、`GM`、`Chat`、`Guild`、`Friend`、`Mail` 均纳入限流;没有客户端 Handler 的模块不会触发检查。

### 共享 Token Bucket 模块

在 `core/gxylimit` 提供并发安全的深模块,对业务调用者只暴露配置、构造和单次准入判断:

```go
type Config struct {
    Rate  float64
    Burst int
}

func NewBucket(config Config) (*Bucket, error)
func (b *Bucket) Allow() bool
```

实现使用单调时间补充令牌,令牌数不超过 `burst`。`rate` 的单位为令牌/秒并允许小于 1 的正数。模块内部负责锁和时间状态;调用者不直接读写令牌。

共享实现默认并发安全,以便 Issue #34 的 Gateway 全局桶后续复用。Role Actor 串行执行,因此这里只承担无竞争锁成本,不维护第二套无锁实现。

### Role guard

`src/apps/role/internal/logic` 内增加私有 `roleModuleGuard`,负责:

- 保存不可变的模块策略;
- 为当前 Role 创建每模块 Token Bucket;
- 按 `disabled -> Allow` 顺序返回 `ok`、`limited` 或 `disabled`;
- 为集成测试提供私有 bucket seam,不把测试时钟暴露给业务调用者。

降级开关使用显式 `disabled=true`,不复用 `enabled=false`。这样不会与 Issue #34 中 `login_limit.enabled=false` 表示“绕过限流”的语义冲突。

### 自动识别 owning module

`gxyutil.MsgHandler.AddHandler` 返回本次注册的 `MethodMeta`;`gxyactor.ActorBase.AddMsgHandler` 透传该返回值。已有调用者可以忽略返回值,其分发行为不变。

`RoleMain.initMsgHandler` 注册每个真实业务模块时,用返回的消息元数据建立:

```text
protobuf message type -> module.GetModName()
```

配置键、Handler 归属和指标标签统一使用 `GetModName()` 的结果,例如 `RoleFlower`。不维护手工协议名单或第二套模块名称转换。`RoleMain` 自身的 Handler 不加入该业务映射;未注册消息继续进入现有 dispatcher 错误路径,不伪装成限流拒绝。

## 配置注入与生命周期

启动链路为:

```text
roleApp.OnModInit
  -> 读取并校验 RoleLimitConfig
  -> 创建该 Role 的 guard 和模块桶
```

配置通过包级可替换变量注入(`SetRoleLimitConfig`,ADR-0001 模式),与 gateway `SetGateTokenVerifier` 先例一致:`roleApp.OnModInit` 严格校验后在 Actor kind 注册前调用 setter,`NewRoleMain()` 保持无参并在内部读取该变量。包级变量仅启动阶段写入、运行期只读;测试通过同包直接赋值或 `swapRoleLimitConfig` 临时替换并恢复。配置只在 Role 启动时读取;修改后通过重启 Role 生效。第一版不做热加载、运行时控制面或已有 Actor 的桶状态迁移。

## 配置契约

```toml
[role_limit.default]
rate = 10.0
burst = 20

[role_limit.modules.RoleFlower]
rate = 5.0
burst = 10

[role_limit.modules.RoleGM]
disabled = true
```

规则:

- `[role_limit.default]` 必须存在;部署模板默认使用 `rate=10.0`、`burst=20`。
- 未配置模块继承完整默认策略;`role_limit.default.disabled` 允许统一降级所有业务模块,未填写时为 `false`。
- 模块可只覆盖 `rate`、`burst` 或 `disabled`;未填写字段继承默认值,显式 `disabled=false` 可重新启用被默认策略降级的模块。
- 覆盖配置必须区分“未填写”和显式零值,不能用零值静默继承。
- `rate` 必须为有限正数;`burst` 必须为正整数。
- `disabled=true` 时,显式填写的非法 `rate` 或 `burst` 仍然导致校验失败。
- 不存在的模块名和被排除的 `RoleMain` 配置项导致启动失败,避免拼写错误静默失效。
- 未知配置字段、TOML 类型错误、缺失默认配置或任何最终无效策略均在 Actor 注册前阻止 Role App 启动。

同步更新 `build/template/config/role.toml.template`、`build/template/config/all.toml.template` 及其环境配置来源;生成的 `config/*.toml` 不直接编辑。

## 请求数据流

`RoleMain.HandleClientMsg` 的处理顺序为:

```text
解码 protobuf
  -> 检查 RoleState
  -> 查找 owning module
  -> 检查 disabled
  -> 消耗 Token Bucket
  -> 调用业务 Handler
  -> 封装响应
```

约束:

- Role 状态不允许处理的请求不消耗令牌。
- `disabled` 先于 Token Bucket;降级拒绝不消耗令牌。
- Token Bucket 放行后,即使业务 Handler 返回错误,本次请求仍然消耗令牌。限流控制进入 Handler 的请求量,而不是成功请求量。
- 限流或降级拒绝返回 Ack 并保持 Session 和 Role Actor 存活。
- 请求不排队、不等待,也不返回 `Retry-After`。

## Ack 协议

将 `protocol/client/ack.proto` 的 `code` 改为正式枚举:

```proto
enum AckCode {
    ACK_CODE_OK = 0;
    ACK_CODE_ERROR = 1;
    ACK_CODE_RATE_LIMITED = 2;
    ACK_CODE_MODULE_DISABLED = 3;
}

message Ack {
    AckCode code = 1;
    string id = 2;
    string reason = 3;
}
```

枚举与现有 `int32` 使用相同的 protobuf varint 线格式。服务端和 monorepo 客户端生成代码一次性迁移到强类型字段;现有 `Code:1` 改为 `ACK_CODE_ERROR`。

拒绝契约:

| 场景 | code | reason |
|---|---|---|
| Token Bucket 无令牌 | `ACK_CODE_RATE_LIMITED` | `rate limited` |
| 模块降级 | `ACK_CODE_MODULE_DISABLED` | `module disabled` |

`code` 是机器可读契约;`reason` 只用于展示和诊断,客户端不得解析其文本决定行为。

## 可观测性

增加:

```text
role_module_limit_total{module,result}
role_module_disabled{module}
```

- `role_module_limit_total.result` 只允许 `ok`、`limited`、`disabled`。
- `role_module_disabled` 是进程静态配置状态,值为 0 或 1。
- 现有 `client_requests_total.result` 同步使用 `ok`、`limited`、`disabled`、`error`、`ignored`。
- 不增加 `role_id` 标签,不暴露每个桶的令牌数,不允许请求数据成为标签。

限流和降级是预期控制流:不创建带栈错误,不逐请求打印 Error/Warn,避免攻击流量造成日志风暴;通过 Ack 和指标观测。

## 验证

### `core/gxylimit`

- 新桶恰好允许初始 `burst` 次,第 `burst+1` 次拒绝。
- 根据可控时钟和 `rate` 精确补充令牌,且不超过 `burst`。
- 支持 `rate < 1`。
- 拒绝非正数、NaN、无穷 `rate` 和非正 `burst`。
- 多 goroutine 同时调用时,总放行数不超过可用令牌。
- 时间测试不使用 `time.Sleep`。

### Role 配置与集成

- 默认继承和模块部分覆盖正确。
- 缺失默认值、未知模块、`RoleMain` 和非法数值阻止启动。
- 正常请求进入 Handler;limited/disabled 请求不进入 Handler。
- 超限和降级返回稳定 Ack 错误码并保持 Session。
- 同一 Role 的不同模块互相隔离,不同 Role 的同一模块互相隔离。
- `ReqAccountLogin`、`ReqAccountLogout` 绕过业务 guard。
- `RoleGM` 与其他业务模块使用相同路径。
- 指标只产生约定的低基数标签。
- Role 独立配置和 `all.toml` 配置均能解析。

实现完成后执行:

```bash
go test ./core/gxylimit
go test ./src/apps/role/...
make test
make lint
build/script/e2e_all.sh
```

E2E 使用正常配置验证登录、业务请求和持久化链路没有回归;令牌补充的精确时间行为由可控时钟单元测试验证。

## 后果

正面影响:

- 单个玩家只能耗尽自己的模块额度,不会连带限制其他玩家或其他模块。
- 模块归属来自真实 Handler 注册,新增协议不会依赖手工限流名单。
- 配置拼写和数值错误在启动前失败。
- 共享 Token Bucket 可供 Gateway 后续复用。
- 指标保持低基数,限流流量不会制造日志风暴。

代价:

- 每个活跃 Role 为有客户端 Handler 的模块保存一个小型桶状态。
- Role 串行路径使用并发安全 Bucket,承担一次无竞争锁开销。
- Ack 字段生成类型发生源码级变化,需要同时重新生成并迁移 server/client 调用者。
- 配置变更需要重启 Role,第一版没有即时控制面。

## 否决方案

1. **进程级或两级 Role 限流**:会让不同玩家争抢额度,并扩大配置与拒绝语义;等真实容量数据证明需要后再设计。
2. **通用 MsgHandler interceptor**:当前只有 Role 需要该行为,会为假想复用扩大通用分发接口。
3. **手工协议到模块映射**:新增 Handler 时容易漏配并与真实注册关系漂移。
4. **Redis/数据库共享桶**:Issue #35 不要求集群一致性,会增加延迟、故障面和运维依赖。
5. **`enabled=false` 表示模块降级**:与 Gateway 的 limiter bypass 语义相反,改用明确的 `disabled=true`。

## 非目标

- 跨 Role、进程级或集群级业务限流
- Gateway 登录速率限制和并发门控
- 动态配置热加载、管理接口或 GM 动态开关
- 请求队列、等待和重试提示
- 按协议权重或每协议独立桶
- Token Bucket 状态持久化
- 每 Role 指标或高基数标签
