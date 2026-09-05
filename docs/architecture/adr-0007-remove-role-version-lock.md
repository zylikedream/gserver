# ADR 0007: Role 持久化移除模块 Version 乐观锁

- 日期：2026-09-05
- 状态：**Accepted**
- 关联：ADR 0006 Actor 定位与所有权 Fencing

## 背景

Role Actor 的正常写入由同一个 Actor PID 的 mailbox 串行处理。跨节点 ownership 变化则由 Redis lease/epoch 协调，并由 PostgreSQL `role_actor_fence` 在保存事务中拒绝旧 owner。

此前每个 Role 模块状态还携带 `Version`，保存时执行 `UPDATE ... WHERE role_id = ? AND version = ?`，并在内存中维护版本回滚。该机制主要防御同一 `role_id` 出现多个 writer 或旁路写入，与 Actor 串行化和 PostgreSQL ownership fence 存在部分重复，同时增加了 `IPersistState` 接口、首次写入分支、版本回滚和测试复杂度。

## 决策

1. 移除 `IPersistState.GetVersion`、`IPersistState.SetVersion` 和 `RolePersistState.Version`。
2. Role 模块使用 `role_id` 作为唯一持久化身份，通过 GORM `Save` 完成新行插入或已有行更新，不再执行模块 Version 比较。
3. `role_actor_fence` 继续作为唯一的 ownership fencing 机制：
   - Role Actor 初始化时推进自己的 `node_id + epoch` fence；
   - 每次 Role 保存事务先对完全匹配的 `role_id + node_id + epoch` 执行 `SELECT ... FOR UPDATE`；
   - fence 不匹配时整个保存事务回滚，不得写入业务表。
4. 该决策依赖 Role Actor mailbox 的正常串行写入，以及 Activator 对同一 `role_id` 的唯一激活约束。重复 activation 必须 fail closed，不得创建第二个 writer。
5. `AutoMigrate` 不自动删除已部署数据库中的旧 `version` 列。旧列不再映射到 Go 状态模型，仅作为无语义的遗留列保留；是否物理删除由未来独立、可审计的数据库迁移决定。

## 保存流程

```text
Role Actor mailbox
    → collect dirty modules
    → BEGIN
    → lock exact role_actor_fence(role_id, node_id, epoch)
    → GORM Save each module by role_id
    → COMMIT
    → clear dirty state
```

如果 fence 校验失败或模块写入失败，事务回滚且 dirty 状态保留，下一次保存仍可重试。

## 后果

### 正面

- `IPersistState` 接口更小，模块状态不再承载与业务无关的版本维护。
- 保存路径不再区分 `version == 0`、版本递增、版本冲突和内存版本回滚。
- ownership fencing 与模块持久化职责分离：`role_actor_fence` 负责 writer 资格，Actor mailbox 负责正常串行化。
- 每次 Role 保存仍只锁定一行 fence，不会因为不同 Role 并发保存而互相阻塞。

### 风险与约束

- 同一 `role_id` 如果出现两个相同 `node_id + epoch` 的 writer，二者都可能通过 fence，数据库将按最后写入者生效，可能产生静默覆盖。
- 因此 Activator 的本地重复激活检测、Claim-before-Spawn 和 fail-closed 行为是必要约束。
- 任何绕过 Role Actor 直接写模块表的代码都不受 Actor mailbox 保护，新增旁路写入必须另行设计并发控制。
- 已有数据库可能保留未使用的 `version` 列；这不影响运行，但不会被本次变更自动清理。

## 拒绝的方案

### 保留模块 Version 乐观锁

防御能力更强，但在当前 single-writer Actor + PostgreSQL epoch fence 模型下属于第二道保险，带来接口、状态和保存分支复杂度。当前选择以更小的持久化接口换取更清晰的 ownership 约束。

### 用模块 Version 替代 `role_actor_fence`

Version 只能检测部分旧快照覆盖，不能保证旧 epoch 在 ownership 转移后没有写权限；它不能替代 PostgreSQL exact owner fence。

### 本次自动删除旧数据库列

`AutoMigrate` 不提供安全的列删除迁移语义，直接删除可能影响回滚和历史部署。本次只移除运行时映射，物理清理另行执行。
