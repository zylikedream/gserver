# ADR 0006: Actor 定位与所有权 Fencing

- 日期：2026-09-03
- 修订：2026-09-04
- 状态：**Accepted**
- 关联：Actor Locate 并发压测与所有权改造

## 背景

改造前的 actor locate 使用 per-player Redis key 保存 `nodeInstanceName`，actor 创建后直接 `SET`，销毁时直接 `DEL`，Role 保存前只比较节点名。该实现的正常查询是 O(1)，但无法同时正确处理并发激活、actor 下线清理失败、节点故障接管、网络分区和延迟写入。

`ktm_server` 的节点 Set 扫描方案将续约责任集中到节点，但每次定位需要遍历节点并执行大量 `SISMEMBER`。本地 1000 节点、1000 QPS 测试中 Redis CPU 为 75.04%，定位 p99 为 119.957ms，因此 GServer 保留 per-player O(1) direct lookup。

网络分区时，旧 owner 可能无法访问 Redis，但仍可访问 Gateway；与此同时 Redis 可能在 lease 到期后把 ownership 授予新节点。系统不可能同时保证旧节点继续可用和严格单 writer。

## 决策

Role ownership 在网络分区时选择**一致性优先（CP）**。目标不变量不是物理上只存在一个 actor 进程，而是：

> 同一玩家最多只有一个 actor 具备处理新请求和提交持久化副作用的权限。

采用**Activation Directory + 节点级 Lease + 原子 Claim + PostgreSQL Fencing**：

1. Redis owner key 表示当前 actor activation 的 directory entry，不表示玩家永久归属，也不直接证明 actor 实例存活。
2. Locate 热路径使用 per-player O(1) direct lookup，不扫描节点 Set。
3. 每个节点进程持有一个带 TTL 的 lease；`nodeInstanceName` 同时作为唯一进程身份和 lease token。
4. actor 激活必须先执行 Redis Lua 原子 Claim。Claim 验证候选 lease、检查现有 owner lease，并在接管时递增 epoch。
5. owner key 在 actor 在线期间不设置固定 TTL，防止长生命周期 actor 的 key 先于 actor 过期。
6. actor 正常退出执行 compare-and-delete，必须匹配 `nodeInstanceName + epoch + leaseToken`。删除失败只产生可自愈的 stale directory entry，不影响单 writer 正确性。
7. Redis 命中不能直接构造 PID。请求必须经过 owner 节点 Activator，由本地 ActorMgr 判断 activation 是否存在。
8. owner 节点仍存活但本地 activation 不存在时，Activator 条件删除 stale entry 并返回 `RetryLocate`；调用方按当前健康节点重新选择和 Claim。
9. 节点无法在 lease 安全截止时间内确认续租时必须 self-fence；停止新请求并终止进程，不允许静默继续。
10. Redis owner 检查不是持久化 fencing。Role activation 必须在 PostgreSQL 建立 epoch fence；每次 Role 保存事务必须先验证并锁定该 fence，数据库拒绝旧 epoch。
11. Redis 错误 fail closed：不创建新 actor，不把基础设施错误当作 locate miss。
12. Consul 只负责 `nodeInstanceName -> address` 服务发现，不决定 ownership。Consul 暂时查不到地址但 Redis lease 仍有效时，不允许其他节点抢占。

## 数据模型

```text
gserver:locate:node:actor:{kind}:{id}
  -> {nodeInstanceName}|{epoch}|{leaseToken}
  TTL: none while activation is owned

gserver:locate:node:lease:{nodeInstanceName}
  -> {leaseToken}
  TTL: 15s

gserver:locate:node:epoch
  -> global monotonic counter

role_actor_fence
  -> role_id, node_id, epoch, update_at
```

`leaseToken` 防止旧进程续租或清理新 owner；`epoch` 是下游持久化的 fencing token。

## 激活与释放

```text
ActivateActor
  -> Locate
  -> owner exists and lease valid
       -> call owner Activator
       -> local activation exists: return PID
       -> local activation absent: conditional Release + RetryLocate
  -> no valid owner
       -> choose candidate from current healthy nodes
       -> candidate Claim
       -> only acquired candidate may SpawnNamed
```

```text
actor shutdown
  -> save under PostgreSQL fence
  -> conditional Release
  -> Redis failure leaves a stale entry
  -> next activation self-heals through owner Activator
```

## 扩缩容

- 新增节点不迁移在线 actor。
- 已在线玩家保持原 PID 和 owner，直到自然下线或节点 drain。
- 离线玩家正常释放 owner；再次登录时按当前节点集合重新分配，因此新节点逐步承接负载。
- 缩容使用 drain：停止接收新 activation，等待玩家自然退出；超时后保存并断线重连。
- GServer 长期不建设 shard ownership、在线 actor handoff、mailbox 或内存状态迁移；这不是后续版本待办，只有新的业务证据和替代 ADR 才能推翻。

## 故障处理

| 场景 | 行为 |
|---|---|
| 并发 Claim | Redis Lua 只授予一个 owner |
| actor 初始化失败 | 条件释放 owner，不返回成功 PID |
| actor 下线删除失败 | 保留 stale entry；下次激活由 owner Activator 自愈 |
| owner 节点崩溃 | lease 到期后新节点接管，epoch 递增 |
| owner 与 Redis 分区 | owner 在安全截止时间 self-fence |
| Redis 不可用 | 禁止新 activation；已有节点只服务到已确认 lease 的截止时间 |
| Consul 地址暂时缺失 | lease 有效时返回不可用，不抢占 |
| 旧 actor 延迟保存 | PostgreSQL fence 拒绝旧 epoch |
| Gateway 缓存旧 PID | 发送失败后重新 Activate 或断线重连 |

## PostgreSQL Fencing

仅在保存前读取 Redis 存在 TOCTOU：

```text
old actor reads Redis epoch=41
new owner claims epoch=42
old actor writes PostgreSQL
```

因此每个 Role activation 在加载业务状态前，先将 `role_actor_fence` 推进到自己的 epoch。推进规则只允许更大的 epoch，或同一 node 的同一 epoch 幂等重试。每个 Role 保存事务首先锁定并校验完全相同的 `role_id + node_id + epoch`，随后在同一事务中写业务表。

新 owner 建立数据库 fence 时会等待已开始的旧事务结束；fence 建立后，任何旧 epoch 事务都不能再开始写入。

## 拒绝的方案

### 网络分区时继续服务

选择可用性会允许旧 owner 和新 owner 同时处理请求，违反 Role single-writer 不变量。

### Redis 命中后直接构造 PID

Directory entry 可能因下线清理失败而残留；直接构造 PID 无法判断本地 activation 是否存在。

### 永久玩家归属

会使登录过的玩家长期固定在旧节点，新增节点无法逐步承接离线老玩家。

### Shard Ownership 与在线 Actor 迁移

这类能力需要 shard coordinator、handoff、Gateway PID 重定位、在线 actor drain、失败回滚和更大的测试矩阵。其复杂度与运维成本显著高于离线玩家逐步再分配带来的体验收益，因此作为长期非目标拒绝，而非延期实现。

### 每 Actor Heartbeat 或固定 Owner TTL

每 actor heartbeat 增加任务和 Redis 写入量；固定 TTL 会让长生命周期 actor 的 owner key提前过期并造成双 active 风险。

### 仅在保存前查询 Redis

跨 Redis/PostgreSQL 的检查与写入不原子，不能阻止检查后的 ownership 变更。

## 后果

- 网络分区和 Redis 故障期间可能暂时无法登录或继续游戏，这是选择一致性优先的必然后果。
- 新增节点只承接新 activation，不迁移在线玩家，负载随玩家下线和重新登录逐步均衡。
- Redis stale owner key 可以存在，但不再是正确性风险；后台 GC 仅属于空间优化，不参与 correctness。
- 所有权复杂度必须封装在 Actor Directory 模块中，业务调用方只使用 `ActivateActor`。
- 任何绕过 PostgreSQL fence 的权威副作用仍可能由旧 actor 提交；这类副作用必须另行使用 fencing token 或幂等键。
