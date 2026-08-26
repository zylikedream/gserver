# 公会业务 E2E 测试指南

公会全流程真实联调:建会 → 申请 → 审批(含批量部分成功)→ 入会 → 重连持久化 → 清理。脚本: `build/script/e2e_guild.sh`(进 git,可重复运行)。环境初始化见 [开发环境初始化](../public/svr_init.md)，登录链路见 [客户端登录接入](../public/client-login.md)。

## 1. 快速跑

```bash
# 前置: 3 节点运行(gate/account/game)+ postgres + redis + hy 已构建
go run node/main.go --config config/all.toml     # game(role/guild/chat/friend) actor
go run node/main.go --config config/gate.toml    # gate TCP :11086
go run node/main.go --config config/account.toml # account prelogin HTTP :18080

bash build/script/e2e_guild.sh
# 输出 7 步全 PASS: 建会→申请→审批→入会→重连→DB 断言→清理
```

环境变量：`HY`（默认使用 monorepo 根目录 `bin/hy`，即 `make build` 产物）、`ACCOUNT_URL`、`PGPASSWORD`（应与当前开发环境配置一致）。

## 2. 覆盖的流程与验证点

| 步骤 | 操作 | 断言 |
|---|---|---|
| 1. 建会 | A: `add_goods 1 10000` + `set_player_level 15` + `guild.create "<名>" "<desc>" "<icon>" 1` | 响应 `guildId` |
| 2. 申请 | B: `guild.apply <guild_id>` | `RspGuildApply` |
| 3. 批量审批 | A: `guild.approve_apply 1 0 1`(approve=true, apply_ids=[0,1]) | 无效 ID 跳过, 有效入会, `RspGuildApproveApply` 正常返回 |
| 4. 入会 | B: `guild.info` | 响应含 `guild.id` |
| 5. 重连持久化 | B 同 uid 重连 `guild.info` | 公会关系仍在(OnModStart→ReloadGuildID 从 DB 加载) |
| 6. DB 断言 | `SELECT count(*) FROM role_guild WHERE guild_id=N` | = 2(A 会长 + B 成员) |
| 7. 清理 | A: `guild.kick <B_role_id>` + `guild.disband` | role_guild 归零 |

## 3. 踩坑(全部实测)

| 坑 | 说明 |
|---|---|
| **建会消耗是道具 id=1 × 10000** | `garden_tbguildconfig.json` 的 `create_cost` 是 **id=1**(金币道具),不是 10001(花)。加错道具报"创建公会消耗不足" |
| **建会前置** | 需要 Lv15(`set_player_level 15`)+ 道具 1×10000(`add_goods 1 10000`),gm 命令要完整字符串引号包裹 |
| **同 uid = 同账号** | hy 的 platform_uid 是账号标识。**换 uid = 新号**(role_id 重新分配)。重连/持久化验证必须保持 uid 一致——排查中两次"假 bug"(重连缓存丢失、未入会)全是换了 uid |
| **need_approval 分流** | 建会时 `need_approval=0` → B 申请**直接入会**;`=1` → 走审批流(apply_list/approve_apply) |
| **approve 参数顺序** | `guild.approve_apply <approve> <apply_ids>`:role_id 被客户端过滤,repeated 吞剩余参数。`approve_apply 1 0 1` = approve=true, apply_ids=[0,1] |
| **批量部分成功** | 无效 ID 跳过继续(服务端 #17 修复);旧代码中途 return 导致成功项不通知 + 客户端误判失败 |
| **解散前置** | 有成员时 `guild.disband` 报"请先转让会长"——必须先 `guild.kick` 清空成员 |
| **hy 管道模式** | promptPlatformUID 预读 bug 已修(gclient PR #2),`printf 'uid\ncmd\nquit\n' | hy` 可用。管道模式是脚本化的前提 |

## 4. 与 chat-e2e.md 的关系

- `chat-e2e.md`:聊天域(世界广播/好友/私聊),含环境级踩坑(uid counter/k8s 死注册)
- 本文件:公会域全流程 + 脚本
- 环境级前置(uid counter 对齐、consul 死注册清理)两边通用,见 chat-e2e.md 第 2 节
