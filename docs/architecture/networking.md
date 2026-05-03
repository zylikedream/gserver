# 网络层

GServer 的网络层基于自研的 `gxynet` 框架，使用 **gnet v2** 作为底层事件驱动引擎。

## 架构

```
┌────────────────────────────────────┐
│         Network Module              │
│   OnModStart → peer.Start(handler)  │
├────────────────────────────────────┤
│              Peer                  │
│     TCPServer (监听端口)           │
├────────────────────────────────────┤
│        EventHandler                │
│  OnOpen → OnMessage → OnClose     │
├────────────────────────────────────┤
│         Endpoint                  │
│  每个连接一个，封装 net.Conn       │
├────────────────────────────────────┤
│      Packet Codec                 │
│   LTPV (数据包编解码)             │
└────────────────────────────────────┘
```

## 模块结构

| 包 | 说明 |
|-----|------|
| `core/gxynet/gxynet.go` | Network 模块，作为 App 的子模块启动 |
| `core/gxynet/peer/` | Peer 接口及 TCP 实现 |
| `core/gxynet/endpoint/` | 连接端点抽象 |
| `core/gxynet/codec/` | 消息编解码、元信息注册 |
| `core/gxynet/packet/` | LTPV 数据包协议 |
| `core/gxynet/message/` | 消息定义 |
| `core/gxynet/connection/` | gnet 连接封装 |
| `core/gxynet/processor/` | 消息处理器 |

## Network 模块

```go
type Network struct {
    gxymodule.ModuleBase
    peer    peer.Peer
    handler endpoint.EventHandler
}
```

- 配置从 TOML 读取（`gxynet.peer`）
- `OnModStart` 创建 Peer 并启动监听

## 事件处理（EventHandler）

```go
type EventHandler interface {
    OnOpen(ep Endpoint) error
    OnMessage(ep Endpoint, msg *message.Message) error
    OnClose(ep Endpoint, err error)
}
```

Gateway 的 `GateHandler` 实现了该接口：

- **OnOpen**: 创建 Session Actor 并绑定到 Endpoint
- **OnMessage**: 消息体发送给 Session Actor 处理
- **OnClose**: 通知 Session Actor 停止

## Endpoint

```go
type Endpoint interface {
    SendData(data []byte, path string) error
    SendMsg(msg any) error
    Conn() net.Conn
    GetData() interface{}
    SetData(interface{})
}
```

每个连接对应一个 Endpoint，可在其上绑定自定义数据（Gateway 绑定 Session PID）。

## 消息编解码

### LTPV 协议

二进制协议，消息格式：

```
| Length (4B) | PathLen (2B) | Path (var) | Body (var) |
```

- Length: 整包长度（包含自身）
- PathLen: 路径字符串长度
- Path: 消息名称路径（用于消息路由）
- Body: protobuf 序列化消息体

### 消息元信息（`codec/meta.go`）

```go
type MessageMeta struct {
    ID   string          // 消息ID（如 "21001"）
    Name string          // 消息类型名
    Type reflect.Type    // Go 类型
}
```

- 通过 `RegisterMessageMeta(ID, msg)` 注册
- 全局映射 `metaByName` 和 `metaByID`
- 支持按名称、ID 或消息对象查找元信息

## 配置

```toml
[gxynet]
  [gxynet.peer]
  # TCP server 配置
```

## 消息流

```
客户端 TCP 数据
  → gnet 事件循环
  → connection 读取 → LTPV 解码
  → Endpoint 路由
  → EventHandler.OnMessage
  → Session Actor 处理
  → 响应 → Endpoint.SendMsg → LTPV 编码 → TCP 发送
```

## 源码位置

| 文件 | 说明 |
|------|------|
| `core/gxynet/gxynet.go` | Network 模块定义 |
| `core/gxynet/endpoint/endpoint.go` | Endpoint 接口 |
| `core/gxynet/endpoint/handler.go` | EventHandler 接口 |
| `core/gxynet/endpoint/tcp.go` | TCP Endpoint 实现 |
| `core/gxynet/codec/meta.go` | 消息元信息注册 |
| `core/gxynet/codec/protobuf.go` | protobuf 编解码 |
| `core/gxynet/packet/ltpv.go` | LTPV 协议 |
| `core/gxynet/peer/tcpserver.go` | TCP 服务器 |
| `core/gxynet/connection/connection.go` | gnet 连接封装 |
