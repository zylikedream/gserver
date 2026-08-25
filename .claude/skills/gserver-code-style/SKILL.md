---
name: gserver-code-style
description: GServer 项目代码风格规范——命名一致性、魔法字符串常量化、Protobuf 协议命名、Go handler 命名、proto 文件组织。当用户需要添加新协议、修改协议命名、创建新 proto 文件、review 代码风格一致性、问"表名/消息ID要不要定义常量"时触发。
---

# GServer 代码风格规范

## 1. 魔法字符串必须常量化

跨文件引用或重复 ≥2 次的字符串字面量,必须提为包级常量,禁止散落字符串:

| 场景 | 例子 | 规则 |
|---|---|---|
| 表名 | `"chat_guild_message"` | 定义在 model 文件:`const chatGuildMessageTable = "..."`,TableName()/策略/测试 SQL 断言统一引用 |
| 消息 ID | `"28020"` | 已有 proto msg_id 机制,代码侧引用走 codec meta;手写 ID 必须常量 |
| Redis key | `"gserver:locate:node:actor:role:%d"` | 常量 + fmt 模板 |
| 定时器名 | `"channel_stop"` | 常量 |

判定:review 时 grep 字符串字面量,同值出现 ≥2 处且跨文件 → 要求提常量。

## 2. 命名一致性(同域同构)

同一业务域的类型/表名命名必须同构,新类型对齐已有:

- 聊天域:表/模型 `ChatXxxMessage`(ChatPrivateMessage / ChatSystemMessage / ChatGuildMessage),表名 `chat_*_message` 前缀统一
- 频道:值对象无状态类型 `XxxChannel`(WorldChannel / GuildChannel)
- 会话:role 的 `RoleXxx` 模块(RoleBag / RoleBasic / RoleChat),持久化状态 `RoleXxxState`

判定:新增类型前,先看同域已有命名模式;grep 同目录类型清单对齐。

## 3. Protobuf 协议命名

- 消息:`Req`/`Rsp`/`Notify` 前缀 + 业务语义(ReqChatSendChannel / RspChatSendChannel / NotifyChatChannel)
- `option (msg_id)` 唯一,按业务段编号(chat 28xxx / friend 29xxx…),新增消息在 proto 文件内按 ID 排序
- 字段名 snake_case(proto 惯例),生成 Go 自动 camelCase
- 枚举值全大写下划线(GardenEChatChannelType_WORLD)

## 4. Go handler 命名

- HTTP handler 类型:业务名 + `Handler`(ChatHandler),文件内按"大厅/私聊/系统"分节
- actor 消息处理:`HandleMessage`(统一入口)+ 业务方法 `ReqXxx`/`OnXxx`(role 模块内)
- 命名空间:req/rsp 类型 `XxxReq`/`XxxRsp` 与 proto 同名(HTTP 层)

## 5. proto 文件组织

- `protocol/client/` 客户端协议、`protocol/server/` 服务端内部;同域一个 .proto(chat.proto)
- 生成代码进 `protocol/pb/`,**手改无效**(`make pb` 重新生成,会 strip omitempty)
- proto 真源在 protocol/client,改协议后 server/client 双端 PB 同步(make pb)

## 6. 引用其他规范

- 错误处理:`docs/development/error-handling.md`(cockroachdb/errors 唯一, 产生点带栈, 包装 Wrap/Wrapf, 哨兵返回 WithStack, 禁 %w 吞栈)
- 日志:`docs/development/logging.md`(统一 gxylog, 结构化字段, 错误 gxylog.Err(err) 打栈, 打印点只在最终处理处)
