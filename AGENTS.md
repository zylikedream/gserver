# Repository Guidelines

```bash
go build ./...                 # compile all packages
make test                      # run all tests
make lint                      # run golangci-lint
go test ./pkg/...              # run tests for a specific package
go run node/main.go --config config/<name>.toml  # run server with config

# Protobuf generation (always run after editing .proto files)
make pb
```

## Key Conventions

- **Plan first**: for architecture changes or new features, present a plan before writing code
- **ADR**: architecture decisions recorded in `docs/architecture/adr-*.md`; new architecture changes write an ADR before code
- **Feature branch + PR**: develop on feature branches, merge via PR
- **gofmt**: format all Go code with `gofmt -w` before committing
- **Commit style**: concise, focus on why not what (Chinese or English OK)

## Architecture

Distributed game server on the **Actor model** (protoactor-go) + GoFrame v2.

### 5-Layer Architecture

```
业务层       src/apps/       (gateway/account/role/chat/friend/guild/thanks)
业务支撑层   src/lib/        (公共业务库: actor操作/广播/Token)
     ↓
协议层       protocol/       (protobuf 定义 + 生成代码)
     ↓
基础设施层   core/           (Actor/网络/DB/缓存/注册/监控/日志/追踪)
     ↓
部署层       build/          (配置模板/部署/运维脚本)
```

### 各层详情

**基础设施层 — `core/`**

| 分组 | 模块 |
|------|------|
| 应用框架 | gxymodule(生命周期) → gxyapp(App基类) → gxynode(进程入口) + gxynodeenv |
| Actor模型 | gxyactor(激活/通信) → gxyservice(RPC框架) → gxymq(消息队列) |
| 网络通信 | gxynet(TCP) + gxyhttp(HTTP) |
| 中间件 | gxyredis / gxypgx / gxyregistery(Consul) / gxylock(分布式锁) |
| 可观测性 | gxylog / gxytrace(Tempo) / gxymetrics(Prometheus) |
| 工具 | gxytimer / gxyutil |

**协议层 — `protocol/`**

- `client/` — 子模块，客户端协议定义
- `server/` — 服务端协议定义
- `pb/` — 生成的 Go protobuf 代码

**业务层 — `src/`**

- `apps/` — 可独立部署的微服务 (gateway/account/role/chat/friend/guild/thanks)
- `lib/` — 公共业务库 (gatetoken/rolelib/guildlib/broadcast)
- `util/` — 通用工具 (ets/list/uid/time)
- `pkg/gameconfig/` — 游戏配置表管理器
- `api/` — 共享数据模型

**部署层 — `build/`**

- `env/` — 环境配置
- `template/config/` — 各模块 TOML 启动配置模板 (gen_config.py 渲染)
- `template/deploy/` — Docker Compose 部署模板
- `template/script/` — 运维脚本模板

## Gotchas

- **测试不用 gomonkey** — 依赖注入 + 可替换函数变量,详见 `docs/architecture/adr-0001-dependency-injection.md`
- **`make pb`** strips `omitempty` from generated JSON tags via sed
- **Dev infra**: redis/consul via docker, grafana/prometheus/tempo via docker compose (`deploy/docker/`)
- **Submodules**: `protocol/client` and `gameconfig` — init after clone
- **Run config**: `config/*.toml` selects which apps to start (`gate.toml`, `all.toml`, etc.)

## development tips
- **每次开发功能需要先拉取新分支来开发, 如果开发之前有未提交的更改，提醒我先提交**
- **提交并推送后必须检查 CI action 结果**: 推送后查看 GitHub Actions 对应 run, 有报错先修复(workflow 解析失败/lint/test 失败均算), 确认全绿后才可合并或继续下一步
- **错误处理规范**: 见 `docs/development/error-handling.md`(cockroachdb/errors 唯一错误库, 错误产生点带栈, 禁止 %s/%v 吞错误)
- **日志规范**: 见 `docs/development/logging.md`(统一 gxylog, 结构化字段, 错误必须 gxylog.Err(err) 打栈, 打印点只在最终处理处)
