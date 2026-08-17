package chat

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"gserver/core/gxyhttp"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"
	"gserver/src/lib"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"

	"github.com/gogf/gf/v2/frame/g"
)

type ChatHandler struct {
	g.Meta `method:"POST"`
	d      deps.Deps
}

// NewChatHandler 构造注入依赖(组装根)。
func NewChatHandler() *ChatHandler {
	return &ChatHandler{d: deps.Deps{DB: gxypgx.DB(), Redis: gxyredis.Redis(), Cfg: gameconfig.Get()}}
}

// ===== 大厅 =====

type JoinLobbyReq struct {
	g.Meta `path:"/join_lobby"`
	RoleID int64 `p:"role_id" v:"required"`
}

func (h *ChatHandler) JoinLobby(ctx context.Context, req *JoinLobbyReq) (any, error) {
	lobbyID, err := JoinLobby(ctx, h.d, req.RoleID)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return map[string]string{"lobby_id": strconv.FormatInt(lobbyID, 10)}, nil
}

type LeaveLobbyReq struct {
	g.Meta  `path:"/leave_lobby"`
	RoleID  int64 `p:"role_id" v:"required"`
	LobbyID int64 `p:"lobby_id" v:"required"`
}

func (h *ChatHandler) LeaveLobby(ctx context.Context, req *LeaveLobbyReq) (any, error) {
	if err := LeaveLobby(ctx, h.d, req.RoleID, req.LobbyID); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return nil, nil
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
	if sender == nil || sender.GetRoleId() <= 0 {
		return nil, gxyhttp.NewErrCode(1, "invalid sender role_id")
	}
	ts, err := StorePrivateMsg(ctx, h.d, sender.GetRoleId(), req.TargetID, strings.TrimSpace(req.Content))
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
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
	msgs, err := GetPrivateHistory(ctx, h.d, req.RoleID, req.FriendID, count)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return msgs, nil
}

// ===== 系统消息 =====

type StoreSystemMsgReq struct {
	g.Meta  `path:"/store_system"`
	Content string `p:"content" v:"required"`
}

func (h *ChatHandler) StoreSystemMsg(ctx context.Context, req *StoreSystemMsgReq) (any, error) {
	content := strings.TrimSpace(req.Content)
	ts, err := StoreSystemMsg(ctx, h.d, content)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	// 发布全服广播
	_ = lib.Publish(ctx, "role", &lib.BroadcastMsg{
		MsgType: lib.BroadCastTypeSystemMsg,
		Data:    content,
	})
	return map[string]int64{"timestamp": ts}, nil
}

type SystemHistoryReq struct {
	g.Meta `path:"/system_history"`
	Count  int `p:"count"`
}

func (h *ChatHandler) SystemHistory(ctx context.Context, req *SystemHistoryReq) (any, error) {
	count := req.Count
	if count <= 0 {
		count = 50
	}
	msgs, err := GetSystemHistory(ctx, h.d, count)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return msgs, nil
}
