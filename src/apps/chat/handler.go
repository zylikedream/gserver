package chat

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gserver/core/gxyhttp"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
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

func (h *ChatHandler) SendWorldChat(r *ghttp.Request) {
	var req struct {
		Sender  *pb.PRolePublic `json:"sender"`
		Content string          `json:"content"`
		LobbyID int64           `json:"lobby_id"`
	}
	if err := r.Parse(&req); err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	cfg := GetConfig()
	trimmed := strings.TrimSpace(req.Content)
	if trimmed == "" {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: "消息不能为空"})
		return
	}
	if len([]rune(trimmed)) > cfg.MsgMaxLength {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: "消息超过字数限制"})
		return
	}
	msg := &chatMsgJSON{Sender: req.Sender, Content: trimmed, Timestamp: time.Now().Unix()}
	data := msgToJSON(msg)
	if err := StoreWorldMsgData(r.Context(), data, req.LobbyID); err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	if err := PublishWorldChatData(r.Context(), data, req.LobbyID); err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	r.Response.WriteJson(gxyhttp.Response{Code: 0})
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

func (h *ChatHandler) SendSystemChat(r *ghttp.Request) {
	var req struct {
		Sender  *pb.PRolePublic `json:"sender"`
		Content string          `json:"content"`
	}
	if err := r.Parse(&req); err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	msg := &chatMsgJSON{Sender: req.Sender, Content: strings.TrimSpace(req.Content), Timestamp: time.Now().Unix()}
	data := msgToJSON(msg)
	if err := StoreSystemMsgData(r.Context(), data); err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	if err := PublishSystemChatData(r.Context(), data); err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	r.Response.WriteJson(gxyhttp.Response{Code: 0})
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

func (h *ChatHandler) StorePrivateMsg(r *ghttp.Request) {
	var req struct {
		Sender   *pb.PRolePublic `json:"sender"`
		TargetID int64           `json:"target_id"`
		Content  string          `json:"content"`
	}
	if err := r.Parse(&req); err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	ts, err := StorePrivateMsg(r.Context(), req.Sender.GetRoleId(), req.TargetID, strings.TrimSpace(req.Content))
	if err != nil {
		r.Response.WriteJson(gxyhttp.Response{Code: 1, Message: err.Error()})
		return
	}
	notify := &chatMsgJSON{Sender: req.Sender, Content: strings.TrimSpace(req.Content), Timestamp: ts}
	_ = PublishPrivateChat(r.Context(), req.TargetID, msgToJSON(notify))
	r.Response.WriteJson(gxyhttp.Response{Code: 0, Data: map[string]int64{"timestamp": ts}})
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
