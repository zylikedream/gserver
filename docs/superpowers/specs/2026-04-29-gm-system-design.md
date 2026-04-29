# GM 系统 MVP 设计

> 日期: 2026-04-29
> 状态: 已批准

## 目标

提供 HTTP 接口的 GM（Game Master）命令系统，用于开发测试。命令定义在一个模块中，方法注释即帮助文档，通过 `go/doc` 自动提取。

## 范围

**做：**
- GM 命令定义模块，方法注释作为帮助文本
- `go/doc` 启动时自动提取注释生成帮助
- 反射路由命令字符串到方法调用
- HTTP 接口：执行命令 + 查看帮助
- 命令通过 Actor 消息在 RoleMain 上下文执行

**不做：**
- 鉴权（仅开发环境使用）
- Proto 消息定义（纯内部结构体）
- GM 命令权限分级

## 架构

```
HTTP POST /gm/cmd {role_id, cmd}
  → GMHttpHandler 解析命令
  → ActivateRole(roleID) 拿到 PID
  → Call(RolePID, GMCommand{Name, Args}, 10s)
  → RoleMain.HandleMessage 收到 GMCommand
  → GM 反射路由到对应方法（actor 上下文执行）
  → 返回结果

HTTP GET /gm/help
  → 返回 go/doc 提取的命令列表和说明
```

## 文件结构

| 文件 | 操作 | 说明 |
|------|------|------|
| `src/apps/role/internal/gm/gm.go` | 新建 | GM 命令定义 + 帮助提取 + 反射路由 |
| `src/apps/role/internal/gm/handler.go` | 新建 | HTTP handler |
| `src/apps/role/internal/logic/role_main.go` | 修改 | HandleMessage 处理 GMCommand |
| `src/apps/role/role_app.go` | 修改 | 注册 GM HTTP handler |

## 详细设计

### 1. GM 命令模块 (gm.go)

```go
package gm

// GM 命令处理器，持有 RoleMain 引用，在 actor 上下文内执行
type GM struct {
    role *logic.RoleMain
}

func NewGM(role *logic.RoleMain) *GM {
    return &GM{role: role}
}

// CmdDoc 命令文档
type CmdDoc struct {
    Name    string // 方法名（snake_case 命令名）
    Brief   string // 注释第一行
    Usage   string // "用法:" 行
    Example string // "示例:" 行
}

// AddGoods 添加物品或货币
// 用法: add_goods [物品ID] [数量]
// 示例: add_goods 1001 10
func (g *GM) AddGoods(itemID int, num uint64) error {
    return g.role.Bag.AddItem(g.role.ActorContext(), []bag.Item{
        {ID: itemID, Num: num},
    })
}

// RemoveGoods 移除物品或货币
// 用法: remove_goods [物品ID] [数量]
// 示例: remove_goods 1001 5
func (g *GM) RemoveGoods(itemID int, num uint64) error {
    return g.role.Bag.DecItem(g.role.ActorContext(), []bag.Item{
        {ID: itemID, Num: num},
    })
}
```

#### 帮助提取

启动时用 `go/doc` 解析 `gm.go` 源文件：

1. `parser.ParseFile` 读取源文件 AST
2. `doc.New` 提取类型 `GM` 的方法文档
3. 遍历导出方法，解析 doc comment：
   - 第一行 → `Brief`
   - `用法:` 开头行 → `Usage`
   - `示例:` 开头行 → `Example`
4. 方法名按驼峰转 snake_case 作为命令名
5. 构建 `map[string]CmdDoc` + `[]reflect.Method`

#### 命令路由

`ExecCommand("add_goods 1001 10")` 流程：
1. 按空格拆分为 `["add_goods", "1001", "10"]`
2. 从 map 查找 `"add_goods"` → 得到 `reflect.Method`
3. 用 `gconv` 将字符串参数转为方法参数类型（int, uint64 等）
4. 反射调用 `method.Func.Call(...)`
5. 返回 error 或 nil

### 2. HTTP Handler (handler.go)

```go
package gm

type GMCmdReq struct {
    g.Meta `path:"/gm/cmd" method:"POST"`
    RoleID int64  `json:"role_id"`
    Cmd    string `json:"cmd"`
}

type GMCmdRsp struct {
    Result string `json:"result"`
}

type GMHelpReq struct {
    g.Meta `path:"/gm/help" method:"GET"`
}

type GMHelpRsp struct {
    Commands []CmdDoc `json:"commands"`
}

type Handler struct{}

func (h *Handler) Cmd(ctx context.Context, req *GMCmdReq) (*GMCmdRsp, error) {
    // 1. ActivateRole(req.RoleID)
    // 2. Call(rolePID, GMCommand{Name, Args}, 10s timeout)
    // 3. 返回结果
}

func (h *Handler) Help(ctx context.Context, req *GMHelpReq) (*GMHelpRsp, error) {
    // 返回 go/doc 提取的命令列表
}
```

### 3. Actor 消息

```go
// GMCommand GM 命令消息（内部结构体，不走 proto）
type GMCommand struct {
    Name string
    Args []string
}

// GMResult GM 命令结果
type GMResult struct {
    Err error
}
```

RoleMain.HandleMessage 中处理：

```go
case *gm.GMCommand:
    g := gm.NewGM(r)
    err := g.ExecCommand(msg.Name, msg.Args)
    // Respond with GMResult
```

### 4. 注册 (role_app.go)

在 `OnModInit` 中注册 GM HTTP handler：

```go
gxyhttp.HttpSystem().SetHandler(ctx, "role", gm.NewHandler())
```

需要在 actor/http app start 之前注册。

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 执行上下文 | Actor 消息 | 和 Erlang call_role 一致，保证线程安全 |
| 命令定义位置 | role app 内部 | MVP 主要是角色命令，后续可按 app 扩展 |
| 帮助文档 | go/doc 提取 | 注释写一处，避免冗余，维护简单 |
| 命令路由 | 反射 | 和项目 MsgHandler 模式一致 |
| 传输协议 | HTTP JSON | curl/浏览器即可调用，开发测试最方便 |
| 鉴权 | 不做 | 仅开发环境使用 |
