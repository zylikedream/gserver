# GM 系统 MVP 设计（协议方案）

> 日期: 2026-04-29
> 状态: 已批准

## 目标

通过游戏协议实现 GM 命令系统，用于开发测试。客户端发送 `ReqGMCommand` 协议，服务端通过反射路由到对应命令方法。命令注释即帮助文档，通过 `go/doc` 自动提取。

## 范围

**做：**
- Proto 消息定义（ReqGMCommand / ReqGMHelp）
- GM 命令定义模块（`gm` 包），方法注释作为帮助文本
- `go/doc` 首次调用时提取注释生成帮助
- 反射路由命令字符串到方法调用
- RoleGM 模块接收 proto 消息，委托 gm 包执行

**不做：**
- 鉴权（MVP 不限制，任何登录用户都能调用）
- GM 命令权限分级
- HTTP 接口

## 架构

```
客户端发 ReqGMCommand {cmd: "add_goods 1001 10"}
  → Gateway 转发到 RoleMain
  → HandleClientMsg → MsgHandler 路由到 RoleGM.ReqGMCommand
  → gm.NewGM(r.Role).ExecCommand("add_goods", ["1001", "10"])
  → 反射查找 add_goods → GM.AddGoods(1001, 10)
  → role.Bag.AddItem(...)
  → 返回 RspGMCommand {result: "ok"}

客户端发 ReqGMHelp {}
  → RoleGM.ReqGMHelp
  → gm.GetCmdDocs() 返回 go/doc 提取的命令列表
  → 返回 RspGMHelp
```

## 文件结构

| 文件 | 操作 | 说明 |
|------|------|------|
| `protocol/client/gm.proto` | 新建 | Proto 消息定义 (22001~22099) |
| `src/apps/role/internal/gm/gm.go` | 新建 | GM 命令定义 + go/doc 帮助提取 + 反射路由 |
| `src/apps/role/internal/logic/role_gm.go` | 新建 | RoleGM 模块，ReqGMCommand/ReqGMHelp handler |
| `src/apps/role/internal/logic/role_main.go` | 修改 | roleModules 加 `GM *RoleGM` 字段 |

## 详细设计

### 1. Proto 消息 (gm.proto)

```proto
// ID: 22001~22099
syntax = "proto3";
option go_package="./pb;pb";
package galaxy.protocol;

import "msg_options.proto";

message ReqGMCommand {
    option (msg_id) = 22001;
    string cmd = 1;
}

message RspGMCommand {
    option (msg_id) = 22002;
    string result = 1;
}

message ReqGMHelp {
    option (msg_id) = 22003;
}

message RspGMHelp {
    option (msg_id) = 22004;
    repeated PCmdDoc commands = 1;
}

message PCmdDoc {
    string name = 1;
    string brief = 2;
    string usage = 3;
    string example = 4;
}
```

### 2. GM 包 (gm.go)

```go
package gm

// GM 命令处理器，持有 RoleMain 引用
type GM struct {
    role *logic.RoleMain
}

func NewGM(role *logic.RoleMain) *GM {
    return &GM{role: role}
}

// CmdDoc 命令文档
type CmdDoc struct {
    Name    string
    Brief   string
    Usage   string
    Example string
}
```

#### 帮助提取

首次调用 `GetCmdDocs()` 时用 `go/doc` 解析 `gm.go` 源文件：

1. `runtime.Caller(0)` 获取 gm.go 自身路径
2. `parser.ParseDir` 读取 AST
3. `doc.New` 提取 `GM` 类型的导出方法文档
4. 解析注释：第一行 → Brief，`用法:` → Usage，`示例:` → Example
5. 方法名驼峰转 snake_case 作为命令名

#### 命令路由

`ExecCommand("add_goods", ["1001", "10"])` 流程：
1. 从 map 查找 `"add_goods"` → reflect.Method
2. 用 `gconv` 将字符串参数转为方法参数类型
3. 反射调用 `method.Func.Call(...)`
4. 返回 error 或 nil

#### MVP 命令

```go
// AddGoods 添加物品或货币
// 用法: add_goods [物品ID] [数量]
// 示例: add_goods 1001 10
func (g *GM) AddGoods(itemID int, num uint64) error {
    return g.role.Bag.AddItem(g.ctx, []bag.Item{
        {ID: itemID, Num: num},
    })
}

// RemoveGoods 移除物品或货币
// 用法: remove_goods [物品ID] [数量]
// 示例: remove_goods 1001 5
func (g *GM) RemoveGoods(itemID int, num uint64) error {
    return g.role.Bag.DecItem(g.ctx, []bag.Item{
        {ID: itemID, Num: num},
    })
}
```

### 3. RoleGM 模块 (role_gm.go)

```go
type RoleGM struct {
    RoleModule
}

func (r *RoleGM) ReqGMCommand(ctx context.Context, req *pb.ReqGMCommand) (*pb.RspGMCommand, error) {
    parts := strings.Fields(req.Cmd)
    if len(parts) == 0 {
        return nil, errors.New("empty command")
    }
    g := gm.NewGM(r.Role)
    err := g.ExecCommand(parts[0], parts[1:])
    if err != nil {
        return &pb.RspGMCommand{Result: err.Error()}, nil
    }
    return &pb.RspGMCommand{Result: "ok"}, nil
}

func (r *RoleGM) ReqGMHelp(ctx context.Context, req *pb.ReqGMHelp) (*pb.RspGMHelp, error) {
    docs := gm.GetCmdDocs()
    cmds := make([]*pb.PCmdDoc, 0, len(docs))
    for _, d := range docs {
        cmds = append(cmds, &pb.PCmdDoc{
            Name: d.Name, Brief: d.Brief, Usage: d.Usage, Example: d.Example,
        })
    }
    return &pb.RspGMHelp{Commands: cmds}, nil
}
```

### 4. roleModules 修改

在 `role_main.go` 的 `roleModules` 结构体添加：

```go
type roleModules struct {
    Bag    *RoleBag
    Basic  *RoleBasic
    Public *RolePublic
    Extra  *RoleExtra
    GM     *RoleGM
}
```

自动被 `initRoleModules` 通过反射发现和初始化。

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 传输协议 | 游戏协议 | 复用现有 gateway→role 管道，无需独立 HTTP 服务 |
| 命令定义 | 独立 gm 包 | go/doc 只提取命令方法，不混入 handler 方法 |
| 执行上下文 | RoleModule 内 | 和 RoleBag 一致，直接访问其他模块 |
| 帮助文档 | go/doc 提取 | 注释写一处，避免冗余 |
| 命令路由 | 反射 | 和项目 MsgHandler 模式一致 |
| 鉴权 | 不做 | MVP 阶段，任何登录用户可调用 |
