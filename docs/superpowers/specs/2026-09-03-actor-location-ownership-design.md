# Actor Location Ownership Design

## Goal

将 actor 定位收敛为一致性优先的 Activation Directory：同一玩家最多只有一个 actor 具备处理新请求和提交持久化副作用的权限；actor 下线清理失败、节点故障和网络分区均不能产生两个合法 writer。

## Scope

本设计覆盖：

- `core/gxyactor` 的 direct locate、Claim、Release、stale owner 自愈和节点 lease watchdog；
- owner Activator 对本地 activation 的权威判断；
- Role activation 的 PostgreSQL epoch fence；
- Role 保存事务的 fence 校验；
- 新增节点只承接离线玩家的新 activation。

GServer 明确不建设 shard ownership、在线 actor 迁移、本地 locate 缓存、节点 Set 扫描或 mailbox/state 内存迁移。这是长期架构边界，不是延期到后续版本的待办；只有新的业务证据和替代 ADR 才能改变该决策。

## CAP Policy

Role ownership 在网络分区时选择一致性优先：

- Redis 不可用时禁止创建新 actor；
- 节点只能在最近一次已确认 lease 的安全期限内继续处理请求；
- lease 到期前仍无法确认续租时，节点必须 self-fence 并终止进程；
- 新 owner 只能在旧 lease 失效后 Claim；
- PostgreSQL 必须拒绝旧 epoch 的事务。

允许旧 actor 进程物理存在，但它不能继续成为合法 writer。

## Domain Model

### Directory Entry

Redis owner key 表示当前 activation 的路由归属，不表示永久玩家归属，也不直接证明 actor 实例存活。

### Node Lease

Node lease 表示节点进程当前是否有资格持有 directory entry。`nodeInstanceName` 每次进程启动唯一，同时作为 lease token。

### Local Activation

只有 owner 节点本地 `ActorMgr` 能判断 actor 实例是否存在。Redis 命中后必须请求 owner Activator，不能直接构造 PID。

### Fencing Epoch

每次 ownership 接管获得更大的 epoch。Redis 决定 owner；PostgreSQL 在事务 seam 强制执行 epoch，拒绝旧 writer。

## Invariants

1. owner 变更只能由 Redis Lua Claim 决定。
2. 未取得 Claim 的节点不得 `SpawnNamed`。
3. owner key 包含 `nodeInstanceName + epoch + leaseToken`。
4. Release 只能删除完全匹配的 owner。
5. owner key 在线期间没有固定 TTL；有效性由 node lease 决定。
6. owner 命中必须经过 owner Activator 验证本地 activation。
7. live owner 节点发现本地 activation 缺失时，条件释放 stale entry 并要求调用方重新定位。
8. Redis 错误不得转换为 miss 或 fallback spawn。
9. 节点超过已确认 lease deadline 后不得处理业务 actor 消息。
10. Role 加载业务状态前必须在 PostgreSQL 建立当前 epoch fence。
11. 每次 Role 保存必须在同一 PostgreSQL 事务中锁定并验证 fence。
12. 在线 actor 不因扩容迁移；离线玩家再次登录时按当前节点集合重新分配。

## Redis State

```text
gserver:locate:node:actor:{kind}:{id}
  -> {nodeInstanceName}|{epoch}|{leaseToken}
  TTL: none while owned

gserver:locate:node:lease:{nodeInstanceName}
  -> {leaseToken}
  TTL: 15s

gserver:locate:node:epoch
  -> global monotonically increasing counter
```

Locate Lua 原子读取 owner，并验证对应 node lease 是否存在。不存在的 owner 返回 miss，但保留 key 供下一次 Claim 原子覆盖。

## PostgreSQL State

新增表：

```text
role_actor_fence
  role_id   bigint primary key
  node_id   text not null
  epoch     bigint not null
  update_at timestamptz not null
```

Role actor 在 `DelayInit` 加载模块状态前执行 fence advance：

```sql
INSERT INTO role_actor_fence(role_id, node_id, epoch, update_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(role_id) DO UPDATE
SET node_id = EXCLUDED.node_id,
    epoch = EXCLUDED.epoch,
    update_at = EXCLUDED.update_at
WHERE role_actor_fence.epoch < EXCLUDED.epoch
   OR (
       role_actor_fence.epoch = EXCLUDED.epoch
       AND role_actor_fence.node_id = EXCLUDED.node_id
   );
```

`RowsAffected == 0` 表示调用者是旧 owner，actor 初始化失败。

每次保存事务首先锁定并验证：

```sql
SELECT epoch
FROM role_actor_fence
WHERE role_id = ? AND node_id = ? AND epoch = ?
FOR UPDATE;
```

没有完全匹配行时返回 ownership-lost 错误，不写任何业务表。新 owner 推进 fence 与旧 owner 保存通过同一行锁串行化：已经开始的旧事务可以先完成；新 fence 建立后，旧 epoch 不能再开始保存。

## Directory Interface

调用方只使用 `ActivateActor`；Redis 状态和 retry 留在 `core/gxyactor` 内部。

```go
type ActorOwner struct {
    NodeID string
    Epoch  uint64
}

func (l *actorLocator) locate(
    ctx context.Context,
    kind, id string,
) (ActorOwner, error)

func (l *actorLocator) claim(
    ctx context.Context,
    kind, id string,
) (owner ActorOwner, acquired bool, err error)

func (l *actorLocator) release(
    ctx context.Context,
    kind, id string,
    owner ActorOwner,
) (released bool, err error)
```

`Release` 返回是否实际删除，便于区分成功、stale no-op 和基础设施错误。

## Activation Flow

### Directory Miss

```text
getActor
  -> choose candidate from current healthy nodes
  -> send ActorActive{allow_spawn=true}
  -> candidate Claim
  -> acquired: SpawnNamed with ActorOwner
  -> owned by another node: ActorLocateRetry
```

### Directory Hit

```text
getActor
  -> resolve owner address through Consul
  -> send ActorActive{allow_spawn=false}
  -> owner Claim validates current ownership
  -> local ActorMgr contains id: return existing PID
  -> local ActorMgr missing id:
       conditional Release
       return ActorLocateRetry
  -> caller repeats Locate against current node set
```

`getActor` uses a bounded retry loop. `spawn=false` callers return not-found after stale cleanup instead of creating a new actor.

`ActorActive` gains `allow_spawn`; `ActorLocateRetry` is a typed protobuf response. Error strings are not used as control flow.

## Actor Shutdown

```text
actor save under PostgreSQL fence
  -> actor stop
  -> conditional Redis Release
```

Release error is logged with structured fields. It does not keep a dead actor alive and does not become a correctness dependency. A stale key is repaired by the next owner Activator request or overwritten after node lease expiry.

## Node Lease Watchdog

Lease acquisition and every successful renewal record a conservative local deadline using the command start time plus lease TTL.

- token mismatch fences immediately;
- Redis renewal error logs the infrastructure failure but may retry only before the last confirmed deadline;
- reaching the deadline invokes a fatal callback exactly once;
- production callback terminates the process;
- tests inject a non-terminating callback;
- graceful module stop cancels the watchdog before releasing the lease.

This prevents a partitioned node from continuing indefinitely after Redis may grant a new owner.

## Expansion and Drain

- Scale-out does not migrate online actors.
- Online actors remain on the old node until logout or drain.
- Normal logout conditionally releases the directory entry.
- If logout cleanup failed, the next login asks the old owner Activator; absent local activation causes conditional release and retry.
- After release, the current consistent-hash node set includes newly added nodes, so a subset of offline old players moves naturally.
- Scale-in marks the node draining, rejects new activations, waits for natural logout, then saves and disconnects remaining players before lease release.

## Error Policy

- Redis command error: wrapped infrastructure error; never miss.
- Invalid candidate lease: reject activation.
- Active owner address unavailable while lease remains valid: return unavailable; do not steal.
- Stale owner on live node: conditional release plus typed retry.
- Retry exhaustion: return explicit locate-retry-exhausted error.
- PostgreSQL fence advance rejected: actor initialization fails.
- PostgreSQL fence validation rejected: save fails, dirty state remains, actor stops through the existing save-error path.
- Lease deadline reached: fatal self-fence.

## Verification

Tests cover:

1. concurrent Claim produces exactly one owner;
2. active owner blocks another Claim;
3. expired lease permits takeover with a greater epoch;
4. stale Release cannot delete a newer owner;
5. owner key does not expire while the node lease is active;
6. live owner with missing local activation conditionally releases and returns retry;
7. retry performs a fresh node selection and permits offline redistribution;
8. `spawn=false` never creates an actor;
9. Redis error is not interpreted as miss;
10. renewal token mismatch and deadline expiry self-fence exactly once;
11. PostgreSQL fence advance rejects an older epoch;
12. save transaction rejects a stale epoch before business writes;
13. same node and epoch retry is idempotent;
14. full package tests and `go build ./...` pass.

## Permanent Non-goals

- no online actor migration;
- no shard coordinator or shard handoff;
- no mailbox or in-memory actor state migration;
- no per-actor heartbeat;
- no fixed owner TTL;
- no node Set scan in locate;
- no local locate cache;
- no transparent availability during ownership-store partition;
- no migration to another actor framework.

这些能力带来的体验收益不足以覆盖协调状态机、故障恢复、测试和运维复杂度，不作为后续自然演进方向。
