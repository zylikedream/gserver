# ergo 迁移实测记录

- 日期：2026-09-16（2026-09-17 改名自 `ergo-migration-design.md`）
- 关联：ADR 0008（原文档的决策已拆分为 ADR 0009–0013）
- 版本基线：`ergo.services/ergo v1.999.330`（文档自称 3.3.0，模块路径不带 `/v3`）

本文记录迁移的落地细节、实测证据与实现顺序。

> **本文件零约束力。** 它记录的是"当时的理由与实测结果"，不是规则。有约束力的只有 `invariants.md` 与状态为 `Accepted` 的 ADR（见 `invariants.md` 效力规则 1）。引用本文内容压设计前，必须对代码或 ergo 源码重验一遍。

> **前提（本项目部署事实，非 ergo 保证，零约束力）**
>
> 下列是迁移设计依赖的**本项目部署事实**，不是架构不变量，因此不进 `invariants.md`。它们失效时，上表相关不变量的**承载**需重估：
>
> - **A 同一节点名至多一个活进程**（编排约束，一名一实例）。注意换用自定义 registrar 后，运行时**不再**替我们拒绝重名——默认 registrar 才返回 `ErrTaken`（`ergo-foundation.md` §5）。这是不变量 #6 的 `SetNX` 成为承重项的原因；失效则重估 #5（身份分层是否还有必要）、#6。
> - **B 编排器会重启启动失败的实例**（k8s 默认 `restartPolicy: Always`，`deploy/k8s/` 未显式覆盖）。这是不变量 #6 以"获取失败即启动失败"为承载的前提——失败必须能被自动吸收；失效则重估 #6。

## 一、已实测的 ergo 语义

以下结论来自本机对 `v1.999.330` 的实测与文档核对。**与本文件不一致之处，以 `ergo-foundation.md`（源码勘测，带行号）为准**——本节的"配置陷阱""其他"两节已按该勘测纠正过三处（端口分配、Flags 代入链、静态路由版本补全）。

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

**推论：`Down` / `HandleChildTerminate` / 进程表消失都不是"已存盘"屏障。** 这是所有权释放不能依据死亡通知的原因——释放必须放在 actor 自己的终止路径内、且在落库之后（不变量 #4），而不是靠外部观察进程消失来推断。

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

**跨节点发送时 Creation 不符会直接返回 `ErrProcessIncarnation`，不发包。** 秒级精度不足以单独承担实例唯一性：其碰撞由"租约仅当不存在时写入"（不变量 #6）与每实例随机令牌共同吸收。

### 配置陷阱

`gen.DefaultNetworkFlags` 的代入分两级，不能一概而论（勘测见 `ergo-foundation.md` §0）：

- **网络级**：仅当 `Flags.Enable == false` 时**整体替换**为 `DefaultNetworkFlags`。因此写 `Flags: gen.NetworkFlags{Enable: true, ...}` 字面量会让未列出项保持 false（会静默关闭 `EnableTracing`、`EnableSoftwareKeepAlive` 等）。
- **acceptor 级**：另有代入链（handshake 的 flags → 网络级 options.Flags → `DefaultNetworkFlags`），即使字面量 `Enable=false` 也会被逐级代入。

正确写法（改一项时从默认值派生）：

```go
flags := gen.DefaultNetworkFlags
flags.EnableRemoteSpawn = false // 只改需要改的
options.Network.Flags = flags
```

`StartNode` 需要 `NodeName@host` 且 host 可解析；`AcceptorOptions.Port` 为 `uint16`，传 `0` **不是**系统随机端口——它被改写为 `gen.DefaultPort`(11144) 后按 `PortRange` 向后扫描，实际端口须经 `Acceptors()` 读取（勘测纠正见 `ergo-foundation.md` §0）。

### 其他

```text
简单 registrar 可用：Register 为 no-op，Resolve 返回 {Host:"", Port:N}
  → ergo 在 Host 为空时用 name.Host() 填充（node/network.go connect()）
  → Resolve 返回的 Route 必须自带 HandshakeVersion/ProtoVersion；不填则 connect() 找不到
     handler，最终以 gen.ErrNoRoute 冒泡（实测）
     【与静态路由的差异】AddRoute 会给未填的版本补 n.defaultHandshake/defaultProto，
     所以文档说静态路由的版本"不设则用默认"是对的；Resolver 返回的 Route 走
     gen.NetworkRoute{Route: route} 直接构造，无此补默认步骤，两者不可类推
GetConnection 先查 connections 缓存，仅在未连接时调用 Resolver.Resolve
SimpleOneForOne 的 StartChild 不注册名字（act/supervisor_sofo.go 从不设置 register）
OneForOne / AllForOne / RestForOne 的 spec.Name 会注册到节点（cs.register = true）
Supervisor 的 HandleMessage 返回非 nil error 会终止 supervisor 自身
AddChild 可在 OneForOne 运行时追加命名子进程（源码 act/supervisor_ofo.go 的 childAddSpec 设
  cs.register = true；本轮未做端到端验证，OFO 要求 Init 时至少一个 child spec，
  且占位 child 正常终止会触发 supervisor auto-shutdown）
meta-process 的 Spawn 只接受 MetaBehavior，无法创建普通进程
```

### 追踪（据文档，未逐项实测）

```text
TracingFlags：Send（Sent 观测）/ Receive（Delivered + Processed，业务 span 也走它）
              / Procs（Spawn + Terminate）/ Inherit（子进程继承）
TracingKind ：Send / Request / Response / Spawn / Terminate
采样器      ：TracingSamplerAlways / Ratio(f) / RateLimit(n)；仅在没有活动 trace 时被咨询
不被追踪    ：SendExit（控制面）、SendEvent（避免扇出风暴）、SendAfter（定时动作，
              每次触发是独立采样起点）
Trace 级别  ：不能在运行期用 SetLevel 打开，只能启动时经 NodeOptions.Log.Level 或
              ProcessOptions.LogLevel 设置（防误操作刷爆存储）
```

后三条影响追踪适配器的预期：跨 actor 的链路是完整的，但控制面与事件路径不在其中，排障时不要据此判定"消息未送达"。

**实现后追加（2026-09-19 实测）：**

```text
采样器分两级，只设节点级得不到任何 span（实测踩到）：
  node.SetTracingSampler      → 仅对"节点自身发起"的 Send/Call 生效（n.tracingSampler）
  process.SetTracingSampler   → 进程发起消息时读的是 p.tracingSampler，逐进程设置
  → 适配时在 actor 初始化段对每个 actor 设置进程级采样器，否则业务链路全空
Init 阶段不产生新 trace（propagatingTrace 见状态为 Init 即返回空）
  → 初始化期间的自投消息不产生 span；纯本地激活（SpawnRegister）本身也不产生消息，
    因此"建一个本地 actor"这类路径不产生任何 span，这不代表适配器失效
Span 标识是 [2]uint64（128 位）+ uint64（64 位），与 OTel 的 16/8 字节等宽
  → 直接搬运比特即可；父子的标识同源，字节序只需自洽
  → 实测某条链路：sent ReqGuildApply(parent=信封 processed) → sent ActorError(parent=请求 span)
Exporter 由专用 worker 串行调用；队列满丢 span 并计数
一节点多能力时各节点的观测混在同一条 trace 里（实测 20 条 trace 中 10 条跨 ≥2 节点）
```

### 指标

```text
ergo.services/actor/metrics v0.3.2（要求 ergo v1.999.330，与当前版本一致）
必须先 node.Network().RegisterTypes(metrics.NetworkTypes()) + RegisterErrors(...)，
  否则 ProcessInit 直接失败：类型未登记时指标无法解码，数值会静默保持为零
Options 三态（实测配置方式）：
  Shared 为空                → 独立模式：自建 HTTP 端点 + 采集 + 自定义指标
  Shared 非空 + Port/Mux     → 主实例：采集基础指标 + 端点 + 自定义指标
  Shared 非空 且无 Port/Mux  → worker：只处理自定义指标，不采集基础指标
→ 要"并入既有单端点"必须走主实例，因此传入一个不对外服务的 Mux（满足其角色判定，
  同时不让它自建第二个端口）
registry 在 Init 返回前为 nil，必须用 Shared.Registry() 提前拿到
运行时指标自带 node const label；自定义 collector 不能再声明 node
```

### 日志（据文档 + 实测）

```text
gen.LogLevelPanic = 框架捕获了 actor 回调内的 panic，节点继续运行
  → 不得映射到 gxylog.Fatal（后者 os.Exit(1)），否则单个 actor panic 会终止整个进程
默认 logger 写到 os.Stdout；生产模式应 DefaultLogger.Disable = true 并注册自定义 logger，
  否则与 gxylog 的 console core 重复输出（实测）
注册 API 有两种形态，不要混用：
  node.LoggerAdd(name string, logger gen.LoggerBehavior, filter ...gen.LogLevel) error   // 运行期
  gen.NodeOptions{Log: gen.LogOptions{Loggers: []gen.Logger{{Name, Logger, Filter}}}}    // 启动期
LoggerAddPID 会把该进程自身日志级别设为 Disabled（防 logger 递归），LoggerDeletePID 恢复
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

> 下表记录的是**当时**（迁移到 ergo 那次）的处置。其后业务接入形态又被 ADR 0017 改过一次
> （业务对象不再内嵌运行时类型、业务入口改名、异步段由门面驱动），本表的名字已与现状不符。
> 现状以 `actor-system.md` 与 ADR 0017 为准。

| 现有符号 | 行数 | 处置 |
|---|---|---|
| `Receive` + `doReceive` | 66 | 删（ergo 直接回调 handler） |
| `initSpan` / `readonlyHeaderCarrier` / `messageEnvelopeCarrier` / `injectTrace` / `IUnspanMessage` / `unspanMessage` | 77 | 删（原生追踪） |
| `ActorContext` / `ContextDecorator` / `ActorInitMsg` / `ActorTimerMsg` | 18 | 删（`Spawn(factory, opts, args...)`） |
| `Self` / `Sender` / `Stop` / `respond` | 18 | 改 `PID()` / handler `from` 参数 / `return` |
| `newSupervisor` / `decider` | 7 | 改 `SupervisorSpec` |
| `logger.go`（slog 适配） | 68 | 重写为 `gen.LoggerBehavior`（约 40 行） |
| `activator_manager.go` + `actor_mgr.go` | 667 | 重写：保留租约（#6）、分片调度、能力目录选择与重定位循环、陈旧记录的条件释放与重试（#8）；删除会合机制与本地实例索引（`actor_mgr.go` 随之删除） |
| `newSystem` / `Address` / `StopActor` / send 四件套 | ~62 | 改 ergo node |
| `actor_locator.go` | 418 | 保留（改成：`nodeID` 用稳定节点名、`leaseToken` 改为每实例随机；获取方式不变） |
| `gxyutil.MsgHandler` | — | 全部保留 |

### 业务侧调用点改造

| 调用点 | 生产 | 测试 | 处置 |
|---|---|---|---|
| `Timer()` | 17 | 19 | 不变 |
| `Sender()` | 17 | 26 | 改用 handler 的 `from` 参数 |
| `Self()` | 13 | 22 | 改用 `PID()` |
| `ActivateActor` | 4 | 0 | 改 node 级 `SpawnRegister`（名字注册式创建，不挂监督者） |
| `PidEqual` | 3 | 0 | 改 `gen.PID` 比较（含 `Creation`） |
| `CallSync` | 1 | 2 | 改 `Call` + 显式 sender |
| `AutoHandleMsg` | 2 | 0 | 改响应机制 |
| `GetLocalActor` | 2 | 0 | 改按名查表（常数时间，非扫描）；本地实例索引随之删除 |
| `GetLocalActorAll` | 1 | 0 | 改 `ProcessRangeShortInfo`（枚举本机进程） |
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

> 与既有实现的差异：所有权获取（claim）与释放**不再由激活协调层代管**，改由角色进程自身在初始化同步段获取、在终止路径释放（不变量 #3/#4）。协调层只做能力目录选择、重定位循环，以及"记录指向本节点但本地无实例"时的条件释放与重试（#8）。

```text
ActivateRole(roleID)
  ├─ locate(roleID)
  │    ├─ 有 owner → 发往 owner 节点的协调层，由其校验本地实例：
  │    │    ├─ 本地有该实例 → 返回 PID
  │    │    └─ 本地无该实例 → 条件释放陈旧记录 + RetryLocate（不变量 #8；ADR 0006 #7/#8）
  │    │         ※ 不得直接按名构造 PID 调用——那会绕开 #8，陈旧记录永不收敛
  │    └─ 无 owner → ConsistentHashSelector 从 kind 记录选候选节点
  │         → 发给该节点分片 activator：{kind}_activator_{hash(id)%N}
  │
  └─ 候选节点侧 activator
       ├─ node.SpawnRegister("role/"+id, factory, opts, id)   ← node 级
       │    └─ Init 同步段：claim（redis Lua，blocking I/O —— 分片的原因）
       │         → 校验通过才开始处理消息；失败即终止（不变量 #3）
       └─ 返回 PID

调用方发送业务消息（CallImportant）
  ├─ 成功
  ├─ ErrProcessUnknown → 服务端重试 1–2 次（含重新激活）
  └─ ErrNoConnection / ErrRetryExhausted → 失败
```

角色进程终止路径（不变量 #4）：先完成最终落库（写事务内锁定 `role_id + node_id + epoch`，不变量 #2），**之后**才条件释放所有权。不得依据死亡通知释放——运行时的名字释放与 Down 通知都早于终止回调完成（`ergo-foundation.md` §1）。

activator 内部消化 relocate 循环上限沿用 `actorLocateMaxAttempts = 3`。`ErrRelocate` 不冒泡给客户端。

分片数 N 待定：串行瓶颈只是 claim 的 redis 往返（初始化其余部分为纯内存或异步），N 应由该往返耗时与预期登录峰值倒推，而非照搬旧实现的 5 路池。

## 三、实现顺序

> 与绑定不变量冲突处以 `invariants.md` 为准。本清单是执行序，不是决策依据。

1. **wire envelope**：`WireEnvelope` + `Pack`/`Unpack`，节点启动时 `Network().RegisterType`。〔完成〕
2. **`consulRegistrar`**：`gen.Registrar` 适配（`Register` no-op、`Resolve` 读 Consul）。〔完成；登记粒度为节点级记录,见 ADR 0011〕
3. **身份改造**〔完成〕：`nodeID` = ergo 节点名；`leaseToken` 独立随机；租约获取**保持"仅当不存在时写入"**（不变量 #6，获取失败即启动失败；其可接受性依赖前提 B）。
4. **`gxyactor` 门面重写**〔完成〕：门面基类内嵌 `act.Actor`；业务直接内嵌门面基类,不保留业务侧接口与适配层（见 ADR 0014）。含 `ctx` 串联、error 语义转换、metrics 埋点搬移、`gen.Logger`。
5. **activator 重写**〔完成〕：`node` 级 `SpawnRegister`；删除 `pending`/`waiters`/`Touch`/`actor_mgr`；所有权获取/释放移出协调层。
   **偏离计划**：未保留分片——所有权随初始化下移到 actor 后,同一实体的初始化段已由原子名字注册保证至多一个,分片不再解决任何问题（见 ADR 0012）。
6. **角色初始化拆分**〔完成〕：同步段做**所有权获取 + 纯内存校验**（不变量 #3），耗时加载与外部访问移入异步段；终止路径先落库再释放（不变量 #4，且不得依据死亡通知释放）。
7. **业务侧替换**〔完成〕：`Sender()` / `Self()` / `PidEqual` / `ActivateActor` 等 30+ 处。
8. **可观测性**：追踪适配器、metrics 合并、日志、cron 迁移。〔2026-09-19 完成：追踪适配器 + metrics 合并；单端点实测含 49 个 ergo 指标族；20 条 trace 中 10 条跨 ≥2 节点。日志与 cron 此前已完成〕
9. **停机顺序**〔完成〕：actor 域先于共享客户端（数据库、缓存）停止,且用等待终止回调完成的优雅停止（不变量 #10）。
10. **文档更新**：〔2026-09-19 完成〕改：`actor-system.md`（整篇重写）、`overview.md`、`README.md`、`AGENTS.md`、`service-discovery.md`、`app-role.md`、`app-gateway.md`、`docs/public/{dev-ops,logging}.md`、`.agents/skills/gserver-{dev,selfcheck}/SKILL.md`、`blog-actor-model-game-server.md`。归档：`actor-init-race.md`、`tracing.md`、`issue-protoactor-go-endpointwriter.md` → `docs/archive/`（各加归档缘由）。历史记录不动：`docs/superpowers/**`、`docs/pressure/runs/**`（带日期的一次性记录）。

## 四、验证要求

- wire：双节点真实 pb 载荷往返（已先行验证方案可行性）。
- 激活：并发同名激活仅产生一个实例；`Init`/`Terminate` 不重复执行。
- ownership：跨节点接管后旧 epoch 的保存被 `role_actor_fence` 拒绝。
- 关停：`node.Stop()` 等待全部 `Terminate` 回调完成——停机的存盘保障由此提供（不变量 #10），无需另建屏障。
- 可观测性：`/metrics` 单端点同时含业务与 ergo 指标（**已验证**：单端点含 49 个 ergo 指标族 + 业务指标）；Tempo 中可见跨节点 trace（**已验证**：链路同一 trace 内跨 gate/game，响应 span 的父为请求 span）。
- 时序：`ServiceInfo.NodeHost` 中的端口与 ergo 实际监听端口一致。
