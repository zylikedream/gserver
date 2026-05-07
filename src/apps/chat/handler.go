package chat

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"gserver/core/gxyhttp"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/frame/g"
)

type ChatHandler struct {
	g.Meta `method:"POST"`
}

// ===== 大厅 =====

type JoinLobbyReq struct {
	g.Meta `path:"/join_lobby"`
	RoleID int64 `p:"role_id" v:"required"`
}

func (h *ChatHandler) JoinLobby(ctx context.Context, req *JoinLobbyReq) (any, error) {
	lobbyID, err := JoinLobby(ctx, req.RoleID)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return map[string]string{"lobby_id": strconv.FormatInt(lobbyID, 10)}, nil
}

type LeaveLobbyReq struct {
	g.Meta `path:"/leave_lobby"`
	RoleID  int64 `p:"role_id" v:"required"`
	LobbyID int64 `p:"lobby_id" v:"required"`
}

func (h *ChatHandler) LeaveLobby(ctx context.Context, req *LeaveLobbyReq) (any, error) {
	if err := LeaveLobby(ctx, req.RoleID, req.LobbyID); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return nil, nil
}

// ===== 世界频道 =====

type SendWorldChatReq struct {
	g.Meta  `path:"/send_world"`
	Sender  string `p:"sender" v:"required"`
	Content string `p:"content" v:"required"`
	LobbyID int64  `p:"lobby_id" v:"required"`
}

func (h *ChatHandler) SendWorldChat(ctx context.Context, req *SendWorldChatReq) (any, error) {
	cfg := GetConfig()
	trimmed := strings.TrimSpace(req.Content)
	if trimmed == "" {
		return nil, gxyhttp.NewErrCode(1, "消息不能为空")
	}
	if len([]rune(trimmed)) > cfg.MsgMaxLength {
		return nil, gxyhttp.NewErrCode(1, "消息超过字数限制")
	}
	var sender *pb.PRolePublic
	if err := json.Unmarshal([]byte(req.Sender), &sender); err != nil {
		return nil, gxyhttp.NewErrCode(1, "parse sender error")
	}
	msg := &chatMsgJSON{Sender: sender, Content: trimmed, Timestamp: time.Now().Unix()}
	data := msgToJSON(msg)
	if err := StoreWorldMsgData(ctx, data, req.LobbyID); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	if err := PublishWorldChatData(ctx, data, req.LobbyID); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return nil, nil
}

type WorldHistoryReq struct {
	g.Meta  `path:"/world_history"`
	LobbyID int64 `p:"lobby_id" v:"required"`
	Count   int   `p:"count"`
}

func (h *ChatHandler) WorldHistory(ctx context.Context, req *WorldHistoryReq) (any, error) {
	cfg := GetConfig()
	count := req.Count
	if count <= 0 {
		count = cfg.WorldMsgKeep
	}
	msgs, err := GetWorldHistory(ctx, req.LobbyID, count)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return msgs, nil
}

// ===== 系统频道 =====

type SendSystemChatReq struct {
	g.Meta  `path:"/send_system"`
	Sender  string `p:"sender"`
	Content string `p:"content" v:"required"`
}

func (h *ChatHandler) SendSystemChat(ctx context.Context, req *SendSystemChatReq) (any, error) {
	var sender *pb.PRolePublic
	if req.Sender != "" {
		_ = json.Unmarshal([]byte(req.Sender), &sender)
	}
	msg := &chatMsgJSON{Sender: sender, Content: strings.TrimSpace(req.Content), Timestamp: time.Now().Unix()}
	data := msgToJSON(msg)
	if err := StoreSystemMsgData(ctx, data); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	if err := PublishSystemChatData(ctx, data); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return nil, nil
}

type SystemHistoryReq struct {
	g.Meta `path:"/system_history"`
	Count  int `p:"count"`
}

func (h *ChatHandler) SystemHistory(ctx context.Context, req *SystemHistoryReq) (any, error) {
	cfg := GetConfig()
	count := req.Count
	if count <= 0 {
		count = cfg.SystemMsgKeep
	}
	msgs, err := GetSystemHistory(ctx, count)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return msgs, nil
}

// ===== 私聊 =====

type StorePrivateMsgReq struct {
	g.Meta   `path:"/store_private"`
	Sender   string `p:"sender" v:"required"`
	TargetID int64  `p:"target_id" v:"required"`
	Content  string `p:"content" v:"required"`
}

func (h *ChatHandler) StorePrivateMsg(ctx context.Context, req *StorePrivateMsgReq) (any, error) {
	var sender *pb.PRolePublic
	if err := json.Unmarshal([]byte(req.Sender), &sender); err != nil {
		return nil, gxyhttp.NewErrCode(1, "parse sender error")
	}
	ts, err := StorePrivateMsg(ctx, sender.GetRoleId(), req.TargetID, strings.TrimSpace(req.Content))
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	notify := &chatMsgJSON{Sender: sender, Content: strings.TrimSpace(req.Content), Timestamp: ts}
	_ = PublishPrivateChat(ctx, req.TargetID, msgToJSON(notify))
	return map[string]int64{"timestamp": ts}, nil
}

type PrivateHistoryReq struct {
	g.Meta   `path:"/private_history"`
	RoleID   int64 `p:"role_id" v:"required"`
	FriendID int64 `p:"friend_id" v:"required"`
	Count    int   `p:"count"`
}

func (h *ChatHandler) PrivateHistory(ctx context.Context, req *PrivateHistoryReq) (any, error) {
	count := req.Count
	if count <= 0 {
		count = 50
	}
	msgs, err := GetPrivateHistory(ctx, req.RoleID, req.FriendID, count)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return msgs, nil
}
