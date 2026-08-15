---
name: gserver-dev
description: GServer 项目开发知识库——架构、环境、协议、构建测试命令、客户端用法、目录结构、中间件与调试方式。当用户问"项目说明"、"客户端在哪"、"如何构建/测试"、"架构是什么"、"协议格式"、"开发环境"、"怎么起服务"、"怎么连客户端"时触发。
---

# GServer 开发知识库

分布式游戏服务器(protoactor-go Actor 模型 + GoFrame v2)。源码:`/home/zyr/workspace/gserver_github`。

## 架构

```
node/main.go --config config/<name>.toml 启动一个 Node,按配置装配 apps
├─ gate     : 网关(客户端 TCP 入口 :11086, LTIV 协议, 不注册 consul)
├─ account  : 账号服务(prelogin 签发 gate_token, HTTP :18080)
├─ role     : 角色主逻辑(actor, 按 roleID 哈希路由)
├─ chat     : 聊天(频道 actor + 大厅 Lua + 消息持久化)
├─ friend   : 好友
└─ guild    : 公会
```

- `core/` 共享框架:gxyactor(actor 封装)/ gxynet(网络)/ gxyredis / gxypgx / gxymodule / gxytimer / gxylog / gxyhttp
- `src/apps/` 可部署微服务;`src/lib/` 跨 app 工具(rolelib/guildlib/gatetoken);`src/pkg/` 共享包(deps 依赖容器/gameconfig 配表)
- `protocol/` protobuf 定义(client/ + server/)+ 生成代码(pb/);**客户端协议是 submodule**(gclient_github 内)
- `gameconfig/` 策划配表(子模块)

## 环境前提

- 部署目录:`/home/zyr/workspace/gserver_github`(旧 `gserver` 已废弃)
- systemd 托管:`~/.config/systemd/user/gserver@.service`,6 个 app 各一个实例(gate/account/role/chat/friend/guild),二进制 `bin/gserver-node`(`go build -o bin/gserver-node ./node`)
- **客户端**:`/home/zyr/workspace/gclient_github`(新 hy 客户端,旧 hy_client 不兼容)
- 中间件密码:postgres/redis 均 `@zyc0131`(来自 `build/env/dev_zy.env.toml`)
- 容器(全部 Up):gserver-postgres(5432)/ gserver-redis(6379)/ gserver-consul(8500)/ gserver-prometheus(9092)/ gserver-grafana(3000)/ gserver-tempo(4317)/ gserver-loki(3100)/ gserver-promtail

## 配置机制

- `config/*.toml` 是**生成文件**,由 `./build/script/svr_init.sh dev_zy` 生成——勿手改
- 改配置:改 `build/env/dev_zy.env.toml`(env 缺 per-app metrics 段如 `[metrics_gate]` 会导致生成 `addr = ":"` 服务起不来),重新生成
- `config/all.toml` 单节点跑多 app(dev 调试常用);各 app 独立 toml 配 systemd 托管

## 开发命令

```bash
go build ./...                    # 编译
make test                         # 全量测试
make lint                         # golangci-lint(CI 门禁, 必须 0 issues)
go test -shuffle=on -count=1 ./... # 顺序随机复验
make pb                           # 改 proto 后重新生成
go run node/main.go --config config/all.toml   # dev 单节点起 chat/role/friend/guild
go run node/main.go --config config/gate.toml  # dev 起 gate
```

## 登录协议与客户端

1. **prelogin**:客户端 POST account HTTP(:18080 本地 / :30080 k8s)拿 `gate_token`(HS256,secret/issuer 在 config `[token]` 段)
2. **连 gate**(:11086):FIRST_PACKET(ReqHandShake{gate_token})→ 服务端激活 role actor → RspHandShake
3. **数据消息**:DATA_PACKET,LTIV 包格式 `size(2) + type(1) + id(2) + payload(protobuf)`,id = 消息 msg_id(如 chat.send_channel=28020),byte_order little

### hy 客户端(REPL)

```bash
cd /home/zyr/workspace/gclient_github
printf '<platform_uid>\nquit\n' | ./bin/hy --account-server=http://127.0.0.1:18080 --platform=guest --client-version=1.0.0
# 判定: prelogin ok → handshake ok → login ok
```

- 所有 `Req*` 协议自动注册为 `domain.action` 命令(`help` 查看),如 `chat.init` / `chat.send_channel` / `chat.channel_history` / `bag.info`
- `bin/bench -config cmd/bench/bench.yaml` 压测 bot(YAML 定义脚本,无需改 Go)
- 参数:`--addr`(gate 地址)/ `--platform-uid` / `--client-version`

## 单元测试基建

- **禁 gomonkey**(ADR-0001):依赖注入 + 可替换函数变量(如 `verifyGateToken`/`sendClient` 包级 var)
- go-sqlmock(gorm 断言:注意 Create(map) 走 Exec+事务、Save 主键零值走 INSERT RETURNING Query、LIMIT 也是参数)
- miniredis(Lua 脚本测试)
- actor 测试模式:`fakeActx`(最小 actor.Context)+ `Receive(&actor.Started{})` 初始化 timer + TestMain 初始化全局 app
- 覆盖率:`go test -cover ./...`;chat/gateway-session/role-core 已补齐(见各包 _test.go)

## 错误/日志规范

- 唯一错误库 cockroachdb/errors(见 `docs/development/error-handling.md`):产生点自动带栈;包装用 Wrap/Wrapf;哨兵返回处 WithStack;禁 fmt.Errorf %w/Newf %w/`%s`/`%v` 吞错误
- 统一 gxylog(见 `docs/development/logging.md`):结构化字段;错误必须 `gxylog.Err(err)` 打栈;打印点只在最终处理处
- 新日志为单行 JSON(带 mod/roleID/trace_id),可用 `jq '{level, msg, mod, roleID, trace_id}'` 解析

## 调试

- dlv:yama ptrace_scope=1 挡 attach → 用 **launch 方式**(dlv 自起进程),`adapter=dlv`;玩家消息统一入口 `RoleMain.HandleClientMsg`(role_main.go:250)、`Session.handleHandshake`(gateway session.go)
- 日志:`log/<app>/<app>.log`;systemd:`journalctl --user -u gserver@<app> -f`
- 全链路自检(部署→运行→日志→trace→基础设施→调试闭环):跑 `skill://gserver-selfcheck`

## 开发流程(AGENTS.md)

- 每次开发**先拉新分支**;开发前有未提交改动先提醒提交;合入走 PR(master 分支保护)
- 架构变更先写 ADR(`docs/architecture/adr-*.md`)
- 改 proto 后 `make pb`(会 strip omitempty)
- 功能改动后闭环:单元测试(本库基建)+ 冒烟(hy 客户端)+ 全量测试 + lint 0 issues

## 业务专项文档(本地 docs/development/)

- `docs/development/chat-e2e.md` — 聊天业务 E2E(多客户端广播/好友/私聊),含环境踩坑(uid counter/k8s 冲突/hy 管道 bug)
