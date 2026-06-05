# Token 验签机制

本文说明客户端进入 `gate` 前使用的 `gate_token` 是如何签发、携带和校验的。

## 服务端前提

当前登录链路至少需要这 3 个节点同时运行：

1. `account`
2. `gate`
3. `all`

其中：

- `account` 负责预登录和签发 token
- `gate` 负责握手验签
- `all` 负责角色和后续业务逻辑

## 目的

`gate_token` 的作用不是维持长期登录态，而是作为客户端建立新 `gate` 连接时的短期凭证。

设计目标：

1. 客户端不能直接用裸 `account_id` 登录 `gate`
2. `gate` 不需要回源账号服即可完成本地验签
3. 账号服负责登录前校验和签发 token
4. 已建立的游戏内 session 不依赖 token 持续续期

## 登录链路

完整流程如下：

1. 服务端先启动 `account + gate + all`
2. 客户端先调用账号服 HTTP 接口
3. 账号服完成：
   - 平台身份接入
   - 版本校验
   - 账号映射 / 建号
   - 生成 `gate_token`
4. 账号服返回：
   - `account_id`
   - `role_id`
   - 公共 `gate` 地址
   - `gate_token`
5. 客户端连接 `gate`
6. 客户端首包 `ReqHandShake` 携带 `gate_token`
7. `gate` 本地验签
8. `gate` 从 token claims 中取出 `account_id` 和 `role_id`
9. `gate` 激活对应 `role actor`，建立 session

## 为什么需要 token

如果客户端直接把 `account_id` 或 `role_id` 发给 `gate`，服务端无法判断这个身份声明是否可信。

加入签名 token 后：

- 身份声明由账号服签发
- `gate` 只接受自己信任的签发结果
- 客户端只能转发 token，不能伪造有效身份

## Token 位置

握手协议在 `login.proto` 中定义：

```proto
message ReqHandShake {
    option (msg_id) = 10001;
    string gate_token = 1;
}
```

也就是说，`gate_token` 只出现在连接 `gate` 的首包里。

## Token 内容

当前 `gate_token` 使用的 claims 包括：

- `account_id`
- `role_id`
- `platform`
- `env`
- `issued_at`
- `expires_at`
- `issuer`

这些字段在代码中定义于：

- `src/lib/gatetoken/token.go`

## issuer 的作用

`issuer` 表示 token 的签发方身份。

它解决的是：

- 这个 token 是否由我信任的签发方发出

它不负责加密，也不负责签名算法本身。

在当前设计中：

- 账号服签 token 时写入 `issuer`
- `gate` 验 token 时除了验签，还要校验 `issuer`

这样可以避免“签名有效，但来源不对”的 token 被误接受。

## 签名算法

当前支持两种签名方式：

1. `HS256`
2. `Ed25519`

公共配置放在：

```toml
[token]
algorithm = "hs256"
issuer = "account-service"
expire_seconds = 300
env = "dev"
```

算法私有配置分别放在：

```toml
[token.hs256]
secret = "replace-me"

[token.ed25519]
private_key = ""
public_key = ""
```

### HS256

特点：

- 对称签名
- 账号服和 `gate` 使用同一个 secret
- 接入简单

适合：

- 开发环境
- 小规模部署
- 第一版快速落地

风险：

- 验签方也持有签发密钥

### Ed25519

特点：

- 非对称签名
- 账号服持有私钥签名
- `gate` 只持有公钥验签

适合：

- 更严格的密钥边界
- 多 `gate` 节点部署
- 对密钥暴露面更敏感的环境

## 验签时检查什么

`gate` 在验 token 时至少检查：

1. token 格式是否合法
2. 签名是否正确
3. 算法是否与配置匹配
4. `issuer` 是否匹配
5. `expires_at` 是否未过期

校验通过后，`gate` 才会信任其中的 `account_id` 和 `role_id`。

## 过期时间的含义

`gate_token` 是短期 token，默认有效期较短，例如 `300` 秒。

过期影响的是：

- 建立新的 `gate` 连接

不直接影响的是：

- 已经建立完成的游戏内 session

也就是说：

- 玩家已经连上 `gate` 后，即使 token 之后过期，当前连接不需要立刻断开
- 但如果断线重连，就必须重新去账号服拿新的 `gate_token`

## 为什么不需要 refresh token

当前设计里，客户端进入 `gate` 前必须先请求账号服。

因此第一版不需要引入 refresh token：

- 想建立新的 `gate` 连接
- 就重新调用账号服预登录接口
- 拿新的短期 `gate_token`

这是一个“预登录换进服票据”的模型，不是“客户端长期持有访问令牌”的模型。

## 配置读取方式

token 配置使用结构体反序列化，而不是零散读取：

- 通用 token 逻辑在 `src/lib/gatetoken/`
- `account` 和 `gate` 都依赖同一套 signer / verifier 逻辑

这样做的好处是：

- 配置结构清晰
- 算法切换时边界明确
- 账号服和网关对 token 行为保持一致

## 与 session 的关系

要区分三件事：

1. `gate_token`
   - 建立新连接时使用
2. `gate` session
   - 连接建立后存在于网关
3. `role actor`
   - 承载玩家运行时状态

`gate_token` 校验通过后，才会进入 session 和 role actor 的绑定流程。

## 当前限制

当前方案的边界如下：

- 一个账号只对应一个角色
- token 只用于进入 `gate`
- 不包含 refresh token
- 不做服务端撤销列表
- 不做单设备登录控制
- 不做平台服务端验票

这些能力后续可以继续扩展，但不属于当前机制的必需部分。

## 相关文件

- `protocol/client/login.proto`
- `protocol/pb/login.pb.go`
- `src/lib/gatetoken/token.go`
- `src/apps/account/account_service.go`
- `src/apps/account/logic/prelogin.go`
- `src/apps/gateway/gate_app.go`
- `src/apps/gateway/internal/logic/session.go`

---
*Last updated: 2026-05-29*
