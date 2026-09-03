# Actor Location Ownership Design

## Goal

将 actor 定位从“缓存写入 + 无条件删除”改为可验证的单 owner 协调模块,支持节点故障接管并拒绝旧 owner 的延迟副作用。

## Scope

本设计覆盖 `core/gxyactor` 的 actor locate 注册、定位、Claim、Release 和节点 lease,以及 Role 保存时的 owner/fencing 校验。它不改变 Consul 服务发现、节点选择算法或 actor PID 格式;不在第一版增加本地 locate 缓存或 Redis 节点 Set 扫描。

## Invariants

1. 同一玩家的 owner 变更必须由 Redis 原子 Claim 决定。
2. Claim 未成功的节点不得创建该玩家的可服务 actor。
3. owner 记录必须包含 `node_id` 和单调递增的 `epoch`。
4. Release 只能删除调用者当前持有的 owner 记录。
5. epoch 小于 Redis 当前值的保存或其他持久化副作用必须被拒绝。
6. Redis 不可用时不允许通过本地 fallback 创建 actor。
7. locate 热路径只能做 O(1) direct lookup。

## Interface

在 `core/gxyactor` 内提供一个小的 `ActorLocator` 接口,调用方不直接拼 Redis key 或执行 Redis 命令:

```go
type ActorOwner struct {
    NodeID string
    Epoch  uint64
}

type ActorLocator interface {
    Locate(ctx context.Context, kind, id string) (ActorOwner, error)
    Acquire(ctx context.Context, kind, id string) (ActorOwner, error)
    Release(ctx context.Context, kind, id string, owner ActorOwner) error
}
```

lease token 只属于实现内部,不暴露给业务层。实现内部还维护节点 lease:

```text
node lease key -> random lease token, EX TTL
heartbeat -> only refresh the same token
```

实际配置使用现有节点生命周期注入,不在业务调用方创建 Redis client。

## Redis state

第一版继续使用 direct per-actor key,避免同时改变查询布局和所有权语义:

```text
gserver:locate:node:actor:{kind}:{id}
  -> encoded owner {node_id, epoch}
  TTL: owner retention safety net

gserver:locate:lease:{node_id}
  -> random lease token
  TTL: node lease

gserver:locate:epoch
  -> monotonically increasing fencing counter
```

值编码必须可解析且保留 `node_id` 与 `epoch`;不得只保存节点名。后续若内存仍是瓶颈,独立实验 sharded Hash,不改变 Claim/Release 接口。

## Atomic operations

### Claim

Claim Lua 在一次 Redis 原子执行中完成:

1. 验证候选节点的 lease key 仍等于调用者 token。
2. 读取 actor owner。
3. owner 存在且其节点 lease 有效时返回当前 owner。
4. owner 缺失或其节点 lease 过期时递增 epoch并写入新 owner。
5. 同一 owner 的重复 Claim 返回成功且保持幂等。

脚本结果必须区分 `acquired`、`already_owned`、`owned_by_other`、`invalid_lease` 和基础设施错误,调用方不得把未知错误当成未命中。

### Release

Release Lua 必须比较当前 owner 的 `node_id + epoch` 和调用者提供的 owner,匹配才删除;不匹配返回 no-op。不得使用裸 `DEL`。

### Heartbeat

节点启动获得随机 lease token,定期只刷新 token 相同的 lease。节点重启即使复用逻辑节点名也必须生成新的 token。lease 过期后 Claim 才能接管。

## Actor lifecycle integration

```text
getActor
  -> Locate direct lookup
  -> owner healthy: construct PID and route
  -> miss/stale/dead owner: Acquire
  -> acquired: activate locally/remotely
  -> not acquired: route to returned owner

actor registration
  -> owner registration is Claim, not SET

actor termination
  -> conditional Release

role save
  -> validate owner node + epoch before persistence
```

注册失败、Touch 失败和远程 spawn 失败必须按 owner 身份执行条件清理,不能删除后来产生的 owner。

## Error policy

- Redis command error: return wrapped infrastructure error; do not treat as offline.
- Redis Nil/missing owner: enter Acquire path.
- Invalid lease: stop activation on this node and refresh/reacquire node lease.
- Owner exists: return owner without creating a second actor.
- Fencing mismatch on save: skip/reject write and log structured context with player, expected epoch and current epoch.

## Verification

Tests must cover real Redis behavior through an injectable Redis adapter or a temporary Redis instance:

1. concurrent Claim by two nodes produces one owner;
2. active owner prevents a second Claim;
3. expired lease permits takeover and increases epoch;
4. old Release cannot remove the new owner;
5. old epoch save is rejected;
6. Redis errors are not interpreted as misses;
7. actor activation does not proceed after losing Claim;
8. full existing `core/gxyactor` tests remain green.

The existing Redis layout benchmark remains separate. After semantic correctness is green, rerun direct locate latency and resource measurements; only then decide whether sharded Hash is worthwhile.

## Non-goals

- no full node Set scan in locate;
- no local cache in first implementation;
- no change to Consul service registration;
- no claim that Redis TTL alone solves network partitions;
- no migration to a new actor framework.
