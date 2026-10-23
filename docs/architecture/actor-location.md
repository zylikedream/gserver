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

## 身份分层：actorKey / PID / ActorPid

同一个 actor 在系统中有三种表示，对应三个不同的职责与生命周期，**不可混用**（这是 invariants #5 身份分层的细化）：

| 类型 | 语义 | 内容 | 生命周期 |
|------|------|------|----------|
| `actorKey` | 身份（WHO）：哪个逻辑 actor | `kind` + `id` | 稳定，跨重启 |
| `PID` | 地址（WHERE）：消息发到哪个实例 | 本机进程标识 / 节点+注册名 | 含**代际**，重启失效 |
| `pb.ActorPid` | wire（传输格式） | `Address`+`Name`+`Pid`+`Creation` | 传输瞬间 |

**为什么身份必须先于地址存在：**

- **激活之前就要能表达身份**——所有权记录的键、跨节点寻址的目标，都是在实例（地址）尚不存在时就要使用的；此时没有 PID，只有 `kind`+`id`。
- **身份稳定，地址会失效**——同一逻辑 actor 重启后，PID 含"哪一代实例"的代际信息，对端重启后旧 PID 即失效（防误投）；而 `kind`+`id` 稳定，注册名 `<kind>/<id>` 是稳定的逻辑身份。
- **地址必须含代际**——投递要落到"哪个节点、哪一代实例"，不能只用身份，否则重启后误投给旧实例。

派生关系：`actorKey.name()` 产生注册名 `<kind>/<id>`，是 PID 跨节点引用的 `Name`、也是 `pb.ActorPid.Name`。**身份是根，地址与 wire 格式都由身份派生。**

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
| 注册名 | 节点身份 `nodeInstanceName`（`{podName}@{host}`，见 ADR 0018） |
| TTL | 10s（配置 `registery.consul.ttl`） |
| 续约 | 每 10s 刷新一次 |
| 内容 | `NodeHost` = actor system address（`host:random_port`） |
| 用途 | `GetHashServices` 列举所有健康节点 |

## 核心流程

### 定位与激活（`activateActor`）

```
GetRoleActor(roleID)            spawn=false，只查
ActivateRole(roleID)            spawn=true，不存在时创建
  → gxyactor.ActivateActor(kind, id, spawn)
    → activateActor：最多 3 轮，每轮重新 Locate（事实可能已变）
      │
      ├─ 有 owner
      │   ├─ owner 是本节点 → resolveLocal
      │   │     ├─ 本地有实例（按注册名查，O(1)）→ 返回
      │   │     └─ 本地无实例 → 条件删除陈旧 owner → 重试
      │   └─ owner 是其他节点 → Call 该节点的 activator/router（allowSpawn=false）
      │         ├─ ActorPid → 返回（地址由对端给出，本地不重拼）
      │         └─ ActorLocateRetry → 重试
      │
      └─ 无 owner
          ├─ spawn=false → 不存在
          └─ 一致性哈希选候选节点
                ├─ 候选是其他节点 → Call 对方 router（allowSpawn=true）
                └─ 候选是本节点 → SpawnNamed
                      ├─ 竞争落败（ErrTaken / ErrNotOwner）→ 重试
                      └─ 成功 → 返回
```

`allowSpawn` 只控制"无 owner 时能不能创建"，并不跳过校验：已有 owner 的查询也必须经 owner 节点重新校验它自己的本地实例，避免 Redis 定位结果与远端处理之间的竞态。

重试只发生在 `activateActor` 一层——被调函数只回答"这一轮有没有形成结论"（`errLocateRetry`），自己从不重试。需要重试的只有三类：**陈旧记录刚被释放**、**同名实例正在创建**、**远端要求重新定位**；其它错误直接上抛，否则会被磨成"重试耗尽"，盖掉真正的原因。

### Claim 与 Release

```
节点启动
  → SET node lease(token) NX EX TTL（获取失败即启动失败，不静默接管旧实例）
  → heartbeat 只刷新 token 相同的 lease

ActorActive 到达候选节点（无 owner 且 allowSpawn）
  → SpawnNamed（注册名 = <kind>/<id>）
  → actor 同步初始化段：Claim 验证候选 lease → 检查现任 owner lease
  → 现任失效 → INCR epoch 写入新 owner；现任活跃 → Claim 失败（ErrNotOwner）
  → Claim 失败 → Init 报错 → 进程终止，实例不成立；调用方按竞争落败重试

actor terminate（正常退出）
  → 业务最终落库之后：compare-and-delete(nodeInstanceName, epoch, token)
  → 不匹配时保持当前 owner
```

关键点：
- 正常 locate 是一次 O(1) Redis direct lookup + 一次常量时间的名字查找，不扫描进程表。
- `Claim` 在 actor 的**同步初始化段**执行（`SpawnNamed` 之后、消息处理与状态加载之前）：注册名占用先把同节点的并发创建收敛成一个（`ErrTaken`），Redis 仲裁再保证跨节点单写者（`ErrNotOwner`）；两者在激活层都被归为"竞争落败"并重试（不变量 #3）。
- `epoch` 必须传入 Role 保存等持久化路径；旧 epoch 的副作用被拒绝。
- Redis 错误与 owner miss 分开处理；Redis 错误不进入本地 fallback 创建。

### Actor 生命周期

```
node 启动
  → OnModInit 取节点身份 nodeInstanceName = {podName}@{host}（不含时间戳，见 ADR 0010/0018）
  → 创建 node lease，token 为每实例随机值（不取节点身份，见 ADR 0010）
  → heartbeat 定期续租；token 不匹配立即 self-fence
  → Redis 错误仅可在上次确认的 lease deadline 前重试
  → deadline 到期仍无法确认续租时终止进程
  → serviceApp 以 nodeInstanceName 注册 Consul

实例创建
  → SpawnNamed（注册名 = <kind>/<id>，首个 Init 参数 = id）
  → 同步初始化段：业务绑标识 → 门面 Claim（失败即进程终止，调用方视为竞争落败）
  → 门面自投异步初始化消息 → 业务异步加载 → 消息循环

actor terminate（正常退出）
  → compare-and-delete 当前 node + epoch + token

node 停机（优雅）
  → 先停节点：等所有 actor 的终止回调返回（最终落库 + compare-and-delete 归属）
  → 再停 heartbeat 并释放 node lease
  → 顺序反过来（先交还租约）会让其他节点在最终落库之前就能接管，
    旧实例的存盘随即被 fence 拒绝，静默丢数据

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
| `core/gxyactor/actor_locator_scripts.go` + `core/gxyactor/lua/*.lua` | 归属与租约的 Lua 脚本（//go:embed 嵌入；公共函数在构建期拼到每个脚本前） |
| `core/gxyactor/activator_manager.go` | 定位入口 `activateActor`（重试单点）、owner 节点验活、条件 Release、SpawnNamed |
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
| Actor owner 注册 | Claim Lua | 在 actor 同步初始化段原子仲裁；双创建由注册名占用（`ErrTaken`）与 Claim（`ErrNotOwner`）双重收敛 |
| Node lease | 每节点一个 TTL lease | 续约责任集中在节点，不为每个 actor 续约 |
| owner TTL | 不设置 | 防止长生命周期 actor 的 owner key 过期；残留记录由 lease 判定失效并在下一次 Claim 时覆盖 |
| Redis 命中后行为 | 请求 owner 节点 | 只有 owner 节点能确认本地实例是否存在（按注册名查，O(1)）；残留 owner 由它条件删除后重试 |
| 接管规则 | lease 失效或 token 不匹配后递增 epoch | 新 owner 版本高于旧 owner |
| 注销方式 | compare-and-delete(node, epoch, token) | 延迟旧清理不能删除新 owner |
| 持久化校验 | PostgreSQL 事务锁定 exact node + epoch | Redis 预查存在 TOCTOU；旧 actor 的整个保存事务必须被拒绝 |
| 节点选择 | 一致性哈希 | 同一 ID 稳定选择候选节点 |
| 节点端口 | `remote.Configure(host, port.actor)` | 使用配置端口，避免随机端口带来的防火墙/注册问题 |

## 变更记录

| 日期 | 变更 | 说明 |
|------|------|------|
| 2026-10-19 | 文档对齐实现 | 激活流程改写为 `activateActor` 的真实分支；删除不存在的 pending/Touch/ActorMgr 步骤；修正 Claim 与 SpawnNamed 的先后；补 Lua 脚本位置 |
| 2026-09-03 | owner fencing 改造 | direct lookup 保留性能，增加节点 lease、原子 Claim/Release 和 epoch |
| 2026-05-02 | v2 重构 | 以 `nodeInstanceName` 代替 `address:port`，去掉续约，去掉 gxylocator |