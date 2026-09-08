# Ergo 源码学习笔记(v3.3.0 / v1.999.330)

学习基准:上游 tag `v1.999.330`(commit `c73c353`,2026-09-04,Release "3.3.0"),即 `go.mod` 锁定版本。本地研究克隆在 `~/workspace/ergo-v330`(上游 master 的 worktree)。所有 file:line 相对该树。

> 版本勘误:上游 tag `v1.999.310`=v3.1.0、`v1.999.320`=v3.2.0、`v1.999.330`=v3.3.0。`zylikedream/ergo` fork 的 master 基于约 v3.1(+self-messaging priority 修复 + 自定义 `Whereis` 提交),**不是**学习基准;fork 的 `gen.Node.Whereis` 在上游不存在,上游对应 API 是 `ProcessPID(name)`/`ProcessName(pid)`(node/node.go:1979/2005 区段)。

---

## 1. 总体架构:契约层 + 唯一实现 + 行为库

```
gen/   (~7.4k 行, 30 个公开接口)   契约层:全部接口 + 值类型,零第三方依赖
node/  (~13k 行)                  唯一产品实现:node 实现 gen.Node + gen.Core
act/   5 个行为骨架:Actor/Supervisor/Pool/Router/WebWorker(实现 gen.ProcessBehavior)
net/   handshake(EHS)/proto(ENP)/edf/registrar(ESRD)  分布式四件套
meta/  端口元进程:Port/TCP/UDP/Web
app/   system 应用(inspect 只读平面 + manage 变更平面)
testing/  check/mock/unit/stage/tests 五层测试体系
```

两层接口刀(理解 ergo 的钥匙):
- **路由面 `gen.Core`**(gen/core.go:3-72):`RouteSend*`/`RouteCall*`/`RouteLink*`/`RouteSpawn`/`MakeRef`——process 与网络层(net/proto/enp.go:44)都通过它路由;v3.3 允许 `NodeOptionsExtra.WrapCore` 装饰整个路由面(node/node.go:160-168),这是测试拦截钩子。
- **行为面**:`process.Mailbox()` / `process.Behavior()` / `process.Forward()`——act 骨架通过它们对接运行时。

`gen` 接口 → node 实现(关键项):

| gen 接口 | node 实现 | 备注 |
|---|---|---|
| `gen.Node`(gen/node.go:14) | `node`(node/node.go:61) | "Node is not an actor - it's a container and runtime" |
| `gen.Core`(gen/core.go:3) | 同一 node 兼任,可装饰 | 路由面,`to.Node != n.name` 即转网络 |
| `gen.Process`(gen/process.go:243) | `process`(node/process.go:15) | 方法注释标注可用状态 |
| `gen.ProcessBehavior`(gen/process.go:23) | 用户实现(act 骨架或裸写) | ProcessInit/ProcessRun/ProcessTerminate/ProcessKind |
| `gen.TargetManager`(gen/target.go:3) | `node/tm` 无锁实现(node/tm/manager.go:33) | v3.3 重做,link/monitor 消费者表 |
| `gen.MetaBehavior/MetaProcess`(gen/meta.go:28/58) | `meta`(node/meta.go:11) | 端口元进程,双队列 |
| `gen.ApplicationBehavior`(gen/application.go:88) | 用户实现 + `app.Application` embed 基座 | PreLoad 绑定,"DO NOT OVERRIDE" |
| `gen.Network/Registrar/Cron/Log` | node/network、node/cron、node/log | Registrar 可注入 etcd/saturn |

## 2. Process 模型:状态机 + 四队列 + 单消费者 goroutine

**状态机**(gen/process.go:152-232,位掩码):`Init=1 / Sleep=2 / Running=4 / WaitResponse=8 / Terminated=16 / Zombee=32`。状态即权限表:接口方法注释明确各状态可用性(Init 可 Spawn/Send,禁 Call/Link/Monitor/RegisterName)。

**邮箱**:`gen.ProcessMailbox{Main, System, Urgent, Log}` 四条 lock-free MPSC 队列(gen/process.go:1310)。优先级不是堆而是**队列选择**:Normal→Main、High→System、Max→Urgent;消费顺序固定 Urgent→System→Main→Log(act/actor.go:160-181)。队列实现 `lib.QueueMPSC`:Push 用 `atomic.SwapPointer(&q.head)`(lib/mpsc.go:55-62),Pop 单消费者前移 tail;限长版 v3.3 修了竞态(原子占位,mpsc.go:65-74),`Len()` 是近似值。

**调度——与 protoactor"常驻循环"最大的架构差异**:
```
发送方: RouteSendPID → 选队列 Push → messagesIn++ → p.run()
p.run (node/process_run.go:14):
  CAS(Sleep→Running) 失败即返回          ← 保证全进程只有一个 reader goroutine
  go func: behavior.ProcessRun()         ← act.Actor 在此逐条 Pop 分派
  ProcessRun 返回 err → Terminated → unregisterProcess + ProcessTerminate(err)
  正常返回 → CAS(Running→Sleep)
    失败 = 被 Kill(zombee 收尾)
    成功 → 四队列 Item() 再查,有剩则 CAS(Sleep→Running) 复用本 goroutine goto next
```
含义:handler 内**绝不能长时间阻塞**(饿死本进程所有消息);Call 是唯一预期阻塞(状态 WaitResponse 保证 Kill 语义正确);空闲进程零 goroutine,按消息"睡眠-唤醒"。

**v3.3 并发安全升级**:process 内可变配置全部原子化——`priority atomic.Int32`、`keeporder/important atomic.Bool`、`compression atomic.Pointer`、log level `atomic.Int32`(node/process.go:55-65;node/log.go:21),支持运行时跨 goroutine 调整。

## 3. Spawn 全链与 InitTimeout

`node.Spawn/SpawnRegister`(node/node.go:431/474)→ 内部 spawn(node/node.go:2837-3157):

1. 合成 `ProcessOptionsExtra`(ParentPID/ParentLeader/ParentEnv/Args……);**initArgs 原样透传** `behavior.ProcessInit(bp, options.Args...)`(node/node.go:3045/3094)。
2. 建邮箱(先于名字发布)→ 预占注册名(`names.LoadOrStore`,冲突 ErrTaken)→ 分配 PID(`{Node, ID: atomic.AddUint64(&n.nextID,1), Creation: 节点启动秒}`,node/node.go:2893-2898)。
3. `factory()` 产出 behavior,记录 `ProcessKind()`(node/node.go:2946-2951)。
4. **early registration**:`n.processes.Store` 在 ProcessInit 之前(node/node.go:2981)——Init 期间即可被 Link/Monitor/RegisterEvent/RegisterName 寻址。
5. ProcessInit 在独立 goroutine 执行,**带超时裁决**:超时方 Kill,完成方 CAS "completed" 胜出回传 err(node/node.go:3024-3092)。`InitTimeout` 零值=5s,上限 15s(gen/process.go:1047-1052)。
6. LinkParent 落链 → 状态置 Sleep → `p.run()`(兜 Init 期间 self-send,node/node.go:3139-3154)。

Init 失败:进程不注册,Spawn 返回该 error;Init panic→TerminateReasonPanic。

## 4. 消息模型:Send / Call / Response / Forward

- **Send**:`process.Send(to any)` 按 PID/ProcessID/Alias/Atom(string)分派(node/process.go:711-736)→ `RouteSendPID`(node/core.go:14)本地按优先级选队列,消息壳 `gen.MailboxMessage{From, Ref, Type, Target, Message, Tracing}` 从 sync.Pool 取(gen/mailbox.go:30-40)。邮箱满→有 Fallback 则转投 fallback 进程,否则 `ErrProcessMailboxFull`(**不丢消息,返回错误**)。`SendImportant` 对远端投递要 ACK。
- **Call**:`Call = CallWithTimeout(默认 5s)`(node/process.go:1229)→ 请求带 Ref 进目标 mailbox(Type=Request)→ 调用方 `waitResponse`(node/process.go:2309):状态 CAS Running→WaitResponse,select timer/response;ref 不匹配的迟到响应丢弃;v3.3 修了 timer/response 竞态(超时先 drain 再判超时)。**self-call 直接 ErrNotAllowed;Init 态禁 Call**。响应走专用旁路:`response chan`(容量 10,node/process.go:66),`RouteSendResponse` 本地非阻塞推入,无人等→`ErrResponseIgnored`。超时取消的本质是**没有取消**:调用方离开,迟到响应靠 ref 校验丢弃。
- **HandleCall 四种返回组合**(act/actor.go:253-288):
  1. `(result, nil)` → 自动 SendResponse,继续跑;
  2. `(nil, nil)` → 异步,稍后自己 `SendResponse/SendResponseError`;
  3. `(result, TerminateReasonNormal)` → **先回复后正常终止**("reply, then stop" 官方惯用法);
  4. `(x, 其他 error)` → 终止且不回复,调用方只能超时——**同步报错必须用 SendResponseError**,HandleCall 返回的 error 不会传给调用方。
- **Forward**(node/process.go:2240-2277):把**已取出的 MailboxMessage 原壳**转投他人队列,零拷贝,messagesIn/Out 双计——act.Pool/Router 的转发基石。
- **exit 也是消息**:`RouteSendExit → sendExitMessage` 把 Exit 消息 Push 进目标 **Urgent 队列**(node/core.go:1595-1614),由目标进程自己的 goroutine 处理;`node.Kill` 才是强杀(打 Zombee,goroutine 自行发现后收尾,node/node.go:1949-1983)。

## 5. act 行为库:五个骨架

业务结构体内嵌 act 骨架,官方惯用法(testing/tests/local/node_test.go:13-15):
```go
type MyActor struct {
    act.Actor          // 内嵌:自动满足 gen.ProcessBehavior;Name()/Log()/Env() 来自内嵌 gen.Process
    state string
}
func (a *MyActor) Init(args ...any) error { return nil }
func (a *MyActor) HandleMessage(from gen.PID, message any) error { return nil } // 非 nil→终止
func (a *MyActor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) { return "pong", nil }
func (a *MyActor) Terminate(reason error) {}
func factoryMyActor() gen.ProcessBehavior { return &MyActor{} }
```
桥接机制:node 只认 `gen.ProcessBehavior`;`act.Actor.ProcessInit` 用 `process.Behavior().(ActorBehavior)` 类型断言拿到用户实现(act/actor.go:110-127),ProcessRun 分派到用户回调。除 `Supervisor.Init`/`Pool.Init`/`Router.Init+RouteMessage+RouteCall` 外所有回调可选。

### Actor(act/actor.go)
- `SetTrapExit(true)`:非父进程发的 exit 降级为普通消息;**父进程的 exit 无条件终止**(act/actor.go:299-306)。
- `SetSplitHandle(true)`:按寻址目标分流 HandleMessage/HandleMessageName/HandleMessageAlias。
- Terminate reason:`TerminateReasonNormal/Kill/Panic/Shutdown` 四哨兵(gen/process.go:211-231);link/monitor 对端死亡是包装 error(需 Unwrap)。

### Supervisor(act/supervisor.go + _ofo/_arfo/_sofo)
- 4 种 Type:OFO/AFO/RFO/SOFO(act/supervisor.go:96-117);策略 Transient(默认,仅异常重启)/Temporary/Permanent/Inherit(:136-156)。
- **v3.3 新增**:per-child `SupervisorChildRestart` 覆盖(仅 OFO/SOFO 支持,AFO/RFO 校验拒绝 :845-848)、`OnExceed`(超 intensity 后默认杀掉自己,可选 `OnExceedDisable` 只禁 child,:158-183)。
- child 双向 link(LinkChild+LinkParent,:658-659);重启可**过继 mailbox**(`PreserveMailbox` + `adoptMailbox`,:660-662;node/process.go:1997)。
- **与 Erlang 相反的默认**:孩子全部正常退出→supervisor 自杀(auto-shutdown 默认开,`DisableAutoShutdown` 关闭;Permanent 策略下忽略)。SOFO 需显式 StartChild,不自动启动。

### Pool(act/pool.go)
- Round-robin 转发到 worker(`Forward`),worker 死→**惰性 respawn**(forward 撞上才重建);**任何 worker 的 exit 消息会连带给池子致命错误**(act/pool.go:284-287)——高可用需 supervisor 包池或改用 Router。
- High/Max 优先级消息走池自身 admin 回调,不转发。

### Router(v3.3 新增,act/router.go,894 行)
- 用户实现 `RouteMessage/RouteCall` 决定目标 route 名;`RouteDiscard` 丢弃(Call 方收 `gen.ErrDiscarded`);管理流量走 High/Max 优先级进 HandleMessage/HandleCall。
- 命名 route + eager respawn(worker exit 即重建,:731-780)+ AddRoute/RemoveRoute/DisableRoute/ReplaceRoute 管理 API(pending 期间返回 ErrBusy);失败投递回吐 `MessageRouteFailed` 给路由器自身处理。

### WebWorker(act/web_worker.go)
- 配合 `meta.CreateWebHandler` 把 HTTP 转 `MessageWebRequest`,按 method 分发 HandleGet/Post/...,保证 `Done()` 释放;默认 501,超时 504。

## 6. 进程协作原语

- **link/monitor**(v3.3 重做为 `node/tm`):`targets sync.Map` + `reverse sync.Map` 双索引;关系链是带 tombstone 的无锁 MPSC 链表(≥32 个墓碑且 ≥live 时由唯一 walkClaim 赢家压缩,node/tm/relations.go:20-28)。终止扇出:link→Exit 消息、monitor→Down 消息(High 优先级)、远程→SendTerminate(node/tm/terminate.go:183-230)。注册后 recheck 防泄漏。
- **注册名**:三张 sync.Map——`processes`(PID→p)/`names`(Atom→p)/`aliases`(Alias→p)(node/node.go:88-90)。一个进程只能持一个注册名(`registered.CompareAndSwap`);Send-by-name 每次实时 `names.Load`,不缓存。PID 重启失配:Creation 变化→`ErrProcessIncarnation`。
- **定时器**:`lib.TakeTimer/ReleaseTimer` 池复用 `time.Timer`(池内预停止,首次 Reset 免竞争,lib/timer.go:8-34)——裸建 timer 应避免。`SendAfter`→`time.AfterFunc` 回调里走 core.RouteSend*(绕状态检查);v3.3 新增 SendEvery/SendExitAfter。
- **事件**:`RegisterEvent(name, EventOptions)` 得 Ref 作 token;SendEvent 带 token(可委托);link/monitor 事件即订阅;v3.3 支持节点级 open events。
- **meta 进程**:`SpawnMeta` 挂靠宿主 Process,有自己的 Alias+双队列(system/main),线程自管(供 HTTP/IO 回调线程调用);网络关闭时完全可用。
- **Cron**:分钟级 timer + spool 队列 + 64 位 mask 位图;Day 与 WeekDay 同时指定按 OR(标准 cron 语义);时钟跳变检测。

## 7. 分布式层(单机可整体关闭)

四件套,全部仅在 `to.Node != 本机` 时触达(node/core.go:31-50 等每个 Route* 开头):

| 层 | 协议 | 作用 |
|---|---|---|
| net/handshake(EHS) | magic=87,5 种消息,cookie 双向 sha256 摘要认证 + TLS 指纹 | 连接建立;v3.3 两阶段 accept(先注册 ConnectionID 再补发 Accept,保证池化连接命中);双向连接合并(节点名字典序定单主)+ 池化 join(默认池 3) |
| net/proto(ENP) | 8 字节头,Send 101-107/Call 121-124/响应 129-130/包装层 Z 压缩、F 分片、T tracing、K keepalive | 跨节点消息;order=`from.ID%255` 分队列保序;每消息预留 128B 头空间零拷贝包裹 |
| net/edf | TLV 二进制,**不是** Erlang Distribution Format | 序列化:数值/string/[]byte/time/嵌套 struct/指针(v3.3)/注册类型;握手期交换 RegCache/AtomCache/ErrCache 压缩类型名;未注册类型直接报错 |
| net/registrar(ESRD) | 4499 端口,Register/Resolve | **只做 节点名→Route{Host,Port,TLS} 发现**;同机自选举(抢端口),跨机 UDP 询问;真正对位 Consul 的是外部 etcd/saturn registrar 模块 |

**单机部署可不用清单**(依赖闭包已核对):`gen.NodeOptions.Network.Mode = gen.NetworkModeDisabled` 一行即整体关闭(node/network.go:1309-1312)——network/acceptor/handshake/proto/registrar/edf 全部不实例化;本地 RouteSend*/注册名/tm/meta/cron/application 不受影响。分布式四协议留待组网时接入,接口不变。

## 8. Application 与 system 应用

- 应用生命周期:`ApplicationLoad→ApplicationStart`:先递归起 Depends,再按 `spec.Group` 逐个 spawn;任一失败即回滚 Kill 已起成员。v3.3 四阶段 Initializing→Running→Stopping,**Terminate 回调推迟到全部进程结束后**(node/application.go:240-355,573+)。应用环境三层合并:node env < spec.Env < start options Env。
- `app/system` 每节点自动注入(ergo.go:29-33):`system_sup`(OFO,Permanent)下挂 `system_inspect`(27+ 只读检查能力,事件式快照)与 `system_manage`(act.Pool,20 worker,变更操作带前/后 ref.IsAlive() 检查与回滚)。旧版 metrics 上报已删除。
- ApplicationSpec 支持 Weight(负载均衡)/Tags(蓝绿、金丝雀)/Map(角色→进程名映射)。

## 9. 测试体系(可直接借鉴到 GServer)

五层:check(共享断言语法 `For/Where/Once/None/Within`)← mock(12 个 gen 接口假件,per-method `On<Method>` override)← unit(mock node 单 actor,同步确定性,egress 全记录)← stage(真节点多节点端到端,内存 registrar 隔离并行)+ tests(框架自测,local 50 文件/distributed 19 文件)。

设计哲学:(mock 接口文件注释与 testing/README.md)
- "drive input → assert on records",不戳私有状态;
- ingress 由测试主动 Deliver,"assert a reaction to a delivery, not the delivery itself";
- 负路径一等公民:`OnCall().Fail(gen.ErrTimeout)`;
- harness 靠 `WrapCore/WrapProcess` 装饰器观测真实节点,而非侵入内核。

全仓 ~208 个测试文件;gen 30 个接口全部有对应 mock。

## 10. 版本演进要点(v3.0→v3.3)与陷阱清单

时间线: v3.0.0(2024-09,从零重写)→ v3.1.0(2025-09,Cron/meta.Port/JSON logger/extra library 拆独立 module)→ v3.2.0(2026-02,InitTimeout/Ref.Deadline/Node 级 Call/mTLS/NAT/Init 期间可用面大增)→ v3.3.0(2026-09,分布式 tracing/四层测试框架/act.Router/open events/无锁 TargetManager/EDF 指针)。

**GServer 迁移相关陷阱**(源码证据见上文):
1. **v3.3 默认值已变**:分片/keepalive/tracing/wrapped-errors 默认全开(gen/default.go:40-55),对照检查 NodeOptions。
2. `HandleCall` 返回 error **不会**回传调用方(只有 result=Normal+result≠nil 会回);同步报错用 `SendResponseError`。
3. handler 长阻塞会饿死本进程消息;空闲进程 goroutine 会退出(睡眠-唤醒),与 protoactor 常驻 mailbox 循环不同,不要在 process 内缓存"当前 goroutine"假设。
4. supervisor auto-shutdown 默认开(与 Erlang 直觉相反);pool worker exit 连带池死。
5. 包级 `edf.RegisterTypeOf` 已弃用,改 `node.Network().RegisterType`(跨节点消息前必须注册自定义类型)。
6. `MailboxSize`>0 才限长;限长队列 Push 原子占位,`Len()` 近似值。
7. Call 默认超时 5s、InitTimeout 默认 5s(上限 15s);Ref 内嵌 deadline。
8. PID 含 Creation(节点启动秒):节点重启后旧 PID 失配(ErrProcessIncarnation)——GServer 的 ownership 层需注意。
9. 定时器用 lib timer 池;跨 goroutine 可变配置(priority/compression/log level)已全部原子化,运行时可调。
10. fork 补丁提示:fork 的 `Whereis` 需 rebase 到 v3.3(上游 `ProcessPID` 已提供等价能力,可能可直接弃用 fork 补丁)。

## 11. 官方文档逐章精读(docs.ergo.services,2026-09-08 起)

### 第 1 章 Actor Model(basics/actor-model)

- 心智模型:actor = 私有状态 + 行为 + 邮箱;逐条串行处理消息;只做三件事(发消息/建 actor/决定下一条怎么处理)。
- **关键边界(Go 特有)**:内存隔离是纪律不是约束——同节点消息按 Go 值直接投递,**零拷贝零序列化**;发 map/slice/pointer 则两进程共享内存。跨节点消息才编码(即拷贝)。规则:发值不引用,或发送即放弃所有权。`argus` vet 工具 A1001 规则检测此违规。
- 对 GServer:同节点高频路径(如 chat 广播)本地 Send 无序列化开销,这是相对 protoactor 的性能预期基础;但共享可变消息体就是 data race,迁移审查点。

### 第 2 章 Generic Types(basics/generic-types)

寻址五类型(源码 gen/types.go 已核对字段):

| 类型 | 结构 | 用途 | 打印形态 |
|---|---|---|---|
| `gen.Atom` | string | 节点名/进程名/事件名;网络栈对 atom 做缓存映射省带宽 | `'name'` |
| `gen.PID` | Node+ID(uint64)+Creation(int64) | 唯一进程标识;Creation=节点启动代际,重启即变 | `<CRC32.hi.lo>` |
| `gen.ProcessID` | Name+Node | 按注册名寻址,跨重启稳定 | `<CRC32.name>` |
| `gen.Ref` | Node+Creation+ID[3]uint64 | Call 关联/事件 token;ID[2] 可嵌 deadline,`IsAlive()` 判过期 | `Ref#<CRC32.a.b.c>` |
| `gen.Alias` | = Ref 结构 | 临时地址,无需注册名;meta 进程主标识 | `Alias#<...>` |

- 打印里的 CRC32 是节点名 CRC32(自定义多项式 crc32q)——日志可读性设计,非加密。
- `gen.Env`:环境变量名大小写不敏感,统一转大写。
- **`gen.Error`**(exit reason 专用,gen/errors.go:20):`{Msg, Wrapped []error, Mailbox *ProcessMailbox}`;`gen.Errorf` ≈ fmt.Errorf 但保留 wrap 链;**`Unwrap() []error` 多错误形态,`errors.Unwrap`/`errors.Join` 对它返回 nil**,判型必须 `errors.Is/As`。Mailbox 字段 `edf:"-"` 不上网。
- 两大内建用途:supervisor 重启预算溢出 reason(`errors.Is(err, gen.ErrExceeded)` 可查)、panic 进程邮箱捕获(`PreserveMailbox`)随重启回放。
- 接口分层:`gen.Node`(任意 goroutine 可调)/`gen.Process`(状态机门禁)/`gen.Network`/`gen.RemoteNode`/`gen.Application`。业务结构体内嵌 `act.Actor` 即获得 Process 方法面。
- 对 GServer:PID.Creation 语义 = P0.2 node naming 决策的强约束——节点重启后旧 PID 全部失配(ErrProcessIncarnation),Redis ownership 记录的 PID 必须接受代际翻转;`gen.Error` 的 Wrapped 语义是 P0.3 Call 错误通道选型的关键事实(跨节点错误身份可保留,前提是 sentinel 已注册)。
