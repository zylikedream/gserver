# Gateway 网关

网关负责管理客户端 TCP 连接，处理消息路由。

## 架构

```
客户端 TCP 连接
  │
  ▼
┌────────────────────────────┐
│   GateHandler              │
│   OnOpen → SpawnSession    │
│   OnMessage → Session.Handle│
│   OnClose → StopSession    │
└──────────┬─────────────────┘
           │
┌──────────▼─────────────────┐
│   Session Actor             │
│   - 管理连接生命周期         │
│   - 握手 → 角色激活         │
│   - 消息转发                │
└──────────┬─────────────────┘
           │ protoactor-go PID
┌──────────▼─────────────────┐
│   Role Actor (远程/本地)    │
└────────────────────────────┘
```

## 启动流程

1. `gate_app.go:OnModInit` — 创建 `Network` 模块（TCP 监听器）、创建 `SessionMgr`
2. 网络层接收新连接 → `GateHandler.OnOpen` → `SpawnSession` 创建 Session Actor
3. Session Actor 等待客户端发送握手包

## Session Actor（`session.go`）

每个客户端连接对应一个 Session Actor，管理连接生命周期。

### 生命周期

```
Connected → Handshake → Login → (Disconnected)
```

### 状态说明

| 状态 | 说明 |
|------|------|
| `StateConnected` | TCP 连接建立，等待握手 |
| `StateHandshake` | 握手完成，角色已激活 |
| `StateLogin` | 已登录（收到 RspAccountLogin 后） |
| `StateDisconnected` | 连接断开 |

### 消息处理

**客户端消息（`OnHandleClientMessage`）**：

| 消息类型 | 处理方式 |
|----------|----------|
| `MESSGE_TYPE_FIRST_PACKET` | 握手：账号认证 → 激活 Role Actor → 回复 |
| `MESSAGE_TYPE_DATA_PACKET` | 转发给 Role Actor 处理 |
| `ReqAccountLogout` | 主动登出 |

**服务端消息（`OnHandleServerMessage`）**：

来自 Role Actor 的响应消息，序列化后通过 Endpoint 发送给客户端。

### 空闲超时

- 客户端空闲超时：10 分钟（`SESSION_CLIENT_IDLE_TIMEOUT`）
- 服务端无响应超时：10 分钟（`SESSION_SERVER_IDLE_TIMEOUT`）
- 检查间隔：30 秒

## SessionMgr（`session_mgr.go`）

全局会话管理器，维护 `roleID → Session PID` 的映射。

## 消息路径（完整流程）

```
客户端 → TCP → GateHandler.OnMessage → Session.HandleClientMessage
  → ActivateRole(roleID) → (RPC to target node)
  → Role Actor 处理 → protobuf 响应
  → Session.OnHandleServerMessage → TCP → 客户端
```

## 源码位置

| 文件 | 说明 |
|------|------|
| `src/apps/gateway/gate_app.go` | Gate App 定义、Session 创建/停止 |
| `src/apps/gateway/gate_handler.go` | TCP 事件处理器 |
| `src/apps/gateway/internal/logic/session.go` | Session Actor 实现 |
| `src/apps/gateway/internal/logic/session_mgr.go` | 会话管理器 |
