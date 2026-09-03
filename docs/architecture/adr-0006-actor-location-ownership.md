# ADR 0006:Actor 定位与所有权 fencing

- 日期:2026-09-03
- 状态:**Accepted**
- 关联:actor locate 并发压测与所有权改造

## 背景

当前 actor locate 使用 per-player Redis key 保存 `nodeInstanceName`,actor 创建时直接 `SET`,销毁时直接 `DEL`,保存时只比较节点名。该实现的正常查询是 O(1),但 `SET`/`SETNX`/TTL/无条件 `DEL` 不能共同保证并发激活、节点崩溃接管、延迟清理和网络分区下的所有权安全。

`ktm_server` 的节点 Set 扫描方案虽然将续约责任集中到节点,但一次定位需要遍历节点并执行大量 `SISMEMBER`。本地 1000 节点、1000 QPS 测试中 Redis CPU 为 75.04%,定位 p99 为 119.957ms;因此不能将该扫描逻辑放入 GServer locate 热路径。

## 决策

采用**直接玩家定位 + 节点级 lease + 原子 Claim + fencing epoch**:

1. Redis 保存玩家到 owner 节点的直接映射,定位只执行 O(1) direct lookup;第一版保留 per-player String,后续单独评估 sharded Hash 的内存收益。
2. 每个节点只维护一个带 TTL 的 lease。lease token 是每次节点进程租约的随机身份,节点重启必须生成新 token。
3. actor 激活通过 Redis Lua 原子 Claim 完成。Claim 必须验证候选节点 lease,检查现有 owner lease,接管时递增 epoch,并返回当前 owner 或新 owner。
4. actor 释放通过 compare-and-delete 完成,必须匹配 `node_id + lease token + epoch`;禁止无条件 `DEL`。
5. owner 记录包含 `node_id + fencing_epoch`;保存和其他持久化副作用必须拒绝小于当前 epoch 的旧操作。
6. Redis 不可用时 fail closed,禁止回退到本地创建 actor。
7. 节点 Set 仅用于后台统计、审计和恢复,不参与 locate 数据面查询。

## 语义

```text
lease token: 标识某个节点进程的当前租约
lease TTL:   节点多久没有心跳后可以被接管
fencing epoch:每次所有权变更递增的版本号
```

`lease token` 防止同一 `nodeInstanceName` 的旧进程冒充新进程;`epoch` 防止旧 owner 的延迟保存、释放或其他副作用覆盖新 owner。Redis owner 记录本身不足以阻止网络分区中的旧进程,因此 fencing 信息必须进入下游写入校验。

## 数据流

```text
node startup
  -> acquire node lease(token, TTL)
  -> heartbeat lease

GetActor
  -> ActorLocator.Locate(playerID)
  -> direct Redis lookup
  -> healthy owner: route to owner
  -> miss/stale owner: ActorLocator.Acquire(playerID)
  -> atomic Claim Lua
  -> only winner activates actor

actor shutdown
  -> conditional Release(playerID, nodeID, token, epoch)
```

## 失败处理

- 并发 Claim:Redis 脚本串行化检查和写入,只有一个节点取得新 epoch。
- owner 崩溃:lease 过期后允许接管;新 owner 获得更大的 epoch。
- 延迟旧请求:epoch 小于当前值时拒绝。
- 延迟旧清理:身份和 epoch 不匹配时不删除当前 owner。
- Redis 故障:拒绝创建新 actor,避免双 owner。

## 拒绝的方案

### 节点 Set + 每次全量扫描

查询成本随节点数增长,压测已证明在 1000 QPS 下 Redis CPU 和尾延迟过高。

### 普通 SET

并发 GET miss 后会互相覆盖,两个节点都可能已经创建 actor。

### 仅 SETNX

能处理首次竞争,但不能处理 owner 崩溃后的接管,也不能阻止旧 owner 的延迟副作用。

### 仅 TTL、不使用 epoch

TTL 只能表达协调系统对存活状态的判断,不能让网络分区中的旧进程停止写入。

## 边界

该决策保证 Redis 可用且所有关键副作用经过 fencing 校验时的单 owner 语义。若下游绕过 owner/epoch 直接写入,系统只能保证 Redis 映射唯一,不能保证所有外部副作用唯一。

第一版不引入本地 locate 缓存;缓存属于后续性能优化,不能作为 Claim 的依据。
