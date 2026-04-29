# 网络协议

## 封包格式 (LTIV)

```
[Size: 2B LE][Type: 1B][ID: 2B LE][Payload: protobuf]
```

- Size: 后续数据总长度 (Type + ID + Payload)
- Type: 0=首包(握手), 1=数据包
- ID: 消息ID (uint16)
- 最大包体: 3MB

## 通信流程

1. 连接建立 → ReqHandShake(Type=0) → RspHandShake
2. 请求响应 → ReqXxx → RspXxx，失败返回 Ack{code=1, path=请求ID}
3. 服务端推送 → NotifyXxx (无需请求)

## 消息ID分配

| 前缀 | 模块 | 范围 |
|------|------|------|
| 010 | 系统 | 01001~01099 |
| 100 | 登录 | 10001~10099 |
| 200 | 基础 | 20001~20099 |
| 210 | 背包 | 21001~21099 |

## 消息ID获取方式

每条消息的 ID 在 proto 文件中通过 `option (msg_id) = XXXXX` 声明，定义在 `msg_options.proto`：

```proto
import "google/protobuf/descriptor.proto";

extend google.protobuf.MessageOptions {
    uint32 msg_id = 50000;
}
```

各消息使用方式：

```proto
message ReqHandShake {
    option (msg_id) = 10001;
    string account_uid = 1;
}
```

客户端生成代码后，通过 message descriptor 读取 `msg_id` option 获取消息ID。不同语言示例：

**C#:**
```csharp
uint msgId = (uint)ReqHandShake.Descriptor.CustomOptions[MsgOptions.MsgId];
```

**Python:**
```python
msg_id = ReqHandShake.DESCRIPTOR.GetOptions().msg_id
```

**TypeScript (protobufjs):**
```typescript
const msgId = root.lookupType("ReqHandShake").options["(msg_id)"];
```

## 当前协议文件

| 文件 | 用途 |
|------|------|
| `login.proto` | 握手、登录、登出 |
| `role.proto` | 角色相关 |
| `basic.proto` | 基础信息（名称、头像） |
| `bag.proto` | 背包系统 |
| `ack.proto` | 通用响应/错误 |
| `gactor.proto` | Actor 通信（ActorActive, ActorStop, ActorError） |

---
*Last updated: 2026-04-29*
