# Coding Conventions

## 项目级约定

### 框架与业务分离
- `core/` 目录只放框架通用代码，不依赖任何业务模块
- `apps/` 目录放业务代码，可依赖 core 但不能互相依赖
- `lib/` 放业务公共工具（如 `GetRoleGrain`），可被多个 app 引用

### 模块化设计
- 所有组件都实现 `IModule` 接口，通过 `ModuleBase` 管理生命周期
- 组件之间通过 `AddModule` 组合成树状结构
- 初始化顺序：Init(父→子) → Start(父→子) → StartAfter(子→父)
- 停止顺序：Stop(子→父)

### Actor 模式
- 业务实体建模为 Grain（虚拟 Actor），通过 `GrainBase` 继承
- 工具/管理类建模为普通 Actor，通过 `ActorBase` 继承
- 每个 Actor 的消息处理方法签名：`HandleXxx(ctx context.Context, msg *pb.XxxMsg) (proto.Message, error)`
- 消息路由通过 `MsgHandler` 基于反射自动完成，不需要手动 switch-case

## 代码风格

### 错误处理
- 使用 GoFrame 的 `gerror.New` / `gerror.Wrapf` 创建错误（带堆栈）
- 使用 `gutil.TryCatch` 包裹 Actor 的 Receive 方法，防止 panic 导致 Actor 崩溃
- 错误日志使用 `glog.Errorf`，带上下文信息

### 日志规范
- Debug: 消息收发详情、定时器触发、保存操作
- Info: 连接建立/断开、登录/登出、模块启动/停止
- Error: 消息处理失败、数据库操作失败、定时器异常
- 通过 `SetLogValue` 在 context 中附加 roleID 等上下文

### 命名规范
- 接口：`I` 前缀（`IActor`, `IGrain`, `IModule`）
- 基类：`Base` 后缀（`ActorBase`, `GrainBase`, `ModuleBase`）
- 应用：`App` 后缀（`gateApp`, `roleApp`）
- 生命周期：`On` 前缀（`OnModInit`, `OnCreate`, `Terminate`）
- 常量：全大写下划线（`SESSION_ALIVE_INTERVAL`）
- 私有方法/字段：小写开头

### 结构体标签
- PostgreSQL 列：`db:"snake_case"`
- hash 排除：`hash:"-"`（version、update_at 等不参与脏检查的字段）
- 内嵌继承：`db:"inline"`（RolePersistState 嵌入子结构体）
- 非持久化字段：`db:"-"`（ModuleBase、Role 等非持久化字段）
- Map/slice 字段自动映射到 JSONB 列

### 消息处理模式
```go
// 1. 定义 protobuf 消息 (protocol/pb/*.proto)
// 2. 在模块中实现 Handle 方法：
func (r *RoleBasic) ReqUpdateName(ctx context.Context, req *pb.ReqUpdateName) (*pb.RspUpdateName, error) {
    r.Basic.RoleName = req.Name
    return &pb.RspUpdateName{}, nil
}
// 3. 通过 MsgHandler.AddHandler(mod) 自动注册路由
// 4. 消息按 protobuf 消息类型名（ReqUpdateName）自动路由到对应方法
```

### 定时器使用
```go
// Tick: 重复间隔
r.Timer().AddTick(ctx, &gxytimer.Tick{Name: "save", Interval: 5*time.Second}, handler)

// Once: 单次延迟
r.Timer().AddOnce(ctx, &gxytimer.Once{Name: "timeout", After: 10*time.Minute}, handler)

// Cron: 定时任务（可持久化恢复）
r.Timer().AddCron(ctx, gxytimer.DayRefresh, handler)
r.Timer().SetCronState(state) // 设置可持久化的状态
r.Timer().RestoreCron(ctx)    // 启动时恢复
```

### 事件总线
```go
// 发布事件
r.eventBus.Publish(event.EventType("role_level_up"), levelData)

// 订阅事件
r.eventBus.Subscribe(event.EventType("item_add"), func(param event.EventParam) {
    // 处理事件
})
```

## 配置约定
- 配置文件通过 `--config` 命令行参数指定（TOML 格式）
- 使用 GoFrame 的 `g.Cfg()` 读取
- 节点配置：`node.name`, `node.host`, `node.apps`
- 数据库配置：PostgreSQL 连接串、Redis 地址、服务发现后端等

## 包组织约定
- 每个包一个目录
- `internal/` 子包放私有实现（如 `apps/role/internal/logic/`, `apps/gateway/internal/logic/`）
- protobuf 生成代码放在 `protocol/pb/`
- 游戏配置表放在 `gameconfig/src/`，由工具自动生成

---
*Last updated: 2026-04-22*
