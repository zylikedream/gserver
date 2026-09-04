# Actor 定位方案调研

## 结论

旧 `ktm_server` 方案比改造前 GServer 的“每个 actor 一个 12 小时 locate key”更适合作为第一版在线 actor 定位基础，原因是续约责任集中在游戏节点，而不是分散到每个 actor：

```text
节点级 heartbeat → 刷新本节点在线 actor 集合 → 集合整体过期
```

它降低了孤儿 key 管理、续约协程数量和节点重启清理的复杂度。但旧方案本身是**节点级在线索引 + Redis 原子注册脚本**，不是严格意义上的 fencing 方案；网络分区或错误故障判断仍可能造成双活。当前已采用节点级 lease、per-player direct lookup、原子 Claim/Release 与 epoch fencing。

## 调研范围

- 旧项目：`/home/zyr/workspace/ktm_server`
- 当前项目：`/home/zyr/workspace/gserver_github`
- 外部一手资料：Redis、HashiCorp Consul、Microsoft Orleans 官方文档

## 旧方案：节点级在线玩家集合

### 续约责任

旧项目在 `apps/statistics/src/gs_statistics_server.erl` 中定义：

```erlang
-define(SYNC_ROLELIST_INTERVAL, 10).
```

每 10 秒执行 `do_tick_rolelist/0`，游戏节点调用 `update_rolelist/0`，再进入 `util_loadbalance:updateOnlineRoles/0`。该函数从本节点 ETS 读取全部在线角色，并调用 `db_redis:onlineRoleSetInit/2`。

来源：

- `/home/zyr/workspace/ktm_server/apps/statistics/src/gs_statistics_server.erl:22-23,298-321`
- `/home/zyr/workspace/ktm_server/apps/tool/src/util/util_loadbalance.erl:43-46`

在线集合的刷新是一个 Redis `MULTI/EXEC`：

```text
DEL __ONLINE_ROLE_IDS_<node>
SADD __ONLINE_ROLE_IDS_<node> 当前在线玩家
EXPIRE __ONLINE_ROLE_IDS_<node> 20
```

来源：

- `/home/zyr/workspace/ktm_server/apps/db/src/db_redis.erl:74-75,1633-1645`

节点信息同样设置 20 秒过期时间：

```text
ZADD __SERVER_RANK_KEY__ server
SETEX __SERVER_INFO_<node> 20 serverInfo
```

来源：

- `/home/zyr/workspace/ktm_server/apps/tool/src/util/util_loadbalance.erl:50-56`
- `/home/zyr/workspace/ktm_server/apps/db/src/db_redis.erl:1647-1655`

因此，旧方案的续约者是每个游戏节点中的统计/负载均衡进程，不是每个 role 进程。

### 玩家注册

`register_role.lua` 在 Redis Lua 脚本中完成：

1. 遍历服务器排名集合；
2. 检查玩家是否已经属于某个仍有有效 `SERVER_INFO` 的节点；
3. 如果已存在，返回原节点信息；
4. 如果不存在，将玩家加入目标节点集合，并刷新集合 TTL。

来源：

- `/home/zyr/workspace/ktm_server/apps/db/priv/lua/register_role.lua:15-21,29-50`
- `/home/zyr/workspace/ktm_server/apps/db/src/db_redis.erl:1657-1670`

这一步是 Redis 内部原子执行的，所以在所有节点都能访问同一个 Redis、节点信息仍有效的正常场景下，能够避免两个普通并发登录请求同时把玩家注册到两个节点。

### 玩家查找

`get_gameserver_by_roleid.lua` 遍历服务器排名集合，逐个检查：

```text
__ONLINE_ROLE_IDS_<node> 是否包含 roleID
__SERVER_INFO_<node> 是否存在
```

如果没有找到玩家，再选择一个有效游戏服。

来源：

- `/home/zyr/workspace/ktm_server/apps/db/priv/lua/get_gameserver_by_roleid.lua:33-52`
- `/home/zyr/workspace/ktm_server/apps/tool/src/util/util_loadbalance.erl:94-107`

## 改造前的 GServer 方案

改造前方案使用单独的 per-player key：

```text
gserver:locate:node:actor:<kind>:<id> → nodeInstanceName
```

actor spawn 成功后写入 Redis，TTL 为 12 小时且不续期；actor terminate 时直接删除 key。

来源：

- `87b5ce1:docs/architecture/actor-location.md:15-26,67-80,83-101`
- `87b5ce1:core/gxyactor/activator_manager.go:146-165,226-246`

改造前查找流程是：

```text
GET per-player locate key
→ 根据 nodeInstanceName 查询 Consul 地址
→ 命中且节点存活则直接返回 PID
→ 未命中或节点失效时，一致性哈希选择节点并远程 spawn
```

来源：

- `87b5ce1:core/gxyactor/activator_manager.go:406-450`
- `87b5ce1:docs/architecture/actor-location.md:40-65`

改造前 per-player key 的优点是正常查找 O(1)，且不需要遍历全部节点；缺点是 locate key 同时承担路由缓存、所有权记录、存活判断和清理信号，导致 `SET`、TTL、无条件 `DEL` 之间存在语义冲突。

## 差异对比

| 维度 | 旧节点集合方案 | 改造前 GServer per-player key |
|---|---|---|
| 续约者 | 每个节点一个 heartbeat | 没有 actor locate 续约 |
| 续约粒度 | 整个节点的玩家集合 | 每个玩家 key |
| 正常查找 | 遍历节点集合，O(节点数) | 直接 GET，O(1) |
| 孤儿处理 | 节点集合整体过期 | 单个 key 最长残留 12 小时 |
| 并发注册 | Redis Lua 原子扫描并加入集合 | 当前 spawn 后无条件 SET |
| 正常注销 | SREM 玩家并刷新节点集合 | 无条件 DEL per-player key |
| 节点重启 | 新节点名/节点集合自然隔离 | 新 nodeInstanceName 使旧 key 失效，但旧 key 仍残留 |
| 网络分区 | 仍可能双活 | 仍可能双活 |
| 主要成本 | 查询节点数、快照一致性窗口 | actor 生命周期和 ownership 竞态 |

## 旧方案的真实边界

### 优点

1. 续约责任单一：每个节点一个定时任务。
2. 节点崩溃时整批玩家记录一起失效，不会留下大量长期 per-player 孤儿 key。
3. `register_role.lua` 将“查已有 owner”和“注册到目标节点”放在同一个 Redis 原子操作中。
4. 节点实例信息与在线集合都有 TTL，故障后可以自动恢复可用性。

### 缺陷

1. `updateOnlineRoles/0` 先读取 ETS 快照，再 `DEL + SADD`。玩家注册或下线与快照刷新交错时，可能出现短暂漏记或多记。
2. 玩家查找是 O(节点数)，节点规模扩大后 Redis Lua 扫描成本上升。
3. 节点 heartbeat 过期只表示“协调系统认为节点失效”，不代表旧进程已经停止；网络分区时可能出现新旧节点同时处理同一玩家。
4. 旧方案没有下游 fencing。TTL 和 Redis owner 判断不能阻止旧 actor 在接管后继续执行数据库或外部副作用。

## 外部方案对照

### Redis

Redis 官方文档将单实例锁的基本模式描述为：

```text
SET resource_name unique_value NX PX 30000
```

释放时必须比较 value 后再删除，不能直接 `DEL`，否则延迟的旧客户端可能删除新 owner 的锁。官方还明确提醒：锁可能在持有者暂停或网络分区期间过期，涉及长耗时操作时应使用 fencing tokens。

来源：

- [Redis Distributed Locks](https://redis.io/docs/latest/develop/clients/patterns/distributed-locks/)
- [Redis SET](https://redis.io/docs/latest/commands/set/)

这说明：`SETNX` 只能解决“同时抢占时谁成功”，不能单独解决旧 owner 失效后的安全写入。

### Consul

Consul session 可以绑定节点、健康检查、TTL 和 KV lock。官方文档明确区分了两个取舍：

- 使用健康检查或 TTL：获得故障后的可用性，但可能误判并释放仍存活 owner 的锁；
- 不使用健康检查：安全性更强，但故障后可能需要人工介入才能释放锁。

Consul 的 `(Key, LockIndex, Session)` 可以作为 sequencer 传给下游资源，但 Consul 锁本身是 advisory 的；下游必须主动校验 sequencer，Consul 不会阻止绕过锁的写入。

来源：

- [Consul Sessions and Distributed Locks](https://developer.hashicorp.com/consul/docs/automate/session)

### Orleans

Orleans 将 grain directory 明确建模为“逻辑 actor identity 到当前 activation 所在 silo 的映射”。官方文档同时承认：默认目录是 eventually consistent，在集群不稳定时可能出现重复 activation；需要更强单激活保证时，应使用强一致目录或存储支持的目录实现。

这个模型与本项目更接近：**定位目录本身是 actor 激活管理的一部分，而不是简单缓存**。但 Orleans 的目录也不能自动替代外部资源上的 fencing。

来源：

- [Orleans Grain Directory](https://learn.microsoft.com/en-us/dotnet/orleans/host/grain-directory)

## 推荐演进方向

建议采用“旧方案的节点级续约 + 当前方案的快速定位”混合模型：

```text
节点级 heartbeat
    → 负责本节点 actor 集合的有效期

玩家级 atomic claim
    → 负责同一玩家只能被一个节点激活

per-player locate
    → 作为快速路由缓存，可由节点状态验证

role version / fencing
    → 负责拒绝旧 owner 的持久化副作用
```

具体原则：

1. 续约只由节点级 manager 执行，不为每个 actor 创建续约任务。
2. actor 激活前先执行 Redis 原子 claim；未抢到 claim 的节点返回 `RetryLocate`，不得继续创建可服务 actor。
3. 每个节点实例持有带 token 的 TTL lease；节点重启必须使用新的 `nodeInstanceName`。
4. actor 正常退出使用 compare-and-delete，不能无条件删除新 owner 的记录。
5. Redis 命中必须经过 owner Activator 验证本地 activation；残留 directory entry 条件删除后重试。
6. Redis owner 查询不能充当写库 fencing；Role 必须在 PostgreSQL 事务内锁定 exact epoch fence。

## 已确认决策

采用“可接管 + fencing”：

- 节点级 heartbeat 负责 lease 存活，不为每个 actor 续租。
- locate 使用 per-player direct lookup，永久不使用节点 Set 全量扫描。
- Claim、接管和 Release 由 Redis Lua 原子完成。
- owner 记录包含 `nodeInstanceName + epoch + leaseToken`，且 lease token 必须精确匹配。
- Redis 命中请求 owner Activator，不直接构造 PID；本地 activation 缺失时条件清理并重试。
- Role activation 推进 PostgreSQL fence；每次保存事务先锁定 exact node 和 epoch。
- lease 续租失败超过安全 deadline 时节点 self-fence 并退出。
- Redis 错误 fail closed，不允许 fallback 本地创建 actor。
- shard ownership、在线 actor 迁移、本地 locate 缓存、节点 Set 扫描及 mailbox/state 迁移是永久非目标。

已有模块 version 乐观锁继续作为同一 owner 内的数据库冲突保护，但不替代 actor owner fencing。
