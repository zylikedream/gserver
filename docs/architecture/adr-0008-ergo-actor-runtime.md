# ADR 0008: Ergo Actor Runtime 与 GServer 运行时契约

- 日期：2026-09-05
- 状态：**Accepted**
- 关联：`adr-0006-actor-location-ownership.md`、`actor-location.md`

## 背景

GServer 需要替换通用 Actor 运行时，但不能改变已有的 Role single-writer 不变量。运行时负责 mailbox、进程生命周期、PID 传输和监督；Actor Directory、节点 lease、ownership fencing 和持久化 fencing 必须继续由 GServer 控制。运行时替换采用 clean cutover，不提供两种运行时之间的 PID 兼容或在线迁移。

## 决策

选择 [Ergo](https://github.com/ergo-services/ergo) 作为 GServer 的通用 Actor runtime。`core/gxyactor` 保留为唯一业务-facing seam，Ergo 具体类型只允许存在于内部 adapter。

### 责任边界

| 能力 | 责任方 | 约束 |
|---|---|---|
| mailbox、进程调度、ProcessInit/ProcessTerminate、子进程监督 | Ergo runtime | 只提供进程运行能力，不授予业务 ownership |
| runtime PID、节点间传输、网络连接、Registrar/Grid | Ergo runtime | Registrar/Grid 只用于发现或传输，不是 single-writer fencing |
| Actor Kind 注册、ActivateActor、Actor Directory | GServer adapter/Activator | 激活仍必须走 owner 查询和 Activator |
| Redis node lease、原子 Claim/Release、epoch | GServer Actor Directory | Claim 必须在任何 Spawn 之前；Redis 错误 fail closed |
| PostgreSQL `role_actor_fence` | GServer persistence | 每次 Role 保存事务锁定并校验 exact `role_id + node_id + epoch` |

Ergo Registrar 只能发现 Ergo 节点或进程。它不能替代 Redis owner key、Claim/Release、节点 lease、epoch，也不能决定哪个节点可以创建或写入 Role Actor。现有 Consul `nodeInstanceName -> address` 服务发现同样只负责地址发现。

### GServer 公共 Actor seam

业务包只能依赖 GServer 的 runtime-neutral contract，不得依赖 Ergo `gen.*`、`act.*` 或任何其他 runtime 类型。公共 seam 提供以下语义操作：

- `RegisterActorKind(kind, factory)`：注册业务 Actor 工厂。
- `ActivateActor(kind, id, allowSpawn)`：经 Actor Directory 定位并在获 Claim 后激活；`allowSpawn=false` 绝不创建进程。
- `GetLocalActor(pid)` / `GetLocalActorAll(kind)`：读取已由 Activator 确认的本地 activation。
- `Send(ctx, pid, message)`：一次性投递，不等待业务响应。
- `Call(ctx, pid, message, timeout)`：请求并等待响应，调用方 timeout 必须生效。
- `Respond(request, value, err)`：在当前请求 callback 中回复；响应错误不得伪装为成功的 `any` 值。
- `Stop(pid)`：请求正常停止，必须是幂等操作。

`message` 在业务 seam 上是 opaque value；本地实现可以直接传递，远程业务消息必须使用下文的 protobuf envelope。Actor 工厂和回调不暴露 Ergo context、PID、process、envelope 或 supervisor 类型。

#### 生命周期映射

单个 GServer Actor 的顺序固定为：

1. Ergo 创建进程并调用 `ProcessInit`；adapter 在此阶段调用业务 `Init(ctx, args)`。
2. `Init` 成功后注册反射 handler，再执行 GServer `DelayInit(ctx)`。在 `DelayInit` 成功前不接受正常业务消息。
3. 激活确认（现有 `Touch` 的语义）只在 `ProcessInit`、handler 注册和 `DelayInit` 全部成功后完成；此时才发布 PID 到本地 `ActorMgr`，并回复 pending waiters。
4. 业务消息在 Ergo mailbox 中串行执行并调用 `HandleMessage(ctx, message)`。handler 返回错误或 panic 都终止当前 Actor。
5. Ergo 调用 `ProcessTerminate`；adapter 先停止 timer，再调用业务 `Terminate(ctx, err)`，最后按 `nodeInstanceName + epoch + leaseToken` 执行条件 Release。

初始化失败不得返回成功 PID：停止该进程、只释放匹配的 owner，并向所有 pending waiters 返回 `ActorInitFailed`。`Terminate` 只执行一次；正常 Stop 不是 ownership 接管，也不触发在线 Actor 迁移。

#### 规范化 PID

GServer PID 是 runtime-neutral、不可由业务拼接或修改的值，至少包含以下字段：

```text
PID {
  Runtime   // runtime identity, 当前为 ergo-v1
  Node      // canonical nodeInstanceName，不是 address:port
  ID        // namespaced logical ID，例如 role/10001
  Creation  // runtime creation/incarnation，保留 Ergo PID 的 creation 信息
}
```

`Runtime` 防止不同 runtime 的 PID 混用；`Node` 采用带进程身份的 `nodeInstanceName`，因此节点重启不会复用旧身份；`ID` 必须包含 Actor Kind 命名空间；`Creation` 是运行时提供的 opaque incarnation 标记。PID equality 只有四个字段全部相等时成立，任一字段不同都视为不同 PID。PID 不携带 Redis epoch；epoch 属于 ownership metadata，必须由 Activator 和 PostgreSQL fence 单独校验。地址解析也不属于 PID。

#### 调用上下文规则

普通 Go goroutine 可以调用 `ActivateActor`、本地查询、`Send`、带 timeout 的 `Call` 和 `Stop`。Actor callback 内可以向其他 PID `Send`、`Call`，回复当前请求 `Respond`，以及停止自身或其子 Actor；不得直接操作另一 Actor 的状态或绕过 Activator 创建 Actor。Actor 不得对自身执行同步 Call；依赖环必须通过异步 Send 或由调用方 timeout 截止。`Respond` 需要当前 callback 的 request handle，普通 goroutine 没有该 handle 时不可调用。

### 错误与停止语义

公共 seam 使用稳定的 GServer 错误类别，底层 Ergo 错误只作为 cause 保留：

| 场景 | 必须行为 |
|---|---|
| PID 不存在或 creation/runtime 不匹配 | 返回 `UnknownPID`，不隐式激活、不 fallback |
| Call 超过调用方 timeout | 返回 `Timeout`；不把晚到响应当作成功结果 |
| 远程节点断开、地址不可达或连接丢失 | 返回 `RemoteNodeUnavailable`；不把它当作 owner miss |
| `Init`/`DelayInit` 失败 | 返回 `ActorInitFailed`，不发布 PID，条件释放匹配 owner |
| Actor 已停止或正在终止 | Send/Call 返回 `ActorStopped`；正常 `Stop` 本身幂等成功 |
| handler error/panic | 当前 Actor 按 stop supervision 终止，并执行一次清理 |

### 远程 protobuf envelope

跨节点业务消息不直接依赖 Ergo EDF 对生成 protobuf struct 的编码。统一编码为注册到 Ergo network type registry 的 GServer envelope：

```text
GServerEnvelope {
  Type  // 稳定的 protobuf message type identifier
  Data  // proto.Marshal(message) 生成的 protobuf bytes
}
```

`Type` 映射到预先注册的 protobuf constructor，不能使用不稳定的 Go package path 作为业务协议契约；`Data` 只接受对应类型的 protobuf bytes。未知 `Type`、缺失注册或 malformed bytes 必须返回 typed wire error。Envelope 只承载远程业务消息；Actor lifecycle、activation、Touch、ownership 等控制消息留在 runtime/adapter 内部，不暴露给业务协议。客户端/server 现有 protobuf 定义不因本 ADR 改变。

### 监督与节点关闭

Ergo 使用 one-for-one 的 stop 语义：handler 错误或 panic 只停止失败的 Actor，由父级或 Activator 处理后续；不得对带内存状态的 Role Actor 自动 restart。重新激活必须重新 Claim，并建立新的或经校验的 PostgreSQL fence。

节点进入 drain 后停止新的 activation 和 Spawn，等待现有 Actor 按正常 `ProcessTerminate` 保存并条件 Release；到达 drain deadline 后保存并断线重连。节点停止 heartbeat，或在安全截止时间内无法确认 lease 续租时，必须 self-fence、停止新请求并终止进程。崩溃节点由 lease 到期后才可被新节点接管并递增 epoch。任何路径都不迁移在线 Actor 的 PID、mailbox 或内存状态。

### 部署切换

这是 clean cutover：发布后集群只运行 Ergo adapter，不存在 Protoactor/Ergo 混合 PID 路由、跨 runtime PID 转换或旧 PID 兼容层。Gateway 缓存的旧 PID 发送失败时重新 `ActivateActor` 或断线重连；不能把旧 PID 转换成 Ergo PID。回滚若需要，必须作为另一个完整部署决策，不能在线混跑两种 PID 协议。

## Ownership 不变量覆盖

以下行为继承并保持 ADR 0006，不因 Ergo Registrar、Grid、PID 或监督机制改变：

| ADR 0006 场景 | Ergo 迁移后的行为 |
|---|---|
| 并发 Claim | Redis Lua 原子 Claim 只授予一个 owner；Claim 失败禁止 Spawn |
| Actor 初始化失败 | `ProcessInit`/`DelayInit` 失败不发布 PID，只做匹配 owner 的条件 Release |
| 下线删除失败 | 留下 stale directory entry；下次由 owner Activator 验活、自愈并 `RetryLocate` |
| owner 节点崩溃 | node lease 到期后才允许接管，接管递增 epoch |
| owner 与 Redis 分区 | 到达 lease 安全截止时间 self-fence；停止新请求并终止 |
| Redis 不可用 | 新 activation fail closed；已有节点只服务到已确认 lease deadline |
| Consul/Registrar 地址暂缺 | 不可用并 fail closed；有效 lease 存在时不能抢占 owner |
| 旧 Actor 延迟保存 | PostgreSQL `role_actor_fence` 拒绝旧 `node_id + epoch` 的保存事务 |
| Gateway 持有旧 PID | 发送失败后重新 Activate 或断线重连，绝不转换或直接构造 PID |

Ergo 的 discovery、Grid、process registry 和 supervision 都不是持久化 fencing。Role 的权威副作用仍必须经过 PostgreSQL fence；Redis 查询与写库之间的 TOCTOU 仍由 PostgreSQL 解决。

## 适配器边界：禁止泄漏的 Protoactor 符号

迁移完成后，以下旧 runtime 符号不得出现在业务包、`core/gxyactor` 公共 API 或业务测试中；如迁移期间仍需读取，只能暂存在 `core/gxyactor/internal/ergo` 适配器边界内，并最终删除：

- `actor.ActorSystem`、`actor.Context`、`actor.Actor`、`actor.PID`、`actor.Props`
- `actor.RootContext`、`actor.Future`、`actor.MessageEnvelope`
- `actor.Started`、`actor.Stopped`、`actor.Directive`、`actor.OneForOneStrategy`
- `remote.Remote`、`remote.Configure` 以及 Protoactor remote/process-table 类型
- 任何 `github.com/asynkron/protoactor-go/...` import、Protoactor PID alias 或跨 runtime 转换函数

业务代码只能看到 GServer 的 `PID`、Actor lifecycle contract 和 Send/Call/Respond/Stop 操作。Ergo `gen.Node`、`gen.Process`、`gen.PID`、`act.Actor`、`gen.Network` 等同样只能存在于适配器实现中。

## 后果

- GServer 保留 Redis direct lookup、node lease、原子 Claim/Release、epoch 和 PostgreSQL fence，runtime 替换不改变 single-writer 安全性。
- 业务接口稳定且 runtime-neutral；Ergo 升级或替换由 adapter 吸收，但必须保持本 ADR 的 PID、生命周期和错误契约。
- 远程业务消息增加一次 protobuf marshal/unmarshal，但获得稳定、可注册、与 Ergo EDF 解耦的 wire contract。
- 节点分区、Redis 故障和 remote loss 可能导致短暂不可用，这是 fail-closed 与一致性优先的必然后果。
- 不建设 shard ownership、online handoff、mailbox/state migration；只有新的业务证据和替代 ADR 才能推翻该非目标。
