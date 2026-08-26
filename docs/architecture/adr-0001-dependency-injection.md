# ADR 0001:依赖注入取代全局单例(为可测试性)

- 日期:2026-08-13
- 状态:**Accepted(执行中)** — PR 1(基础设施 + RoleMail 样板)已完成,铺开其余 20 文件

## 背景

业务代码通过全局单例硬编码访问基础设施:

```go
// 37 个业务文件、约 100+ 调用点
gxypgx.DB().WithContext(ctx).Table("mail").Find(...)
gxyredis.Redis().Get(ctx, key).Result()
gameconfig.Get().TbItem.Get(id)
```

后果(覆盖率数据为证):

- `gxypgx` / `gxyredis` 自身覆盖率 0.0%(单例初始化代码不可测)
- role 的 DB 持久化路径 0 覆盖(gorm 链式调用无法打桩)
- 测试只能靠 gomonkey 运行时汇编打桩(`-gcflags=-l` 禁内联是它的代价)

## 决策

1. **弃用 gomonkey**,测试改用主流 mock:
   - 业务接口:go.uber.org/mock(mockgen 生成)
   - Redis:miniredis(内存 fake,测真实命令语义)
   - DB:go-sqlmock(断言 SQL 语句与绑定参数)
2. **新增 `src/pkg/deps.Deps` 依赖容器**(DB/Redis/Cfg),业务模块构造注入
3. **`RoleModule` 访问器带全局兜底**——渐进迁移的命门:未注入时回退全局单例,生产行为不变,文件可分批改造
4. **去"隐式全局访问",保留进程内单例本身**——连接池/redis 客户端进程内一个是合理设计,消灭的是隐式获取
5. **保留 `gxylog`/`gxymetrics` 全局**——横切关注点,函数式 API 是 Go 社区共识,强注入是过度设计
6. **不引 wire/fx 容器**——5 个 app 的规模手写组装根(模块 `OnModInit` 处组装)

目标形态:

```
组装根(node/模块启动)                测试
┌─────────────────────┐     ┌──────────────────────┐
│ Deps{DB,Redis,Cfg}  │     │ Deps{DB:sqlmock,     │
│   ↓ 注入             │     │  Redis:miniredis,   │
│ RoleMain.deps       │     │  Cfg:testTables}     │
│   ↓ 访问器           │     │   ↓ 注入              │
│ RoleModule.DB()     │     │ RoleMain.deps        │
│   ↓                  │     │                      │
│ 业务代码 r.DB()      │     │ 业务代码 r.DB()       │
└─────────────────────┘     └──────────────────────┘
```

## 被拒方案

| 方案 | 拒绝理由 |
|---|---|
| gomonkey 打桩 | 运行时汇编黑魔法,非官方机制,Go 升级/内联优化可静默失效;需 `-gcflags=-l`;race 不安全;patch 全局状态污染测试间隔离 |
| 消灭全部单例(每次新建连接) | 连接池/客户端进程内单例本就合理,消灭单例是走极端 |
| 一次性全量重构(一个 PR) | 37 文件 + 17 处 gomonkey 测试,半重构状态运行风险不可控 |
| 引入 wire/fx 容器 | 5 个 app 规模过度设计,容器本身引入学习和调试成本 |
| 可替换函数变量作为唯一手段 | account_lookup 已有先例(编译期安全,保留),但对"链式调用"依赖(gorm)无能为力,只能作为辅助 |

## 后果

**正面**

- 业务逻辑可注入 mock,持久化路径可测(role 覆盖率 37.8% → 38.9%,PR 1 已实证)
- 测试不再依赖 `-gcflags=-l`(铺开后逐步移除)
- 依赖在结构体中显式可见,架构更清晰

**代价/待办**

- 37 个业务文件逐文件改造(访问器替换,每文件一个 commit)
- `gxypgx`/`gxyredis` 全局 var 最终删除(组装根保留单例)
- 17 处 gomonkey 测试随模块改造逐步替换
- 其他 app(chat/friend/guild/account)15 文件无 RoleModule 基类,各自结构体持有依赖
- CI 补齐后,`-gcflags=-l` 从 Makefile 移除

## 相关

- 样板实现:PR「refactor(role): 依赖注入重构 RoleMail + go-sqlmock 测试样板」
- 依赖容器:`src/pkg/deps/deps.go`
- 访问器:`src/apps/role/internal/logic/role_module.go`
