# Codebase Structure

## 目录总览

```
gserver/
├── node/                    # 进程入口
│   └── main.go              # main 函数，启动 Node
├── core/                    # 框架核心层
│   ├── gxyactor/            # Actor 系统
│   │   ├── actor.go         # ActorBase, PID 类型, 消息API
│   │   ├── system.go        # ActorApp, ActorSystem 初始化
│   │   ├── activator_manager.go # activatorManager, actorActivator, activatorRouter
│   │   ├── actor_timer.go   # ActorTimer (Tick/Once/Cron)
│   │   ├── actor_mgr.go     # ActorMgr (sync.Map PID 集合)
│   │   ├── helper.go        # 全局函数 (Send, Call, Spawn 等)
│   │   ├── types.go         # ActorService 类型
│   │   └── logger.go        # protoactor 日志适配器
│   ├── gxymodule/           # 模块系统
│   ├── gxynet/              # 网络层 (gnet v2, LTPV codec)
│   ├── gxypgx/              # PostgreSQL 封装 (GORM)
│   ├── gxyredis/            # Redis 封装
│   ├── gxytimer/            # 定时器
│   ├── gxylocator/          # 分布式定位器 (Lua 脚本)
│   │   ├── gxylocator.go    # Locator 实现
│   │   ├── script.go        # Lua 脚本加载
│   │   └── script/
│   │       └── locate.lua   # Redis Lua 脚本
│   ├── gxyservice/          # 服务注册/发现
│   ├── gxyregistery/        # 注册中心实现 (Consul/etcd)
│   ├── gxyhttp/             # HTTP 服务
│   ├── gxylog/              # 日志系统
│   ├── gxyutil/             # 通用工具
│   │   ├── msg_handler.go   # 消息路由处理器
│   │   ├── reflect.go       # 反射工具 (GetObjectHash, NewObject)
│   │   └── common.go        # 通用工具函数
│   ├── gxyapp/              # 应用基类
│   │   └── app.go           # IApp 接口, App 基类
│   ├── gxynode/             # 节点模块
│   │   └── node.go          # Node, App 注册, 依赖加载
│   └── gxymq/               # 消息队列 (Redis Pub/Sub / Pulsar)
├── src/apps/                    # 业务应用层
│   ├── gateway/             # 网关应用
│   │   ├── gate_app.go      # 网关 App 定义
│   │   ├── gate_handler.go  # 连接事件处理
│   │   └── internal/logic/  # 会话管理
│   │       ├── session.go   # Session Actor
│   │       └── session_mgr.go # 会话管理器
│   ├── role/                # 角色应用
│   │   ├── role_app.go      # 角色 App + Schema 初始化
│   │   ├── role_service.go  # 角色 ActorService 定义
│   │   └── internal/logic/  # 角色业务逻辑
│   │       ├── role_main.go      # RoleMain Actor (核心)
│   │       ├── role_module.go    # 模块接口 + PersistState 基类
│   │       ├── role_account.go   # 账号管理
│   │       ├── role_basic.go     # 基础信息模块
│   │       ├── role_bag.go       # 背包模块
│   │       ├── role_public.go    # 公开信息模块
│   │       ├── role_extra.go     # 扩展数据模块
│   │       ├── role_schema.go    # 数据库表结构迁移
│   │       ├── const.go          # 常量定义
│   │       ├── bag/              # 背包子模块
│   │       │   └── item.go       # 物品定义
│   │       └── event/            # 事件系统
│   │           ├── event.go      # 事件定义
│   │           └── eventbus.go   # 事件总线实现
│   └── api/                 # API 应用 (预留)
├── protocol/                # 协议定义 (git submodule)
│   └── pb/                  # Protobuf 生成代码
│       ├── message.go       # 消息类型常量
│       ├── login.pb.go      # 登录协议
│       ├── role.pb.go       # 角色协议
│       ├── bag.pb.go        # 背包协议
│       ├── basic.pb.go      # 基础协议
│       ├── ack.pb.go        # 响应协议
│       └── gactor.pb.go     # Actor 通信协议
├── src/lib/                     # 业务公共库
│   └── actor.go             # Actor 辅助函数 (ActivateRole)
├── gameconfig/              # 游戏配置表 (git submodule)
│   ├── gameconfig.go        # 配置加载器
│   └── gosrc/               # Excel 导出的配置结构体
│       ├── Tables.go
│       ├── item.*.go        # 物品配置
│       ├── global.*.go      # 全局配置
│       └── controller.*.go  # 时间控制器配置
├── config/                  # 配置文件目录 (TOML)
├── hack/                    # 开发工具脚本
├── log/                     # 日志目录
├── .vscode/                 # VS Code 配置
├── go.mod                   # Go 模块定义
├── go.sum                   # 依赖校验
├── Makefile                 # 构建脚本
└── test.go                  # 测试入口
```

## 关键文件说明

### 入口文件
- `node/main.go` — 程序入口，解析命令行参数，创建 Node，启动模块树

### 框架核心
- `core/gxyactor/actor.go` — Actor 系统 API 入口（Send, Call, ActivateActor, Spawn 等）
- `core/gxyactor/system.go` — ActorApp 定义，protoactor-go ActorSystem/Remote 初始化
- `core/gxyactor/activator_manager.go` — Actor 管理器，注册（SETNX）、定位、创建、销毁、路由
- `core/gxyactor/helper.go` — 全局便捷函数 (SpawnFunc, Send, Call 等)
- `core/gxylocator/gxylocator.go` — Redis 定位器，SETNX 注册、Lua 批量续约、条件注销
- `core/gxylocator/script/locate.lua` — Redis Lua 脚本（条件删除）
- `core/gxypgx/pgx.go` — PostgreSQL 封装，GORM 连接、AutoMigrate
- `core/gxymodule/module.go` — 模块系统，IModule 接口和 ModuleBase 树状结构
- `core/gxynode/node.go` — 节点入口，App 注册和递归依赖加载
- `core/gxyutil/msg_handler.go` — 基于反射的 protobuf 消息路由
- `core/gxynet/` — TCP 网络层（gnet v2）

### 业务核心
- `src/apps/role/internal/logic/role_main.go` — 角色主逻辑，最核心的业务文件
- `src/apps/role/internal/logic/role_module.go` — 角色模块接口和持久化基类
- `src/apps/gateway/internal/logic/session.go` — 会话 Actor，处理客户端连接和消息转发

### 消息路由
- `core/gxyutil/msg_handler.go` — 基于反射的 protobuf 消息路由，按消息类型名自动分发

## 命名约定

| 模式 | 含义 | 示例 |
|------|------|------|
| `gxy` 前缀 | 框架核心模块 | gxyactor, gxymodule |
| `I` 前缀 | 接口类型 | IActor, IModule, IRoleModule |
| `Base` 后缀 | 基础实现 | ActorBase, ModuleBase |
| `App` 后缀 | 应用模块 | gateApp, roleApp |
| `pb.` 前缀 | Protobuf 消息 | pb.ReqAccountLogin, pb.ServerMsg |
| `Tb` 前缀 | 配置表结构 | TbItem |
| `E` 前缀 + `Type` 后缀 | 枚举类型 | EItemType |
| `On` 前缀 | 生命周期回调 | OnModInit, OnModStart, OnCreate |
| `Req` / `Rsp` 前缀 | 请求/响应消息 | ReqAccountLogin, RspAccountLogin |
| `snake_case` 文件名 | Go 惯例 | role_main.go, activator_manager.go |

---
*Last updated: 2026-04-29*
