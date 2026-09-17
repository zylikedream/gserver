# ADR 0008: Actor 运行时从 protoactor-go 迁移到 ergo

- 日期：2026-09-16
- 状态：**Proposed**
- 关联：ADR 0001 依赖注入、ADR 0006 Actor 定位与所有权 Fencing、ADR 0007 移除模块 Version 乐观锁

## 背景

底层 actor 运行时 `github.com/asynkron/protoactor-go` 已不再维护，继续依赖它意味着无人修复的缺陷和无人应答的兼容性问题。

耦合面实测（静态统计）：

| 项 | 数量 |
|---|---|
| import protoactor 的 Go 文件 | 15（`core/` 8 + `src/` 7，含 4 个 `_test.go`） |
| 用到的子包 | 3（`actor` / `remote` / `router`） |
| `core/gxyactor` 非测试代码 | 1910 行（总 3821，测试占 1911） |

`gxyactor` 之所以有这么大体积，根因是 **protoactor 只提供裸 mailbox 循环**：`actor.Actor` 接口仅有一个 `Receive(actor.Context)`，`actor.Context` 无版本化的消息头，也没有 `Call` 应答、进程句柄、监督回调、日志或追踪接口。项目为此手写补齐了生命周期派发、进程句柄、Init 参数传递、OTel 追踪注入、supervisor 决策、日志适配和 activator 状态机。

评估候选框架后选定 `ergo-services/ergo`：Erlang/OTP 思路的 Go 实现（`name@host` 节点、进程、监督树、Link/Monitor、EDF 序列化），MIT、零依赖、Go 1.21+、实发版本 `v1.999.330`（2026-09-04）。

## 决策

### 1. 替换范围

- `gxynode`（147 行）**保留**：它是配置驱动的装配根，无网络能力。ergo node 顶替的是 `gxyactor` 内的 `actor.ActorSystem + remote.Remote`。
- `gxymodule` / `gxyapp`（生命周期与装配）**保留**，**不引入** `ergo.app.Application`：后者与 `gxymodule` 职责重叠，引入会得到双份生命周期与两套失败语义。
- `gxyregistery` / `gxyservice`（服务注册与 kind 目录）**保留**，仅收窄职责。
- `gxytimer`（cron/补发）**保留调度语义**，定时器实现改由 ergo 承担。

### 2. 跨节点消息编码（wire）

采用**单一 envelope**，`MsgID` 复用仓库已有的 `core/gxynet/codec` 注册表：

```go
type WireEnvelope struct {
    MsgID string // "10002" 或 "ActorActive"，取自 codec 注册表
    Data  []byte // proto.Marshal 结果
}
```

不采用"为每个 pb 类型实现 `MarshalEDF`"。理由：envelope 只需注册 1 个类型、无生成代码、业务侧 `Send`/`Call` 签名不变，且实测固定开销约 32 字节（2KB 消息时 +1.6%）。

### 3. 节点身份

- ergo 节点名 = `${node.name}@${host}`（host 取 `POD_IP`），**稳定身份**。
- **移除** `NodeInstanceName` 中的 nanotime。ergo 用 `gen.PID.Creation`（节点启动时间戳）原生解决陈旧引用问题，其秒级精度不足以承担唯一性。
- redis lease 的 `nodeID` 直接用 ergo 节点名；`leaseToken` 改为**独立随机值**（不再与 nodeID 同值），`SetNX` 改为无条件 `SET` + token 校验。
- **必须成对修改**：nodeID 稳定后若 `leaseToken` 仍等于 nodeID，`claim` 会命中 `already_owned` 分支而**不递增 epoch**，导致新老世代共享同一 epoch，`role_actor_fence` 将放行旧 writer。`leaseToken` 独立是 ADR-0006 fencing 的前提。

### 4. 服务发现

分三层，各回答一个正交问题：

| 层 | 问题 | 载体 |
|---|---|---|
| 节点 → 地址 | ergo 节点名解析为 `ip:port` | 自定义 `gen.Registrar`，**读 Consul 的 `ServiceInfo.NodeHost`** |
| kind → 节点列表 | 哪些节点能承载 role | `gxyservice` + `ConsistentHashSelector`（现状保留） |
| id → 归属 | roleID 归哪个节点的哪个世代 | redis lease + epoch（ADR-0006，保留） |

- **不引入** ergo 的 `ApplicationRoute` / `ResolveApplication`：它无 `Host` 字段（一个节点上多个 HTTP 服务端口各异，`Node` 粒度表达不了）、无 `Draining` 语义、无按 key 哈希的原语；且 `gxyservice` 因承载 4 个 HTTP 端点的发现而**无论如何都要保留**，迁入 `ApplicationRoute` 只会形成两套机制。
- 不新增给 ergo 用的独立节点记录：`gen.Registrar.Register` 不额外写记录，`Resolver.Resolve` 读 `gxyservice` 已有的 `ServiceInfo.NodeHost`。**全系统只有 `gxyservice` 一个组件写 Consul。**
- actor 端口可交由系统分配（`AcceptorOptions{Port: 0}`），端口随 `NodeHost` 一并落 Consul，因此**不需要固定端口约定，也不需要端口表**。
- 不采用静态路由：`AddRoute` 是每节点本地的，且静态路由**独占**（匹配后不再回退发现，失败即 `ErrNoRoute`），线上横向扩容需要所有已有节点改配置。

### 5. Actor 门面

`core/gxyactor` 重写，**删除约 900 行补偿代码**（生命周期派发、追踪注入、Init 参数传递、进程句柄、supervisor 决策、日志适配、activator 状态机中依赖 protoactor 竞态的部分）。

保留不动：`gxyutil.MsgHandler`（按消息类型反射派发）、`gxytimer` 调度语义、`SetLogValue`/`Context()`、metrics 埋点、`actor_locator.go`（ADR-0006）。

新增：`ctx` 串联适配层（ergo 回调签名无 `ctx`，而 `gxylog` 依赖它）、`gen.Logger` 实现、`HandleCall` 的 error 语义转换。

### 6. 子进程命名与激活

采用 **`node.SpawnRegister`**，不使用 supervisor 承载 role actor：

- 竞态良性：并发 `SpawnRegister` 同名时败者的 `Init` 与 `Terminate` **都不执行**（实测 6 并发 → 1 胜 5 `ErrTaken`，零多余 Init/Terminate）。
- 名字在 `Init` **之前**由框架原子注册，`Init` 期间到达的消息排队而非失败。
- 作为对照，`SimpleOneForOne` 的 `StartChild` 走 `Spawn` 路径**不注册名字**（`act/supervisor_sofo.go` 从不设置 `register`），需子进程在 `Init` 内自注册，从而引入"重复激活会跑 `Terminate`"的副作用——而 `lockRoleActorFence` 的 WHERE 条件是 `node_id + epoch`，**同节点重复激活会放行**，唯一防护是 `save()` 的 dirty 检查。

初始化拆分：`Init` 只做纯内存校验，全部 DB 操作移入异步初始化（原 `DelayInit`）：

- `Init` 不再阻塞 activator，激活吞吐不再受加载耗时影响。
- 顺序保证：`Init` 期间自投的异步初始化消息排在 mailbox 首位，而调用方要等 `SpawnRegister` 返回才拿到 PID，因此异步初始化必然先于业务消息被处理。
- 副作用：异步初始化失败时 `SpawnRegister` 已返回成功，调用方拿不到失败原因。

### 7. 失败语义

- 详细失败原因（fence 丢失、模块加载失败、账号不存在）**只进日志与指标**，不跨进程传给调用方。
- 调用方按**错误类型**而非**失败原因**决策：`ErrProcessUnknown` → 服务端重试 1–2 次；`ErrNoConnection` / `ErrRetryExhausted` → 失败。
- 失败原因的分类只服务于运维定位（`gxymetrics.RoleLogins` 增加 `load_failed` / `fence_lost` 等取值）。
- 不建墓碑、不做激活前账号校验预检。

### 8. Activator 形态

- N 路分片以保持 `Claim` 的顺序性，由 supervisor（`OneForOne` + `Transient`）管理，分片按 `${kind}_activator_{shard}` 稳定注册名部署。
- **角色 actor 必须用 node 级 `SpawnRegister` 创建**，不用 `process` 级：否则 activator 崩溃会级联杀掉其名下全部角色。

### 9. 可观测性

- **追踪**：实现 `gen.TracingBehavior` 适配器，把 `gen.TracingSpan` 转成 OTel span 送入现有 OTLP exporter（Tempo 链路不变）。删除 `gxyactor` 的手写传播代码（`injectTrace` / `messageEnvelopeCarrier` / `readonlyHeaderCarrier` / `IUnspanMessage`）与 `docs/architecture/tracing.md` 记录的 protoactor 绕行方案。业务侧 `Span().SetName(%T)` 可删（ergo 自动带消息类型名），`SetAttributes` 改 `SetTracingSpanAttribute`。
- **指标**：用 `metrics.Options{Shared, Mux}` + `prometheus.Gatherers` 把 ergo 运行时指标并入**单一 `/metrics`**，对外端点与 k8s 探针不变。
- **日志**：写 `gen.LoggerBehavior` 实现（约 40 行）替换 protoactor 的 `slog.Handler` 适配，`gxylog` 主体与业务调用点零改动。
- **cron**：改用 ergo 形式（5 段 crontab，Sunday=7，最小粒度分钟）；`RestoreCron` 的补发语义作为应用层计算保留（纯调度回溯，已用 ergo 复现）。

### 10. 定时与 lease 的形态

- lease 心跳**保持 goroutine**（`gxymodule` 生命周期内），不 actor 化：自 fence 是节点级决策（`gxylog.Fatal` → 进程退出），且续约是阻塞 I/O，actor 化会让 deadline 安全网排在 mailbox 之后而迟到。
- meta-process 不适用：其 `Spawn` 只接受 `MetaBehavior`，**无法创建普通进程**。

## 后果

### 正面

- 去掉不再维护的依赖；`gxyactor` 从 1910 行降到约 700–900 行。
- 激活路径不再被 `Spawn`/`Init` 分离的竞态驱动，`pending` / `waiters` / `Touch(10s)` / `30s` 请求超时这一整套会合机制可以删除。
- 获得原生能力：跨节点追踪与采样、`PreserveMailbox`、消息分片、每进程压缩、`SendImportant`/RR-2PC 投递语义、`ErrProcessIncarnation` 陈旧 PID 检测、mailbox 延迟与每类型编解码统计（`-tags=latency` / `-tags=typestats`）、Observer UI、mTLS。
- 只有 `gxyservice` 写 Consul；ergo 侧节点发现零外部依赖、零 keep-alive。

### 风险与约束

- **`NetworkFlags` 必须从 `gen.DefaultNetworkFlags` 派生**。写结构体字面量会静默关闭未列出的能力（`EnableTracing`、`EnableSoftwareKeepAlive` 等）。
- **`gen.LogLevelPanic` 不得映射到 `gxylog.Fatal`**：前者是"框架捕获了 panic"，后者会 `os.Exit(1)`，映射错误会让单个 actor panic 终止整个进程。
- **必须显式 `Log.DefaultLogger.Disable = true`**，否则 ergo 默认 logger 与 `gxylog` 的 console 输出重复。
- **metrics actor 的 `Path` 不得与 `/metrics` 重名**，`http.ServeMux` 重复注册会 panic。
- **角色 actor 不得挂在 activator 之下**（node 级 spawn）。
- **`leaseToken` 必须独立于 `nodeID`**（见决策 3）。
- 迁移期**不能新旧节点混跑**：protoactor remote 用 gRPC（`remote.proto` 的 `Remoting` service），ergo 用裸 TCP + ENP/EDF 帧，传输层协议不同。必须停服切换。
- 同 host 上 ergo 节点名必须唯一（换端口无效，实测 `resource is taken`），且 host 部分须为可解析的 FQDN/IP 且不能带端口。

### 未验证

- 单 supervisor 承载数十万子进程的可扩展性（分片形态下压力已分散，但仍未压测）。
- 动态端口（`Port: 0`）与 `gxyservice` 注册的时序对齐：`gen.Registrar.Register` 发生在 `StartNode` 时，而 `gxyservice` 的服务注册在 `OnModStartAfter`，实现时需保证 `NodeHost` 里的端口与实际监听一致。
- k8s pod 间 UDP 可用性不再重要（本方案不依赖 ergo 内置 registrar），但若未来改用内置 registrar 需重新评估。

## 拒绝的方案

### 继续使用 protoactor-go 并自行 vendor 打补丁

需长期自持一个无上游的运行时，且 `gxyactor` 为补齐其缺失能力而写的手工代码仍要维护。

### 替换为其他 actor 框架

候选（`anthdm/hollywood`、`goakt`、NATS 派生 actor、Dapr actors、gRPC + 分片）在远程传输、监督语义或 protobuf 处理上均需重新评估，未取得优于 ergo 的证据。

### 为每个 protobuf 类型实现 EDF `MarshalEDF`

需为 160 个类型生成约 1000 行适配代码，且注册 160 个类型。相比 envelope 只省约 29 字节/消息。可作为后续对热点类型的增量优化（`RegisterType` 按类型分派，与 envelope 不冲突）。

### 用 ergo 的 `ApplicationRoute` 承载 kind 目录

见决策 4：缺少 `Host` 与 `Draining`，且 `gxyservice` 无法删除。

### 用静态路由替代节点发现

静态路由独占且 `AddRoute` 为节点本地，横向扩容需要所有节点改配置。见决策 4。

### 用 `SimpleOneForOne` + `StartChild` 承载角色

需要串行化以避免重复 spawn（`StartChild` 同步阻塞 supervisor mailbox），且子进程需自注册名字，从而依赖"`RegisterName` 必须在 `Init` 第一行"和"必须吞掉 `StartChild` 的 error"两条编码纪律。`node.SpawnRegister` 的良性竞态消解了这两条约束。

### 为异步初始化失败建墓碑

调用方无法也不必处理每一种底层失败原因；墓碑会引入状态与过期清理。见决策 7。

### 将 lease 心跳 actor 化

见决策 10。
