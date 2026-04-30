# hy_client

《我的花园世界》调试客户端，连接 gserver 服务器，模拟客户端行为，用于调试和测试。

## 快速开始

```bash
make build                                    # 编译
./bin/hy                                      # 使用默认配置
./bin/hy --addr=127.0.0.1:10086               # 指定服务器地址
./bin/hy --config=config/default.toml         # 使用配置文件
./bin/hy --addr=127.0.0.1:10086 --account=test001  # 完整参数
```

启动后会提示输入账号（配置文件中的 account 作为默认值，直接回车使用）：

```
Account [account001]: _
```

登录成功后进入 REPL：

```
connecting to 127.0.0.1:10086...
connected.
handshake ok, role_id=1001
login ok.
Type 'help' for available commands, 'quit' to exit.
hy> _
```

## 更新协议

```bash
make proto-update    # 从远端拉取最新 proto 文件并重新生成 Go 代码
```

## 命令列表

### 基础模块 (basic)

| 命令 | 说明 | 参数 |
|------|------|------|
| `basic.info` | 查看角色基础信息 | 无 |
| `basic.set_name <name>` | 设置角色名 | name: 字符串 |
| `basic.set_head <head>` | 设置头像 | head: 字符串 |

### 背包模块 (bag)

| 命令 | 说明 | 参数 |
|------|------|------|
| `bag.info` | 查看背包信息 | 无 |

### 培育模块 (breed)

| 命令 | 说明 | 参数 |
|------|------|------|
| `breed.info` | 查看花朵培育信息 | 无 |
| `breed.start <flower_id>` | 开始培育 | flower_id: 数字 |
| `breed.finish <flower_id>` | 完成培育 | flower_id: 数字 |

### GM 模块 (gm)

| 命令 | 说明 | 参数 |
|------|------|------|
| `gm.cmd <command>` | 执行 GM 命令 | command: 字符串 |
| `gm.help` | 查看可用 GM 命令 | 无 |

### 系统命令

| 命令 | 说明 |
|------|------|
| `help` | 显示所有可用命令 |
| `reconnect` | 断开并重新连接服务器 |
| `quit` | 退出客户端 |

## 参数格式

### 普通参数

空格分隔，按顺序传入：

```
hy> basic.set_name 张三
hy> breed.start 101
```

### 字符串参数（含空格）

用双引号包裹，双引号本身不会被传入：

```
hy> gm.cmd "add item 1001 10"
```

服务器收到的是 `add item 1001 10`（不含双引号）。

### 数组参数

用 `[]` 包裹，逗号分隔：

```
hy> friend.delete [1001,1002,1003]
```

### 嵌套对象数组参数

用 `[]` 包裹，内部使用 JSON 格式：

```
hy> friend.apply [{"role_id":1001,"source":"search"},{"role_id":1002,"source":"recommend"}]
```

## 输出格式

所有响应以 JSON 格式输出：

```
hy> basic.info
← RspBasicInfo {"role_id":1001,"name":"张三","create_tm":1745800000,"head":"default"}
```

服务端推送消息会实时显示：

```
[push] NotifyBagUpdate {"goods":[{"prop_id":1001,"pre_num":0,"num":10}]}
```

错误响应：

```
[ack] code=1 id=22003 reason="已签到"
```

## 配置文件

`config/default.toml`：

```toml
[server]
addr = "127.0.0.1:10086"
account = "account001"
```

命令行参数会覆盖配置文件中的值。

## 项目结构

```
hy_client/
├── pkg/client/       # 核心 SDK（连接、编解码、消息注册）
├── cmd/hy/           # REPL 控制台前端
├── pb/               # 生成的 protobuf Go 代码
├── proto/client/     # proto 源文件（git subtree）
├── config/           # 配置文件
└── docs/             # 设计文档
```
