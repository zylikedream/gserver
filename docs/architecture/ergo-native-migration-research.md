# GServer Ergo Native 化研究:现状 vs 官方用法

- 日期:2026-09-08
- 状态:**Research**(供 ADR 讨论用,非决策)
- 关联:`adr-0008-ergo-actor-runtime.md`、`ergo-runtime-study.md`、`ergo-migration-verification.md`

## 1. 结论先行

当前接入是**"把 Ergo 当消息总线用的 wrapper 架构"**,不是官方 native 架构。两边的根本差异:

| 维度 | 当前 GServer 接入 | Ergo native(docs.ergo.services) |
|---|---|---|
| 组织单位 | gxyapp 模块 + activator actor + 业务 actor 三层自制结构 | **Application(Group)→ Supervisor 树 → actor**,框架原生 |
| 业务 actor 类型 | `IActor`(自研 4 回调接口) + `ActorProcess` 自研 wrapper | 结构体内嵌 `act.Actor`,回调即框架回调 |
| 消息类型 | `proto.Message` + `GServerEnvelope{Type,Data}` protobuf 封包(registry 注册) | Go 值直接发,**跨节点才需序列化**(EDF);本节点零拷贝 |
| actor 生命周期 | activator 手工管理:Claim→Spawn→Touch→pending/waiters| `Spawn` 同步等 Init;失败即 Spawn 返回 error;**supervisor 重启**原生 |
| 容错 | actor 死→activator 收 ActorTerminatedMessage→release ownership;**无自动重启** | supervisor 4 种策略 + intensity + per-child 预算 + OnExceedDisable + PreserveMailbox |
| 定时器 | gf `gtimer/gcron` 独立 goroutine → LocalSend 回邮箱(桥接层) | `SendAfter/SendEvery` 直接走邮箱,无额外 goroutine |
| 广播 | Pulsar(gxymq) + NotifyLocalAll | **events**(RegisterEvent/LinkEvent/Open/Buffer),节点级 CoreEvent |
| 服务发现 | Consul(gxyservice) + Redis owner | 内嵌 registrar/etcd + `ResolveApplication` + Tags/Weight |
| 观测 | 自研 metrics + otel 手工 span | HandleInspect(`ergo:*` 键) + Observer + 原生 tracing 传播 |

**关键判断:方向对,姿势偏。** wrapper 解决了"protoactor 时代遗留接口不变"的问题,但也把 Ergo 最值钱的东西(supervision、events、native 定时、零拷贝消息、观测)全部挡在了 seam 外面。整个 `internal/ergo` 包 1000+ 行,大部分在重新发明框架已有的轮子。

## 2. 现状盘点(证据)

### 2.1 接入拓扑

```
node/main.go
 └─ gxynode.NewNode (gxyapp 模块树,自研生命周期)
     ├─ metrics/trace/redis/pgx/mq/http/service  ← gxyapp.IApp 自研应用
     ├─ actor = ergoapp.NewActorApp              ← 唯一 Ergo 入口
     │   └─ ergo.Start(Options) → ergo.StartNode ← 每个 gserver node 一个 ergo node
     ├─ account/chat/friend/role/gate/guild      ← 业务 app(与 actor 并列!)
     └─ 业务 app OnModStart → gxyactor.RegisterActorKind(kind, producer)
```

问题:业务 app 和 actor app 是**平行模块**,业务代码通过包级全局函数(`gxyactor.Send/Call/ActivateActor`,helper.go)经 `SetRuntime` 全局单例访问 actor——不是依赖注入,是 service locator。

### 2.2 双重 actor 抽象(internal/ergo/actor.go:18-49)

```
ergoActor{ act.Actor 嵌入 }          ← 真正的 Ergo 进程(唯一 native 部分)
   └─ host *gxyactor.ActorProcess    ← 自研 wrapper:timer、msgHandler 反射注册、metrics
        └─ actor IActor              ← 业务逻辑:Init/DelayInit/HandleMessage/Terminate
```

- `IActor` 4 回调与 `act.Actor` 回调**几乎一一对应**(Init↔Init、HandleMessage↔HandleMessage、Terminate↔Terminate),wrapper 只额外贡献:DelayInit 时序、反射 handler 注册(`AutoHandleMsg`)、timer、metrics、log context。
- 每条消息路径:`decode(envelope)` → 映射 Down 消息 → 构造 actorContext → trace span 手工开关 → `host.HandleMessage` → 反射分派。**otel tracing 是手搓的**,而 ergo v3.3 已内建 tracing 传播(`MessageOptions.Tracing`/`PropagatingTrace`),adapter 只消费不生产。

### 2.3 激活协议(activator_manager.go,610 行)

每个 kind 一个 `activator` actor(`SpawnNamed("activator", ...)`,activator_manager.go:466),职责:
1. 收 `pb.ActorActive` Call → Redis `locator.claim` → `decideActivation` 五分支(activator_manager.go:108-126)
2. `spawnActivatorActor` → 注册 pending + waiters → **go goroutine** confirm → 结果发回自己 mailbox → Touch 成功才 `mgr.Add` + 回复 waiters(:264-318)
3. `ActorTerminatedMessage`(手工转发的 Down 消息,actor.go:56-63)→ release ownership

这 610 行在框架侧都有对应物:
- Claim/Spawn 串行化 → **ergo Spawn 本身同步等 ProcessInit**(成功即 init 完成,代码注释自己也承认:"Ergo waits for ProcessInit synchronously",activator_manager.go:50)
- pending/waiters → Spawn 是同步的,调用方直接拿到 (PID, error),**不需要 waiters 列表**
- child 死亡感知 → **supervisor 的 HandleChildTerminate 或 link**(native)
- Touch 失败停进程释放 owner → Init 返回 error 时 Spawn 即失败(半程 native)

**无法 native 化的部分(必须保留)**:Redis Claim-before-Spawn、epoch fencing、PostgreSQL `role_actor_fence`——这是 ADR-0006 的 single-writer 不变量,Ergo 有意不管(ADR-0008 已明确"Ergo Registrar 只能发现,不能决定 ownership")。

### 2.4 消息与序列化(message.go + runtime.go:561-585)

- 所有 `proto.Message` 一律装 `GServerEnvelope{Type string, Data []byte}`;本地消息也走 encode/decode 吗?——不,`encodeFor` 判断 `pid.Node == a.nodeName` 时直传原值(runtime.go:580-585)。但**同进程内的 Send(activator↔activator、role↔chat 跨 app)** 仍全走 `decode→registry.Decode→proto.Unmarshal`?查 `LocalSend`:直通 `Send`,本机节点不经 envelope;远程才封包。**这一点已经做对了**(本机零拷贝、远程 proto)。
- 残留问题:`GServerEnvelope` 只注册了 3 个控制类型到 `node.Network().RegisterType`(runtime.go:103-109);业务 proto 用 registry 而非 EDF。这**没有错**(EDF 对 proto.Message 也能传 []byte),但放弃了两点:①EDF 注册类型的握手期缓存压缩;②文档的项目结构约定(types/ 集中注册)。proto envelope 是合理选择,可保留。

### 2.5 定时器(actor_timer.go + gxytimer)

`ctx.Timer().AddTick/AddOnce/AddCron` → gf `gtimer/gcron` 独立 goroutine 触发 → 回调里 `LocalSend(pid, ActorTimerMsg)` 回邮箱。对照官方反模式原文:"The wrong way is to start a `time.Timer` or `time.Ticker` inside a callback"——**当前实现正踩在这条线上**(每 actor 一个 gtimer 引擎 goroutine 池,回调虽只发消息,但生命周期与 actor 不绑定:actor 死后 timer 要靠 `ProcessTerminate` 手工清理,清理遗漏即泄漏)。native 对应物:`SendAfter/SendEvery`(走邮箱、actor 死自动失效、无外部 goroutine)。cron 类需求 ergo 也有节点级 `NodeOptions.Cron`。

### 2.6 广播(Pulsar + NotifyLocalAll)

`rolelib.NotifyLocalAll` 走 gxymq(Pulsar)pub/sub。native 对应物是 events:
- 节点级总线:`NodeOptions.Events` 预注册 open 事件(如 `player.logged_in`),`LinkEvent` 订阅,Buffer 回放;
- 跨节点:同一 API,框架按订阅关系 fanout(每节点一条网络消息,README 卖点);
- "某应用全集群下线"感知:`gen.CoreEvent`(网络关闭时也可用)。
Pulsar 是否还需要?若未来有跨机房/异构系统对接,保留 gxymq 作外部集成通道;**纯 gserver 内部广播可全部换 events**。

### 2.7 Call 语义对照(internal/ergo/runtime.go:420-488)

- `Call` 自己起 goroutine + timer + select(ctx/timer/done)——**全手工**。ergo 原生 `CallWithTimeout` 已含超时;ctx 取消映射是唯一增量。文档口径:框架 err=timeout,"应用层错误要作为 result 值返回"——当前 `HandleCall` 返回 err 时 adapter 调 `SendResponseError`(actor.go:107-109),**已经绕过了框架的'err=终止原因'语义**,这实际上让业务 err 不杀进程,是 wrapper 的又一个隐藏行为(与官方 `'Return errors for termination, not for caller responses'` 直接冲突)。
- `CallSync`(runtime.go:311)= `Send` 的别名——遗留接口,无 native 对应。

## 3. native 化目标架构(按文档 9.x 映射到 GServer)

```
ergo node (每进程一个,cmd/gserver 或 per-app main)
 └─ gen.ApplicationBehavior ×1:GServerApp(embed app.Application)
     ├─ Load: 声明 Group + Network.RegisterTypes
     ├─ Init: 打开 PG 连接池/Redis client/gameconfig(替代 gxyapp 模块树)
     └─ Group:
         ├─ "dir_sup"  act.Supervisor(OFO, Permanent)
         │    ├─ actor_directory  ← 瘦身后的 activator(只剩 Redis Claim/Release + 版图)
         │    ├─ chat_channel / guild / ... 长驻控制 actor
         │    └─ per-kind 业务 actor(经 activator spawn,SOF 风格)
         └─ "role_sup" act.Supervisor(SOFO, PreserveMailbox, per-child Intensity+OnExceedDisable)
              └─ StartChild("role/<id>") ← RoleMain 等有状态玩家 actor
```

| 现组件 | native 化后 | 备注 |
|---|---|---|
| `gxyapp` 模块树 | `ApplicationSpec.Group` + `Init/Start/Stop/Terminate` 回调 | 15s 默认超时、ref.IsAlive() 检测、依赖排序 Depends 白得 |
| `gxynode.NewNode` | `ergo.StartNode` + `NodeOptions{Applications, Events, ShutdownTimeout}` | app 编排从自研"RegisterApp + AddModule"变成声明式 Group |
| `ergoapp.actorApp` | 并入 GServerApp 的 Load/Init | actor 不再是平行模块 |
| `activator` actor | **保留但瘦身**:留 Redis claim/release/epoch + decideActivation;删 pending/waiters/Touch goroutine(Spawn 同步) | 约 -300 行;HandleChildTerminate 替代手工 Down 转发 |
| `ActorProcess` + `IActor` | 业务结构体内嵌 `act.Actor` | `DelayInit` 并入 `Init`(SendAfter 兜底),`AutoHandleMsg` 反射注册可保留(纯 Go 层) |
| `ActorTimer`/gxytimer | `SendAfter/SendEvery`;cron 用 `NodeOptions.Cron` | 删 gtier/gcron 桥接;timer 随进程死 |
| `GServerEnvelope` registry | 保留(远程 proto 是合理选择);`Network.RegisterTypes` 在 ApplicationSpec.Network 声明 | 本机零拷贝已正确 |
| Pulsar 内部广播 | events(open + NodeOptions.Events 预注册) | gxymq 保留给外部系统 |
| Consul 服务发现 | 内嵌 registrar(单机)/etcd(集群);应用级发现 `ResolveApplication`+Tags/Weight | 与 Redis owner 职责不冲突(ADR-0008 边界不变) |
| `gxyactor.Send/Call` 包级函数 | helper 保留为薄糖,底层直通 `gen.Process`;包级单例 `SetRuntime` 降级为 bootstrap-only | 分阶段,不强求 |
| 自研 otel span | ergo 原生 tracing(`TracingOptions` + span 传播),业务属性用 `SetTracingSpanAttribute` | adapter 里手搓 span 全删 |
| 手写 metrics | actor 内建 `Info()`/`ergo:*` inspect 键 + Observer(或接 Prometheus exporter) | ActorActiveCount 等业务指标保留 |

## 4. 风险与边界(不要 native 化的部分)

1. **Redis ownership/PostgreSQL fencing 原样保留**(ADR-0006/0008 不变量)。ergo 的 supervisor 不理解跨节点 single-writer;`role_sup` 只能在 Claim 成功后 `StartChild`,重启策略只管本节点进程级容错。Claim-before-Spawn 顺序不能倒。
2. **proto envelope 保留**:EDF 对 proto 字段类型支持有限,且客户端协议已是 protobuf;只在 ApplicationSpec.Network 里声明注册,别用已弃用的包级 API。
3. `NetworkFlags` 全有全无陷阱:从 `gen.DefaultNetworkFlags` 拷贝再改,别裸写字面量。
4. 迁移期间 `IActor`→嵌入 `act.Actor` 会触碰全部业务 actor(role/guild/chat/gateway/gate)——建议按 app 逐个切换,activator 双轨注册期间兼容两种 producer。
5. 文档与 v3.3.0 源码一致性:application 回调签名(Load 去 node 参数)、Open events 等均为 3.3.x 口径,我们 pin 的 v1.999.330 匹配,无版本风险 [已核对 go.mod]。

## 5. 建议的落地顺序(每步可独立验证)

> 2026-09-08 补充:经后续三轮讨论(Consul 去留 / registrar 选型 / 可观测性),本节 TODO 已扩展,最终版见文末 "## 11. Native 改造 TODO 总表"。

1. **P0 激活协议瘦身**:activator 删 pending/waiters/Touch-goroutine,直接用同步 Spawn 结果;`ActorTerminatedMessage` 改为 adapter 直发(已有)→ 完成后删 `confirmActivatorActor`。风险低,纯删代码。
2. **P0 Call 语义对齐**:决定业务错误通道——沿用 `SendResponseError`(偏离文档但已稳定)或切官方 `result=error` 约定。二选一,写进 ADR。
3. **P1 timer 桥接**:ActorTimer 内部换 `SendEvery/SendAfter`,对业务 API 不变;cron 单独评估。
4. **P1 Application 重构**:gxynode 的 app 树 → GServerApp(Group),gxyapp 退役;配置仍读 TOML。
5. **P2 events 替换内部 Pulsar 广播**;registrar 评估(先内嵌,etcd 备选)。
6. **P2 业务 actor 换嵌入 act.Actor**(量最大,逐 app 切);`IActor`/`ActorProcess` 退役。
7. **P3 tracing 收编**到 ergo 原生链路;接 Observer 做诊断面(可选 MCP)。

## 6. 参考出处

- 源码笔记:`ergo-runtime-study.md`(全部 file:line 基于 v1.999.330)
- 官方文档:docs.ergo.services `/basics/{process,application,events,actor-model,node,meta-process,project-structure}`、`/actors/{actor,supervisor,pool,router}`、`/advanced/handle-sync`、`/testing/{overview,unit,stage}`、`/networking/{service-discovering,static-routes,remote-spawn-process,network-stack,network-transparency}`、`/tools/{ergo,argus}`
- 本仓库证据:`core/gxyactor/{helper,runtime,activator_manager,actor_locator,actor_timer,process,actor_mgr}.go`、`core/gxyactor/internal/ergo/{runtime,actor,message,pid}.go`、`core/gxyactor/ergoapp/actor_app.go`、`core/gxynode/node.go:136-160`、`src/apps/role/{role_service,role_app}.go`、`src/apps/role/internal/logic/role_main.go:301,420-422`、`core/gxytimer/timer.go:81-110`

## 11. Native 改造 TODO 总表(2026-09-08 收敛版)

综合源码对照、官方文档精读与后续三轮讨论(Consul 去留、registrar 选型、可观测性)的最终落地清单。顺序即依赖顺序;每项独立可验证、可回滚。

### P0 决策与前置(改代码少,定案多)

| # | TODO | 要点 | 产出 |
|---|---|---|---|
| 0.1 | 起草 ADR-0009(ergo-native 改造) | 收编本文档;首个决策点:node naming | ADR |
| 0.2 | **node naming 定案** | ergo node name 从 `game@<hexnano>` 改为 `game@<POD_IP|host>`(真实可路由 host);牵动 Redis owner `NodeID` 与 PID 语义,adapter 已有 transportName↔nodeName 双身份映射(pid.go:22-24)可承接 | ADR 决策记录 |
| 0.3 | **Call 错误通道定案** | 已定:**切官方 `result=error` 约定**——业务错误走 result 值(pb envelope,与消息同通道),`HandleCall` 返回的 err 只作终止原因;同节点错误对象直传(含栈)、跨节点走 pb 后身份/栈问题消失;`gen.Error` 仅限框架三场景(线格式/Mailbox Preservation/supervisor 退出原因),业务代码禁用;`CallSync` 遗留接口一并清理;adapter 自定义 `Error{Kind,Cause}`(runtime.go:27-55)删除,`mapError` 改 cockroachdb `Wrap` | ADR-0009 决策记录 |
| 0.4 | registrar 选型定案 | dev=内嵌/关网;k8s 生产=etcd(默认推荐,client 已在依赖树);Saturn 仅远期数百节点再评估 | ADR 决策记录 |

### P1 激活链路瘦身 + Application 重构(核心结构性改动)

| # | TODO | 要点 |
|---|---|---|
| 1.1 | activator 瘦身 | 删 pending/waiters/Touch-goroutine(`confirmActivatorActor` 一并删):ergo Spawn 同步等 ProcessInit,成功即确认;保留 Redis Claim-before-Spawn、decideActivation、条件 release。约 -300 行 |
| 1.2 | 死亡感知 native 化 | `ActorTerminatedMessage` 手工桥 → supervisor `HandleChildTerminate` 或 link(activator 对业务 actor 已有 Watch) |
| 1.3b | gxymodule/gxyapp 退役 | 全量盘点(15 消费方)确认无人使用模块树能力;基础设施 app→GServerApp 回调;`rolelib.RoleNotify`/`lib.Broadcast`→Group 成员 actor;**RoleMain 假树改显式 `[]IRoleModule`**(删 ModuleBase/AddModule 嵌入,role 自有聚合逻辑保留,`IRoleModule` 接口保留);两套 Deps 中字符串版删除、`deps.Deps` 注入保留(ADR-0001 资产) |
| 1.4 | supervisor 接管容错 | `role_sup`(SOFO,PreserveMailbox,per-child Intensity+OnExceedDisable)管有状态玩家 actor;控制 actor 挂 OFO;**Claim 成功才 StartChild,顺序不倒** |
| 1.5 | timer 换 native | ActorTimer 内部 gtimer/gcron → `SendEvery/SendAfter`(API 不变);cron 类评估 `NodeOptions.Cron`;actor 死 timer 自动失效 |
| 1.6 | 错误边界收口 | 落实 P0-0.3:删 adapter `Error{Kind,Cause}`;节点启动时 `Network().RegisterError(...)` 注册跨节点哨兵清单(EDF ErrCache,`errors.Is` 身份跨节点存活);error-handling.md 补 3 条 Ergo 边界规则(哨兵注册 / 禁用 gen.Error 当业务错误 / 错误分支基于注册哨兵不依赖同异节点差异) |

### P2 发现层与通信层去 Consul 化

| # | TODO | 要点 |
|---|---|---|
| 2.1 | actor 发现切 ergo registrar | 注册节点 + `CallProcessID`(node name 寻址);修复 requestActor 把 HTTP host:port 当 node name 的缺陷(activator_manager.go:576);一致性哈希放置保留,数据源换 `gen.Resolver` routes |
| 2.2 | serving/draining → Tags/Weight | `ApplicationSpec.Tags("ready"/"draining")` + `SetWeight`,经 registrar 推送 |
| 2.3 | 内部广播 Pulsar → events | `NodeOptions.Events` 预注册 open 事件(player.logged_in 等)+ `LinkEvent`;gxymq 保留给外部系统 |
| 2.4 | HTTP 服务发现去 Consul | gateway→app HTTP 迁 ergo `Call`(或交 k8s Service);完成后退役 gxyregistery(consul/redis/etcd/selector/watcher)、gxyservice 注册刷新、gxynodeenv 状态上报 |

### P3 行为层 native 化(量大,逐 app 切)

| # | TODO | 要点 |
|---|---|---|
| 3.1 | 业务 actor 嵌入 act.Actor | 逐 app(role/guild/chat/gateway/gate)把 `IActor`+`ActorProcess` 换成内嵌 `act.Actor`;`DelayInit` 并入 Init(SendAfter 兜底);`AutoHandleMsg` 反射注册保留;双轨注册期兼容两种 producer |
| 3.2 | tracing 收编 | 写 otel exporter 实现 `gen.TracingBehavior`(~150 行,ergo TraceID 直传 OTLP/Tempo)挂 `NodeOptions.Tracing`;删 adapter 手搓 span 桥(actor.go:70-92,163-185);解决跨 actor 链路断裂(HTTP root + ergo 链统一) |
| 3.3 | 指标收编 | 业务指标(gxymetrics)不动;`ActorMessages/Duration` 换数据源:ProcessInfo(messagesIn/runningTime)经 Radar 或小 exporter;`/metrics` 端点保留 |
| 3.4 | 观测面(可选) | Observer 应用(system 自带 inspect/manage 能力)接 UI/诊断;argus vet 进 CI(`go vet -vettool=$(which argus)`) |

### 不可 native 化(边界,ADR-0006/0008 不变量)

- Redis owner/Claim/Release/epoch、节点 lease、PostgreSQL `role_actor_fence`:原样保留;Claim 必须先于任何 Spawn/StartChild。
- proto envelope(远程业务消息)与 EDF 注册并存:注册移到 `ApplicationSpec.Network.RegisterTypes`(包级 edf.RegisterTypeOf 已弃用)。
- gxymq(Pulsar)仅保留给外部系统集成。

## 12. 执行策略与复核点(2026-09-08 "是否另起炉灶"决议)

决议:**继续在本项目改造,不另起炉灶**。依据:63k 行中业务逻辑(~10k)/测试(~17.7k)/协议(pb)/文档均为可搬运资产且与 runtime 低耦合;TODO 净代码量为减法(约 -2000 行);worktree 当前是稳定可运行点(feat/ergo-actor-runtime,64 commits),不存在"改到一半不能上线"。

节奏:
1. P0 四个决策一次过完(半天级)→ **P1-1.1 + 1.3/1.3b**(最不像 native 的两处)→ **暂停,回到稳定点**;
2. 每个 PR 结束时系统可运行、可合回 master、可搁置;
3. 复核点设在 P1 完成 + 真实开服压测后:用压测数据(gserver-pressure-testing 流程)而非感觉决定 P2/P3 是否投入、何时投入。

废弃清单(随对应 P 阶段执行):`gxyapp`、`gxymodule`、`ergoapp.actorApp`(并入 GServerApp)、`gxyregistery`(P2-2.4)、`CallSync`、字符串版 `App.Deps()`、`IActor`/`ActorProcess`(P3-3.1 逐 app 完成后)。

## 13. 全文档精读差缺补漏(2026-09-08,逐章学习 docs.ergo.services 后)

前 11 章基于源码与官方文档精读,本节是按文档目录(basics/actors/meta-processes/networking/testing/advanced)逐章通读后的**增量结论**——只记录此前章节没覆盖、且影响 TODO 或认知的点。

### 13.1 修正/加强既有认知

1. **SendResponseError 的正确定位(docs/advanced/handle-sync.md)**:官方明确 guidance——SendResponse 走 result 通道(业务结果,包括"预期内的错误":not found/校验失败);SendResponseError 只用于"调用方应与传输错误同等对待的基础设施错误"(依赖挂了/连接池满)。**我们 P0-0.3 的结论与此完全一致,但补一个细则**:activator 的基础设施类失败(节点不可达、Claim 后端异常)可以考虑走 SendResponseError,业务激活失败(result 里报错)。当前 actor.go:108 一刀切走 SendResponseError,迁移时应按此二分。
2. **HandleCall 的 err 返回值是终止原因不是响应(actors/actor.md:163-184)**:文档用独立章节反复强调。我们的 seam 已对齐(Respond 分离),但 P3 迁移每个业务 actor 时这是最容易踩的坑,应写进迁移 checklist。
3. **`gen.DefaultNetworkFlags` 全有全无陷阱(network-stack.md:247-253)**:一旦手写 `Flags: gen.NetworkFlags{Enable: true, ...}`,所有未写字段变 false(fragmentation/important delivery/tracing/keepalive 全关)。**节点启动代码必须从 `gen.DefaultNetworkFlags` 拷贝再改**——这条要进 ADR-0009 的实现规范。
4. **本节点消息不拷贝、共享内存是纪律不是约束(actor-model.md:57-59)**:文档首次明确"同节点传 map/slice/pointer 两边共享同一内存",官方用 argus vet 工具(A1001 规则)静态检查。**GServer 语义是 proto envelope,天然值语义,不受影响**;但 P3 迁移后允许 Go 值消息时必须建立"发送即移交所有权"规约,可把 `ergo.tools/argus` 加进 CI。
5. **links 单向、父 exit 不可 trap(links-and-monitors.md:37-41, actor.md:333)**:与 Erlang 双向 link 不同。P1-1.2 死亡感知设计时,activator 对业务 actor 的 Watch 是 monitor 语义(不死),supervisor 的父子则是双 link(可强制杀子树)——两者别混用。
6. **Mailbox Preservation 的精确语义(process.md:111-149, supervisor.md:457-525)**:仅 OFO/SOFO 可用(AFO/RFO init 拒绝);触发消息本身不重放(防毒丸循环);at-least-once 语义要求 handler 幂等;Exit cascade 也触发捕获,Normal/Shutdown 不触发。P1-1.4 role_sup 启用时要按"玩家 actor 消息是否幂等"逐个评估——非幂等的(扣钱、发奖)宁可丢消息也不能重放,或业务层带 requestId 去重。
7. **per-child restart counter 的适用边界(supervisor.md:284-299)**:per-child Intensity 只对 OFO/SOFO 合法,AFO/RFO 直接 init 报错。**P1-1.4 的 role_sup 是 SOFO,可用 per-instance 预算 + OnExceedDisable;控制 actor 的 dir_sup 若是 OFO 也可用;这与 TODO 1.4 写法兼容**。
8. **Kill 与 Zombee 状态(process.md:153)**:对 Running/WaitResponse 进程 Kill 进入 Zombee,所有操作返回 ErrNotAllowed,当前消息处理完才终止。activator 的"迁移中强杀"路径要预期这个窗口——**Claim 释放与进程真正终止之间有延迟,释放逻辑不能假设 Kill 立即生效**(现状已用条件 release 覆盖,保持)。

### 13.2 之前遗漏、现在补上的主题

| 主题 | 要点 | 对 GServer 的影响 |
|---|---|---|
| **Pool 无背压且静默丢弃**(pool.md:64-77) | 满载消息 drop+计数(`ergo:messages_unhandled`),Call 侧表现为超时,Fallback 不生效(Forward 绕过) | 若 P3 后需要并发处理网关消息,不能拿 act.Pool 当限流器;限流必须在自己代码里做(pool 放池化是无状态 worker 用的) |
| **Router 不保留 mailbox、无 intensity**(router.md:170-174) | 自带 slot 重启是"无限重试",严格重启契约要外部 supervisor;slot 内禁放 supervisor | 分服/分桶路由(P3 的 moduleByMessage 分发思路)若要 Router,拓扑是 SupRoot 平级持有 pool+router,router 免费解析注册名 |
| **Application Stop 是等待全树而非成员**(application.md:232) | teardown 等待 ProcessesTotal 归零(含成员派生的无 supervisor 孤儿,框架发 exit+日志点名) | 迁移后 GServerApp.Stop 的 drain 语义白得:draining tag + Stop 回调先行,框架等全树退出,孤儿会被点名——**这个日志要接进告警** |
| **CoreEvent 总线**(events.md:138-173) | 节点自带 `gen.CoreEvent`:app running/stopped、peer 连/断,Buffer 1000,跨节点可订阅 | P2-2.4 去 Consul 后的"应用上下线感知"直接订阅各节点 CoreEvent,不再需要 gxyservice 的 watch 机制;Observer/告警都能复用 |
| **NodeOptions.Events 预注册解决订阅竞态**(events.md:114-136) | 节点启动时预注册 open event,任何 actor 的 Init 可直接 LinkEvent,不会 ErrEventUnknown | P2-2.3 广播事件清单应走 `NodeOptions.Events` 预注册(player.logged_in 等),而非各 actor 运行时注册 |
| **Cron 与 Dst/时钟跳变**(cron.md:117-129) | 弹回的重复小时只跑一次;分钟级校验丢弃 NTP 跳变;宏(@daily 等)故意偏移错峰 | P1-1.5 cron 类评估直接用 native,gtimer 桥的"cron 重放"问题顺带消失 |
| **软件 keepalive**(network-stack.md:109-136) | 3.3 新增,应用层死链检测(15s×3 miss 默认),双方都开才生效 | k8s 内网默认即够用;跨机房/公网部署(若有)应显式调小 period |
| **Trace 日志级别只能启动时设**(logging.md:51) | SetLevel 不能动态开 Trace,防洪水 | 排障预案:预置 `--tags verbose` + LogLevelTrace 的诊断镜像/启动参数,而不是期望线上动态开 |
| **进程级 Logger 注册会静默自身**(logging.md:272) | LoggerAddPID 后该进程自己日志被 Disabled,防自我循环 | 若做 actor 化日志收集器,注意它自己的错误要用其他通道输出 |
| **四个构建标签**(debugging.md) | pprof(goroutine 按 PID 标记!)/norecover/latency(mailbox 积压年龄)/typestats | **压测(pressure-testing)脚本应加 `-tags latency`**,mailbox 延迟直接进 ProcessShortInfo;pprof 标签的 PID 标注让我们能按 actor 定位卡死,这是排障利器 |
| **EDF 错误跨节点身份三定律**(network-transparency.md:372-467) | ①哨兵必须 errors.New 且双侧 RegisterError;②*gen.Error 不能注册(Errorf 链可以带已注册哨兵跨节点);③error 字段里自定义具体类型降级为纯文本 | **再次验证 P0-0.3 决策**:业务错误走 pb envelope 完全绕开这套限制;若 P1-1.6 要注册哨兵,只注册"类"标记(code:xxx 分组),成员标记按需 |
| **Wrap 链跨节点的组/成员模式**(network-transparency.md:435-467) | 组标记(ErrCodeInvalidArgument)与成员标记(ErrInvalidArgB)同时 %w,接收方可按组匹配新成员 | P1-1.6 哨兵清单设计采用两级模式:粗粒度组标记注册、细粒度按需 |
| **Meta-process 双 goroutine 模型**(meta-processes.md) | External Reader(阻塞 IO)+ Actor Handler(消息),不能 Call/不能主动 link,Start 返回即终止 | GServer 暂无阻塞 IO 内嵌需求;gateway 的长连接若将来要从外部网关迁进来,TCP meta + WebWorker 是官方姿势,记入远期备选 |
| **WebWorker + WebHandler**(webworker.md) | HTTP 请求→actor 消息,按方法回调,Done 保证;配 act.Pool 扩并发 | gxyhttp 替代远期可评估;现阶段 k8s Service + ergo Call 已覆盖 |
| **Leader actor(Raft 选举)**(building-a-cluster.md:388-570) | extra-library 现成 leader.Actor,Bootstrap ProcessID 列表,150-300ms 选举 | **活动/全服类逻辑的"单写者"可以直接用**,替代手搓 Redis 分布式锁的候选;记入 backlog 不进本轮 TODO |
| **Metrics actor 内置基础指标**(building-a-cluster.md:572-601) | ergo_node_uptime/processes_total/memory/remote_messages 等,10 行接入 | P3-3.3 指标源切换的最低成本路径:先起 metrics actor 保底,再决定是否自研 collector |
| **unit/stage 测试框架**(project-structure.md:930-1064) | unit.Spawn 断言 ShouldSend;stage 起多节点真网络 | GServer 的 stage_ergo_test 已在用;unit 框架适合 P3 逐 actor 迁移时的单测(比手写 fake node 省事) |

### 13.3 认知校准:文档与源码不一致处

通读中发现文档滞后于 v3.3.0 源码的仅一处:文档多处仍写 `node.Network().RegisterType`(单数),源码与 FAQ 推荐 `RegisterTypes`(复数,自动解依赖顺序)。跟随源码。

### 13.4 对 TODO 总表的修订

1. **P0 增加实现规范条目**:节点启动 flags 必须从 `gen.DefaultNetworkFlags` 拷贝(13.1.3);argus 进 CI 的评估放 P3 开始时。
2. **P1-1.4 细化**:role_sup 启用 PreserveMailbox 前逐 actor 评估幂等性;SOFO+per-instance Intensity+OnExceedDisable 确认可行。
3. **P1-1.6 细化**:哨兵注册采用"组标记+成员标记"两级;activator 错误二分(基础设施→error 通道,业务→result)。
4. **P2-2.3 细化**:广播事件用 NodeOptions.Events 预注册(避免竞态),订阅方 Init 直接 LinkEvent。
5. **P2-2.4 补充**:去 Consul 后节点/应用上下线感知来源 = registrar events(etcd)+ 各节点 CoreEvent,不重建 watch。
6. **压测预案**:pressure-testing 跑 ergo 场景时构建加 `-tags latency`(可观测 mailbox 积压),报告记录 ProcessShortInfo 分布。
7. **backlog(不进本轮)**:leader.Actor 替代 Redis 锁评估;TCP/Web meta 评估 gateway 内嵌;act.Pool 用于无状态 worker 的场景评估。

