package logic

import (
	"context"
	"errors"
	"strings"
	"time"

	"gserver/core/gxyactor"
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

type RoleChatState struct {
	RolePersistState
}

func (RoleChatState) TableName() string { return "role_chat" }

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
	lobbyID, err := chat.JoinLobby(ctx, r.RoleID)
	if err != nil {
		return nil, err
	}
	r.lastLobbyID = lobbyID

	cfg := chat.GetConfig()
	worldMsgs, _ := chat.GetWorldHistory(ctx, lobbyID, cfg.WorldMsgKeep)
	systemMsgs, _ := chat.GetSystemHistory(ctx, cfg.SystemMsgKeep)

	chat.RegisterRole(r.RoleID, lobbyID, r.Role.Self())

	return &pb.RspChatInit{
		LobbyId:        int32(lobbyID),
		WorldMessages:  worldMsgs,
		SystemMessages: systemMsgs,
	}, nil
}

func (r *RoleChat) ReqSendWorldChat(ctx context.Context, req *pb.ReqSendWorldChat) (*pb.RspSendWorldChat, error) {
	cfg := chat.GetConfig()

	if err := validateChatMsg(req.Content, cfg.MsgMaxLength); err != nil {
		return nil, err
	}

	if time.Since(r.lastWorldChatTime) < time.Duration(cfg.WorldCooldown)*time.Second {
		return nil, ErrChatCooldown
	}
	r.lastWorldChatTime = time.Now()

	msg := &pb.PChatMsg{
		SenderId:   r.RoleID,
		SenderName: r.Role.Basic.RoleName,
		Content:    strings.TrimSpace(req.Content),
		Timestamp:  time.Now().Unix(),
	}

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

	if err := validateChatMsg(req.Content, cfg.MsgMaxLength); err != nil {
		return nil, err
	}

	if !isFriend(ctx, r.RoleID, req.TargetId) {
		return nil, ErrChatNotFriend
	}

	ts, err := chat.StorePrivateMsg(ctx, r.RoleID, req.TargetId, strings.TrimSpace(req.Content))
	if err != nil {
		return nil, err
	}

	pid, err := lib.GetRoleActor(req.TargetId, false)
	if err == nil && pid != nil {
		gxyactor.LocalSend(pid, &pb.NotifyPrivateChat{
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

// ===== Internal =====

func (r *RoleChat) chatLeave(ctx context.Context) {
	if r.lastLobbyID > 0 {
		_ = chat.LeaveLobby(ctx, r.RoleID, r.lastLobbyID)
		chat.UnregisterRole(r.RoleID)
		r.lastLobbyID = 0
	}
}

func validateChatMsg(content string, maxLen int) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ErrChatMsgEmpty
	}
	if len([]rune(trimmed)) > maxLen {
		return ErrChatMsgTooLong
	}
	return nil
}
