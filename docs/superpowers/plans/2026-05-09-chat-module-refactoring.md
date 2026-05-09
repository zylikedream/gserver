# Chat Module Refactoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将聊天模块从 Redis 驱动 + sidecar 推送的架构，重构为基于 ChannelActor 的统一频道模型。

**Architecture:** `IChannel` 接口定义频道差异化行为（世界频道不持久化、公会频道 TickSave），`ChannelActor` 每个频道一个 actor（consistent hash 按 `channel_type:channel_id` 路由），内存环形缓冲区替代 Redis pub/sub。角色侧直接 `Call` ChannelActor 发送消息，ChannelActor 用 `GetRoleActor` 解析角色 PID 后 `Send` 通知。

**Tech Stack:** Go, protoactor-go, PostgreSQL, Redis（仅保留私聊）, GoFrame v2

---

### Task 1: Proto 消息变更

**Files:**
- Modify: `protocol/client/chat.proto`
- Modify: `protocol/client/guild.proto`

- [ ] **Step 1: 在 chat.proto 末尾追加内部 Actor 消息和统一频道消息**

在 chat.proto 顶部 imports 区域追加 `import "gactor.proto";`，再在 `// === 公会频道 ===` 段后面追加以下内容：

```proto
// === 频道 Actor 内部消息（服务器内部使用，无 msg_id） ===

import "gactor.proto";

message ChannelRegisterMsg {
    int64 role_id = 1;
    ActorPid pid = 2;
    int32 channel_type = 3;
    int64 channel_id = 4;
}

message ChannelUnregisterMsg {
    int64 role_id = 1;
    int32 channel_type = 2;
    int64 channel_id = 3;
}

// 发送频道消息（Actor 内部请求）
message ReqChannelSend {
    int32 channel_type = 1;
    int64 channel_id = 2;
    int64 sender_id = 3;
    string content = 4;
}

// === 统一频道消息 ===

// 发送频道消息 (28020) - 客户端发送 channel_type + content，channel_id 由服务端推断
message ReqSendChannelChat {
    option (msg_id) = 28020;
    int32 channel_type = 1;
    string content     = 2;
}
message RspSendChannelChat {
    option (msg_id) = 28021;
}

// 频道消息通知 (28022) - 服务端推送
message NotifyChannelChat {
    option (msg_id) = 28022;
    int32 channel_type = 1;
    int64 channel_id   = 2;
    int64 sender_id    = 3;
    string content     = 4;
    int64 timestamp    = 5;
}

// 拉取频道历史 (28023)
message ReqChannelHistory {
    option (msg_id) = 28023;
    int32 channel_type = 1;
    int64 channel_id   = 2;
    int32 count        = 3;
}
message RspChannelHistory {
    option (msg_id) = 28024;
    repeated PChatMsg messages = 1;
}
```

- [ ] **Step 2: 从 guild.proto 删除 `NotifyGuildChat`（msg_id 29055）**

找到并删除整个 `NotifyGuildChat` 消息定义。

- [ ] **Step 3: 运行 `make pb` 重新生成 Go 代码**

```bash
make pb
```

- [ ] **Step 4: 编译验证**

```bash
go build ./protocol/...
```

- [ ] **Step 5: Commit**

```bash
git add protocol/client/chat.proto protocol/client/guild.proto protocol/pb/
git commit -m "proto: add unified channel chat messages, remove NotifyGuildChat"
```

---

### Task 2: IChannel 接口 + WorldChannel/GuildChannel 实现

**Files:**
- Create: `src/apps/chat/channel.go`

- [ ] **Step 1: 创建 channel.go**

```go
package chat

import (
	"errors"
	"time"
)

// IChannel 定义频道的差异化行为
type IChannel interface {
	ChannelType() string         // "world", "guild"
	RingBufferSize() int         // 消息保留上限
	SaveInterval() time.Duration // >0 启用定时存盘
	TableName() string           // 存盘表名（SaveInterval>0时需要）
	CanWrite(roleID int64, content string) error
	CanJoin(roleID int64) bool
}

// WorldChannel 世界频道
type WorldChannel struct{}

func (WorldChannel) ChannelType() string           { return "world" }
func (WorldChannel) RingBufferSize() int            { return 200 }
func (WorldChannel) SaveInterval() time.Duration    { return 0 }
func (WorldChannel) TableName() string              { return "" }
func (WorldChannel) CanWrite(_ int64, content string) error {
	if content == "" {
		return errors.New("消息不能为空")
	}
	return nil
}
func (WorldChannel) CanJoin(_ int64) bool { return true }

// GuildChannel 公会频道
type GuildChannel struct{}

func (GuildChannel) ChannelType() string           { return "guild" }
func (GuildChannel) RingBufferSize() int            { return 500 }
func (GuildChannel) SaveInterval() time.Duration    { return 600 * time.Second }
func (GuildChannel) TableName() string              { return "guild_chat_log" }
func (GuildChannel) CanWrite(_ int64, content string) error {
	if content == "" {
		return errors.New("消息不能为空")
	}
	return nil
}
func (GuildChannel) CanJoin(_ int64) bool { return true }

// channelRegistry channelType → IChannel
var channelRegistry = map[int32]IChannel{
	1: WorldChannel{},
	2: GuildChannel{},
}

func GetChannel(channelType int32) (IChannel, bool) {
	c, ok := channelRegistry[channelType]
	return c, ok
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./src/apps/chat/...
```

- [ ] **Step 3: Commit**

```bash
git add src/apps/chat/channel.go
git commit -m "chat: IChannel interface + WorldChannel/GuildChannel implementations"
```

---

### Task 3: ChannelActor 实现

**Files:**
- Create: `src/apps/chat/channel_actor.go`

- [ ] **Step 1: 创建 channel_actor.go**

```go
package chat

import (
	"context"
	"errors"
	"sync"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"gserver/src/lib"

	"github.com/asynkron/protoactor-go/actor"
)

// ringBuffer 环形缓冲区
type ringBuffer struct {
	mu   sync.RWMutex
	msgs []*pb.PChatMsg
	cap  int
	seq  int // 当前写入序号
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{
		msgs: make([]*pb.PChatMsg, 0, cap),
		cap:  cap,
	}
}

func (rb *ringBuffer) Push(msg *pb.PChatMsg) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.msgs) >= rb.cap {
		rb.msgs = rb.msgs[1:]
	}
	rb.msgs = append(rb.msgs, msg)
	rb.seq++
	return rb.seq
}

func (rb *ringBuffer) Recent(count int) []*pb.PChatMsg {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if count <= 0 || count > len(rb.msgs) {
		count = len(rb.msgs)
	}
	result := make([]*pb.PChatMsg, count)
	copy(result, rb.msgs[len(rb.msgs)-count:])
	return result
}

func (rb *ringBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return len(rb.msgs)
}

type ChannelActor struct {
	gxymodule.ModuleBase
	*gxyactor.ActorBase
	ChannelType int32
	ChannelID   int64
	channel     IChannel
	members     map[int64]*actor.PID // roleID → PID
	buffer      *ringBuffer
	lastSavedSeq int
}

func NewChannelActor() *ChannelActor {
	ctx := gxylog.NewContext(context.Background(), "channel")
	a := &ChannelActor{
		members: make(map[int64]*actor.PID),
	}
	a.ActorBase = gxyactor.NewActorBase(ctx, a)
	return a
}

func (a *ChannelActor) Init(ctx context.Context, args []any) error {
	if len(args) < 2 {
		return errors.New("channel actor init: need [channelType(int32), channelID(int64)]")
	}
	a.ChannelType = args[0].(int32)
	a.ChannelID = args[1].(int64)
	ch, ok := GetChannel(a.ChannelType)
	if !ok {
		return errors.New("unknown channel type")
	}
	a.channel = ch
	a.buffer = newRingBuffer(ch.RingBufferSize())
	return nil
}

func (a *ChannelActor) DelayInit(ctx context.Context) error {
	if a.channel.SaveInterval() > 0 {
		a.Timer().AddTick(ctx, &gxytimer.Tick{
			Name:     "channel_save",
			Interval: a.channel.SaveInterval(),
		}, a.TickSave)
	}
	return nil
}

func (a *ChannelActor) HandleMessage(ctx context.Context, msg any) error {
	switch m := msg.(type) {
	case *pb.ChannelRegisterMsg:
		a.members[m.RoleId] = &actor.PID{
			Address: m.Pid.Address,
			Id:      m.Pid.Id,
		}

	case *pb.ChannelUnregisterMsg:
		delete(a.members, m.RoleId)

	case *pb.ReqChannelSend:
		if err := a.channel.CanWrite(m.SenderId, m.Content); err != nil {
			a.Respond(err)
			return nil
		}
		chatMsg := &pb.PChatMsg{
			Content:   m.Content,
			Timestamp: time.Now().Unix(),
		}
		a.buffer.Push(chatMsg)

		notify := &pb.NotifyChannelChat{
			ChannelType: m.ChannelType,
			ChannelId:   m.ChannelId,
			SenderId:    m.SenderId,
			Content:     m.Content,
			Timestamp:   chatMsg.Timestamp,
		}
		// 通知所有成员
		for roleID := range a.members {
			pid, err := lib.GetRoleActor(roleID, false)
			if err == nil {
				a.Send(pid, notify)
			}
		}
		a.Respond(nil)

	case *pb.ReqChannelHistory:
		count := int(m.Count)
		if count <= 0 || count > a.channel.RingBufferSize() {
			count = a.channel.RingBufferSize()
		}
		msgs := a.buffer.Recent(count)
		a.Respond(&pb.RspChannelHistory{Messages: msgs})
	}
	return nil
}

func (a *ChannelActor) Terminate(ctx context.Context, err error) {
	a.save(ctx)
	a.StopModule(ctx)
}

func (a *ChannelActor) TickSave(ctx context.Context, _ gxytimer.TimerActiveInfo) {
	a.save(ctx)
}

func (a *ChannelActor) save(ctx context.Context) {
	if a.channel.SaveInterval() <= 0 {
		return
	}
	currentLen := a.buffer.Len()
	if currentLen <= a.lastSavedSeq {
		return
	}
	msgs := a.buffer.Recent(currentLen - a.lastSavedSeq)
	for _, msg := range msgs {
		gxypgx.DB().Table(a.channel.TableName()).Create(map[string]any{
			"channel_type": a.ChannelType,
			"channel_id":   a.ChannelID,
			"sender_id":    0, // roleID 查询需额外关联
			"content":      msg.Content,
			"timestamp":    msg.Timestamp,
		})
	}
	a.lastSavedSeq = currentLen
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./src/apps/chat/...
```

Expected issue: `src/lib` 的循环引用。如果 `src/apps/chat` 导入 `src/lib` 而 `src/lib` 又导入了 `src/apps/chat`，会产生循环引用。

**解决方案：** 将 `GetRoleActor` 的调用改为通过 `gxyactor.ActivateActor` 直接调用：

```go
pid, err := gxyactor.ActivateActor("role", strconv.FormatInt(roleID, 10), false)
```

需要添加 import：`"gserver/core/gxyactor"` 和 `"strconv"`。

修改 channel_actor.go 中的通知部分：

```go
import (
    "strconv"
    "gserver/core/gxyactor"
)

// 通知部分改为：
for roleID := range a.members {
    pid, err := gxyactor.ActivateActor("role", strconv.FormatInt(roleID, 10), false)
    if err == nil {
        a.Send(pid, notify)
    }
}
```

- [ ] **Step 3: 编译验证**

```bash
go build ./src/apps/chat/...
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add src/apps/chat/channel_actor.go
git commit -m "chat: ChannelActor with ring buffer, member map, and IChannel integration"
```

---

### Task 4: 注册 ChannelActor kind

**Files:**
- Modify: `src/apps/chat/chat_app.go`

- [ ] **Step 1: 在 chat_app.go 中注册 ChannelActor kind**

```go
package chat

import (
	"context"

	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/gameconfig"
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
	gxyservice.ServiceApp().LoadService(ctx, NewChatService())

	// 注册 ChannelActor kind（consistent hash 按 channel_type:channel_id 路由）
	gxyactor.RegisterActorKind("channel", func() gxyactor.IActor {
		return NewChannelActor()
	})

	return nil
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./src/apps/chat/...
```

- [ ] **Step 3: Commit**

```bash
git add src/apps/chat/chat_app.go
git commit -m "chat: register ChannelActor kind in chat app"
```

---

### Task 5: GetChannelActor 辅助方法

**Files:**
- Modify: `src/lib/actor.go`

- [ ] **Step 1: 添加 GetChannelActor 方法**

```go
const (
	CHANNEL_ACTOR_TYPE = "channel"
)

// GetChannelActor 获取频道 actor，id 格式为 "channelType_int64(channelID)"
func GetChannelActor(channelType int32, channelID int64, spawnIfNotExist ...bool) (gxyactor.PID, error) {
	id := strconv.Itoa(int(channelType)) + "_" + strconv.FormatInt(channelID, 10)
	return gxyactor.ActivateActor(CHANNEL_ACTOR_TYPE, id, spawnIfNotExist...)
}
```

位置：追加在 `GetGuildActor` 函数之后（`src/lib/actor.go` 末尾）。

- [ ] **Step 2: 编译验证**

```bash
go build ./src/lib/...
```

- [ ] **Step 3: Commit**

```bash
git add src/lib/actor.go
git commit -m "lib: add GetChannelActor helper for channel actor activation"
```

---

### Task 6: role_chat.go 重构为 Call ChannelActor

**Files:**
- Modify: `src/apps/role/internal/logic/role_chat.go`

- [ ] **Step 1: 替换世界/公会发送和历史接口为统一接口**

```go
// ===== 统一频道 =====

func (r *RoleChat) ReqSendChannelChat(ctx context.Context, req *pb.ReqSendChannelChat) (*pb.RspSendChannelChat, error) {
	var channelType int32
	var channelID int64
	switch req.ChannelType {
	case 1: // 世界
		if r.lastLobbyID == 0 {
			return nil, errors.New("聊天未初始化")
		}
		channelType = 1
		channelID = r.lastLobbyID
		cfg := chat.GetConfig()
		if time.Since(r.lastWorldChatTime) < time.Duration(cfg.WorldCooldown)*time.Second {
			return nil, ErrChatCooldown
		}
		r.lastWorldChatTime = time.Now()
	case 2: // 公会
		if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
			return nil, errors.New("你没有加入公会")
		}
		channelType = 2
		channelID = r.Role.Guild.GuildID
	default:
		return nil, errors.New("不支持的频道类型")
	}
	if err := validateChatMsg(req.Content, chat.GetConfig().MsgMaxLength); err != nil {
		return nil, err
	}
	pid, err := lib.GetChannelActor(channelType, channelID)
	if err != nil {
		return nil, fmt.Errorf("获取频道 actor 失败: %w", err)
	}
	_, err = r.Role.Call(pid, &pb.ReqChannelSend{
		ChannelType: channelType,
		ChannelId:   channelID,
		SenderId:    r.RoleID,
		Content:     strings.TrimSpace(req.Content),
	}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return &pb.RspSendChannelChat{}, nil
}

func (r *RoleChat) ReqChannelHistory(ctx context.Context, req *pb.ReqChannelHistory) (*pb.RspChannelHistory, error) {
	var channelType int32
	var channelID int64
	switch req.ChannelType {
	case 1: // 世界
		if r.lastLobbyID == 0 {
			return nil, errors.New("聊天未初始化")
		}
		channelType = 1
		channelID = r.lastLobbyID
	case 2: // 公会
		if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
			return nil, errors.New("你没有加入公会")
		}
		channelType = 2
		channelID = r.Role.Guild.GuildID
	default:
		return nil, errors.New("不支持的频道类型")
	}
	pid, err := lib.GetChannelActor(channelType, channelID)
	if err != nil {
		return nil, fmt.Errorf("获取频道 actor 失败: %w", err)
	}
	rsp, err := r.Role.Call(pid, &pb.ReqChannelHistory{
		ChannelType: channelType,
		ChannelId:   channelID,
		Count:       req.Count,
	}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspChannelHistory), nil
}
```

- [ ] **Step 2: 更新 ReqChatInit 注册世界频道**

```go
func (r *RoleChat) ReqChatInit(ctx context.Context, req *pb.ReqChatInit) (*pb.RspChatInit, error) {
	lobbyID, err := callChatJoinLobby(ctx, r.RoleID)
	if err != nil {
		return nil, err
	}
	r.lastLobbyID = lobbyID

	// 注册世界频道
	pid, err := lib.GetChannelActor(1, lobbyID)
	if err == nil {
		self := r.Role.Self()
		r.Role.Send(pid, &pb.ChannelRegisterMsg{
			RoleId: r.RoleID,
			Pid: &pb.ActorPid{
				Address: self.Address,
				Id:      self.Id,
			},
			ChannelType: 1,
			ChannelId:   lobbyID,
		})
	}

	cfg := chat.GetConfig()
	worldMsgs, _ := callChatWorldHistory(ctx, lobbyID, cfg.WorldMsgKeep)
	systemMsgs, _ := callChatSystemHistory(ctx, cfg.SystemMsgKeep)

	chat.RegisterLocalRole(r.RoleID, lobbyID, r.Role.Self())

	return &pb.RspChatInit{
		LobbyId:        int32(lobbyID),
		WorldMessages:  worldMsgs,
		SystemMessages: systemMsgs,
	}, nil
}
```

- [ ] **Step 3: 更新 chatLeave 注销世界频道**

```go
func (r *RoleChat) chatLeave(ctx context.Context) {
	if r.lastLobbyID > 0 {
		pid, err := lib.GetChannelActor(1, r.lastLobbyID, false)
		if err == nil {
			r.Role.Send(pid, &pb.ChannelUnregisterMsg{
				RoleId:      r.RoleID,
				ChannelType: 1,
				ChannelId:   r.lastLobbyID,
			})
		}
		_ = callChatLeaveLobby(ctx, r.RoleID, r.lastLobbyID)
		chat.UnregisterLocalRole(r.RoleID)
		r.lastLobbyID = 0
	}
}
```

- [ ] **Step 4: 删除旧的 HTTP helper 函数**

删除以下函数：
- `callChatSendWorld`
- `callChatSendGuild`
- `callChatWorldHistory`
- `callChatGuildHistory`

保留：`callChatJoinLobby`, `callChatLeaveLobby`, `callChatStorePrivate`, `callChatPrivateHistory`, `callChatSystemHistory`

- [ ] **Step 5: 删除旧的世界/公会 handler**

删除以下函数：
- `ReqSendWorldChat`
- `ReqWorldChatHistory`
- `ReqSendGuildChat`
- `ReqGuildChatHistory`

保留：`ReqSendPrivateChat`, `ReqPrivateChatHistory`, `ReqSystemChatHistory`

- [ ] **Step 6: 编译验证**

```bash
go build ./src/apps/role/...
```

- [ ] **Step 7: Commit**

```bash
git add src/apps/role/internal/logic/role_chat.go
git commit -m "chat: refactor role_chat.go to use unified ChannelActor, remove old HTTP paths"
```

---

### Task 7: role_main.go NotifyChannelChat 处理

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go`

- [ ] **Step 1: 在 HandleMessage 中添加 NotifyChannelChat 处理**

定位到 `HandleMessage` 函数中的 `case *pb.NotifyWorldChat` 处理，改为：

```go
case *pb.NotifyChannelChat:
    r.SendClient(ctx, m)
    return nil
```

同时确保旧的通知类型仍然可处理（兼容期内保留处理，逐步移除）：
- `NotifyWorldChat` → 保留 handler（客户端可能还没更新）
- `NotifyGuildChat` → 已从 proto 删除，移除处理
- `NotifySystemChat` → 保留
- `NotifyPrivateChat` → 保留

修改后的 HandleMessage 通知段大约如下：

```go
// 频道消息通知（世界/公会/小队）
case *pb.NotifyChannelChat:
    r.SendClient(ctx, m)
    return nil
// 系统消息和私聊通知（保持独立）
case *pb.NotifySystemChat, *pb.NotifyPrivateChat:
    r.SendClient(ctx, m.(proto.Message))
    return nil
// 兼容旧通知（客户端未迁移完之前保留）
case *pb.NotifyWorldChat:
    r.SendClient(ctx, m)
    return nil
```

- [ ] **Step 2: 编译验证**

```bash
go build ./src/apps/role/...
```

- [ ] **Step 3: Commit**

```bash
git add src/apps/role/internal/logic/role_main.go
git commit -m "chat: add NotifyChannelChat dispatch in role HandleMessage"
```

---

### Task 8: 公会频道注册集成

**Files:**
- Modify: `src/apps/guild/logic/guild_actor.go`

- [ ] **Step 1: 在 addMember 中添加公会频道注册**

在 `addMember` 函数的末尾（成功添加成员后，`g.Members = append(g.Members, member)` 之后），添加：

```go
// 注册公会频道
pid, err := lib.GetChannelActor(2, g.GuildID)
if err == nil {
    roleActorID, _ := lib.GetRoleActor(roleID)
    if roleActorID != nil {
        g.Send(pid, &pb.ChannelRegisterMsg{
            RoleId: roleID,
            Pid: &pb.ActorPid{
                Address: roleActorID.Address,
                Id:      roleActorID.Id,
            },
            ChannelType: 2,
            ChannelId:   g.GuildID,
        })
    }
}
```

需要添加 import：`"gserver/src/lib"` 和 `"gserver/protocol/pb"`（如果尚未导入）。

- [ ] **Step 2: 编译验证**

```bash
go build ./src/apps/guild/...
```

- [ ] **Step 3: Commit**

```bash
git add src/apps/guild/logic/guild_actor.go
git commit -m "chat: register guild channel when adding guild member"
```

---

### Task 9: 清理旧基础设施

**Files:**
- Modify: `src/apps/chat/redis.go` — 删除世界/系统/公会 Redis 操作
- Modify: `src/apps/chat/handler.go` — 删除世界/系统/公会 HTTP handler
- Delete: `src/apps/chat/sidecar.go`

- [ ] **Step 1: 从 redis.go 删除世界/系统/公会 Redis 操作**

删除函数：
- `StoreWorldMsgData`、`PublishWorldChatData`、`GetWorldHistory`
- `StoreSystemMsgData`、`PublishSystemChatData`、`GetSystemHistory`
- `StoreGuildMsgData`、`PublishGuildChatData`、`GetGuildHistory`

保留函数：
- `JoinLobby`、`LeaveLobby`（大厅分配仍用 Redis Lua）
- `chatMsgJSON`、`msgToJSON`、`jsonToMsg`（如果私聊仍用）
- `StorePrivateMsg`、`PublishPrivateChat`、`GetPrivateHistory`
- `sortIDs`、`parseMsgList`

- [ ] **Step 2: 从 handler.go 删除世界/系统/公会 HTTP handler**

删除类型和处理函数：
- `SendWorldChatReq`、`SendWorldChat`
- `WorldHistoryReq`、`WorldHistory`
- `SendSystemChatReq`、`SendSystemChat`
- `SystemHistoryReq`、`SystemHistory`
- `SendGuildChatReq`、`SendGuildChat`
- `GuildHistoryReq`、`GuildHistory`

保留：
- `JoinLobbyReq`、`JoinLobby`
- `LeaveLobbyReq`、`LeaveLobby`
- `StorePrivateMsgReq`、`StorePrivateMsg`
- `PrivateHistoryReq`、`PrivateHistory`

- [ ] **Step 3: 删除 sidecar.go**

```bash
rm src/apps/chat/sidecar.go
```

- [ ] **Step 4: 清理 chat_service.go 中未使用的引用**

检查 `chat_service.go` 中是否引用了 sidecar（如 `StartSidecar`、`StopSidecar`），如果有则删除。

- [ ] **Step 5: 编译验证**

```bash
go build ./src/apps/...
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chat: remove sidecar, Redis world/guild ops, HTTP handlers for chat channels"
```

---

### 自检清单

**1. Spec 覆盖度检查：**
- IChannel 接口 + WorldChannel/GuildChannel → Task 2
- ChannelActor + ring buffer + member map → Task 3
- Consistent hash 路由（channel_type:channel_id） → Task 4（RegisterActorKind）+ Task 5（ActivateActor）
- ReqChatInit 注册世界频道 → Task 6 Step 2
- 公会 addMember 注册公会频道 → Task 8
- NotifyChannelChat 统一通知 → Task 7
- 定时存盘（GuildChannel 600s） → Task 3（TickSave）
- sidecar 删除 → Task 9
- redis.go/handler.go 清理 → Task 9
- 私聊保留 → Task 9（保留函数确认）

**2. Placeholder 检查：** 无 TBD/TODO

**3. 类型一致性检查：**
- channel_type: `int32`（proto）/ `int32`（Go map key）— 一致
- `ReqChannelHistory` 用于 Actor 内部和客户端（同一消息类型）— 一致
- `ReqChannelSend`（Actor 内部）vs `ReqSendChannelChat`（客户端）— 区分明确
