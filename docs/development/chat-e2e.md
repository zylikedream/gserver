# 聊天业务 E2E 测试指南

多客户端真实联调：世界频道广播、好友流程、私聊推送与历史。环境初始化见 [开发环境初始化](../public/svr_init.md)，登录链路见 [客户端登录接入](../public/client-login.md)。

## 1. 启动节点(3 个)

```bash
go run node/main.go --config config/all.toml     # game(chat/role/friend/guild) actor :25101
go run node/main.go --config config/gate.toml    # gate TCP :11086
go run node/main.go --config config/account.toml # account prelogin HTTP :18080
```

## 2. 环境修复(踩坑前置)

| 现象 | 根因 | 修复 |
|---|---|---|
| prelogin 报 `duplicate key idx_account_role_id` | Redis `uid.role` 计数器落后于 DB 已有 role_id | `redis-cli -a '<pwd>' set uid.role <DB 最大 role_id-100000>` |
| handshake 超时(`timeout waiting for response`) | consul 里有 k8s 集群死注册(10.244.x.x),consistent hash 路由到不可达节点 | 关 k8s(`kind delete cluster --name game-cluster`)+ `curl -X PUT http://127.0.0.1:8500/v1/catalog/deregister -d '{"Node":"<node>","ServiceID":"<id>"}'` 清 10.244 注册 |
| 新账号 role_id 冲突 | 同上 uid counter | 同上 |

## 3. 客户端(hy 管道模式, 已修复)

`promptPlatformUID` 的管道预读问题已在 monorepo `client/` 中修复：`printf 'uid\ncmd\nquit\n' | hy` 可完整执行，不需要 pty。**同 uid = 同账号**（换 uid 就是新号，重连验证必须保持 uid 一致）。

```bash
# 管道模式: uid 一行 + 命令逐行
printf 'e2e_multi_001\nchat.init\nchat.send_channel 1 "hello"\nquit\n' \
  | ./bin/hy --account-server=http://127.0.0.1:18080 --platform=guest --client-version=1.0.0
```

- 自动化脚本:`build/script/e2e_chat.sh`(聊天/好友双客户端实时 push)与 `build/script/e2e_guild.sh`(公会全流程)
- 若需逐行交互调试,仍可 pty 会话(每玩家一个)

## 4. 自动化回归

```bash
bash build/script/e2e_chat.sh
```

覆盖:双客户端登录、世界实时 push、频道历史、非好友私聊拒绝、好友申请实时 push、私聊实时 push、PG 私聊记录、同 uid 重连历史。

脚本每次生成唯一 uid 和消息,**不删除任何业务数据**;只清理 hy 进程、FIFO 和临时日志。保留完整 account/role/friend_data/chat_private_message,便于失败后查库和日志。

## 5. 验证流程

### 世界频道广播(双客户端)

```
A: chat.init                    → RspChatInit {lobbyId:1}
B: chat.init                    → RspChatInit {lobbyId:1}(同大厅)
A: chat.send_channel 1 "hello from A" → RspChatSendChannel {}
B: 应收到 [push] NotifyChatChannel {senderId:<A>, content:"hello from A"}
A: chat.channel_history 1 0 10  → 历史含该消息
```

### 好友前置(私聊要求好友)

```
A: friend.send_request <B_roleID>
B: [push] NotifyFriendNewRequest
B: friend.apply_list           → incoming 含 A
B: friend.accept_request <A_roleID> → RspFriendAcceptRequest
```

### 私聊推送 + 历史

```
A: chat.send_private <B_roleID> "hi B from A" → RspChatSendPrivate {}
B: 应收到 [push] NotifyChatPrivate {sender:{roleId:<A>}, content:"hi B from A"}
B: chat.private_history <A_roleID> 10 → RspChatPrivateHistory 含该消息(PG 落库)
```

### 验证点

- 世界消息:内存 ringBuffer + 同大厅实时广播
- 私聊:好友校验(非好友返回 Ack code=1 "对方不是你的好友")→ `PublishRoleNotify` 在线推送 → PG `chat_private_message` 落库 → 历史查询
- 观察 game/gate/account 日志应无 ERROR

## 6. 清理

```bash
# 自动脚本通过 trap 自行关闭两个 hy 会话、终止残留进程并删除 FIFO
# 手工 pty 会话仍需输入 quit;停 3 个节点
```

## 备注

- 世界频道 `SaveInterval=0` 不落库,历史依赖 actor 内存存活
- 公会频道落库 `guild_chat_log`(schema 已建),启动 `loadHistory` 加载最近 500 条
- 新账号 role_id 从 `uid.role` 计数器递增(offset 100000)
