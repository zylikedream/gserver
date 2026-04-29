# Module System (模块系统)

## 概述

`gxymodule` 是整个项目的骨架系统，提供**树状组件管理**和**有序生命周期**。从进程入口到业务模块，所有组件都通过 Module 系统组织和管理。

核心设计思想：**组合优于继承** — 通过 `AddModule` 将组件组合成树，生命周期自动级联。

## 核心接口

### IModule

```go
// core/gxymodule/module.go

type IModule interface {
    GetModName() string                      // 模块名称（自动从类型名推导）
    GetModID() string                        // 模块 ID（默认与 Name 相同）
    OnModInit(ctx context.Context) error     // 初始化
    OnModStart(ctx context.Context) error    // 启动
    OnModStartAfter(ctx context.Context) error // 启动后回调（处理依赖）
    OnModStop(ctx context.Context) error     // 停止
    OnModStopBefore(ctx context.Context) error // 停止前回调
    BaseModule() *ModuleBase                 // 获取底层 ModuleBase
}
```

### ModuleBase（基类）

```go
type ModuleBase struct {
    name   string      // 模块名称（AddModule 时自动设置）
    self   IModule     // 自身接口引用
    parent IModule     // 父模块引用
    childs []IModule   // 子模块列表（有序）
}
```

**关键方法：**

| 方法 | 作用 |
|------|------|
| `AddModule(ctx, mod)` | 添加子模块，立即调用 `mod.OnModInit()` |
| `StartModule(ctx)` | 启动自身和所有子孙模块 |
| `StopModule(ctx)` | 逆序停止所有子孙和自身 |
| `GetModule(id)` | 按名称查找直接子模块 |
| `Modules()` | 获取所有直接子模块 |
| `GetParent()` | 获取父模块 |
| `BaseModule()` | 返回 `*ModuleBase` 指针（用于嵌入） |

## 生命周期

### 完整生命周期时序

```
                        时间 ──────────────────────────────────────►

AddModule 阶段（逐个添加，添加时立即 Init）:

  parent.AddModule(child1)
    │
    ├─ child1.name = "child1"        // 自动取类型名
    ├─ child1.self = child1           // 绑定自身引用
    ├─ child1.parent = parent         // 绑定父引用
    └─ child1.OnModInit(ctx)          // 立即初始化

  parent.AddModule(child2)
    │
    ├─ child2.name = "child2"
    ├─ child2.self = child2
    ├─ child2.parent = parent
    └─ child2.OnModInit(ctx)

StartModule 阶段:

  parent.StartModule(ctx)
    │
    ├─ parent.OnModStart(ctx)              // 1. 先启动自身
    │
    ├─ child1.StartModule(ctx)             // 2. 深度优先启动子树
    │   ├─ child1.OnModStart(ctx)
    │   ├─ child1-childA.StartModule(ctx)  //   递归
    │   └─ child1-childB.StartModule(ctx)
    │
    ├─ child2.StartModule(ctx)
    │   ├─ child2.OnModStart(ctx)
    │   └─ ...
    │
    ├─ child1.OnModStartAfter(ctx)         // 3. 所有子树启动后回调
    └─ child2.OnModStartAfter(ctx)

StopModule 阶段:

  parent.StopModule(ctx)
    │
    ├─ child2.StopModule(ctx)              // 1. 逆序停止子树
    │   ├─ child2-childB.StopModule(ctx)
    │   ├─ child2-childA.StopModule(ctx)
    │   └─ child2.OnModStop(ctx)
    │
    ├─ child1.StopModule(ctx)
    │   └─ ...
    │
    ├─ parent.OnModStop(ctx)               // 2. 最后停止自身
    │
    ├─ parent.self = nil                   // 3. 清理引用
    ├─ parent.parent = nil
    └─ parent.childs = nil
```

### 生命周期钩子详解

| 钩子 | 调用时机 | 典型用途 |
|------|---------|---------|
| `OnModInit` | `AddModule` 时立即调用 | 初始化内部状态、创建依赖对象 |
| `OnModStart` | `StartModule` 时，先自身后子树 | 启动服务、注册回调、开始监听 |
| `OnModStartAfter` | 所有子树启动完成后回调 | **依赖其他模块的场景**（如确保子模块已就绪） |
| `OnModStopBefore` | 停止前回调 | 保存状态、通知依赖方 |
| `OnModStop` | 子树停止后回调 | 释放资源、关闭连接 |

## App 层（应用模块）

`gxyapp.App` 在 `IModule` 基础上增加了应用级别的概念：

```go
// core/gxyapp/app.go

type IApp interface {
    gxymodule.IModule       // 继承模块接口
    AppName() string        // 应用名称
    SetAppName(name string)
}

type App struct {
    gxymodule.ModuleBase    // 嵌入模块基类
    appName string
    deps    []string
}
```

### App 注册表

```go
var apps map[string]IApp  // 全局 App 注册表

func RegisterApp(name string, app IApp)  // 注册 App
func GetApp(appName string) IApp         // 获取 App
```

所有 App 在 `gxynode.node.registerApps()` 中集中注册：

```go
// core/gxynode/node.go

func (n *node) registerApps() {
    gxyapp.RegisterApp("redis", gxyredis.NewRedisApp())
    gxyapp.RegisterApp("pgx", gxypgx.NewPGXApp())
    gxyapp.RegisterApp("mq", gxymq.NewMessageQueueApp())
    gxyapp.RegisterApp("actor", gxyactor.NewActorApp(n.Name, n.Host))
    gxyapp.RegisterApp("http", gxyhttp.NewHttpApp(n.Name, n.Host))
    gxyapp.RegisterApp("service", gxyservice.NewServiceApp(n.Name))
    gxyapp.RegisterApp("role", role.NewRoleApp())
    gxyapp.RegisterApp("gate", gateway.NewGateApp())
}
```

### App 加载流程

```go
// core/gxynode/node.go — OnModStart

func (n *node) OnModStart(ctx context.Context) error {
    // 1. 预加载基础设施依赖（固定顺序，保证先于业务 App）
    deps := []string{"redis", "pgx", "actor", "service"}
    for _, dep := range deps {
        n.loadApp(ctx, dep, loaded)
    }

    // 2. 根据配置加载业务 App
    //    配置示例: node.apps = ["role", "gate"]
    for _, appName := range n.apps {
        n.loadApp(ctx, appName, loaded)
    }
    return nil
}

// loadApp 递归加载 app 及其依赖，通过 loaded map 防止重复加载
func (n *node) loadApp(ctx context.Context, appName string, loaded map[string]bool) error {
    if loaded[appName] {
        return nil
    }
    app := gxyapp.GetApp(appName)
    loaded[appName] = true
    return n.AddModule(ctx, app)
}
```

## 实际使用示例

### 示例 1: Node 启动（根模块）

```go
// node/main.go

func main() {
    rootModule := gxymodule.ModuleBase{}  // 根模块

    node := gxynode.NewNode("config.toml")
    rootModule.AddModule(ctx, node)       // Init node

    rootModule.StartModule(ctx)           // Start 整棵模块树
    // ... 运行 ...
    rootModule.StopModule(ctx)            // Stop 整棵模块树
}
```

### 示例 2: ActorApp 添加子模块

```go
// core/gxyactor/system.go

func (a *actorApp) OnModInit(ctx context.Context) error {
    a.system = actor.NewActorSystem(...)
    a.remote = remote.NewRemote(a.system, config)
    a.remote.Start()
    a.activatorMgr = NewActivatorManager()
    a.AddModule(ctx, a.activatorMgr)  // activatorManager 作为子模块
    return nil
}
```

### 示例 3: Gateway App 添加网络模块

```go
// src/apps/gateway/gate_app.go

func (s *gateApp) OnModInit(ctx context.Context) error {
    network := gxynet.NewNetwork(g.Cfg(), NewGateHandler())
    s.AddModule(ctx, network)  // 网络模块作为子模块
    logic.NewSessionMgr()
    return nil
}
```

### 示例 4: RoleMain 的模块化（最复杂的用法）

RoleMain 同时是 Actor 和 Module 容器，内嵌了多个业务子模块：

```go
// src/apps/role/internal/logic/role_main.go

type RoleMain struct {
    gxymodule.ModuleBase       // 继承模块基类（作为容器）
    roleModules                // 内嵌子模块集合
    *gxyactor.ActorBase        // 继承 Actor 基类
    RoleID     int64
    session    gxyactor.PID
    state      RoleState
    eventBus   event.IEventBus
}

// 子模块集合 — 通过反射自动发现
type roleModules struct {
    Bag    *RoleBag       // 背包
    Basic  *RoleBasic     // 基础信息
    Public *RolePublic    // 公开信息
    Extra  *RoleExtra     // 扩展数据
}

// 通过反射自动初始化所有实现了 IRoleModule 的字段
func (r *RoleMain) initRoleModules(ctx context.Context) {
    modules := &r.roleModules
    t := gxyutil.TypeReal(modules)
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        if !field.IsExported() {
            continue
        }
        if field.Type.Kind() != reflect.Ptr {
            continue
        }
        if !field.Type.Implements(reflect.TypeFor[IRoleModule]()) {
            continue
        }
        rmod := gxyutil.NewObject(field.Type.Elem())
        reflect.ValueOf(modules).Elem().Field(i).Set(reflect.ValueOf(rmod))
        r.AddModule(ctx, rmod.(IRoleModule))
    }
}
```

### 示例 5: IRoleModule 接口（业务模块标准）

```go
// src/apps/role/internal/logic/role_module.go

type IRoleModule interface {
    gxymodule.IModule          // 继承模块接口
    SetRole(role *RoleMain)    // 绑定所属角色
    OnCreate(ctx context.Context)  // 建号回调
    PersistState() IPersistState  // 返回持久化状态
}

// 持久化状态接口
type IPersistState interface {
    SetRoleID(roleID int64)
    GetUpdateAt() time.Time
    SetUpdateAt(updateAt time.Time)
    GetIndexes() []string
    MarkDirty()
    IsDirty() bool
    ClearDirty()
}

// 基类
type RolePersistState struct {
    RoleID   int64     `gorm:"column:role_id;primaryKey"`
    UpdateAt time.Time `gorm:"column:update_at;autoUpdateTime"`
    dirty    bool
}

// 具体实现示例
type RoleBasic struct {
    RoleModule              // 嵌入模块基类
    RoleBasicState          // 嵌入持久化状态
}

var _ IRoleModule = (*RoleBasic)(nil)  // 编译期接口检查

func (r *RoleBasic) PersistState() IPersistState {
    return &r.RoleBasicState
}
```

## 模块树完整实例

以一个加载了 `role` 和 `gate` 的节点为例：

```
rootModule (ModuleBase)
 └─ node (gxynode.node)
     ├─ redisApp (gxyredis.redisApp)
     ├─ pgxApp (gxypgx.PGXApp) — GORM
     ├─ actorApp (gxyactor.actorApp)
     │   └─ activatorManager (gxyactor.activatorManager)
     │       └─ (动态创建的 activatorRouter + actorActivator actors)
     │
     ├─ httpApp (gxyhttp.httpApp)
     │
     ├─ serviceApp (gxyservice.serviceApp)
     │   └─ roleService (role.roleService)
     │       └─ (注册的 Actor: "role" → RoleMain)
     │
     ├─ roleApp (role.roleApp)
     │   └─ schema migration (GORM AutoMigrate)
     │
     ├─ gateApp (gateway.gateApp)
     │   └─ network (gxynet.network)
     │
     └─ mqApp (gxymq.messageQueueApp)
```

每个 RoleMain Actor 内部也有自己的模块树：

```
RoleMain (ActorBase + ModuleBase)
 ├─ RoleBag (RoleModule)
 │   └─ RoleBagState (PersistState) — table: role_bag
 ├─ RoleBasic (RoleModule)
 │   └─ RoleBasicState (PersistState) — table: role_basic
 ├─ RolePublic (RoleModule)
 │   └─ RolePublicState (PersistState) — table: role_public
 └─ RoleExtra (RoleModule)
     └─ RoleExtraPersistState (PersistState) — table: role_extra
```

## 设计模式总结

### 模式 1: 组合树
- 所有组件通过 `AddModule` 组合成树
- 生命周期自动级联传播
- 父模块通过 `Modules()` 访问子模块

### 模式 2: 嵌入继承
- `struct` 嵌入 `ModuleBase` 获得模块能力
- 可选择性覆盖生命周期方法
- `BaseModule()` 返回嵌入的 `*ModuleBase`

### 模式 3: 自动发现
- RoleMain 通过反射自动发现 `roleModules` 中的 `IRoleModule` 字段
- 新增模块只需在 `roleModules` 结构体中添加字段，无需修改初始化代码

### 模式 4: 双重身份
- RoleMain 同时是 **Actor** 和 **Module**（容器）
- 作为 Actor 接收消息、处理业务
- 作为 Module 管理子模块的生命周期和数据加载

### 模式 5: 配置驱动加载
- Node 通过配置文件决定加载哪些 App
- 不同配置产生不同类型的节点（网关节点、角色节点）
- 基础设施 App（Redis、PGX、Actor、Service）总是预加载

---
*Last updated: 2026-04-29*
