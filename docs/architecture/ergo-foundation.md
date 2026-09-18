# ergo 地基勘测（v1.999.330）

> **本文件零约束力。** 它是勘测记录，不是规则。有约束力的只有 `invariants.md` 与状态为 `Accepted` 的 ADR。
> 本文件只记录 ergo 保证什么，**不含任何迁移取舍**。

- 日期：2026-09-18
- 对象：`ergo.services/ergo v1.999.330`（模块路径不带 `/v3`）
- 方法：源码阅读（7 个切面并行勘测），全部事实带 `文件:行号` 证据（相对模块根）。**未做运行实测。**

## 0. 与 `ergo-migration-notes.md` 的差异（勘测修正）

| 旧记录 | 勘测结果 |
|---|---|
| 「端口可交由系统分配（`AcceptorOptions{Port: 0}`）」 | **不符**。`Port=0` 被改写为 `gen.DefaultPort`(11144)，再按 `PortRange` 尝试监听；**不是**系统随机端口。`node/network.go:1543-1610` |
| 「写 `NetworkFlags` 字面量会静默关闭未列出能力」 | **需分网络级/acceptor 级**。网络级：仅当 `Enable==false` 才整体替换为 `DefaultNetworkFlags`，故写 `Enable=true` 的字面量确实让未列出项保持 false。acceptor 级另有一条代入链（handshake → 网络级 options → `DefaultNetworkFlags`），即使字面量 `Enable=false` 也会被逐级代入。`node/network.go:1334-1337,1434-1438,1641-1643` |
| 「静态路由不设版本则用默认」 | **仅对 `AddRoute` 成立**。`GetConnection` 走 Resolver 的分支直接使用返回的 Route，**无**版本补全。`node/network.go:148-158,220-257,849-914` |
| （未记录） | **`PreserveMailbox` 在 AllForOne / RestForOne 下明确不支持**。`act/supervisor.go:819-824` |
| （未记录） | **`HandleCall` 返回 error 会终止 actor**，且不会给 caller 成功响应（caller 通常超时）。`act/actor.go:234-287` |

## 1. 进程生命周期

### 状态与存活

| 保证 | 证据 |
|---|---|
| alive = 状态属于 `Init`/`Sleep`/`Running`/`WaitResponse`；`Terminated`、`Zombee` 不 alive | `node/process.go:2281-2288` |
| `Init` 正在跑 `ProcessInit`（用户回调可见）；`Sleep` 空闲无用户回调；`Running` 正在处理消息；`WaitResponse` 阻塞于 Call；`Terminated` 正在/已执行 `ProcessTerminate`；`Zombee` 是运行中被 Kill 后的终态 | `gen/process.go:151-202,317-323` |

### 创建

| 保证 | 证据 |
|---|---|
| `Node.Spawn` / `SpawnRegister` / `Process.Spawn` / `SpawnRegister` 都构造带 `InitTimeout` 截止的 Ref 后走同一个 `n.spawn`，默认阻塞等待 Init 结果 | `node/node.go:431-500`；`node/process.go:164-255` |
| `deadline==0` 时在当前调用 goroutine 同步执行 `ProcessInit`；有超时时另起 goroutine，调用方 select 等 errCh 或 timer | `node/node.go:3013-3019,3038-3096` |
| Init 超时由调用方赢：`Kill` 置 Zombee（不注销）、立即返回 `ErrTimeout`；Init goroutine 稍后返回时自行 cleanup 并调 `ProcessTerminate(Kill)` | `node/node.go:3040-3058,3078-3091` |

### 创建顺序（关键）

```
mailbox 四队列初始化
  → names.LoadOrStore(name)        ← 名字已可见，但 p.pid 尚未写入
  → 生成 PID、写 p.pid、建 behavior/log
  → processes.Store(pid)
  → application 记账
  → ProcessInit
  → 状态 Init→Sleep、waitprocesses++、p.run()   ← 首次消费消息
```

证据：`node/node.go:2850-2879,2881-2907,2939-2950,2985-3008,3139-3158`；`node/process_run.go:15-25`

由此推出的四条保证：

- **名字在 Init 前已注册**，且 mailbox 在名字注册**之前**就已就绪（源码注释明示"before publishing the name"）；
- **名字可见 ⟹ PID 未必已写入**：存在"名可查、PID 为零值"的极短窗口；
- **Init 期间到达的消息排队而非被拒**：`isAlive` 接受 `Init`，Push 入队并递增 `messagesIn`；`p.run` 因 CAS 只接受 `Sleep` 而不启动；Init 成功后开始消费。`node/core.go:55-103,120-124`；`node/process_run.go:15-25`
- 邮箱满返回 `ErrProcessMailboxFull`（或走 fallback）；进程被清理后才返回未知/已终止。

### 失败与终止

> 下表中"Down 通知在 `cleanupProcess` 内发出"与"`ProcessTerminate` 回调在 `cleanup` 之后"两条，是**设计相关的关键事实**（原 `invariants.md` 第 8 条，2026-09-18 移入本文件）：它们决定了所有权释放不能依据死亡通知，必须放在终止回调内。

| 保证 | 证据 |
|---|---|
| Init 返回非 nil error：先 `cleanupProcess`（删 processes → 删名字并发 ProcessID terminate → 通知 PID targets）+ `application.removeProcess`，再**另起 goroutine** 调 `ProcessTerminate(initErr)`（带 recover），计 spawnFailed 后返回该 error。该路径**不**增加 waitprocesses，故不调 Done | `node/node.go:3098-3136,3160-3213` |
| `act.Actor` 的 `ProcessInit` 捕获 `behavior.Init` panic，记为 `LogLevelPanic` 并把返回值设为 `TerminateReasonPanic`，因此经 Actor 包装时走上述普通 Init-error 路径 | `act/actor.go:109-137` |
| **普通 behavior 无 spawn 外层 recover**：无超时时 panic 逸出调用栈；有超时时 panic 在 Init goroutine 内且不向 errCh 发送，调用方最终按 timer 超时 Kill；该 goroutine 无后续 cleanup 分支 | `node/node.go:3038-3096` |
| 正常终止顺序：state→`Terminated` → `unregisterProcess` → `cleanupProcess`（删 processes、删名字并发 ProcessID terminate、删 aliases 并通知、`RouteTerminatePID` → `targets.TerminatedProcess`、处理 metas） | `node/process_run.go:45-62`；`node/node.go:3160-3213` |
| **Down 通知在 `cleanupProcess` 的 `RouteTerminatePID` 内发出**：tm.Manager 先 `RemoveTarget` 再 dispatch；先批量发 link 的 exit，再逐个发 monitor 的 Down，最后处理远端；之后才 `targets.TerminatedProcess` | `node/node.go:3197-3198`；`node/core.go:1389-1400`；`node/tm/terminate.go:6-10,198-244` |
| **`ProcessTerminate` 回调在 `cleanup` 之后**：回调返回（含被 recover 的 panic）才执行 defer 记账（`application.processTerminated` → 非 system 进程 `waitprocesses.Done`）。**应用/节点关停屏障不会在回调返回前释放该进程** | `node/node.go:3227-3251` |

⇒ **推论：名字释放、Down 通知都严格早于 `ProcessTerminate`；只有关停屏障晚于它。**

### 停止的同步性

| 保证 | 证据 |
|---|---|
| `Kill` 非阻塞：运行态只置 Zombee 即返回；非运行态同步 cleanup 后用 `go n.finishProcess` 异步执行 Terminate | `node/node.go:1926-1989` |
| `SendExit` 只路由 exit signal 即返回，不等待目标终止或 Terminate；可调用态为 Init/Running/Terminated | `node/process.go:989-1011` |
| `Node.Stop` 同步：向进程发 shutdown exit → `waitProcessesWithEscalation` → `waitApplications` → 停网络；`StopForce` 跳过两个屏障直接 Kill | `node/node.go:1209-1274,1322-1358` |

## 2. 进程命名

| 保证 | 证据 |
|---|---|
| 同名注册是原子占位（`names.LoadOrStore`），单赢家；败者得 `ErrTaken`（`RegisterName` 还会复位 `registered` 标志） | `node/node.go:518-527,2897-2904` |
| **名字唯一性范围是单节点**：names 是 node 结构体上的 `sync.Map`；跨节点各自独立，同名可并存 | `node/node.go:88-90,2001-2014`；`node/core.go:155-204` |
| `ProcessPID` 是 `names.Load` 单键查找（不遍历进程集合），**Init 前已发布的名字可查到** | `node/node.go:2001-2014`；`gen/node.go:131-135` |
| **名字在 Terminate 回调之前就已从 names 删除** | `node/node.go:3186-3190,3230-3251` |
| 名字长度上限 255 **字节**（`len(name)`）；`ErrAtomTooLong` | `node/node.go:468-470,501-503` |
| **每进程最多一个名字**：第二次 `RegisterName`（即使不同名）因 CAS 失败返回 `ErrTaken`；**无原子换名 API** | `node/node.go:518-524`；`node/process.go:415-432` |
| `SpawnRegister` 的 register 为空即不注册；手动 `RegisterName("")` 无空名专门错误 | `node/node.go:2897,501-504` |
| `UnregisterName` 找不到名字返回 `ErrNameUnknown`；成功则删映射、清 `p.name`、复位标志，并以 `ErrUnregistered` 路由该名字的终止通知 | `node/node.go:532-549` |
| 按名本地路由：names 无此名 → `ErrProcessUnknown`；有但 `isAlive` 假 → `ErrProcessTerminated`；目标 Node 不同则走网络，不查本地 names | `node/core.go:155-208,893-934` |
| `PID`（Node/ID/Creation，直接地址）与 `ProcessID`（Name/Node，注册名地址）是两种结构、两条寻址路径 | `gen/types.go:47-74`；`node/core.go:34-76,144-204` |

## 3. 消息投递与错误

### 语义表

| 操作 | 本地 | 远程 |
|---|---|---|
| `Send` | 路由立即查表；目标不存在 `ErrProcessUnknown`，已终止 `ErrProcessTerminated`；入队成功同步 nil | 取连接失败同步返回错误；写入连接成功即 nil。远端目标不存在等投递后错误**仅在 `ImportantDelivery` 开启时**回发确认，否则静默 |
| `Call` | 同路由语义；成功后阻塞等响应，超时 `ErrTimeout` | 同左；远端"目标不存在/邮箱满/已终止"**不回送**，表现为超时 |
| `CallImportant` | — | 设 `ImportantDelivery=true`，远端路由错误作为响应错误返回 |

证据：`node/core.go:17-18,38-46,66-76`；`node/process.go:711-713,1225-1279`；`node/node.go:1418-1449,1516-1630`；`net/proto/connection.go:2087-2118,2324-2375`

**节点未运行时**：`Node.Send/Call/CallImportant` 统一同步返回 `ErrNodeTerminated`；`Process` 在 Init/Running/Terminated 允许 Send，Call 仅 Init/Running，否则 `ErrNotAllowed`。`node/node.go:1422-1425,1523-1529`；`node/process.go:721-748,1229-1247`

**完整错误哨兵**：`ErrProcessUnknown` / `ErrProcessTerminated` / `ErrProcessMailboxFull` / `ErrMetaMailboxFull` / `ErrProcessIncarnation` / `ErrNodeTerminated` / `ErrNoConnection` / `ErrNoRoute` / `ErrTimeout` / `ErrUnsupported` / `ErrTooLarge` / `ErrResponseIgnored`。`gen/errors.go:77-125`

**`ErrProcessIncarnation`**：跨节点 PID/Alias 的 Creation 与 peer 不符时，**在发包前**同步返回；按名（ProcessID）路径不做 Creation 检查。`net/proto/connection.go:549-550,633-684`

### 顺序

| 保证 | 证据 |
|---|---|
| 同一发送者→同一接收者的本地入队是单 MPSC 队列、FIFO | `lib/mpsc.go:48-68,79-88`；`node/core.go:78-85` |
| 优先级严格 `Urgent(Max)` → `System(High)` → `Main(Normal)` → `Log`；Log 队列取出后调 `HandleLog` | `act/actor.go:165-184`；`gen/process.go:175-186` |
| 网络默认 `KeepNetworkOrder=true`，order byte 按 sender PID ID 派生；关闭后 order=0 且不保序 | `net/proto/connection.go:554-558,622-625,686-690` |
| `keeporder` 默认 true，可在 Init/Running 用 `SetKeepNetworkOrder` 修改 | `node/node.go:2859-2861,800-812`；`node/process.go:414-418` |

### 请求-应答

| 保证 | 证据 |
|---|---|
| `Call` 请求以 `MailboxMessageTypeRequest` 带 Ref 入 mailbox；`HandleCall` 返回非 nil result 时框架 `SendResponse` 回传；返回 nil result 表示异步处理，caller 继续等后续 `SendResponse` | `node/core.go:757-875`；`act/actor.go:25-28,234-287` |
| **`HandleCall` 返回 error（除 `TerminateReasonNormal`+非 nil result）会终止 actor**，且不自动回成功响应，caller 通常超时 | `act/actor.go:234-287`；`node/process.go:1252-1279` |
| `SendResponse` / `SendResponseError` 均带 caller 的 ref；非 important 立即 nil，important 等远端确认；caller 收到响应后回 ack；本地 future 已超时则返回 `ErrResponseIgnored` | `node/process.go:1147-1218`；`node/core.go:482-756`；`node/node.go:1601-1628,1691-1717` |
| `Call` 超时后不归还池（迟到响应可能到达），迟到响应找不到 ref → `ErrResponseIgnored` | `node/node.go:1608-1632`；`node/core.go:505-537` |
| **`Call` 打到只实现 `HandleMessage` 的 actor 不报"方法缺失"**：默认 `HandleCall` 记 warning 返回 `(nil,nil)`，框架视为异步请求，caller 等到超时 | `act/actor.go:234-287,425-428` |

### 邮箱与编码

| 保证 | 证据 |
|---|---|
| 邮箱满**不阻塞**：有界队列 Push 原子计数超限即 false。普通 Send 无 fallback 返回 `ErrProcessMailboxFull`，有 fallback 则路由 `MessageFallback`；**Call 不走 fallback**，直接报错 | `lib/mpsc.go:34-69`；`node/core.go:97-112,230-245,395-410,843-967,1090-1097` |
| **本地消息零拷贝**：`RouteSend*`/`RouteCall*` 直接把 `any` 放入 mailbox，无 encode/decode，不要求 EDF | `node/core.go:87-92,220-225,339-344,821-829,930-936,1052-1058` |
| 跨节点必须 EDF 编码；编码失败返回错误；`Network.RegisterTypes` 向所有支持 registry 的协议注册；无可用 registry 返回 `ErrUnsupported`；跨节点 sentinel error 需 `RegisterError` 保身份 | `net/proto/connection.go:563-569,661-667,826-831`；`node/network.go:600-635`；`gen/network.go:140-154` |
| Init 期间进程已在 `processes` 中（名字更早），故按 PID 投递可找到入队；Init 超时/失败会清理并从 processes/name 删除 | `node/node.go:2859-2892,2930-2998,3020-3124` |
| 已终止进程：`isAlive=false` 后路由返回 `ErrProcessTerminated` | `node/core.go:66-76,204-209,757-770`；`node/process_run.go:45-80` |

## 4. 监督树

| 保证 | 证据 |
|---|---|
| 策略：OneForOne 仅影响该子；AllForOne 终止并重启全部；RestForOne 先终止该子之后的、再重启该子及其后；SimpleOneForOne 是同类型动态实例，重启保留实例参数与状态 | `act/supervisor.go:98-116`；`act/supervisor_arfo.go:409-414`；`act/supervisor_sofo.go:107-135,208-215` |
| 子策略：`Transient` 仅异常 reason 重启；`Temporary` 从不；`Permanent` 总是；子级 Strategy 覆盖 supervisor 级 | `act/supervisor.go:132-148,211-218,849-856` |
| 重启强度是**滑动窗口**：窗口内记录数超过 `Intensity` 才算 exceeded | `act/supervisor.go:729-748` |
| OFO：无 per-child Intensity 用全局计数，设置了用该 spec 专用计数；SOFO 对应每实例专用计数；**AFO/RFO 只用全局，per-child Intensity 被拒绝** | `act/supervisor_ofo.go:298-322`；`act/supervisor_sofo.go:177-202`；`act/supervisor_arfo.go:369-377`；`act/supervisor.go:845-847` |
| 超限默认 `OnExceedTerminateSupervisor`：终止运行中子并进入 supervisor shutdown（三种策略皆然） | `act/supervisor_ofo.go:350-367`；`act/supervisor_arfo.go:379-390`；`act/supervisor_sofo.go:223-245` |
| `OnExceedDisable` 仅对启用专用计数的 child 生效。OFO 禁用该 spec 并清本地计数，supervisor 通常存活（若是最后运行子且启用 auto-shutdown 则终止）；SOFO 只丢弃超限实例，spec 仍可 `StartChild` | `act/supervisor.go:177-182`；`act/supervisor_ofo.go:334-348`；`act/supervisor_sofo.go:218-222` |
| **`PreserveMailbox` 仅在异常终止（panic / 异常 callback 返回 / kill）捕获 mailbox**，`Normal`/`Shutdown` 不捕获 | `gen/process.go:1054-1068` |
| 重启时 `extractMailbox` 用 `errors.As` 取出并清空原 `Error.Mailbox`，OFO/SOFO 将其作为新实例的 `Options.Mailbox` | `act/supervisor.go:854-862,660-664`；`act/supervisor_ofo.go:326-331`；`act/supervisor_sofo.go:208-215` |
| **AFO/RFO 的 child `PreserveMailbox` 明确不支持** | `act/supervisor.go:819-824` |
| `EnableHandleChild` 下 `HandleChildStart`/`HandleChildTerminate` 经 supervisor 自身 Main queue 的**普通消息**调用，且在重启策略**完成之后**处理；回调不参与重启决策 | `act/supervisor.go:191-193,488-505,548-561,657-678` |
| **源码不保证 `HandleChildTerminate` 等待子进程 `Terminate` 回调完成**（子先 cleanup/发 Exit，再 `ProcessTerminate`） | `node/node.go:3215-3251`；`node/process_run.go:52-59` |
| `StartChild` 是否注册名字：**OFO / AFO / RFO = 是（register=true）；SOFO = 否** | `act/supervisor_ofo.go:35-47,74-85`；`act/supervisor_arfo.go:39-54,89-100`；`act/supervisor_sofo.go:48-66,82-95` |
| `handleAction` 仅 register=true 时 `SpawnRegister`，否则 `Spawn`；`Children()` 的 Name 仅在 register=true 时填充，Spec 始终是 spec.Name | `act/supervisor.go:650-658,904-923` |
| Supervisor `HandleMessage` 返回非 nil error → 交给 `childTerminated` 走终止路径（接口契约：导致 supervisor 终止） | `act/supervisor.go:33-37,488-505` |
| Init 要求 Children 至少一个 spec，并校验 Name/Factory 与名字唯一 | `act/supervisor.go:359-380` |
| SOFO 要求占位 spec 但 init 不自动启动；SOFO 实例正常终止只移除实例、**不触发 auto-shutdown**，且 `DisableAutoShutdown` 对 SOFO 被忽略 | `act/supervisor_sofo.go:48-66,157-168`；`act/supervisor.go:198-201` |
| OFO/AFO/RFO 按声明顺序从第一个 spec 启动；`Transient`/`Temporary` 子正常终止且无其他运行子时，启用 auto-shutdown 会终止 supervisor | `act/supervisor_ofo.go:35-53,225-268`；`act/supervisor_arfo.go:39-58,318-333` |
| link 关系产生 `MessageExitPID`，monitor 关系产生 `MessageDownPID`（tm 按 KindLink 发 Exit、其他发 Down） | `node/tm/terminate.go:149-177`；`gen/message.go:5-45` |
| **远端终止走 `Connection.SendTerminate*`**，不是 mailbox 的 gen 消息；Supervisor 的 ProcessRun 只处理 Exit 分支，**不处理 Down** | `node/tm/terminate.go:178-210`；`act/supervisor.go:544-589` |
| Supervisor 自身终止：对运行子 `SendExit(reason)`（mailbox full 则 `Node.Kill`），等全部子终止后才返回终止原因，之后调 `behavior.Terminate` | `act/supervisor.go:687-706,776-778` |

## 5. 节点与网络

### 节点名

| 保证 | 证据 |
|---|---|
| 节点名必须按单个 `@` 分为恰好两段且都非空；节点段须合法 UTF-8，禁止 `:/?#%`、空格、非打印字符。host 段无同等校验 | `node/node.go:173-196` |
| `Atom.Host()` 仅在恰好两段时返回第二段，**不会**再拆端口 | `gen/types.go:26-31` |
| 同 host 上节点名必须唯一（默认 registrar 语义下） | 见下 |

### 端口

| 保证 | 证据 |
|---|---|
| 未提供 acceptor 时自动创建，Host 取节点名 host 段，Port 取 `gen.DefaultPort`(11144) | `node/network.go:1383-1392`；`gen/default.go:21-24` |
| **`Port=0` 不是系统随机端口**：改写为 `DefaultPort`，再按 `PortRange` 尝试监听；成功端口写入运行时 `acceptor.port` | `node/network.go:1543-1610` |
| 实际端口可在网络 ready 后经 `Acceptors()` → `Acceptor.Info().Interface` 读到（底层 listener `Addr()`）；对外 Route 在 `RoutePort=0` 时用实际端口 | `node/network.go:1455-1461`；`node/acceptor.go:59-78` |

### Flags

| 保证 | 证据 |
|---|---|
| 网络级：`options.Flags.Enable==false` 时**整体替换**为 `DefaultNetworkFlags`（默认启用远程 spawn、远程 application start、fragmentation、proxy accept、important delivery、simultaneous connect、clock skew、tracing、wrapped errors，及 15s software keepalive） | `node/network.go:1334-1337`；`gen/default.go:40-53` |
| acceptor 级代入链：字面量 `Enable=false` → 试 handshake 的 NetworkFlags → 仍 false 用网络级 options.Flags → 仍 false 在 `startAcceptor` 整体替换为 `DefaultNetworkFlags` | `node/network.go:1434-1438,1641-1643`；`node/acceptor.go:42-52` |

### 连接与路由

| 保证 | 证据 |
|---|---|
| `GetConnection` 先查**按节点名**的连接缓存，命中即返回；未命中且网络未 ready 或无 registrar → `ErrNoRoute` | `node/network.go:791-807` |
| **静态 route 匹配后独占**：按权重尝试全部匹配项（含其 Resolver），全部失败即 `ErrNoRoute`，**不回退** registrar；静态 proxy 同理 | `node/network.go:814-928,931-989` |
| 无静态匹配时才调 `Resolver().Resolve`，直连候选全失败再 `ResolveProxy` | `node/network.go:991-1068` |
| `AddRoute` 加入时为空版本补当前默认 handshake/proto 版本；`GetNodeWithRoute` 的 Resolver 路径按"调用者版本为空才补"处理；**`GetConnection` 的 route-with-Resolver 分支直接用返回的 Route，无补全** | `node/network.go:148-158,220-257,849-914` |
| `Resolve` **无独立长期缓存**；只在连接缓存 miss 时进入解析。默认 registrar 的 `Nodes()` 另有 3 秒缓存，不等同于 Resolve 缓存 | `node/network.go:791-807`；`net/registrar/client.go:194-233` |

### Registrar 接口

| 保证 | 证据 |
|---|---|
| `gen.Registrar`：`Register`、`Resolver`、`RegisterProxy/UnregisterProxy`、`RegisterApplicationRoute/UnregisterApplicationRoute`、`Nodes`、`Config/ConfigItem`、`Event`、`Info`、`Terminate`、`Version`；`gen.Resolver`：`Resolve`、`ResolveProxy`、`ResolveApplication` | `gen/registrar.go:46-168` |
| `RegistrarInfo` 的能力标志分别表示 proxy 注册、application route、集中式 Config、事件通知是否受支持，并给出 Server、是否 embedded、Version | `gen/registrar.go:203-242` |
| **默认 registrar 才有"重复节点名拒绝"**：本地 server 以节点名为唯一键，已存在则 `gen.ErrTaken`；客户端对 `ErrTaken` 等非网络错误**不重试**直接失败 | `net/registrar/server.go:267-276,294-308`；`net/registrar/client.go:399-423` |
| **自定义 registrar 的重复名拒绝条件与错误不由 ergo 接口规定**：节点启动只调用其 `Register`，收到错误即包装返回，是否冲突由实现决定 | `node/network.go:1474-1494`；`gen/registrar.go:46-54` |
| 默认 registrar client 的 `Resolve`：同 host 且有本地 embedded server 时用本地 server，否则按节点名的 host 拼 registrar 端口发 UDP；未知节点返回 `gen.ErrUnknown` | `net/registrar/client.go:73-100`；`net/registrar/server.go:353-365` |

### 保活、重连、认证

| 保证 | 证据 |
|---|---|
| TCP listener/dialer 使用默认 keepalive（15s）；应用层 software keepalive 需双方 period 都 >0 才启用，超时阈值 = 对端 period × misses，超时终止连接，keepalive 帧被静默消费 | `node/network.go:1048-1051,1543-1546`；`net/proto/enp.go:117-134`；`net/proto/connection.go:1931-1979` |
| 连接池成员断开即移除；池空则标记 terminated，下次调用重新拨号；池未满则 filler 持续补齐 | `net/proto/connection.go:1769-1799,1980-2013` |
| 默认 registrar 的注册控制连接断开后循环 `tryRegister` 直到成功或 client 终止 | `net/registrar/client.go:623-653` |
| Cookie 为空时启动生成随机 16 字符；acceptor 与出站 route 未指定时继承节点 Cookie；Cookie 纳入握手 challenge-response digest，不一致则握手失败；参与确定性 ConnectionID 的 HMAC | `node/network.go:1320-1323,1586-1588,1090-1104`；`net/handshake/start.go:20-79`；`net/handshake/negotiate.go:27-73,110-120` |

## 6. 应用与 meta-process

| 保证 | 证据 |
|---|---|
| 回调顺序：`PreLoad/Load`（加载）→ `Init`（启动前）→ Group 成员启动进入 Running → `Start`（启动后）；停止：`Stop`（仅已到 Running 且非强制）→ 等 Group 成员及应用全部进程终止 → `Terminate`。**Init 返回错误时不调用 Terminate** | `gen/application.go:93-112`；`node/application.go:240-346,398-451,487-538` |
| 默认超时 Init/Start/Stop 各 15s；`ApplicationOptions` 非零值覆盖 Spec，Spec 非零值覆盖默认 | `gen/default.go:23-30`；`gen/application.go:115-136,201-208`；`node/application.go:272-273,344-345,421-424` |
| `ApplicationSpec`：Name（唯一标识，与进程名分离）、Description/Version、Group（随生命周期启动的直接成员）、Mode（Temporary/Transient/Permanent）、Map（逻辑角色→进程名）、Env、LogLevel | `gen/application.go:139-198,210-231` |
| `Depends.Applications` 递归启动，未知或循环依赖 → `ErrApplicationDepends`；`Depends.Network` 仅检查 network 非 nil | `node/node.go:2057-2060,2233-2281` |
| `Network.RegisterTypes/RegisterErrors/RegisterAtoms` 在 `ApplicationLoad`、任何应用进程创建前写入节点网络注册表；网络 Disabled 时静默忽略 | `gen/application.go:151-163`；`node/node.go:2079-2094` |
| `Weight` 用于应用实例路由负载，负值使实例退出 resolver 结果；`Tags` 发布给 registrar，可动态增删并触发路由更新 | `gen/application.go:30-38,171-186`；`node/application.go:65-105` |
| 应用记账覆盖 Group 成员及**其后代**；spawn 在 `ProcessInit` **前**加入记账，若应用不处于 Initializing/Running 则回滚并返回 `ErrApplicationStopping` ⇒ **进入 Stopping 后不能再创建该应用的进程** | `node/node.go:2980-2999`；`node/application.go:573-640` |
| 应用停止：先退出直接 Group 成员（每个成员是其监督子树根，退出会带下子树），再等并停止仍记账但已脱离 Group 的进程，超时后分阶段 Kill | `node/application.go:420-486,573-618` |
| 系统应用 Group 仅 `system_sup`：OneForOne + 默认 Permanent，故子进程正常退出也会重启；受重启强度限制 | `app/system/app.go:54-70`；`app/system/sup.go:14-32` |
| `Ref.IsAlive()` 仅看 `Ref.ID[2]` 的 Unix 秒 deadline：0 表示永不过期；Application.Init 的 ref 超时后可见 false，最终返回 `ErrTimeout` | `gen/types.go:116-123`；`node/application.go:150-191` |
| meta-process 是阻塞世界与 actor 世界的桥：`Start` 在独立可长期阻塞的 goroutine；Sleep/Running 可 Send 与 Spawn，Terminated 不可 Spawn | `gen/meta.go:28-57,58-76,109-113`；`node/meta.go:136-144` |
| **meta-process 的 `Spawn` 只接受 `(gen.MetaBehavior, gen.MetaOptions)`**，无法创建普通进程；普通进程创建 API 是 `Process.Spawn/SpawnRegister` | `gen/meta.go:109-113`；`node/meta.go:136-144`；`gen/process.go:278-297` |

## 7. 事件与定时

| 保证 | 证据 |
|---|---|
| 事件注册生成唯一 token；默认要求 producer 持 token 才能 `SendEvent`，`EventOptions.Open=true` 取消该要求但仍只允许 producer Unregister；`Notify=true` 在首个订阅者出现/订阅归零时向 producer 发 `MessageEventStart`/`MessageEventStop` | `node/tm/event.go:113-153,165-167,168-220,299-322` |
| `Buffer>0` 是固定大小环形缓冲，Link/Monitor 返回订阅时快照，边界并发下**可能重复一次** | `node/tm/event.go:30-53`；`CHANGELOG.md:16` |
| `SendEvent` 进入发布计数（按本地 producer / 本地 subscriber / 远端节点）；**无订阅者时发布仍成功** | `node/process.go:965-984`；`node/tm/event.go:100-109,299-351` |
| 跨节点事件扇出**按接收节点聚合**：每个有远端订阅的节点最多一次 `connection.SendEvent`，远端再向本地 subscriber 扇出；buffered 远端事件每订阅者单独 wire-call 取新快照 | `node/tm/event.go:324-351`；`node/tm/wire.go:72-112` |
| `CoreEvent` 是节点 core 拥有、始终可用、带缓冲的本地事件总线，不依赖网络或 registrar；含本节点应用 Started/Stopped 与远端 NodeConnected/NodeDisconnected；注册时强制 `Notify=false` | `gen/core_events.go:3-42`；`node/core.go:1545-1548`；`node/node.go:305-314` |
| `SendAfter`/`SendEvery`/`SendExitAfter` 均可在 Init 或 Running 调用并返回 `CancelFunc`；`SendEvery` 要求 period>0 且复用单 timer；`SendExitAfter` 目标是自身/parent/leader 时被拒绝 | `gen/process.go:516-537,556-564`；`node/process.go:888-953,1015-1044` |
| **定时器不是生命周期记账项**：`SendEvery` 回调检查发送方 `isAlive`，进程不存活后停止复用 timer；`SendAfter`/`SendExitAfter` 的一次性回调**无**发送方存活检查，只能靠 `CancelFunc` 取消 | `node/process.go:897-944,1027-1042,2290-2301` |
| `NodeOptions.Cron` 在节点启动时预注册；Spec 恰 5 段（minute hour day month weekday），调度器每分钟 tick，**最小粒度分钟**；weekday 允许 1..7，Go Sunday=0 转为 7；同时给 day 与 weekday 用 OR | `gen/cron.go:6-13,44-60`；`node/cron.go:37-84`；`node/cron_parse.go:26-43,178-221` |
| 优雅关闭默认 `ShutdownTimeout=3min`：停应用 → 向存活进程发 shutdown exit → 等全部记账进程（WaitGroup 聚合，非串行；每 5s 记录卡住进程）→ 超时 Kill 剩余 → 再等 5s → 仍未结束 `os.Exit(1)`；随后最多再等应用 teardown 5s | `gen/default.go:23-24`；`node/node.go:1224-1277,1298-1320,1322-1368` |

## 8. 可观测性

### 日志

| 保证 | 证据 |
|---|---|
| `gen.LoggerBehavior` 仅 `Log(message gen.MessageLog)` 与 `Terminate()`；`Log` 对每条命中过滤器的日志调用，`Terminate` 在移除 logger 或节点停止时调用；接口注释要求实现避免阻塞（应异步排队） | `gen/log.go:125-143` |
| `LoggerAdd`/`LoggerAddPID` 仅在节点 **Running** 可用；空过滤器替换为 `gen.DefaultLogFilter`；重复名 `ErrTaken`，空名/空实现 `ErrIncorrect`；启动期先注册默认 logger 与 `NodeOptions.Log.Loggers` | `gen/node.go:478-504`；`node/node.go:2459-2504,262-269` |
| 默认 logger 写 `os.Stdout`（可用 `DefaultLoggerOptions.Output` 改写），支持纯文本或 `EnableJSON`，`Disable` 完全关闭；启动默认名 `default` | `gen/default_logger.go:30-119` |
| 级别：`system(-100)`、`trace(-2)`、`debug(-1)`、`default(0，继承)`、`info(1)`、`warning(2)`、`error(3)`、`panic(4)`、`disabled(5)`；进程默认 info | `gen/types.go:284-321`；`gen/process.go:1028-1032` |
| `Log` 接口提供 Level/SetLevel、Logger/SetLogger、Fields 族、Trace…Panic；`SetLogger` 非空名限定到指定 logger，空名恢复向所有非 hidden logger fan-out；`Panic` 方法**只记录级别，不触发 Go panic** | `gen/log.go:7-123`；`node/log.go:17-127`；`node/node.go:2764-2808` |
| **框架捕获 process goroutine panic 后记 `LogLevelPanic`，把进程状态设为 Terminated、注销并 finishProcess** ⇒ 该进程终止，但节点运行时继续 | `node/process_run.go:19-34` |
| `LoggerAddPID` 使该进程收到 `MessageLogNode`/`MessageLogProcess`；`LoggerDeletePID` 是任意状态可用的安全清理（删 logger、恢复原日志级别）；`. ` 开头的 hidden 名不参与默认 fan-out | `gen/node.go:478-508`；`node/node.go:2469-2491` |

### 追踪

| 保证 | 证据 |
|---|---|
| `gen.TracingBehavior` 仅 `HandleSpan(TracingSpan)` 与 `Terminate()`；对象 exporter 每实例独立 worker **串行**执行，队列满**丢 span 并计数**，停止时先停 worker 再 `Terminate`，exporter panic 被恢复并记录 | `gen/tracing.go:139-154`；`node/node.go:2566-2593,2607-2625` |
| `TracingSpan` 含 TraceID/SpanID/ParentSpanID/ParentPoint/Point/Kind/Timestamp/EndTimestamp/Node/From/To/Ref/Behavior/Message/Error/Attributes；`TracingPoint` = sent/delivered/processed/span | `gen/tracing.go:67-114` |
| `TracingFlags`：`Send`（send/call/response）、`Receive`（delivered/processed 及业务 span）、`Procs`（spawn/terminate）、`Inherit`（子进程继承）。匹配：Sent 看 Send，非 Sent 看 Receive，spawn/terminate 看 Procs，业务 span 看 Receive | `gen/tracing.go:15-23`；`node/node.go:2666-2680` |
| `TracingKind`：Send/Request/Response/Spawn/Terminate；`Kind=0` 表示业务 span | `gen/tracing.go:116-137` |
| 自动追踪：Send/Call/Response 的 sent/delivered/processed（Response 无 processed）、Spawn 的 sent/processed、Terminate 的 processed。**`SendExit`（控制面）、`SendEvent`（事件）、`SendAfter`（延迟消息）不携带 trace context，不被自动追踪** | `docs/advanced/distributed-tracing.md:129-178` |
| 采样器：`Always` / `Ratio(rate)` / `RateLimit(perSecond)` / `Disable`；**仅在没有 active trace 时咨询**，有活动 trace 时所有出站消息继承；Init 阶段不创建新 trace | `gen/tracing_sampler.go:8-68`；`node/process.go:631-647` |
| 采样级别**运行期可改**：`node.SetTracingSampler`、`node.SetProcessTracingSampler`；`process.SetTracingSampler` 要求 init/running | `node/node.go:2682-2712`；`node/process.go:603-613` |
| 业务侧：`SetTracingSpanAttribute` 是 one-shot（handler 返回后清除），`SetTracingAttribute` 是持久属性，`ergo.` 前缀被忽略；`StartTracingSpan`/`End`/`EndError` 可建业务区间，未结束的 handler 自动以 `ergo.span.unended=true` 关闭 | `gen/process.go:924-934`；`node/process.go:2021-2185` |

### 指标与 inspect

| 保证 | 证据 |
|---|---|
| metrics actor `Options` 支持 Host/Port/Path/CollectInterval/TopN/Mux/Shared；`Shared` 模式由 `metrics.NewShared()` 共享同一 Prometheus registry；`Mux` 设置后注册到外部 `http.ServeMux` 并跳过自建 server（Host/Port 忽略）；`Registry()` 在 Init 返回前为 nil | `docs/extra-library/actors/metrics.md:39-96,220-263` |
| 自动暴露 node（uptime、进程计数/生命周期、内存/CPU、应用、名字/alias/event、Send/Call 错误）、log 各级计数、网络（连接/消息/字节/重连/握手/碎片/压缩）、mailbox depth/latency、process utilization/init/throughput/wakeups/drain/liveness、event subscriber/publish/delivery；latency 分布需 `-tags=latency` | `docs/extra-library/actors/metrics.md:98-172` |
| 自定义 collector 自动带 node const label，**不能重复声明 node** | `docs/extra-library/actors/metrics.md:98-172` |
| `Process.Inspect(target, item...)` 是同步请求返回 `map[string]string`；`HandleInspect` 是 behavior 回调；**内建键用 `ergo:` 前缀**，behavior 返回同名键可覆盖 | `gen/node.go:416-424`；`gen/process.go:710-716` |
| Supervisor inspect 键：type/strategy/intensity/period、auto_shutdown、restarts_count、children_total/running/disabled、history:*；Router：routes_total/active/disabled/pending、mailbox_size、forwarded/discarded/failed/restarts 及各 route 状态；Pool：pool_size、worker_*、messages_forwarded/unhandled | `act/supervisor_ofo.go:451-479`；`act/supervisor.go:895-901`；`act/router.go:863-893`；`act/pool.go:378-385` |

## 9. 实测验证（2026-09-18）

前八节均为源码阅读。本节是在 `v1.999.330` 上运行一次性原型得到的实测结果，验证的五条假设直接支撑 ADR 0012 的激活设计。原型用完即弃，未进入仓库。

| # | 假设 | 实测结果 | 判定 |
|---|---|---|---|
| 1 | 同名并发创建，败者既不执行 Init 也不执行 Terminate | 6 并发同名：成功 1、`ErrTaken` 5、其它 0；Init 调用 1 次、Terminate 0 次 | **成立** |
| 2 | Init 内阻塞 I/O 不会让所有权泄漏 | `InitTimeout=1s` + Init 阻塞 3s：返回 `ErrTimeout` 耗时 1s；Init 调用 1 次；**Terminate 调用 1 次**（超时后仍清理）；名字已释放 | **成立** |
| 3 | 停机时 Terminate 内的阻塞工作会跑完 | Terminate 内阻塞 800ms：`Stop()` 耗时 801ms，Terminate 开始 1 次、完成 1 次 | **成立**（关停屏障确实等回调） |
| 4 | 同步 Init 期间的按名投递不会被提前处理 | Init 阻塞 800ms 期间按名 `Send` 返回 nil；消息被处理，**晚于 Init 返回 86.7µs**，晚于 claim | **成立**（无提前处理窗口） |
| 5 | Init 返回错误时 Terminate 会被调用（供释放所有权） | Init 返回错误：`SpawnRegister` 返回该错误；Terminate 调用 1 次，`reason` 即该错误；名字已释放 | **成立** |

### 由此确认的设计前提

- **场景 1** 支撑 ADR 0012 §1（不用监督者承载、名字级并发创建是良性的）：败者连 Init 都不执行，故"被拒绝的创建会触发落库"这个副作用不存在。
- **场景 2 + 5** 支撑 ADR 0012 §2（所有权获取放同步段）：无论 Init 成功、失败还是超时，`Terminate` 都会被执行——它是所有权的可靠释放点；反向也说明"claim 已发生"与"Terminate 一定执行"在除硬崩溃外成立。
- **场景 3** 支撑 ADR 0012 §3（释放放终止路径且必须在落库之后）：正常停机时关停屏障会等回调返回，释放能跑完；前提是**先停节点、再关外部客户端**（否则回调内的落库会访问已关闭的连接）。该前提已登记为 `invariants.md` 第 10 条。
- **场景 4** 支撑 ADR 0012 §2 选同步段而非异步段的理由：名字在 Init 前可解析，但按名投递的消息不会在 Init 返回前被处理，故同步段内的获取严格先于任何消息处理。

### 未验证

- 上述均为单节点、本机、无网络的时序观测；**跨节点**路径与真实 Redis/PostgreSQL 未参与。
- 场景 4 的"消息晚于 Init"结论依赖邮箱串行；未测试多发送者并发投递的顺序。

## 9.1 首个垂直切片的实测补充（2026-09-18，guild 链路）

在 guild 一条完整链路上验证（激活 → 初始化 → 存盘 → 释放 → 停机），补充以下事实：

| 事实 | 观测 |
|---|---|
| 所有权获取可与消息处理严格分离 | 在同步初始化段获取所有权后，业务侧收到的第二个初始化参数即本次所有权；`role`/`guild` 现有实现已按该约定读取第二个参数 |
| 释放可晚于业务终止回调 | 业务终止回调执行期间所有权仍被持有；回调返回后才释放（不变量 4） |
| 优雅停机等待终止回调 | 停机时先停 actor（含终止回调），再关闭数据库与缓存（不变量 10） |
| 世代单调递增 | 同一 guild 跨进程重启后 epoch 递增（217 → 219），未见复用 |
| 节点身份不一致会直接破坏按名寻址 | 修复前：服务注册用带时间戳的实例名、运行时节点名是稳定的 `name@host`，二者不一致导致跨节点按名寻址失败（`no route`）。修复后二者统一 |

**发现并已修复（既有缺陷，非本次迁移引入）**：guild 的 actor 从未调用 `SetSelfMod`，导致其模块生命周期的 `OnModStop`（最终落盘）不会执行——actor 停止时不保存。已接线，并同时修掉一处连带隐患：加载失败时不得留下空数据对象，否则终止路径会把空对象写回库。实测：直接改库后停止节点，actor 的落盘将其覆盖回内存中的值。

`chat` 不依赖该机制（在终止回调里直接落盘），`role` 本就正确接线。

### 9.2 跨节点验收（2026-09-18，双节点）

两个节点（`guild@127.0.0.1:25031` 与 `guild@127.0.0.2:25041`）同跑 guild 能力，由一致性哈希分配实例。

| 事实 | 观测 |
|---|---|
| 跨节点激活成立 | 经节点 A 创建 10 个实例，其中 5 个在节点 B 上启动（`guild actor started` 出现在 B 的日志） |
| 所有权归属正确 | 跨节点实例记在 `guild@127.0.0.2` 名下，本机实例记在 `guild@127.0.0.1`；世代全局连续递增（242–251），两实例租约令牌不同 |
| 跨节点释放成立 | 节点 B 正常停机后，其名下实例的所有权与该节点的租约都被释放 |
| 单节点路径不受影响 | 同一批次中本机实例照常创建与释放 |

**必须补齐的两处，缺一则跨节点不可用**（均为本次验收暴露）：

1. **节点互连密钥必须配置**。运行时在密钥为空时为每个节点生成随机值，导致握手失败（`incorrect digest`）。已加入配置模板 `[network] cookie`，所有节点必须一致。
2. **跨节点消息必须走统一信封**（ADR 0009）。protobuf 生成类型含未导出字段，运行时无法直接序列化（`no encoder for type ...`）。已实现信封并在发送、接收、请求与响应四条路径上接入；本机投递不经信封。

**一处未定性观察**：13 次跨节点停机中出现 1 次异常退出（退出码 1，名下 5 条所有权记录未释放），未能复现；当时两节点共用同一日志文件，证据不足以定位。该后果是有界的——陈旧所有权会被后续激活忽略并以更大世代接管（`TestActorLocatorLocateExpiredOwnerReturnsMiss`、`TestActorLocatorRejectsOwnerWhenLeaseTokenDiffers`）。**未验证**：该异常退出的成因。

## 10. 未确认项（源码勘测部分）

- **并发发送者之间的全局顺序**：MPSC FIFO 只能证明单队列 Pop 顺序，跨多 producer 的线性化顺序未实测。
- **跨节点非 important `Send` 在"节点可达但目标不存在/邮箱满"时静默**（源码明确）；连接在写入后断裂的异步时序未实测，故不额外断言。
- **自定义 registrar 对重复节点名无统一契约**：只有"其 `Register` 返回错误会阻止该节点网络启动"可确认。
- **metrics 实现源码缺失**：v1.999.330 模块树中无独立 `actor/metrics` 实现，`metrics.Options` 与指标清单仅据随附文档佐证，无源码行号。
- **meta 无专属可观测性接口**：未发现 meta 专属 logger/tracing/metrics 定义，其运行循环复用 process 路径。
- 第 1–8 节的事实**全部来自源码阅读**（§9 除外，另有实测标注）。
