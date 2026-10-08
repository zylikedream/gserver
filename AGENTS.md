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
- **架构约束清单 `docs/architecture/invariants.md`**: 唯一有约束力的架构约束来源, 写 ADR 前必读。规则:
  - **ADR 只写思考过程**, 不写实现(代码/函数/字段/配置键/行数); 具体细节放零约束力的 notes 文档
  - **一个 ADR 只记一个决策**, 保持精简; 多决策合并会导致改一处被迫重开全部
  - 正文**点名本次触碰的不变量编号**; 保留/取代映射写在 `invariants.md`, 不在 ADR 重复
  - 状态为 `Proposed` 的 ADR、设计/实测文档、机制描述、被拒方案**零约束力**, 不得作为论据压设计
  - 需求 / 承载的分类由设计者决定; 审查方只提供来源事实(它当初是否为了迁就旧实现才长成这样), 不代为分类
  - **只提「不处理会导致状态损坏」且「实际可达」的问题**(三个条件同时成立: 已验证 / 后果是状态损坏而非单次操作失败 / 现实路径可达)。理论可达但代价可接受的情形: 不提出、不记录、不讨论; 已否决或已搁置的议题不得重复提出
  - 审查与讨论**只引用 `invariants.md` 或代码**; 引用 ADR 正文前必须对代码重验一遍
- **Feature branch + PR**: develop on feature branches, merge via PR
- **gofmt**: format all Go code with `gofmt -w` before committing
- **Commit style**: concise, focus on why not what (Chinese or English OK)

## Architecture

Distributed game server on the **Actor model** (ergo) + GoFrame v2.

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
| 工具 | gxylimit / gxyutil |

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
- **提交粒度: 功能开发期间用碎提交做检查点, 功能完成、验证通过后压缩为一条提交再推送/合并**。要求:
  - 碎提交只存在于本地, **不要推送中间态** —— 推送后再压缩需要 force-push, 会破坏远端历史
  - 若中途确需推送(长时间任务备份/跑 CI), 推送前先问一句, 由使用者决定是否接受后续 force-push
  - 压缩用 `git reset --soft <base>` 后重新提交, 或 `git rebase -i` 合并; 压缩后确认 `git diff <base>...HEAD` 与压缩前一致
  - 一条提交 = 一个功能, 提交信息写清为什么做、做了什么、怎么验证
- **提交并推送后必须检查 CI action 结果**: 推送后查看 GitHub Actions 对应 run, 有报错先修复(workflow 解析失败/lint/test 失败均算), 确认全绿后才可合并或继续下一步
- **review 按批次发起,不按 diff 发起**。一次 spawn review 子代理的成本(spawn + 子代理独立重建文件上下文 + 形成意见 + 写报告 + 主代理读报告 + 再改)远超10 行代码本身;按 diff 循环 review 是纯浪费。
  - **必须 review**:跨模块边界或对外契约改动;删除或削弱断言;一个阶段的最后一个提交;写不出「改一行让它红」的测试的不确定处。
  - **不要 review**:机械改动(重命名、表格合并、格式化);50 行以内且行为不变;改动已被一个会失败的测试覆盖。
  - **优先用验证替代 review**:变异测试(删掉守卫/改动分支,看测试是否真的变红)比 reviewer 更便宜且结论更硬——reviewer 只能说「这里可能有问题」,变异测试说「确实会坏」。删除/合并类改动用「脚本比对改前改后的测试函数名集合 + 全量跑」抓漏。
- **长任务(预计 >4h)按阶段切提交,不按功能切**。纯文档与收尾类步骤(改多个 README、归档)单独一个提交,与代码提交分开推;不要把文档改动和迁移代码混在同一份执行清单里按序执行——跑偏后会继续往下走。
- **错误处理规范**: 见 `docs/development/error-handling.md`(cockroachdb/errors 唯一错误库, 错误产生点带栈, 禁止 %s/%v 吞错误)
- **日志规范**: 见 `docs/development/logging.md`(统一 gxylog, 结构化字段, 错误必须 gxylog.Err(err) 打栈, 打印点只在最终处理处)
- **测试规范**: 见 `docs/development/testing.md`(重构时先怀疑测试;只写不变量/边界/裁决/拒绝四类;禁止为绿改生产代码、抄生产逻辑、断言内部调用顺序)
- **代码风格**: 见 `docs/development/coding-style.md`(Whole Value / Composed Method / 控制流单点 / 意图命名 / 非法状态不可表示 / 常量全大写下划线) — **该文件尚未创建,待补**
