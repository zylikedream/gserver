# GM 系统

## 概述

GM（Game Master）命令系统，通过游戏协议提供开发测试命令。命令定义在 `gm` 包中，方法注释即为帮助文档，通过 `go/doc` 自动提取。

## 协议接口

### 执行命令

消息 ID: 22001 (请求) / 22002 (响应)

```proto
message ReqGMCommand {
    string cmd = 1;  // 命令字符串，如 "add_goods 1001 10"
}

message RspGMCommand {
    string result = 1;  // 执行结果，成功为 "ok"
}
```

### 查看帮助

消息 ID: 22003 (请求) / 22004 (响应)

```proto
message ReqGMHelp {}
message RspGMHelp {
    repeated PCmdDoc commands = 1;
}
message PCmdDoc {
    string name = 1;
    string brief = 2;
    string usage = 3;
    string example = 4;
}
```

## 命令列表

| 命令 | 说明 | 用法 |
|------|------|------|
| add_goods | 添加物品或货币 | add_goods [物品ID] [数量] |
| remove_goods | 移除物品或货币 | remove_goods [物品ID] [数量] |

新增命令只需在 `gm.go` 中添加导出方法并写好注释即可，无需改动其他文件。

## 架构

```
客户端发 ReqGMCommand {cmd: "add_goods 1001 10"}
  → Gateway 转发到 RoleMain
  → HandleClientMsg → MsgHandler 路由到 RoleGM.ReqGMCommand
  → gm.NewGM(ctx, r.Role.Bag).ExecCommand("add_goods", ["1001", "10"])
  → 反射查找 add_goods → GM.AddGoods(1001, 10)
  → Bag.AddItem(...)
  → 返回 RspGMCommand {result: "ok"}
```

## 代码位置

| 文件 | 说明 |
|------|------|
| `protocol/client/gm.proto` | Proto 消息定义 |
| `src/apps/role/internal/gm/gm.go` | GM 命令定义 + 帮助提取 + 反射路由 |
| `src/apps/role/internal/logic/role_gm.go` | RoleGM 模块 |
| `src/apps/role/internal/logic/role_main.go` | roleModules 注册 |

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 传输协议 | 游戏协议 | 复用现有 gateway→role 管道 |
| 命令定义 | 独立 gm 包 | go/doc 只提取命令方法，避免循环依赖用接口解耦 |
| 执行上下文 | RoleModule 内 | 和 RoleBag 一致，直接访问其他模块 |
| 帮助文档 | go/doc 提取 | 注释写一处，避免冗余 |
| 鉴权 | 不做 | MVP 阶段 |
