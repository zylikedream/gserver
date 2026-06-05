# 客户端登录接入

本文面向客户端，说明当前版本的登录流程、账号服接口、`gate` 握手方式和重连规则。

## 服务端前提

当前版本要完成完整登录链路，服务端至少需要启动 3 个节点：

1. `account`
2. `gate`
3. `all`

说明：

- `account` 负责 `/account/prelogin` 和签发 `gate_token`
- `gate` 负责客户端 TCP 连接和握手验签
- `all` 负责角色与其他逻辑模块；`gate` 握手成功后还需要把玩家路由到这些业务模块

如果只启动 `account + gate`，客户端虽然能拿到 `gate_token`，但后续角色激活和业务处理不会完整可用。

## 总流程

客户端登录分两段：

1. 先请求账号服 HTTP 接口，拿账号信息和 `gate_token`
2. 再连接 `gate`，用 `gate_token` 完成首包握手

不要直接拿 `account_id`、`role_id` 去连 `gate`。

## 流程时序

```text
客户端 -> 账号服 /account/prelogin
账号服 -> 返回 account_id / role_id / gate / gate_token
客户端 -> 连接 gate host:port
客户端 -> 发送 ReqHandShake(gate_token)
gate -> 返回 RspHandShake
客户端 -> 继续发送游戏内协议
```

## 1. 账号服预登录

### 请求地址

`POST /account/prelogin`

说明：

- `account` 是服务名
- `prelogin` 是 handler 路径
- 完整地址由部署环境决定，例如 `http://account.example.com/account/prelogin`

### 请求体

```json
{
  "platform": "guest",
  "platform_uid": "u_123456",
  "client_version": "1.0.0"
}
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `platform` | string | 是 | 外部平台标识，例如 `guest`、`wechat`、`apple` |
| `platform_uid` | string | 是 | 平台侧用户唯一标识 |
| `client_version` | string | 否 | 客户端版本号，例如 `1.0.0` |

当前服务端会按 `platform + platform_uid` 定位游戏账号。

### 返回格式

HTTP 响应使用统一包装：

```json
{
  "code": 0,
  "message": "",
  "data": {
    "account_id": "acc_xxx",
    "role_id": 100001,
    "is_new_role": true,
    "account_info": {
      "platform": "guest",
      "platform_uid": "u_123456"
    },
    "version_info": {
      "client_version": "1.0.0",
      "min_version": "1.0.0",
      "latest_version": "1.0.0",
      "status": "ok"
    },
    "gate": {
      "host": "gate.example.com",
      "port": 20001
    },
    "gate_token": "xxxxx",
    "expires_in": 300
  }
}
```

### 返回字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `account_id` | string | 游戏内账号 ID |
| `role_id` | int64 | 游戏角色 ID |
| `is_new_role` | bool | 是否首次自动建号 |
| `account_info` | object | 当前回显的账号信息 |
| `version_info` | object | 版本检查结果 |
| `gate.host` | string | 客户端要连接的 `gate` 公网地址 |
| `gate.port` | int | 客户端要连接的 `gate` 端口 |
| `gate_token` | string | 进入 `gate` 的短期凭证 |
| `expires_in` | int | `gate_token` 剩余有效秒数 |

### 客户端收到后要做什么

1. 保存本次返回的 `account_id`、`role_id`
2. 立即使用 `gate.host` 和 `gate.port` 建立 TCP 连接
3. 在首包里携带 `gate_token`
4. 不要把 `gate_token` 当长期登录态使用

## 2. gate 握手

### 握手协议

客户端连接 `gate` 后，第一个包必须是 `ReqHandShake`。

`protocol/client/login.proto`：

```proto
message ReqHandShake{
   option (msg_id) = 10001;
   string gate_token = 1;
}
```

服务端返回：

```proto
message RspHandShake{
   option (msg_id) = 10002;
   string account_uid = 1;
   int64 role_id = 2;
}
```

说明：

- `RspHandShake.account_uid` 现在返回的是游戏内账号 ID
- 字段名还保留旧名，但语义已经等同于 `account_id`

### 客户端握手步骤

1. 建立到 `gate` 的 TCP 连接
2. 发送 `ReqHandShake`
3. `gate_token` 填账号服刚返回的 `gate_token`
4. 等待 `RspHandShake`
5. 握手成功后，再发送后续业务协议

## 3. 断线重连

当前版本的规则是：

- 已经建立成功的游戏连接，不依赖 token 持续续期
- 但一旦断线，重新连 `gate` 前必须重新请求账号服

也就是说，重连流程是：

1. 重新请求 `/account/prelogin`
2. 拿新的 `gate_token`
3. 再次连接 `gate`
4. 重新发送 `ReqHandShake`

不要复用已经过期的旧 `gate_token`。

## 4. 账号创建规则

当前版本是自动建号：

- 如果 `platform + platform_uid` 首次出现
- 服务端会自动创建：
  - `account_id`
  - `role_id`

所以客户端不需要单独调用“注册”接口。

## 5. 错误处理建议

### 账号服返回非 `code = 0`

说明预登录失败，常见原因：

- 版本不支持
- 请求参数错误
- 服务端内部错误

客户端应停止进入 `gate`，并根据 `message` 做提示或重试。

### `gate` 握手失败

常见原因：

- `gate_token` 缺失
- `gate_token` 已过期
- token 非当前环境签发
- token 非法或被篡改

客户端应重新走一遍预登录流程，不要直接重发旧 token。

## 6. 接入约束

当前版本有这些边界：

- 一个游戏账号只对应一个角色
- 客户端必须先请求账号服，再连 `gate`
- 不支持直接用裸 `account_id` 登录 `gate`
- 不提供 refresh token
- 每次重连前都应重新请求 `/account/prelogin`

## 7. 相关文档

- [token-auth.md](/home/zhangyi/workspace/gserver_github/docs/public/token-auth.md:1)
- [protocol.md](/home/zhangyi/workspace/gserver_github/docs/public/protocol.md:1)
