# Actor 定位与节点注册

## 概述

Actor 定位系统管理 actor 在集群中的位置：gate 收到客户端请求时，通过定位系统找到目标 actor 所在节点，将消息路由到正确的节点。系统采用两层架构——**Redis 保存玩家到 owner 的直接映射，Consul 做节点级服务发现**。

核心设计原则：
- **Redis 只存 owner 的 `nodeInstanceName + fencing epoch`，不存 address:port**。
- **locate 热路径只做 O(1) direct lookup**，不扫描节点 Set。
- **节点租约由每个节点统一续期**，不为每个 actor 创建续约任务。
- **actor 激活必须先 Claim**；Claim、接管和 Release 使用 Redis Lua 原子比较。
- **epoch 用于 fencing**：旧 owner 的延迟保存或清理不得覆盖新 owner。
- **Redis 错误 fail closed**，不得把基础设施错误当成未命中后本地创建 actor。

## 数据结构

### Redis — Actor owner

Key 格式：

```text
gserver:locate:node:actor:{kind}:{id}
```

Value 格式：

```text
{nodeInstanceName}|{epoch}|{leaseToken}
```

业务层只使用 `nodeInstanceName + epoch`；`leaseToken` 由定位模块内部用于条件续租和释放。

| 属性 | 值 |
|------|-----|
| owner TTL | 不设置；owner 有效性由对应 node lease 决定 |
| 正常续约 | 不续约单个 actor；节点 lease 统一续期 |
| 注册方式 | Redis Lua Claim，不能直接 SET |
| 清理 | compare-and-delete，不能直接 DEL |

### Redis — Node lease

Key 格式：

```text
gserver:locate:node:lease:{nodeInstanceName}
```

Value 是当前进程的 lease token，TTL 由节点级 heartbeat 刷新。节点重启生成新的 `nodeInstanceName` 和 token。

### Consul — 节点服务注册

Service Key：`gserver-{nodeName}-{serviceName}`

| 属性 | 值 |
|------|-----|
| 注册名 | `nodeInstanceName`（`game-2@uid`） |
| TTL | 10s（配置 `registery.consul.ttl`） |
| 续约 | 每 10s 刷新一次 |
| 内容 | `NodeHost` = actor system address（`host:random_port`） |
| 用途 | `GetHashServices` 列举所有健康节点 |

## 核心流程

### GetActor（定位 actor）

```
GetRoleActor(roleID)
  → ActivateActor("role", roleID, spawn=false)
    → getActor("role", roleID, spawn=false)
      │
      ├─ Redis direct lookup
      │   GET key → owner {nodeInstanceName, epoch, leaseToken}
      │   ↓
      │   命中 → Consul 解析完整 nodeInstanceName 地址
      │         ├── 地址缺失 → fail closed，不接管仍有有效 lease 的 owner
      │         └── 请求 owner Activator（allow_spawn=false）
      │              ├── 本地 ActorMgr 命中 → 返回 PID
      │              └── 本地 activation 缺失 → 条件删除 owner，返回 RetryLocate
      │
      └─ 未命中且 spawn=true（由 ActivateRole 进入）
          → ConsistentHash 选择候选节点
          → 请求候选 Activator（allow_spawn=true）
               ├── acquired → SpawnNamed → Touch 成功后返回 PID
               └── owned_by_other → 返回 RetryLocate，重新读取 directory
```

### Claim 与 Release

```
节点启动
  → SET node lease(token) EX TTL
  → heartbeat 只刷新 token 相同的 lease

ActorActive 到达候选节点
  → Claim 验证候选 lease
  → 检查现有 owner lease
  → owner 失效时 INCR epoch 并写入新 owner
  → Claim 失败不得创建 actor

actor terminate / Touch 失败
  → compare-and-delete(nodeInstanceName, epoch, token)
  → 不匹配时保持当前 owner
```

关键点：
- 正常 locate 是一次 O(1) Redis direct lookup，不扫描节点 Set。
- `Claim` 在 `SpawnNamed` 之前执行，避免两台节点先创建 actor 再互相覆盖 owner。
- `epoch` 必须传入 Role 保存等持久化路径；旧 epoch 的副作用被拒绝。
- Redis 错误与 owner miss 分开处理；Redis 错误不进入本地 fallback 创建。

### Actor 生命周期

```
node 启动
  → OnModInit 生成 nodeInstanceName = nodeName@unixNano
  → 创建 node lease，token 使用本次 nodeInstanceName
  → heartbeat 定期续租；token 不匹配立即 self-fence
  → Redis 错误仅可在上次确认的 lease deadline 前重试
  → deadline 到期仍无法确认续租时终止进程
  → serviceApp 以 nodeInstanceName 注册 Consul

Claim 成功
  → SpawnNamed(props, id, id, ActorOwner{NodeID, Epoch})
  → pending 状态合并同 ID 并发请求
  → Touch 确认 Init 成功后才发布到本地 ActorMgr

actor terminate（正常退出）
  → compare-and-delete 当前 node + epoch + token

进程崩溃或 self-fence
  → heartbeat 停止，node lease 过期
  → owner key 可能残留，但 token 不匹配或 lease 缺失时不再有效
  → 下次 Claim 递增 epoch 后接管
```


### 节点重启和 fencing

```
节点 A 旧进程持有 P1:
  → P1 -> node-A@OLD, epoch=41

节点 A 崩溃，节点 B 接管:
  → 旧 lease 过期
  → Claim 写入 P1 -> node-B@NEW, epoch=42

旧节点 A 的延迟保存:
  → 携带 node-A@OLD, epoch=41
  → PostgreSQL 事务先锁定 role_actor_fence 的 exact owner
  → fence 已是 node-B@NEW, epoch=42，查询不到匹配行
  → 整个保存事务回滚
```

Consul 负责节点服务发现；Redis lease、token 和 epoch 负责 directory 协调。Redis 查询与后续写库之间存在 TOCTOU，不能作为持久化 fencing；PostgreSQL `role_actor_fence` 才是拒绝旧 actor 写入的最终边界。

## 代码位置

| 文件 | 说明 |
|------|------|
| `core/gxynode/node.go` | 生成 `NodeInstanceName`，传入 actorApp 和 serviceApp |
| `core/gxyactor/actor_locator.go` | ActorOwner、node lease deadline、Claim、Release、epoch 和 self-fence |
| `core/gxyactor/activator_manager.go` | owner Activator 验活、pending Touch、RetryLocate、条件 Release |
| `core/gxyactor/system.go` | actorApp 启动 remote、activatorManager 生命周期和 owner 查询 |
| `core/gxyservice/service_app.go` | 以 `nodeInstanceName` 注册 Consul，`GetAddressByNodeName` |
| `core/gxyregistery/types.go` | ServiceInfo 结构体 |
| `src/apps/gateway/internal/logic/session.go` | 客户端接入，调用 ActivateRole |
| `src/apps/role/internal/logic/role_actor_fence.go` | PostgreSQL epoch fence 的推进与事务锁定 |
| `src/apps/role/internal/logic/role_main.go` | Role activation 和保存事务接入数据库 fence |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| Redis 存什么 | `nodeInstanceName + epoch + leaseToken` | 直接 O(1) 定位；epoch 防止旧 owner 副作用 |
| Actor owner 注册 | Claim Lua | 在 SpawnNamed 前原子检查和抢占，避免双创建 |
| Node lease | 每节点一个 TTL lease | 续约责任集中在节点，不为每个 actor 续约 |
| owner TTL | 不设置 | 防止长生命周期 actor 的 owner key 过期；残留记录由 lease 判定失效并在下一次 Claim 时覆盖 |
| Redis 命中后行为 | 请求 owner Activator | 只有 owner 节点本地 ActorMgr 能确认 activation 存在；残留 owner 条件删除后重试 |
| 接管规则 | lease 失效或 token 不匹配后递增 epoch | 新 owner 版本高于旧 owner |
| 注销方式 | compare-and-delete(node, epoch, token) | 延迟旧清理不能删除新 owner |
| 持久化校验 | PostgreSQL 事务锁定 exact node + epoch | Redis 预查存在 TOCTOU；旧 actor 的整个保存事务必须被拒绝 |
| 节点选择 | 一致性哈希 | 同一 ID 稳定选择候选节点 |
| 节点端口 | `remote.Configure(host, port.actor)` | 使用配置端口，避免随机端口带来的防火墙/注册问题 |

## 变更记录

| 日期 | 变更 | 说明 |
|------|------|------|
| 2026-09-03 | owner fencing 改造 | direct lookup 保留性能，增加节点 lease、原子 Claim/Release 和 epoch |
| 2026-05-02 | v2 重构 | 以 `nodeInstanceName` 代替 `address:port`，去掉续约，去掉 gxylocator |