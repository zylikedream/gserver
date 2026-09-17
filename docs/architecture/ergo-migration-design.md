# ergo 迁移设计

- 日期：2026-09-16
- 关联：ADR 0008 Actor 运行时从 protoactor-go 迁移到 ergo
- 版本基线：`ergo.services/ergo v1.999.330`（文档自称 3.3.0，模块路径不带 `/v3`）

本文记录迁移的落地细节、实测证据与实现顺序。决策理由见 ADR 0008。

## 一、已实测的 ergo 语义

以下结论均在本机对 `v1.999.330` 实测得出，不是文档推断。

### 进程注册名

```text
"role/1001" / "guild/12/member/7" / "Role:1001"   → 合法
gen.Atom 长度上限 255 字节                          → 256 报 "too long Atom (max: 255)"
同节点重复注册                                      → "resource is taken"
Kill owner 后重新注册                               → 成功（名字随 owner 终止释放）
一个进程只能有一个名字                              → 第二次 RegisterName 报 taken；UnregisterName() 无参数
```

**注册名唯一性只在单节点内成立**：两个节点可各自 `SpawnRegister("role/1")` 且都可寻址（`Call` 分别返回各自节点的结果）。这是 ADR-0006 的 redis lease 必须保留的直接依据。

### 名字可见性与初始化

```text
SpawnRegister 期间（Init 尚未返回）→ 名字已可通过 ProcessPID(name) 查到
SpawnRegister 返回后立刻 Call       → 成功（Init 期间到达的消息排队）
6 goroutine 并发 SpawnRegister 同名 → 1 胜 5 ErrTaken，仅 1 次 Init，0 次多余 Terminate
Init 返回 error / panic            → 作为 SpawnRegister 的错误返回
```

`ProcessPID(name)` 是 O(1)（`node/node.go` 内 `n.names.Load(name)`），不是扫描。

### 错误语义

| 目标位置 | 目标状态 | `Send` | `Call` | `CallImportant` |
|---|---|---|---|---|
| 本地 | 名字不存在 | 立即 `ErrProcessUnknown` | 立即 `ErrProcessUnknown` | 立即 `ErrProcessUnknown` |
| 本地 | 名字存在 | nil | 正常 / 超时 | 正常 / 超时 |
| 远程 | 名字不存在 | nil（静默） | **超时** | **立即 `ErrProcessUnknown`** |
| 远程 | 节点不可达 / 从未存在 | nil | 超时 | **立即 `ErrNoConnection` / `no route`** |

- `ProcessID` 不填 `Node` → `no route`，**不回退本地查找**。
- 跨节点诊断必须用 `CallImportant`；本地发送即使 `Send` 也能立即拿到 `ErrProcessUnknown`。

### 生命周期与关停

```text
SpawnRegister / StartChild / AddChild  → 同步阻塞到 Init 返回
SendExit(pid, reason)                  → 异步，0s 返回，进程仍在
Kill(pid)                              → 异步，0s 返回
MonitorPID 的 MessageDownPID            → 在 Terminate 完成【之前】到达（进程先出表，再跑 Terminate）
node.Stop()                            → 等待全部 Terminate，并行执行（3×400ms → 401ms）
```

**推论：`Down` / `HandleChildTerminate` / 进程表消失都不是"已存盘"屏障。** 需要存盘屏障时必须自建 flush + ack 协议；且 drainer 不能在 `HandleMessage` 里同步等待 ack（会堵死自己的 mailbox，ack 进不来），必须写成异步状态机。

### Link / Monitor

```text
A.LinkPID(B) → B 终止则 A 终止；A 终止不影响 B（单向，与 Erlang 的双向不同）
MonitorPID    → 收到 MessageDownPID，自身继续运行
```

### 节点命名

```text
"gate@localhost" + "role@localhost"       → 均 OK（名字不同）
"role@localhost" ×2                       → 第二个 "unable to register node: resource is taken"（换端口无效）
"gate@no-such-host-xyz"                   → lookup 失败
"gate@127.0.0.1:15441"                    → lookup 失败（host 部分不能带端口）
"gatenohost"                              → "incorrect FQDN node name (example: node@localhost)"
"gate@127.0.0.1" / "gate@<os.Hostname()>" → OK
```

### `gen.PID.Creation`

```text
节点启动时间戳，内嵌于每个 PID / Ref / Alias
精度为【秒】：同秒启动的 3 个节点只有 1 个不同的 Creation 值
NodeInfo 不暴露该字段，只能从 pid.Creation 或 node.PID() 取
node/Network().Info() 与 node.Info() 均为 (值, error) 双返回
```

**跨节点发送时 Creation 不符会直接返回 `ErrProcessIncarnation`，不发包。** 但秒级精度不能承担实例唯一性（见 ADR 0008 决策 3）。

### 配置陷阱

`gen.DefaultNetworkFlags` 只在 `Flags.Enable == false` 时被代入。一旦写出 `Flags: gen.NetworkFlags{Enable: true, ...}` 字面量，未列出的字段**全部为 false**（会静默关闭 `EnableTracing`、`EnableSoftwareKeepAlive` 等）。正确写法：

```go
flags := gen.DefaultNetworkFlags
flags.EnableRemoteSpawn = false // 只改需要改的
options.Network.Flags = flags
```

`StartNode` 需要 `NodeName@host` 且 host 可解析；`AcceptorOptions.Port` 为 `uint16`，传 `0` 表示系统分配。

### 其他

```text
简单 registrar 可用：Register 为 no-op，Resolve 返回 {Host:"", Port:N}
  → ergo 在 Host 为空时用 name.Host() 填充（node/network.go）
  → Resolve 返回的 Route 必须带 HandshakeVersion/ProtoVersion，否则连接失败并报 "no route"
GetConnection 先查 connections 缓存，仅在未连接时调用 Resolver.Resolve
SimpleOneForOne 的 StartChild 不注册名字（act/supervisor_sofo.go 从不设置 register）
OneForOne / AllForOne / RestForOne 的 spec.Name 会注册到节点（cs.register = true）
Supervisor 的 HandleMessage 返回非 nil error 会终止 supervisor 自身
AddChild 可在 OneForOne 运行时追加命名子进程（源码 act/supervisor_ofo.go 的 childAddSpec 设
  cs.register = true；本轮未做端到端验证，OFO 要求 Init 时至少一个 child spec，
  且占位 child 正常终止会触发 supervisor auto-shutdown）
meta-process 的 Spawn 只接受 MetaBehavior，无法创建普通进程
```

### protobuf 与 EDF

```text
edf.RegisterTypeOf(wrapperspb.StringValue{}) → "struct StringValue has unexported field(s)"
  （protoc-gen-go 生成类型含 state / unknownFields / sizeCache 私有字段）
edf.RegisterTypeOf(WireEnvelope{Type string; Data []byte}) → <nil>
方案 A 双节点实测：真实 pb 载荷经 envelope 跨节点 Call 往返成功，MsgID 取自 codec 注册表
方案 C（每类型手写 MarshalEDF 直接写 protobuf bytes）单类型实测可行，无需注册 pb 类型
```

### codec 注册表覆盖度

```text
core/gxynet/codec/protobuf.go 的 init() 扫描 package galaxy.protocol 下全部 message
  → 有 msg_id（proto 扩展 60101）的用十进制数字 ID 注册（130 个）
  → 无 msg_id 的内部消息用类型名注册（30 个，如 ActorActive / ActorError / ActorLocateRetry）
实测解析率 160 / 160
双向可用：MessageMetaByMsg(msg).ID 编码；MessageMetaByID(id).NewInstance() 解码
注意：RegisterMessageMeta 重复注册会 panic；该包必须被真实 import 否则 init 不执行
```

### envelope 实测开销

| 消息 | proto 字节 | EDF wire 字节 | 开销 |
|---|---|---|---|
| `Ack` | 0 | 30 | +30 |
| `ReqHandShake` | 17 | 49 | +32 |
| `RspHandShake` | 15 | 47 | +32 |
| `ActorError` | 12 | 49 | +37 |
| `RspHandShake`（512B 字符串） | 518 | 550 | +32（+6.2%） |
| `RspHandShake`（2KB 字符串） | 2054 | 2086 | +32（+1.6%） |

开销与消息大小无关，固定约 32 字节。

### 性能实测

```text
node.Spawn（无名字）                    avg =  4µs
node.Spawn + Init 内 RegisterName       avg = 14µs   （RegisterName 净成本约 10µs）
node.SpawnRegister                      avg =  4µs
supervisor StartChild × 200（批量）      每次 =  3µs
名字长度 8 vs 200 字符                    无显著差异
```

`RegisterName` 不是瓶颈。旧实现中"激活耗时长"的量级来自 `DelayInit` 内的 DB 往返（1× 账号校验 + 1× fence UPSERT + 14× 模块加载）。

## 二、代码组织与职责映射

### `core/gxyactor` 的删改

| 现有符号 | 行数 | 处置 |
|---|---|---|
| `Receive` + `doReceive` | 66 | 删（ergo 直接回调 handler） |
| `initSpan` / `readonlyHeaderCarrier` / `messageEnvelopeCarrier` / `injectTrace` / `IUnspanMessage` / `unspanMessage` | 77 | 删（原生追踪） |
| `ActorContext` / `ContextDecorator` / `ActorInitMsg` / `ActorTimerMsg` | 18 | 删（`Spawn(factory, opts, args...)`） |
| `Self` / `Sender` / `Stop` / `respond` | 18 | 改 `PID()` / handler `from` 参数 / `return` |
| `newSupervisor` / `decider` | 7 | 改 `SupervisorSpec` |
| `logger.go`（slog 适配） | 68 | 重写为 `gen.LoggerBehavior`（约 40 行） |
| `activator_manager.go` + `actor_mgr.go` | 667 | 重写（约 100–150 行，仅保留 lease 与分片调度） |
| `newSystem` / `Address` / `StopActor` / send 四件套 | ~62 | 改 ergo node |
| `actor_locator.go` | 418 | 保留（仅改 `leaseToken` 来源与 `SetNX`） |
| `gxyutil.MsgHandler` | — | 全部保留 |

### 业务侧调用点改造

| 调用点 | 生产 | 测试 | 处置 |
|---|---|---|---|
| `Timer()` | 17 | 19 | 不变 |
| `Sender()` | 17 | 26 | 改用 handler 的 `from` 参数 |
| `Self()` | 13 | 22 | 改用 `PID()` |
| `ActivateActor` | 4 | 0 | 改 `SpawnRegister` / `StartChild` |
| `PidEqual` | 3 | 0 | 改 `gen.PID` 比较（含 `Creation`） |
| `CallSync` | 1 | 2 | 改 `Call` + 显式 sender |
| `AutoHandleMsg` | 2 | 0 | 改响应机制 |
| `GetLocalActor` | 2 | 0 | 改 `ProcessRangeShortInfo` |
| `Respond` | 2 | 1 | 改 `return` |
| `AddMsgHandler` | 1 | 0 | 不变 |
| `Span()` | 7 | — | 删 `SetName`（冗余）；`SetAttributes` → `SetTracingSpanAttribute` |

`Sender()` + `Self()` 合计 30 处生产调用是主要机械工作量。

### 进程归属

| 进程 | 归属 | 理由 |
|---|---|---|
| role / guild / chat_channel actor | 游离（node core 持有） | 有 owner 语义与空闲自毁；**不得挂 activator 之下** |
| activator 分片 | supervisor（`OneForOne` + `Transient`） | 无状态但需自愈；需稳定注册名 |
| lease 心跳 | `gxymodule` 内 goroutine | 节点级决策 + 阻塞 I/O 不能排队 |
| k8s 健康探测 | `gxyservice` 5s ticker | 只读外部 API |
| Consul 注册保活 | `gxyservice` | 同上 |

判定规则：需要与其它 actor 交互或有需串行访问的自有状态 → actor；承担节点级决策、有不可排队的阻塞 I/O、或只是读一个 atomic → 不是 actor。

### 服务发现数据分布

| 记录 | 写入方 | 内容 | 消费方 |
|---|---|---|---|
| Consul `ServiceInfo`（actor kind） | `gxyservice` | `NodeName` + `State` + `NodeHost`（ergo 地址） | `ConsistentHashSelector`（仅首次激活） |
| Consul `ServiceInfo`（HTTP 端点） | `gxyservice` | `NodeName` + `Host`（HTTP 地址） | HTTP 调用方（现状不变） |
| redis owner | `actorLocator` | `nodeID \| epoch \| leaseToken` | 激活与 fence（ADR-0006） |

`gen.Registrar` 适配只需约 80 行：`Register` 为 no-op（不额外写记录），`Resolver().Resolve` 读 `ServiceInfo.NodeHost` 拆成 `{Host, Port}` 并补 `HandshakeVersion`/`ProtoVersion`，其余方法返回 `ErrUnsupported` 且 `RegistrarInfo` 能力标志如实置 false。

### 激活链路

```text
ActivateRole(roleID)
  ├─ locate(roleID)
  │    ├─ 有 owner → owner.NodeID 即 ergo 节点名 → 直接 CallImportant（不经 Consul）
  │    └─ 无 owner → ConsistentHashSelector 从 kind 记录选候选节点
  │         → 发给该节点分片 activator：{kind}_activator_{hash(id)%N}
  │
  └─ 候选节点侧 activator
       ├─ Claim（redis Lua，blocking I/O —— 分片的原因）
       ├─ node.SpawnRegister("role/"+id, factory, opts, id, owner)   ← node 级
       └─ 返回 PID

调用方发送业务消息（CallImportant）
  ├─ 成功
  ├─ ErrProcessUnknown → 服务端重试 1–2 次（含重新激活）
  └─ ErrNoConnection / ErrRetryExhausted → 失败
```

activator 内部消化 relocate 循环上限沿用 `actorLocateMaxAttempts = 3`。`ErrRelocate` 不冒泡给客户端。

分片数 N 待定：`Init` 已降为纯内存操作，activator 的串行瓶颈只剩 `Claim` 的 redis 往返，N 应由该往返耗时与预期登录峰值倒推，而非照搬旧实现的 5 路池。

## 三、实现顺序

1. **wire envelope**：`WireEnvelope` + `Pack`/`Unpack`，节点启动时 `Network().RegisterType`。
2. **`consulRegistrar`**：`gen.Registrar` 适配（`Register` no-op、`Resolve` 读 Consul）。
3. **身份改造**：`nodeID` = ergo 节点名、`leaseToken` 独立随机、`SetNX` → `SET`；`actor_locator` 与相关测试同步。
4. **`gxyactor` 门面重写**：`ActorBase` → `act.Actor` 适配（含 `ctx` 串联、error 语义转换、metrics 埋点搬移、`gen.Logger`）。
5. **activator 重写**：分片 + supervisor + `node` 级 `SpawnRegister`；删除 `pending`/`waiters`/`Touch`/`actor_mgr`。
6. **`Init` 拆分**：`Init` 纯内存，DB 移入异步初始化。
7. **业务侧替换**：`Sender()` / `Self()` / `PidEqual` / `ActivateActor` 等 30+ 处。
8. **可观测性**：追踪适配器、metrics 合并、日志、cron 迁移。
9. **文档更新**：`docs/architecture/` 下 `actor-system.md`、`tracing.md`、`service-discovery.md`、`actor-init-race.md`、`networking.md`、`overview.md`、`docs/blog-actor-model-game-server.md`、`README.md`、`AGENTS.md` 中 protoactor 相关描述；`tracing.md` 与 `docs/bugfix/issue-protoactor-go-endpointwriter.md` 可归档。

## 四、验证要求

- wire：双节点真实 pb 载荷往返（已先行验证方案可行性）。
- 激活：并发同名激活仅产生一个实例；`Init`/`Terminate` 不重复执行。
- ownership：跨节点接管后旧 epoch 的保存被 `role_actor_fence` 拒绝。
- 关停：`node.Stop()` 等待全部 `Terminate`；存盘屏障由显式 flush + ack 协议保障。
- 可观测性：`/metrics` 单端点同时含业务与 ergo 指标；Tempo 中可见跨节点 trace。
- 时序：`ServiceInfo.NodeHost` 中的端口与 ergo 实际监听端口一致。
