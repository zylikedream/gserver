# ADR 0003:GServer Monorepo(服务端、协议与工程客户端原子演进)

- 日期:2026-08-24
- 状态:**Accepted(待迁移)**

## 背景

GServer 当前由三个 Git 仓库共同完成一次可验证的功能变更:

- `gserver`:服务端、服务端生成 PB、部署与 E2E 脚本
- `gclient`:仅服务 GServer 的 Go 调试客户端 `hy`、压测工具 `bench` 和客户端运行时
- `gserver_protocol`:客户端 protobuf 源文件

这三个仓库不是独立产品,却形成了三个提交和同步单元。实际后果:

1. chat/guild 真实 E2E 大量依赖 `hy`,服务端脚本默认写死兄弟仓库路径 `$HOME/workspace/gclient_github/bin/hy`。
2. 同一协议以 submodule 分别挂载在 `gserver/protocol/client` 与 `gclient/proto/client`,再分别生成 Go PB。
3. 调查时两个协议指针已发生漂移:`gclient@7d38806`,而 `gserver@5ce2d42` 严格领先一个 handshake `gate_token` 提交。
4. 协议变更需要 protocol/server/client 多仓提交和 PR,中间状态不能独立验证。
5. gclient 没有独立 CI、tag、release 或已知外部消费者;其约 102 个提交主要演进 GServer 的 codec、连接、hy 和 bench。
6. 服务端 CI 只构建、测试和 lint 服务端,无法证明当前 hy 与当前 server 使用同一协议。

正式 Unity/移动端客户端未来会独立开发和发布,但不改变 hy/bench 作为 GServer 工程工具的归属。

## 术语

- **LTIV**:GServer TCP传输帧,格式为Length/Type/ID/Value。
- **PB**:由protobuf源文件生成的Go代码。
- **gitlink**:Git树中指向submodule提交的mode 160000条目。
- **module seam**:由独立 `go.mod` 和依赖规则形成的编译期隔离面。
- **黑盒client**:client可见公开网络合同,但不能import或复用server实现;不表示它必须位于独立进程或Git仓库。

## 决策驱动力

- 协议、server、hy、bench 和 E2E 必须在同一 PR 原子修改和验证。
- hy 必须保持真实外部调用者,只通过 Account HTTP、Gate TCP、LTIV 和 protobuf 使用 server。
- 迁移不能无收益地移动现有服务端根目录、部署路径和约 150 个 `gserver/...` import。
- 协议必须只有一个可写真源;未来正式客户端需要版本化产物,而不是第二个可写 Git 真源。
- 干净 clone 应能通过根命令构建、测试和运行 E2E,不依赖固定的兄弟仓库布局。
- gclient 和 protocol 的演进历史需要继续可追溯。

## 决策

### 1. 仓库产品边界

`gserver` 成为完整的服务端工程 monorepo,包含:

- 服务端运行时
- 客户端与服务端内部协议
- `hy` 调试客户端
- `bench` 压测工具
- 构建、部署、E2E 和运维工具

正式游戏客户端保持独立仓库,按 GServer Release 消费客户端协议产物。

### 2. 目录与 Go module

服务端保持仓库根目录不动;原 gclient 导入 `client/`:

```text
gserver/
├── go.mod                    # module gserver
├── go.work
├── core/ src/ node/ ...      # 现有服务端路径不动
├── protocol/
│   ├── client/               # client proto 唯一可写真源
│   ├── server/               # 服务端内部 proto
│   └── pb/                   # server 生成代码
├── client/
│   ├── go.mod                # module gserver/client
│   ├── cmd/hy/
│   ├── cmd/bench/
│   ├── pkg/client/
│   └── pb/                   # client 生成代码
└── bin/
    ├── gserver-node
    ├── hy
    └── bench
```

采用两个 Go module + 根 `go.work`,而不是单 module。Git 仓库提供原子变更;Go module 提供依赖 seam。

### 3. client 黑盒 seam

`client` 只能通过公开网络协议使用 server。CI 必须禁止:

- `client/go.mod` require 根 module `gserver`
- `client/go.mod` 中任何 `replace` directive
- client Go文件import任意 `gserver/...`,唯一放行为 `gserver/client/...`

采用“拒绝全部、精确放行client自身”的规则,而不是枚举当前server目录。这样未来新增的server package也默认不可见。此约束防止hy复用被测实现,避免server/client同时继承同一个内部bug后E2E仍然通过。

### 4. 协议真源与生成代码

- `protocol/client/*.proto` 是客户端协议唯一可写真源。
- `protocol/server/*.proto` 只包含服务端内部 Actor/服务消息。
- `protocol/pb/` 与 `client/pb/` 都进入 Git,都从同一客户端协议源生成。
- 两份生成包不合并:`protocol/pb` 同时包含服务端内部消息;若 client 共享该包,hy 的 protobuf 反射 registry 会看到不应暴露的内部消息。
- 固定 `protoc` 与 `protoc-gen-go` 版本。迁移以现有生成头为基线:`protoc 3.19.3`、`protoc-gen-go v1.36.11`。
- 根 `make pb-check` 重新生成两份 PB,并要求 `git status --porcelain -- protocol/pb client/pb` 为空(同时捕获已跟踪 diff与新生成的未跟踪文件)。

### 5. 根工程接口

根 Makefile 是唯一主工程接口:

```bash
make build      # gserver-node + hy + bench
make test       # server + client
make lint       # server + client
make tools      # 安装固定生成工具
make pb         # 生成 server/client PB
make pb-check   # 重新生成并检查无漂移
make e2e        # 构建 hy 并运行真实 E2E 编排
```

产物统一写入根 `bin/`。`client/Makefile` 可以保留为薄入口,但不得定义与根入口不同的流程。

### 6. CI 与 E2E

普通 PR 的阻塞门禁:

- server build/test/lint
- client build/test/lint
- client 黑盒依赖检查
- PB 生成漂移检查

真实 E2E 初期通过 `workflow_dispatch`、nightly 和本地 `make e2e` 运行。证明稳定后,再单独决定是否升级为普通 PR 阻塞门禁。

### 7. 协议兼容与发布

正式客户端接入前允许破坏性协议修改,但必须:

- proto/server/hy/bench 在同一 PR 原子修改
- 已发布 tag 永不篡改
- PR/CHANGELOG 明确标记 breaking protocol change

客户端协议随 GServer Release 发布,例如:

```text
gserver v0.8.0
└── protocol-client-v0.8.0.tar.gz
    └── SHA256
```

当前不建立独立协议版本。正式客户端开始开发前,另行制定向后兼容窗口。

协议产物必须从精确GServer tag构建,archive根目录为 `protocol-client-vX.Y.Z/`,且只包含 `protocol/client/*.proto`。归档使用稳定文件顺序、tag提交时间和固定owner元数据生成;Release上传后必须重新下载并验证SHA256。

### 8. 历史与旧仓库

- “完整历史”定义为:冻结时默认分支master的全部ancestry,以及所有tag可达提交。
- 未合branch/PR必须在冻结前逐项决定为合并、cherry-pick或明确放弃;放弃分支只留在归档旧仓,不冒充已接受产品历史。
- gclient的master历史重写到 `client/`;gserver_protocol的master历史重写到 `protocol/client/`。
- 原tag如存在,以 `legacy-gclient/`、`legacy-protocol/` 命名空间导入,避免污染GServer tag。
- 历史迁移会重写commit hash,但保留提交内容、作者、时间和路径级blame。
- 迁移完成后,gclient与gserver_protocol README指向monorepo新路径,随后设为GitHub只读归档。
- 禁止镜像、双向同步和继续在旧仓直接提交。

### 9. 切换策略

使用一个迁移PR完成干净切换,内部按可审查commit分段。迁移期间冻结gclient与gserver_protocol直接提交。该PR必须使用GitHub **Create a merge commit** 合入;禁止squash/rebase merge,否则导入的unrelated histories不会进入master ancestry。合入前再次校验两个旧仓冻结SHA未变化。只有以下闭环状态才能合入master:
- 两段历史已导入
- 旧 protocol gitlink/submodule 已删除
- 双 module/go.work/root Makefile 已建立
- 生成工具和 PB 漂移门禁已建立
- E2E 默认使用根 `bin/hy`
- build/test/lint/pb-check/E2E 全部通过

不提供旧兄弟仓库路径的 symlink 或同步脚本。`HY=/custom/path/hy` 仍作为显式覆盖入口。

## 架构不变量

迁移后必须长期保持:

1. `protocol/client` 只有一个可写真源。
2. client 不 import server 实现。
3. 根 `make test` 和 `make lint` 覆盖两个 module。
4. 协议修改不能只更新一侧 PB。
5. hy/bench 使用当前 PR 内的协议和生成代码。
6. 旧 gclient/gserver_protocol 仓库不再接受功能修改。
7. 正式游戏客户端不进入本 monorepo。

## 被拒方案

| 方案 | 拒绝理由 |
|---|---|
| 保持三仓 + submodule | 已出现协议指针漂移;一个逻辑变更无法原子提交和验证;E2E 依赖兄弟仓库路径 |
| `server/` + `client/` 对称搬迁 | 仅获得目录审美收益,却要修改部署、Docker context、systemd、配置、文档和全部根命令 |
| 单 Go module | 缺少编译器级 client/server seam,hy 容易复用被测实现,削弱 E2E 真实性 |
| 第三个共享 Go protocol module | 当前 client/server proto 同 protobuf package;会让 client registry 看到内部 Actor 消息,并扩大迁移范围 |
| PB 全部不进 Git | 干净 clone 必须先具备完全一致的 protoc 工具链;降低可构建性 |
| server PB 进 Git、client PB 不进 Git | 同一协议采用两套不一致的生成政策 |
| 协议继续独立可写并做 submodule | 重新引入多 PR、中间同步窗口和双真源 |
| monorepo 向旧 gclient/protocol 仓库持续镜像 | 镜像地址容易被误认为可写真源,长期维护两套入口 |
| 立即把真实 E2E 设为所有 PR 阻塞门禁 | 当前依赖 PostgreSQL/Redis/Consul/多节点与时序;先用 nightly 证明稳定性 |
| 现在建立独立 protocol 版本 | 正式客户端兼容周期尚不存在,提前支付版本与兼容矩阵成本 |

## 后果

**正面**

- 协议、server、hy、bench、E2E 在一个 PR 原子演进。
- `make e2e` 不再依赖固定兄弟仓库,干净 clone 即可构建 hy。
- CI 能证明当前 server 与当前客户端工具使用同一协议。
- 双 module 保持 client 依赖小且独立,并保护真实外部调用 seam。
- gclient/protocol 的原始演进历史继续可追溯。
- 未来正式客户端获得不可变、可校验的版本化协议产物。

**代价与风险**

- 根 CI/Makefile 需要显式编排两个 module;根 `go test ./...` 不会自动进入嵌套 module。
- PB 生成代码保留两份,以换取 client/server runtime 隔离。
- 单次迁移 PR 较大,必须按历史导入、协议、module、CI、E2E 分 commit 审查。
- `git filter-repo` 会重写导入仓库 commit hash;旧 hash 仍可在归档仓库查询。
- 固定生成工具增加 bootstrap 维护责任。
- 正式客户端出现后,必须补充协议兼容 ADR,不能永久沿用当前 breaking-change 政策。

## 相关

- client 调试与压测实现:原 `zylikedream/gclient`
- client 协议历史:原 `zylikedream/gserver_protocol`
- 真实 E2E:`build/script/e2e_chat.sh`、`build/script/e2e_guild.sh`
- 工程知识:`.claude/skills/gserver-dev/SKILL.md`
