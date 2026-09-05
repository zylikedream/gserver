# Core Framework Usage Guide

> 日志使用规范见 [logging.md](logging.md)(字段命名、级别、上下文注入、反模式)。

## App

App 是进程内的顶层功能单元，在 `core/gxyapp/` 定义。每个 App 是一个 Module，有自己的生命周期。

### 创建一个 App

```go
package mygame

import (
    "context"
    "gserver/core/gxyapp"
)

type myGameApp struct {
    gxyapp.App                      // 嵌入基类，获得默认生命周期实现
}

func NewMyGameApp() *myGameApp {
    return &myGameApp{}
}

func (a *myGameApp) AppName() string {
    return "mygame"                  // 在 node.apps 配置中用这个名字
}

func (a *myGameApp) OnModInit(ctx context.Context) error {
    // 在这里 AddModule、注册服务、初始化资源
    return nil
}
```

### 注册 App

在 `core/gxynode/node.go` 的 `registerApps()` 中注册：

```go
gxyapp.RegisterApp("mygame", mygame.NewMyGameApp())
```

### 配置启用

在 `config/*.toml` 的 `[node]` 段添加：

```toml
[node]
    apps = ["gate", "role", "mygame"]
```

Node 启动时，会按注册顺序初始化 App。`apps` 配置只控制业务 App，基础设施（redis/pgx/actor）默认加载。

### 完整示例：Chat App

```go
// src/apps/chat/chat_app.go
type chatApp struct {
    gxyapp.App
    host string
}

func NewChatApp(host string) *chatApp {
    return &chatApp{host: host}
}

func (a *chatApp) OnModInit(ctx context.Context) error {
    gxyregistery.RegisterServiceKind("chat", a.host)

    // 注册 Actor Kind，让其他节点可以定位到 Chat Actor
    gxyactor.RegisterActorKind("chat", func() gxyactor.IActor {
        return newChatActor()
    })

    // 注册 HTTP 服务（可选）
    gxyservice.ServiceApp().LoadService(ctx, newChatHttpService(a.host))
    return nil
}
```

---

## Service

Service 是能在 Consul 中被发现的服务单元。有三种类型：

### ActorService

用于 Actor 类型的服务（如 role、chat、friend），通过 Actor 系统的 Remote 通信：

```go
// src/apps/role/role_service.go
type roleService struct {
    gxyactor.ActorService       // 嵌入 ActorService 基类
}

func (s *roleService) ServiceName() string {
    return "role"               // Consul 中的服务名
}

func (s *roleService) Weight() int {
    return gxyactor.GetActorCount(s.ServiceName())  // 动态权重
}

func (s *roleService) OnModStart(ctx context.Context) error {
    // 注册 Actor Kind，activator 才能按需创建
    gxyactor.RegisterActorKind(s.ServiceName(), func() gxyactor.IActor {
        return logic.NewRoleMain()
    })
    return nil
}
```

### HttpService

用于提供 HTTP API 的服务：

```go
// src/apps/chat/chat_http_service.go
type chatHttpService struct {
    gxyhttp.HttpService         // 嵌入 HttpService 基类
    host string
}

func (s *chatHttpService) ServiceName() string {
    return "chat-http"
}

func (s *chatHttpService) OnModStart(ctx context.Context) error {
    port := g.Cfg().MustGet(ctx, "port.chat").Int()
    svr := gxyhttp.HttpSystem().NewHttpServer(fmt.Sprintf("%s:%d", s.host, port))
    gxyhttp.SetHandler(svr, ctx, s.ServiceName(), &ChatHandler{})
    svr.Start()
    s.Svr = svr
    return nil
}
```

### 注册 Service

在 App 的 `OnModInit` 中通过 `ServiceApp().LoadService` 注册：

```go
func (a *myApp) OnModInit(ctx context.Context) error {
    gxyservice.ServiceApp().LoadService(ctx, &myService{})
    return nil
}
```

Service 注册后会自动在 Consul 中注册，并在进程退出时反注册。

---

## RoleModule

RoleModule 是角色 Actor 内部的功能模块系统。每个模块负责一个独立领域（背包、邮件、鲜花等），共享同一个角色上下文。

### IRoleModule 接口

```go
type IRoleModule interface {
    gxymodule.IModule                         // Init → Start → Stop
    SetRole(role *RoleMain)                    // 设置角色上下文
    OnCreate(ctx context.Context)              // 建号时调用
    AfterLogin(ctx context.Context)            // 每次登录后调用
    BeforeLogout(ctx context.Context)          // 登出前调用
    PersistState() IPersistState               // 返回持久化状态对象
}
```

### 创建一个 RoleModule

```go
package logic

// 1. 定义持久化状态
type MyModuleState struct {
    RolePersistState                          // 嵌入基类，包含 RoleID/UpdateAt/dirty
    Score int64 `gorm:"column:score"`         // 业务字段
}

func (MyModuleState) TableName() string { return "role_my_module" } // 数据库表名

// 2. 定义模块
type MyModule struct {
    RoleModule                                // 嵌入基类
    state MyModuleState                       // 状态对象
}

var _ IRoleModule = (*MyModule)(nil)          // 编译期接口检查

// 3. 返回状态对象（框架通过这个指针读写 DB）, 如果返回空指针，该模块不会持久化
func (m *MyModule) PersistState() IPersistState {
    return &m.state
}

// 4. 登录后初始化（如果有缓存需要重建）
func (m *MyModule) AfterLogin(ctx context.Context) {
    // 例如邮件模块在这里刷新邮件缓存
}

// 5. 协议处理方法（框架自动路由）
func (m *MyModule) ReqMyAction(ctx context.Context, req *pb.ReqMyAction) (*pb.RspMyAction, error) {
    // 修改状态后标记脏，框架会在下一个保存周期自动写入 DB
    m.state.Score += 100
    m.state.MarkDirty()
    return &pb.RspMyAction{}, nil
}
```

### 注册 RoleModule

在 `role_main.go` 的 `roleModules` 结构体中添加字段：

```go
type roleModules struct {
    Bag           *RoleBag
    Mail          *RoleMail
    Flower        *RoleFlower
    // ...
    MyModule      *MyModule      // 新增：框架通过反射自动发现
}
```

不需要手动注册，`initRoleModules()` 用反射遍历 `roleModules` 的所有字段，自动创建实现了 `IRoleModule` 的实例。

### 生命周期

```
建号:   OnCreate → save
登录:   loadModules (DB → state) → SetRole → StartModule → AfterLogin
运行:   协议处理 → MarkDirty → 定时 TickSave → DB
登出:   save → BeforeLogout
```

---

## RolePersistState 和持久化

### 工作原理

每个 RoleModule 通过 `PersistState()` 暴露一个 `IPersistState`。框架的保存流程：

1. `dirtyRoleModules()` 遍历所有模块，检查 `IsDirty()`
2. 保存事务先锁定完全匹配的 `role_actor_fence(role_id, node_id, epoch)`
3. fence 校验失败时拒绝整次保存，保留脏标记
4. fence 校验通过后，各模块按 `role_id` 写入数据库
5. 全部写入成功后调用 `ClearDirty()`

Actor mailbox 保证同一个 Role Actor 的正常写入串行；`role_actor_fence` 防止旧 owner 在 ownership 转移后继续写入。

整个保存流程在单库事务中执行，配合 `globalRoleSaveLimiter` 控制并发（最多 16 个同时保存）。

### 何时标记脏

模块状态变化后调用 `MarkDirty()`。例子：

```go
func (m *MyModule) doSomething() {
    m.state.Score += 10
    m.state.MarkDirty()     // 通知框架需要持久化
}
```

不需要手动调用 `save`，框架的定时器（默认 600s）和关键流程（登出、定时保存）会自动处理。

### 表结构约定

每个模块一张表，表名由 `TableName()` 返回。通用字段（`RolePersistState`）：

| 字段 | 类型 | 说明 |
|------|------|------|
| role_id | bigint PK | 角色 ID |
| update_at | timestamptz | 自动更新时间 |

业务字段在子结构体中定义，可以灵活使用 JSONB：

```go
type GameProgressState struct {
    RolePersistState
    Data ProgressData `gorm:"column:data;type:jsonb;serializer:json"`
}
```
# 创建新协议
手动创建容易造成协议ID冲突，建议使用 `make newproto MOD=MyModule` 生成新协议。
`make newproto MOD=MyModule` 会根据已有协议id，自动往后顺延为新模块创建协议
---
