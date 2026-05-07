# Chat System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现全 Redis + PostgreSQL 混合驱动的实时聊天系统，支持世界频道（分线大厅）、好友私聊、系统广播三个频道。

**Architecture:** Chat App 作为独立 App 注册到 Node，运行 PubSub relay 后台 goroutine 管理实时消息推送。Role 侧的 RoleChat 模块作为 IRoleModule 处理 proto 请求（同步调用 Redis/DB），Chat relay 负责异步推送（PubSub → 本节点 role actor）。世界频道和系统频道用 Redis List + PubSub；私聊用 PostgreSQL `chat_private_message` 表。

**Tech Stack:** Go, protoactor-go (actor system), go-redis/v9 (Redis + PubSub + Lua), GORM (PostgreSQL), protobuf

**Design Spec:** `docs/superpowers/specs/2026-05-07-chat-system-design.md`

---

## File Structure

| 文件 | 职责 |
|------|------|
| `protocol/client/chat.proto` (新) | Proto 消息定义 28001-28015 |
| `src/apps/chat/chat_app.go` (新) | Chat App 注册、启动 relay |
| `src/apps/chat/relay.go` (新) | PubSub 订阅 + 本节点 role 注册表 + 推送 |
| `src/apps/chat/redis.go` (新) | Lua 脚本 + Redis 操作（大厅分配、消息存储） |
| `src/apps/chat/model.go` (新) | ChatPrivateMessage GORM 模型 |
| `src/apps/chat/schema.go` (新) | AutoMigrate |
| `src/apps/chat/config.go` (新) | 聊天配置常量 |
| `src/apps/role/internal/logic/role_chat.go` (新) | RoleChat 模块：proto handler、冷却、验证 |
| `src/apps/role/internal/logic/role_main.go` (改) | roleModules 加 Chat，login/logout 联动 |
| `src/apps/role/internal/logic/role_schema.go` (改) | AutoMigrate 加 RoleChatState |
| `core/gxynode/node.go` (改) | registerApps 加 chat |
| `config/game.toml` (改) | node.apps 加 "chat" |

---

### Task 1: Proto 文件定义 + 代码生成

**Files:**
- Create: `protocol/client/chat.proto`

- [ ] **Step 1: 创建 chat.proto**

```protobuf
// ID: 28001~28099
syntax = "proto3";
option go_package="./pb;pb";
package galaxy.protocol;

import "msg_options.proto";

// === 通用 ===

message PChatMsg {
    int64 sender_id = 1;
    string sender_name = 2;
    string content = 3;
    int64 timestamp = 4;
}

// === 登录初始化 ===

// 聊天初始化 (28001) - 登录时自动调用
message ReqChatInit {
    option (msg_id) = 28001;
}
message RspChatInit {
    option (msg_id) = 28002;
    int32 lobby_id = 1;
    repeated PChatMsg world_messages = 2;
    repeated PChatMsg system_messages = 3;
}

// === 世界频道 ===

// 发送世界消息 (28003)
message ReqSendWorldChat {
    option (msg_id) = 28003;
    string content = 1;
}
message RspSendWorldChat {
    option (msg_id) = 28004;
}

// 拉取世界频道历史 (28005)
message ReqWorldChatHistory {
    option (msg_id) = 28005;
    int32 count = 1;
}
message RspWorldChatHistory {
    option (msg_id) = 28006;
    repeated PChatMsg messages = 1;
}

// 世界频道消息通知 (28007) - 服务端推送
message NotifyWorldChat {
    option (msg_id) = 28007;
    PChatMsg message = 1;
}

// === 私聊 ===

// 发送私聊 (28008)
message ReqSendPrivateChat {
    option (msg_id) = 28008;
    int64 target_id = 1;
    string content = 2;
}
message RspSendPrivateChat {
    option (msg_id) = 28009;
}

// 拉取私聊历史 (28010)
message ReqPrivateChatHistory {
    option (msg_id) = 28010;
    int64 friend_id = 1;
    int32 count = 2;
}
message RspPrivateChatHistory {
    option (msg_id) = 28011;
    repeated PChatMsg messages = 1;
}

// 私聊消息通知 (28012) - 服务端推送
message NotifyPrivateChat {
    option (msg_id) = 28012;
    int64 sender_id = 1;
    string sender_name = 2;
    string content = 3;
    int64 timestamp = 4;
}

// === 系统频道 ===

// 拉取系统消息 (28013)
message ReqSystemChatHistory {
    option (msg_id) = 28013;
    int32 count = 1;
}
message RspSystemChatHistory {
    option (msg_id) = 28014;
    repeated PChatMsg messages = 1;
}

// 系统消息通知 (28015) - 服务端推送
message NotifySystemChat {
    option (msg_id) = 28015;
    PChatMsg message = 1;
}
```

- [ ] **Step 2: 生成 Go 代码**

Run: `make pb`
Expected: `protocol/pb/chat.pb.go` 生成成功，无错误

- [ ] **Step 3: 验证编译**

Run: `go build ./...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add protocol/client/chat.proto protocol/pb/chat.pb.go
git commit -m "feat(chat): add chat proto definitions (28001-28015)"
```

---

### Task 2: Chat App 骨架 + DB 模型

**Files:**
- Create: `src/apps/chat/chat_app.go`
- Create: `src/apps/chat/model.go`
- Create: `src/apps/chat/schema.go`
- Create: `src/apps/chat/config.go`
- Modify: `core/gxynode/node.go`

- [ ] **Step 1: 创建 config.go — 聊天配置常量**

```go
package chat

import "gserver/gameconfig"

type Config struct {
	LobbyMaxCapacity int
	WorldCooldown    int // 秒
	MsgMaxLength     int
	WorldMsgKeep     int
	PrivateKeepDays  int
	SystemMsgKeep    int
}

func GetConfig() *Config {
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()
	return &Config{
		LobbyMaxCapacity: int(cfg.ChatLobbyMaxCapacity),
		WorldCooldown:    int(cfg.ChatWorldCooldown),
		MsgMaxLength:     int(cfg.ChatMsgMaxLength),
		WorldMsgKeep:     int(cfg.ChatWorldMsgKeep),
		PrivateKeepDays:  int(cfg.ChatPrivateMsgKeepDays),
		SystemMsgKeep:    int(cfg.ChatSystemMsgKeep),
	}
}
```

**注意:** 需要在 `gameconfig/json/garden_tbfriendconfig.json` 中追加 6 个 chat_ 开头的字段，并重新生成 `gameconfig/gosrc/garden.FriendConfig.go`。如果 gameconfig 生成工具链暂不可用，可先在 config.go 中硬编码默认值：

```go
package chat

type Config struct {
	LobbyMaxCapacity int
	WorldCooldown    int
	MsgMaxLength     int
	WorldMsgKeep     int
	PrivateKeepDays  int
	SystemMsgKeep    int
}

func GetConfig() *Config {
	return &Config{
		LobbyMaxCapacity: 100,
		WorldCooldown:    5,
		MsgMaxLength:     200,
		WorldMsgKeep:     100,
		PrivateKeepDays:  30,
		SystemMsgKeep:    50,
	}
}
```

- [ ] **Step 2: 创建 model.go — 私聊消息 GORM 模型**

```go
package chat

import "time"

type ChatPrivateMessage struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	MinRoleID  int64     `gorm:"column:min_role_id;not null;index:idx_chat_pm_pair_time"`
	MaxRoleID  int64     `gorm:"column:max_role_id;not null;index:idx_chat_pm_pair_time"`
	SenderID   int64     `gorm:"column:sender_id;not null"`
	Content    string    `gorm:"column:content;not null;type:text"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;index:idx_chat_pm_pair_time"`
}

func (ChatPrivateMessage) TableName() string { return "chat_private_message" }
```

- [ ] **Step 3: 创建 schema.go — AutoMigrate**

```go
package chat

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

func InitChatSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(&ChatPrivateMessage{}); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Info(ctx, "[schema] chat tables migrated successfully")
}
```

- [ ] **Step 4: 创建 chat_app.go — App 注册**

```go
package chat

import (
	"context"

	"gserver/core/gxyapp"
)

type chatApp struct {
	gxyapp.App
}

func NewChatApp() *chatApp {
	return &chatApp{}
}

func (c *chatApp) ServiceName() string {
	return "chat"
}

func (c *chatApp) OnModInit(ctx context.Context) error {
	c.AddModule(ctx, gameconfig.NewGameConfig())
	InitChatSchema(ctx)
	return nil
}

func (c *chatApp) OnModStart(ctx context.Context) error {
	StartRelay(ctx)
	return nil
}

func (c *chatApp) OnModStop(ctx context.Context) error {
	StopRelay()
	return nil
}
```

- [ ] **Step 5: 修改 node.go — 注册 chat app**

在 `core/gxynode/node.go` 的 import 中添加 `"gserver/src/apps/chat"`，在 `registerApps()` 中添加：

```go
gxyapp.RegisterApp("chat", chat.NewChatApp())
```

加在 `friend` 行之后。

- [ ] **Step 6: 修改 config/game.toml — 添加 chat app**

将 `node.apps` 从 `["role", "friend"]` 改为 `["chat", "role", "friend"]`。chat 在 role 之前加载，确保 relay 在 role actor 启动前就绪。

- [ ] **Step 7: 创建 relay.go 空壳（先占位，Task 4 实现）**

```go
package chat

import "context"

func StartRelay(ctx context.Context) {
	// Task 4 实现
}

func StopRelay() {
	// Task 4 实现
}

// RegisterRole 将本节点 role 注册到 relay（role 登录时调用）
func RegisterRole(roleID int64, lobbyID int64) {
	// Task 4 实现
}

// UnregisterRole 从 relay 移除 role（role 登出时调用）
func UnregisterRole(roleID int64) {
	// Task 4 实现
}
```

- [ ] **Step 8: 验证编译**

Run: `go build ./...`
Expected: 编译成功

- [ ] **Step 9: Commit**

```bash
git add src/apps/chat/ core/gxynode/node.go config/game.toml
git commit -m "feat(chat): add chat app skeleton, DB model, and node registration"
```

---

### Task 3: Redis Lua 脚本 + 数据操作

**Files:**
- Create: `src/apps/chat/redis.go`

- [ ] **Step 1: 创建 redis.go — Lua 脚本和所有 Redis 操作**

```go
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"gserver/core/gxyredis"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

// ===== Lua 脚本 =====

var luaJoinLobby = redis.NewScript(`
-- KEYS[1] = chat:lobby:sizes
-- ARGV[1] = maxSize
-- ARGV[2] = roleID
local maxSize = tonumber(ARGV[1])
local roleID = ARGV[2]

local available = redis.call('ZREVRANGEBYSCORE', KEYS[1], maxSize - 1, 0, 'LIMIT', 0, 1)
local lobbyID
if #available == 0 then
    lobbyID = tostring(redis.call('INCR', 'chat:lobby:counter'))
else
    lobbyID = available[1]
end

redis.call('SADD', 'chat:lobby:' .. lobbyID, roleID)
redis.call('ZINCRBY', KEYS[1], 1, lobbyID)
redis.call('PERSIST', 'chat:lobby:' .. lobbyID)
redis.call('PERSIST', 'chat:msg:lobby:' .. lobbyID)

return lobbyID
`)

var luaLeaveLobby = redis.NewScript(`
-- ARGV[1] = roleID
-- ARGV[2] = lobbyID
local roleID = ARGV[1]
local lobbyID = ARGV[2]

redis.call('SREM', 'chat:lobby:' .. lobbyID, roleID)
redis.call('ZINCRBY', 'chat:lobby:sizes', -1, lobbyID)

local size = redis.call('SCARD', 'chat:lobby:' .. lobbyID)
if size == 0 then
    redis.call('EXPIRE', 'chat:lobby:' .. lobbyID, 259200)
    redis.call('EXPIRE', 'chat:msg:lobby:' .. lobbyID, 259200)
end
return 1
`)

// ===== 聊天消息 JSON =====

type chatMsgJSON struct {
	SenderID   int64  `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Content    string `json:"content"`
	Timestamp  int64  `json:"timestamp"`
}

func msgToJSON(msg *pb.PChatMsg) string {
	b, _ := json.Marshal(&chatMsgJSON{
		SenderID:   msg.SenderId,
		SenderName: msg.SenderName,
		Content:    msg.Content,
		Timestamp:  msg.Timestamp,
	})
	return string(b)
}

func jsonToMsg(data string) (*pb.PChatMsg, error) {
	var j chatMsgJSON
	if err := json.Unmarshal([]byte(data), &j); err != nil {
		return nil, err
	}
	return &pb.PChatMsg{
		SenderId:   j.SenderID,
		SenderName: j.SenderName,
		Content:    j.Content,
		Timestamp:  j.Timestamp,
	}, nil
}

// ===== 大厅操作 =====

// JoinLobby 加入大厅，返回 lobbyID
func JoinLobby(ctx context.Context, roleID int64) (int64, error) {
	cfg := GetConfig()
	cmd := luaJoinLobby.Run(ctx, gxyredis.Redis(), []string{"chat:lobby:sizes"},
		cfg.LobbyMaxCapacity, strconv.FormatInt(roleID, 10))
	lobbyStr, err := cmd.Text()
	if err != nil {
		return 0, fmt.Errorf("chat join lobby: %w", err)
	}
	lobbyID, err := strconv.ParseInt(lobbyStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("chat parse lobby id: %w", err)
	}
	return lobbyID, nil
}

// LeaveLobby 退出大厅
func LeaveLobby(ctx context.Context, roleID, lobbyID int64) error {
	cmd := luaLeaveLobby.Run(ctx, gxyredis.Redis(), nil,
		strconv.FormatInt(roleID, 10), strconv.FormatInt(lobbyID, 10))
	if _, err := cmd.Int(); err != nil {
		return fmt.Errorf("chat leave lobby: %w", err)
	}
	return nil
}

// ===== 世界频道 =====

// StoreWorldMsg 存储世界消息并广播
func StoreWorldMsg(ctx context.Context, msg *pb.PChatMsg, lobbyID int64) error {
	cfg := GetConfig()
	key := fmt.Sprintf("chat:msg:lobby:%d", lobbyID)
	data := msgToJSON(msg)
	pipe := gxyredis.Redis().Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, int64(cfg.WorldMsgKeep-1))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("chat store world msg: %w", err)
	}
	return nil
}

// PublishWorldChat 发布世界频道广播
func PublishWorldChat(ctx context.Context, msg *pb.PChatMsg, lobbyID int64) error {
	channel := fmt.Sprintf("chat:pub:lobby:%d", lobbyID)
	data := msgToJSON(msg)
	return gxyredis.Redis().Publish(ctx, channel, data).Err()
}

// GetWorldHistory 拉取世界频道历史
func GetWorldHistory(ctx context.Context, lobbyID int64, count int) ([]*pb.PChatMsg, error) {
	key := fmt.Sprintf("chat:msg:lobby:%d", lobbyID)
	results, err := gxyredis.Redis().LRange(ctx, key, 0, int64(count-1)).Result()
	if err != nil {
		return nil, err
	}
	return parseMsgList(results)
}

// ===== 系统频道 =====

// StoreSystemMsg 存储系统消息
func StoreSystemMsg(ctx context.Context, msg *pb.PChatMsg) error {
	cfg := GetConfig()
	data := msgToJSON(msg)
	pipe := gxyredis.Redis().Pipeline()
	pipe.LPush(ctx, "chat:msg:system", data)
	pipe.LTrim(ctx, "chat:msg:system", 0, int64(cfg.SystemMsgKeep-1))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("chat store system msg: %w", err)
	}
	return nil
}

// PublishSystemChat 发布系统消息广播
func PublishSystemChat(ctx context.Context, msg *pb.PChatMsg) error {
	data := msgToJSON(msg)
	return gxyredis.Redis().Publish(ctx, "chat:pub:system", data).Err()
}

// GetSystemHistory 拉取系统消息历史
func GetSystemHistory(ctx context.Context, count int) ([]*pb.PChatMsg, error) {
	results, err := gxyredis.Redis().LRange(ctx, "chat:msg:system", 0, int64(count-1)).Result()
	if err != nil {
		return nil, err
	}
	return parseMsgList(results)
}

// ===== 私聊 =====

// StorePrivateMsg 存储私聊消息到 PostgreSQL
func StorePrivateMsg(ctx context.Context, senderID, targetID int64, content string) (int64, error) {
	minID, maxID := sortIDs(senderID, targetID)
	msg := &ChatPrivateMessage{
		MinRoleID: minID,
		MaxRoleID: maxID,
		SenderID:  senderID,
		Content:   content,
		CreatedAt: time.Now(),
	}
	if err := db().WithContext(ctx).Create(msg).Error; err != nil {
		return 0, fmt.Errorf("chat store private msg: %w", err)
	}
	return msg.CreatedAt.Unix(), nil
}

// GetPrivateHistory 拉取私聊历史
func GetPrivateHistory(ctx context.Context, roleID, friendID int64, count int) ([]*pb.PChatMsg, error) {
	minID, maxID := sortIDs(roleID, friendID)
	var msgs []ChatPrivateMessage
	err := db().WithContext(ctx).
		Where("min_role_id = ? AND max_role_id = ?", minID, maxID).
		Order("created_at DESC").
		Limit(count).
		Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	result := make([]*pb.PChatMsg, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		result = append(result, &pb.PChatMsg{
			SenderId:  m.SenderID,
			Content:   m.Content,
			Timestamp: m.CreatedAt.Unix(),
		})
	}
	return result, nil
}

// ===== 工具函数 =====

func sortIDs(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

func parseMsgList(results []string) ([]*pb.PChatMsg, error) {
	msgs := make([]*pb.PChatMsg, 0, len(results))
	for _, data := range results {
		msg, err := jsonToMsg(data)
		if err != nil {
			glog.Warningf(context.Background(), "chat parse msg error: %v", err)
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
```

- [ ] **Step 2: 添加 db() 辅助函数**

在 redis.go 底部（或单独文件）添加 GORM 访问：

```go
package chat

import "gserver/core/gxypgx"

func db() *gxypgx.DBType {
	return gxypgx.DB()
}
```

**注意:** 需要确认 `gxypgx.DB()` 的返回类型。参考项目中用法：`gxypgx.DB().Table(...)` / `gxypgx.DB().AutoMigrate(...)` / `gxypgx.DB().WithContext(ctx).Create(...)`。

- [ ] **Step 3: 验证编译**

Run: `go build ./src/apps/chat/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add src/apps/chat/redis.go
git commit -m "feat(chat): add Redis Lua scripts and chat data operations"
```

---

### Task 4: PubSub Relay — 实时消息推送

**Files:**
- Rewrite: `src/apps/chat/relay.go`

- [ ] **Step 1: 实现 relay.go — PubSub 订阅 + 本节点角色注册表 + 推送**

```go
package chat

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

var (
	localRoles   sync.Map // roleID → *roleEntry
	relayCancel  context.CancelFunc
	relayMutex   sync.Mutex
)

type roleEntry struct {
	lobbyID int64
	pid     gxyactor.PID
}

// StartRelay 启动 PubSub relay
func StartRelay(ctx context.Context) {
	relayMutex.Lock()
	defer relayMutex.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	relayCancel = cancel

	go runRelay(ctx)
	glog.Info(ctx, "[chat] relay started")
}

// StopRelay 停止 PubSub relay
func StopRelay() {
	relayMutex.Lock()
	defer relayMutex.Unlock()
	if relayCancel != nil {
		relayCancel()
	}
}

// RegisterRole 将本节点 role 注册到 relay
func RegisterRole(roleID int64, lobbyID int64, pid gxyactor.PID) {
	localRoles.Store(roleID, &roleEntry{lobbyID: lobbyID, pid: pid})
}

// UnregisterRole 从 relay 移除 role
func UnregisterRole(roleID int64) {
	localRoles.Delete(roleID)
}

func runRelay(ctx context.Context) {
	sub := gxyredis.Redis().PSubscribe(ctx, "chat:pub:lobby:*", "chat:pub:system")
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			handlePubSub(ctx, msg)
		}
	}
}

func handlePubSub(ctx context.Context, msg *redis.Message) {
	chatMsg, err := jsonToMsg(msg.Payload)
	if err != nil {
		glog.Warningf(ctx, "[chat] parse pubsub msg error: %v", err)
		return
	}

	channel := msg.Channel
	if strings.HasPrefix(channel, "chat:pub:lobby:") {
		// 世界频道消息
		parts := strings.SplitN(channel, ":", 4)
		if len(parts) < 4 {
			return
		}
		lobbyID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		notify := &pb.NotifyWorldChat{Message: chatMsg}
		localRoles.Range(func(key, value any) bool {
			entry := value.(*roleEntry)
			if entry.lobbyID == lobbyID {
				gxyactor.LocalSend(entry.pid, notify)
			}
			return true
		})
	} else if channel == "chat:pub:system" {
		// 系统消息 — 推送给本节点所有 role
		notify := &pb.NotifySystemChat{Message: chatMsg}
		localRoles.Range(func(key, value any) bool {
			entry := value.(*roleEntry)
			gxyactor.LocalSend(entry.pid, notify)
			return true
		})
	}
}
```

**关键设计决策:**
- 使用 `sync.Map` 存储本节点 role 注册表，并发安全
- `PSUBSCRIBE chat:pub:lobby:*` 模式订阅，自动覆盖所有 lobby channel
- 收到 PubSub 消息后遍历本节点注册表，只推送给本节点的 role actor
- 使用 `gxyactor.LocalSend` 推送（同节点，不需要跨进程序列化）

**注意:** `NotifyWorldChat` / `NotifySystemChat` 是 proto 消息，跨节点消息必须是 proto.Message。此处用 `LocalSend`（本地发送，接受 any 类型）。但如果后续需要跨节点推送（比如 relay 和 role 不在同一节点），需要改用 `gxyactor.Send`，此时消息必须是 proto.Message——当前设计已满足这个要求。

- [ ] **Step 2: 更新 chat_app.go 的 import**

确保 chat_app.go 引用了 gameconfig：

```go
package chat

import (
	"context"
	"gserver/core/gxyapp"
	"gserver/gameconfig"
)

type chatApp struct {
	gxyapp.App
}

func NewChatApp() *chatApp { return &chatApp{} }

func (c *chatApp) ServiceName() string { return "chat" }

func (c *chatApp) OnModInit(ctx context.Context) error {
	c.AddModule(ctx, gameconfig.NewGameConfig())
	InitChatSchema(ctx)
	return nil
}

func (c *chatApp) OnModStart(ctx context.Context) error {
	StartRelay(ctx)
	return nil
}

func (c *chatApp) OnModStop(ctx context.Context) error {
	StopRelay()
	return nil
}
```

- [ ] **Step 3: 验证编译**

Run: `go build ./...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add src/apps/chat/relay.go src/apps/chat/chat_app.go
git commit -m "feat(chat): implement PubSub relay for real-time message delivery"
```

---

### Task 5: RoleChat 模块 — Proto Handler + 冷却 + 验证

**Files:**
- Create: `src/apps/role/internal/logic/role_chat.go`
- Modify: `src/apps/role/internal/logic/role_main.go` — roleModules 加 Chat + login/logout 联动
- Modify: `src/apps/role/internal/logic/role_schema.go` — AutoMigrate 加 RoleChatState

- [ ] **Step 1: 创建 role_chat.go**

```go
package logic

import (
	"context"
	"errors"
	"strings"
	"time"

	"gserver/core/gxyredis"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/apps/chat"
	"gserver/src/lib"
)

var (
	ErrChatCooldown   = errors.New("发言太频繁，请稍后再试")
	ErrChatMsgEmpty   = errors.New("消息不能为空")
	ErrChatMsgTooLong = errors.New("消息超过字数限制")
	ErrChatNotFriend  = errors.New("对方不是你的好友")
)

// ===== Persist State（无额外持久化字段）=====

type RoleChatState struct {
	RolePersistState
}

func (RoleChatState) TableName() string { return "role_chat" }

// ===== 模块 =====

type RoleChat struct {
	RoleModule
	RoleChatState
	lastLobbyID       int64
	lastWorldChatTime time.Time
}

var _ IRoleModule = (*RoleChat)(nil)

func (r *RoleChat) PersistState() IPersistState { return &r.RoleChatState }

func (r *RoleChat) OnModInit(ctx context.Context) error { return nil }

func (r *RoleChat) OnCreate(ctx context.Context) {}

// ===== Proto Handlers =====

func (r *RoleChat) ReqChatInit(ctx context.Context, req *pb.ReqChatInit) (*pb.RspChatInit, error) {
	// 加入大厅
	lobbyID, err := chat.JoinLobby(ctx, r.RoleID)
	if err != nil {
		return nil, err
	}
	r.lastLobbyID = lobbyID

	// 拉取历史
	cfg := chat.GetConfig()
	worldMsgs, _ := chat.GetWorldHistory(ctx, lobbyID, cfg.WorldMsgKeep)
	systemMsgs, _ := chat.GetSystemHistory(ctx, cfg.SystemMsgKeep)

	// 注册到 relay（用于 PubSub 推送）
	chat.RegisterRole(r.RoleID, lobbyID, r.Role.Self())

	return &pb.RspChatInit{
		LobbyId:         int32(lobbyID),
		WorldMessages:   worldMsgs,
		SystemMessages:  systemMsgs,
	}, nil
}

func (r *RoleChat) ReqSendWorldChat(ctx context.Context, req *pb.ReqSendWorldChat) (*pb.RspSendWorldChat, error) {
	cfg := chat.GetConfig()

	// 验证
	if err := r.validateMessage(req.Content, cfg); err != nil {
		return nil, err
	}

	// 冷却检查
	if time.Since(r.lastWorldChatTime) < time.Duration(cfg.WorldCooldown)*time.Second {
		return nil, ErrChatCooldown
	}
	r.lastWorldChatTime = time.Now()

	// 构建消息
	msg := &pb.PChatMsg{
		SenderId:   r.RoleID,
		SenderName: r.Role.Basic.RoleName,
		Content:    strings.TrimSpace(req.Content),
		Timestamp:  time.Now().Unix(),
	}

	// 存储 + 广播
	if err := chat.StoreWorldMsg(ctx, msg, r.lastLobbyID); err != nil {
		return nil, err
	}
	if err := chat.PublishWorldChat(ctx, msg, r.lastLobbyID); err != nil {
		return nil, err
	}

	return &pb.RspSendWorldChat{}, nil
}

func (r *RoleChat) ReqWorldChatHistory(ctx context.Context, req *pb.ReqWorldChatHistory) (*pb.RspWorldChatHistory, error) {
	count := int(req.Count)
	if count <= 0 {
		count = chat.GetConfig().WorldMsgKeep
	}
	msgs, err := chat.GetWorldHistory(ctx, r.lastLobbyID, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspWorldChatHistory{Messages: msgs}, nil
}

func (r *RoleChat) ReqSendPrivateChat(ctx context.Context, req *pb.ReqSendPrivateChat) (*pb.RspSendPrivateChat, error) {
	cfg := chat.GetConfig()

	// 验证内容
	if err := r.validateMessage(req.Content, cfg); err != nil {
		return nil, err
	}

	// 验证好友关系
	if !isFriend(ctx, r.RoleID, req.TargetId) {
		return nil, ErrChatNotFriend
	}

	// 存储私聊消息
	ts, err := chat.StorePrivateMsg(ctx, r.RoleID, req.TargetId, strings.TrimSpace(req.Content))
	if err != nil {
		return nil, err
	}

	// 通知目标（如果在线）
	pid, err := lib.GetRoleActor(req.TargetId, false)
	if err == nil && pid != nil {
		gxyredis.LocalSend(pid, &pb.NotifyPrivateChat{
			SenderId:   r.RoleID,
			SenderName: r.Role.Basic.RoleName,
			Content:    strings.TrimSpace(req.Content),
			Timestamp:  ts,
		})
	}

	return &pb.RspSendPrivateChat{}, nil
}

func (r *RoleChat) ReqPrivateChatHistory(ctx context.Context, req *pb.ReqPrivateChatHistory) (*pb.RspPrivateChatHistory, error) {
	count := int(req.Count)
	if count <= 0 {
		count = 50
	}
	msgs, err := chat.GetPrivateHistory(ctx, r.RoleID, req.FriendId, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspPrivateChatHistory{Messages: msgs}, nil
}

func (r *RoleChat) ReqSystemChatHistory(ctx context.Context, req *pb.ReqSystemChatHistory) (*pb.RspSystemChatHistory, error) {
	count := int(req.Count)
	if count <= 0 {
		count = chat.GetConfig().SystemMsgKeep
	}
	msgs, err := chat.GetSystemHistory(ctx, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspSystemChatHistory{Messages: msgs}, nil
}

// ===== 内部方法 =====

func (r *RoleChat) validateMessage(content string, cfg *chat.Config) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ErrChatMsgEmpty
	}
	if len([]rune(trimmed)) > cfg.MsgMaxLength {
		return ErrChatMsgTooLong
	}
	return nil
}

// chatLeave 在角色登出时调用
func (r *RoleChat) chatLeave(ctx context.Context) {
	if r.lastLobbyID > 0 {
		_ = chat.LeaveLobby(ctx, r.RoleID, r.lastLobbyID)
		chat.UnregisterRole(r.RoleID)
		r.lastLobbyID = 0
	}
}
```

**注意:** `ReqSendPrivateChat` 中推送通知使用了 `gxyredis.LocalSend` 但实际需要用 `gxyactor.LocalSend`（不是 gxyredis 包的）。在 Task 6 编译时会修正为正确的 import。

- [ ] **Step 2: 修改 role_main.go — 添加 Chat 字段到 roleModules**

在 `roleModules` 结构体中添加：

```go
Chat *RoleChat
```

在 `afterRoleLogin` 末尾添加：

```go
// Chat relay 在登录后由客户端发送 ReqChatInit 触发
```

在 `dologout` 的 `r.state = RoleStateLogout` 之前添加：

```go
// 退出聊天大厅
r.Chat.chatLeave(ctx)
```

在 `Terminate` 方法的 `r.StopModule(ctx)` 之前添加：

```go
// 异常退出也要清理聊天
r.Chat.chatLeave(ctx)
```

- [ ] **Step 3: 修改 role_schema.go — AutoMigrate 加 RoleChatState**

在 `InitRoleSchema` 的 `AutoMigrate` 参数列表中添加 `&RoleChatState{}`。

- [ ] **Step 4: 修正 import — ReqSendPrivateChat 中的推送调用**

`role_chat.go` 中的 `gxyredis.LocalSend` 应该是 `gxyactor.LocalSend`。确保 import 使用：

```go
"gserver/core/gxyactor"
```

并将推送行改为：

```go
gxyactor.LocalSend(pid, &pb.NotifyPrivateChat{...})
```

- [ ] **Step 5: 验证编译**

Run: `go build ./...`
Expected: 编译成功

- [ ] **Step 6: Commit**

```bash
git add src/apps/role/internal/logic/role_chat.go src/apps/role/internal/logic/role_main.go src/apps/role/internal/logic/role_schema.go
git commit -m "feat(chat): add RoleChat module with proto handlers, cooldown, and validation"
```

---

### Task 6: RoleMain HandleMessage — 处理 PubSub 推送通知

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go`

- [ ] **Step 1: 在 HandleMessage 中添加 PubSub 通知处理**

`RoleMain.HandleMessage` 接收来自 PubSub relay 的推送通知（`NotifyWorldChat`、`NotifySystemChat`）。这些是服务端主动推送给客户端的消息。

在 `HandleMessage` 方法中，在 `AutoHandleMsg` 之前添加通知转发：

```go
func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
	// 聊天 PubSub 推送 → 转发给客户端
	switch m := msg.(type) {
	case *pb.NotifyWorldChat, *pb.NotifySystemChat, *pb.NotifyPrivateChat:
		r.SendClient(ctx, m.(proto.Message))
		return nil
	}
	_, err := r.AutoHandleMsg(ctx, msg)
	return err
}
```

**注意:** 需要在 role_main.go 的 import 中添加 `"google.golang.org/protobuf/proto"` 如果尚未存在。`SendClient` 方法接受 `proto.Message`，所以需要类型断言。

- [ ] **Step 2: 验证编译**

Run: `go build ./...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add src/apps/role/internal/logic/role_main.go
git commit -m "feat(chat): handle PubSub push notifications in RoleMain.HandleMessage"
```

---

### Task 7: 编译验证 + 最终提交

**Files:** 无新文件

- [ ] **Step 1: 全量编译**

Run: `go build ./...`
Expected: 无错误

- [ ] **Step 2: 检查所有改动**

Run: `git diff --stat master`

预期改动文件列表：
```
protocol/client/chat.proto          (新)
protocol/pb/chat.pb.go              (新)
src/apps/chat/chat_app.go           (新)
src/apps/chat/config.go             (新)
src/apps/chat/model.go              (新)
src/apps/chat/redis.go              (新)
src/apps/chat/relay.go              (新)
src/apps/chat/schema.go             (新)
src/apps/role/internal/logic/role_chat.go   (新)
src/apps/role/internal/logic/role_main.go   (改)
src/apps/role/internal/logic/role_schema.go (改)
core/gxynode/node.go               (改)
config/game.toml                    (改)
```

- [ ] **Step 3: 如果有遗漏修正，做一次 amend 或新 commit**

确保所有文件都已提交。

---

## Spec Coverage Self-Review

| Spec 章节 | Task | 状态 |
|-----------|------|------|
| §2.1 整体结构（Chat App + ChatHub） | Task 2, 4 | ✅ |
| §2.3 Role 侧职责（lastLobbyID, lastWorldChatTime） | Task 5 | ✅ |
| §3 Redis 数据结构 | Task 3, 4 | ✅ |
| §3.1 私聊消息表 | Task 2 (model), Task 3 (StorePrivateMsg) | ✅ |
| §4.1 加入大厅 Lua 脚本 | Task 3 | ✅ |
| §4.2 退出大厅 Lua 脚本 | Task 3 | ✅ |
| §5.1 玩家登录 | Task 5 (ReqChatInit) | ✅ |
| §5.2 玩家下线 | Task 5 (chatLeave) | ✅ |
| §5.3 世界频道发言 | Task 5 (ReqSendWorldChat) | ✅ |
| §5.4 私聊发言 | Task 5 (ReqSendPrivateChat) | ✅ |
| §5.5 拉取历史消息 | Task 5 (ReqWorldChatHistory, ReqPrivateChatHistory, ReqSystemChatHistory) | ✅ |
| §5.6 系统消息 | Task 3 (StoreSystemMsg + PublishSystemChat) | ✅ |
| §6 发言限制（冷却+字数） | Task 5 (validateMessage + cooldown) | ✅ |
| §7 Proto 设计 28001-28015 | Task 1 | ✅ |
| §9 涉及文件 | All Tasks | ✅ |

## Placeholder Scan

无 TBD、TODO、或占位符。所有代码步骤包含完整实现。

## Type Consistency Check

- `chat.JoinLobby` 返回 `(int64, error)` → `RoleChat.ReqChatInit` 用 `lobbyID int64` → `RoleChat.lastLobbyID int64` ✅
- `chat.StoreWorldMsg` 接受 `*pb.PChatMsg, int64` → `RoleChat.ReqSendWorldChat` 传入正确 ✅
- `chat.StorePrivateMsg` 返回 `(int64, error)` → `ts int64` → `NotifyPrivateChat.Timestamp` ✅
- `chat.GetConfig()` 返回 `*Config` → 所有 cfg 字段名一致 ✅
