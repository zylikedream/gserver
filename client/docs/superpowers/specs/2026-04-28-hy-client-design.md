# hy_client — 《我的花园世界》调试客户端设计

## 目标

为 gserver（《我的花园世界》游戏服务器）构建一个非可视化的客户端 SDK 和控制台工具，用于模拟客户端行为、调试服务器协议。核心 SDK 抽离后可复用于控制台客户端、网页客户端、压测机器人等场景。

首期交付：控制台 REPL 客户端。

## 技术选型

- **语言**: Go
- **网络**: 标准库 `net`（TCP）+ 自实现 LTIV codec
- **协议**: Protobuf，git subtree 引入 proto 源文件，项目内独立生成 Go 代码
- **配置**: 命令行参数 + TOML 配置文件（命令行覆盖配置文件）
- **REPL**: 交互式命令行，按模块分组命令

## 协议规格（与 gserver 一致）

### 封包格式 LTIV

```
[Size: 2B LE][Type: 1B][ID: 2B LE][Payload: protobuf]
```

- Size: 后续数据总长度（Type + ID + Payload）
- Type: 0=首包(握手), 1=数据包
- ID: 消息 ID（uint16）
- 最大包体: 3MB

### 通信流程

1. TCP 连接 → ReqHandShake(Type=0) → RspHandShake
2. ReqAccountLogin → RspAccountLogin
3. 业务请求 → ReqXxx → RspXxx，失败返回 Ack{code=1, path=请求ID}
4. 服务端推送 → NotifyXxx（无需请求）

### 消息 ID 分配

| 前缀 | 模块 | 范围 |
|------|------|------|
| 010 | 系统 | 01001~01099 |
| 100 | 登录 | 10001~10099 |
| 200 | 基础 | 20001~20099 |
| 210 | 背包 | 21001~21099 |
| 220 | 签到 | 22001~22099 |
| 230 | 好友 | 23001~23099 |
| 240 | 麻将 | 24001~24099 |

消息 ID 通过 proto 文件的 `option (msg_id) = XXXXX` 声明。

## 项目结构

```
hy_client/
├── proto/                    # git subtree: protocol 源文件 (.proto)
│   └── client/
│       ├── msg_options.proto
│       ├── login.proto
│       ├── basic.proto
│       ├── bag.proto
│       ├── sign.proto
│       ├── friend.proto
│       ├── mahong.proto
│       └── ...
├── pb/                       # protoc 生成的 Go 代码
│   ├── login.pb.go
│   └── ...
├── pkg/
│   └── client/               # 核心 SDK
│       ├── codec.go          # LTIV 编解码
│       ├── registry.go       # msg_id ↔ proto 类型映射注册表
│       ├── conn.go           # TCP 连接管理、读写循环
│       ├── client.go         # Client 高层 API
│       └── client_test.go
├── cmd/
│   └── hy/
│       └── main.go           # 控制台 REPL 入口
├── config/
│   └── default.toml          # 默认配置
├── go.mod
├── Makefile                  # build, proto 生成
└── CLAUDE.md
```

## 核心 SDK 设计 (`pkg/client/`)

### codec.go — LTIV 编解码

封包参数硬编码（与 gserver 一致）：
- SizeLength = 2, TypeLength = 1, IDLength = 2
- ByteOrder = LittleEndian
- MaxSize = 3MB

```
Encode(Message{Type, Path, Payload}) → []byte
  1. 写 Type(1B LE) + ID(2B LE) + Payload
  2. 计算总长度，写 Size(2B LE) + body

Decode([]byte) → (consumed int, Message, error)
  1. 读 2B Size，判断数据是否足够
  2. 解出 Type(1B) + ID(2B) + Payload
  3. 返回 Message
```

### registry.go — 消息注册表

初始化时扫描 protobuf 全局注册表，读取所有消息的 `msg_id` option，建立双向映射：

```go
RegisterMessages()                    // 自动注册所有带 msg_id 的消息
MessageByID(id string) reflect.Type   // 数字 ID → 类型
IDByMessage(msg proto.Message) string // 消息实例 → 数字 ID
NewMessageByID(id string) proto.Message // 数字 ID → 新实例
```

### conn.go — TCP 连接

封装 `net.Conn`：

```go
Connect(addr string) error            // 建立连接，启动读协程
Close() error                         // 关闭连接
Send(msg proto.Message) error         // 编码并发送
```

读协程循环：解码字节 → 根据消息 ID 查注册表创建实例 → protobuf 反序列化 → 分发给 Client 的消息处理。

### client.go — Client 高层 API

```go
type Config struct {
    Addr       string // 服务器地址
    AccountUID string // 账号
}

type Client struct { ... }

NewClient(cfg Config) *Client
Connect() error                                       // TCP 连接
Close() error
Handshake() (*RspHandShake, error)                    // 发送 ReqHandShake，等待响应
Login() (*RspAccountLogin, error)                     // 发送 ReqAccountLogin，等待响应
Send(msg proto.Message) error                         // 发送任意消息
Request(msg proto.Message, resp proto.Message) error  // 发送并等待匹配响应
OnMessage(handler func(msg proto.Message))            // 注册推送/通知回调
```

请求-响应匹配：根据消息 ID 命名规则（Req 前缀对应 Rsp 前缀，ID 相邻），Send 时记录 pending 请求，收到 Rsp 时匹配。超时 10s。

## 控制台 REPL 设计 (`cmd/hy/`)

### 启动

```bash
hy --addr=127.0.0.1:9000 --account=account001
hy --config=config/default.toml
```

启动流程：
1. 连接服务器（Connect）
2. 提示输入账号，默认值为配置中的 account（直接回车使用默认值）
3. Handshake → Login
4. 进入 REPL

### 命令格式

模块.操作，参数空格分隔。数组参数规则：
- **简单类型数组**（`repeated int64` 等）：`[]` 包裹，如 `friend.delete [1001,1002,1003]`
- **复杂对象数组**（`repeated PFriendApplyData` 等）：`[]` 包裹内联 JSON，如 `friend.apply [{"role_id":1001,"source":"search"}]`
- **非数组参数**：空格分隔

| 命令 | 对应协议 | 参数 |
|------|----------|------|
| `basic.info` | ReqBasicInfo | 无 |
| `basic.set_name 张三` | ReqBasicSetName | name |
| `basic.set_head xxx` | ReqBasicSetHead | head |
| `bag.info` | ReqBagInfo | 无 |
| `sign.info` | ReqSignInfo | 无 |
| `sign.draw` | ReqSignDraw | 无 |
| `sign.patch 1` | ReqSignPatch | patch_times |
| `sign.accum_draw 2` | ReqSignAccumDraw | stage |
| `friend.info` | ReqFriendInfo | 无 |
| `friend.apply 1001,1002` | ReqFriendApply | role_id 列表 |
| `friend.deal_apply 1001 1` | ReqFriendDealApply | role_id, deal |
| `friend.delete 1001` | ReqFriendDelete | role_id |
| `friend.send_gift 1001` | ReqFriendSendGift | role_id |
| `friend.recv_gift 1001` | ReqFriendRecvGift | role_id |
| `mahong.create_room 1 4 8 4` | ReqMahongCreateRoom | mode,turn,max_fan,max_player |
| `mahong.join_room 12345 0` | ReqMahongJoinRoom | room_id, identify |
| `mahong.operate 1 2` | ReqMahongOperate | cmd, val |
| `mahong.set_ready 1` | ReqMahongSetReady | ready |
| `reconnect` | 重连 | 无 |
| `help` | 显示帮助 | 无 |
| `quit` | 退出 | 无 |

### 命令注册机制

```go
type Command struct {
    Name   string
    Help   string
    Params []Param
    Exec   func(c *Client, args []string) error
}
```

所有命令在 init() 时注册到全局 map。新增协议只需添加一个 Command。

### 输出格式

响应以 JSON 打印：

```
hy> basic.info
← RspBasicInfo {"role_id":1001,"name":"张三","create_tm":1745800000,"head":"default"}
```

服务端推送实时打印：

```
[push] NotifyBagUpdate {"goods":[{"prop_id":1001,"pre_num":0,"num":10}]}
```

Ack 错误：

```
[ack] code=1 id=22003 reason="已签到"
```

### 配置文件 (`config/default.toml`)

```toml
[server]
addr = "127.0.0.1:9000"
account = "account001"
```

## 错误处理

- **连接断开**：REPL 打印错误，提供 `reconnect` 命令
- **Ack 错误**：打印 `[ack] code=X id=YYYY reason="xxx"`
- **请求超时**：10s 超时，打印 `timeout waiting for response`
- **命令解析错误**：打印用法提示

## 测试

- `codec_test.go`：LTIV 编解码单元测试
- `registry_test.go`：msg_id 注册和双向映射验证
- `client_test.go`：mock server 测试握手和收发

## Makefile

```makefile
make build     # go build -o bin/hy ./cmd/hy/
make proto     # protoc 生成 pb/
make test      # go test ./...
```
