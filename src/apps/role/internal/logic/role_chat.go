package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gserver/core/gxyhttp"
	"gserver/protocol/pb"
	"gserver/src/apps/chat"

	"github.com/gogf/gf/v2/util/gconv"
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
	lobbyID, err := callChatJoinLobby(ctx, r.RoleID)
	if err != nil {
		return nil, err
	}
	r.lastLobbyID = lobbyID

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

func (r *RoleChat) ReqSendWorldChat(ctx context.Context, req *pb.ReqSendWorldChat) (*pb.RspSendWorldChat, error) {
	if r.lastLobbyID == 0 {
		return nil, errors.New("聊天未初始化")
	}
	cfg := chat.GetConfig()

	if err := validateChatMsg(req.Content, cfg.MsgMaxLength); err != nil {
		return nil, err
	}

	if time.Since(r.lastWorldChatTime) < time.Duration(cfg.WorldCooldown)*time.Second {
		return nil, ErrChatCooldown
	}
	r.lastWorldChatTime = time.Now()

	if err := callChatSendWorld(ctx, r.Role.Public.GetRolePublic(ctx),
		strings.TrimSpace(req.Content), r.lastLobbyID); err != nil {
		return nil, err
	}

	return &pb.RspSendWorldChat{}, nil
}

func (r *RoleChat) ReqWorldChatHistory(ctx context.Context, req *pb.ReqWorldChatHistory) (*pb.RspWorldChatHistory, error) {
	if r.lastLobbyID == 0 {
		return nil, errors.New("聊天未初始化")
	}
	count := int(req.Count)
	if count <= 0 {
		count = chat.GetConfig().WorldMsgKeep
	}
	msgs, err := callChatWorldHistory(ctx, r.lastLobbyID, count)
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

	_, err := callChatStorePrivate(ctx, r.Role.Public.GetRolePublic(ctx),
		req.TargetId, strings.TrimSpace(req.Content))
	if err != nil {
		return nil, err
	}

	return &pb.RspSendPrivateChat{}, nil
}

func (r *RoleChat) ReqPrivateChatHistory(ctx context.Context, req *pb.ReqPrivateChatHistory) (*pb.RspPrivateChatHistory, error) {
	count := int(req.Count)
	if count <= 0 {
		count = 50
	}
	msgs, err := callChatPrivateHistory(ctx, r.RoleID, req.FriendId, count)
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
	msgs, err := callChatSystemHistory(ctx, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspSystemChatHistory{Messages: msgs}, nil
}

// ===== 公会频道 =====

func (r *RoleChat) ReqSendGuildChat(ctx context.Context, req *pb.ReqSendGuildChat) (*pb.RspSendGuildChat, error) {
	if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
		return nil, errors.New("你没有加入公会")
	}
	if req.GuildId != r.Role.Guild.GuildID {
		return nil, errors.New("公会ID不匹配")
	}
	cfg := chat.GetConfig()
	if err := validateChatMsg(req.Content, cfg.MsgMaxLength); err != nil {
		return nil, err
	}
	if err := callChatSendGuild(ctx, r.Role.Public.GetRolePublic(ctx), req.GuildId, req.Content); err != nil {
		return nil, err
	}
	return &pb.RspSendGuildChat{}, nil
}

func (r *RoleChat) ReqGuildChatHistory(ctx context.Context, req *pb.ReqGuildChatHistory) (*pb.RspGuildChatHistory, error) {
	if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
		return nil, errors.New("你没有加入公会")
	}
	if req.GuildId != r.Role.Guild.GuildID {
		return nil, errors.New("公会ID不匹配")
	}
	count := int(req.Count)
	if count <= 0 || count > 50 {
		count = 20
	}
	msgs, err := callChatGuildHistory(ctx, req.GuildId, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspGuildChatHistory{Messages: msgs}, nil
}

// ===== Internal =====

func (r *RoleChat) chatLeave(ctx context.Context) {
	if r.lastLobbyID > 0 {
		_ = callChatLeaveLobby(ctx, r.RoleID, r.lastLobbyID)
		chat.UnregisterLocalRole(r.RoleID)
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

// ===== HTTP helpers =====

func callChatJoinLobby(ctx context.Context, roleID int64) (int64, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("join_lobby?role_id=%d", roleID))
	if err != nil {
		return 0, err
	}
	data := struct {
		LobbyID string `json:"lobby_id"`
	}{}
	if err := gconv.Scan(rsp.Data, &data); err != nil {
		return 0, fmt.Errorf("parse lobby_id: %w", err)
	}
	return gconv.Int64(data.LobbyID), nil
}

func callChatLeaveLobby(ctx context.Context, roleID, lobbyID int64) error {
	_, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("leave_lobby?role_id=%d&lobby_id=%d", roleID, lobbyID))
	return err
}

func callChatSendWorld(ctx context.Context, sender *pb.PRolePublic, content string, lobbyID int64) error {
	sj, _ := json.Marshal(sender)
	_, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("send_world?sender=%s&content=%s&lobby_id=%d",
			url.QueryEscape(string(sj)), url.QueryEscape(content), lobbyID))
	return err
}

func callChatSendGuild(ctx context.Context, sender *pb.PRolePublic, guildID int64, content string) error {
	sj, _ := json.Marshal(sender)
	_, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("send_guild?sender=%s&guild_id=%d&content=%s",
			url.QueryEscape(string(sj)), guildID, url.QueryEscape(content)))
	return err
}

func callChatStorePrivate(ctx context.Context, sender *pb.PRolePublic, targetID int64, content string) (int64, error) {
	sj, _ := json.Marshal(sender)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("store_private?sender=%s&target_id=%d&content=%s",
			url.QueryEscape(string(sj)), targetID, url.QueryEscape(content)))
	if err != nil {
		return 0, err
	}
	data := struct {
		Timestamp int64 `json:"timestamp"`
	}{}
	if err := gconv.Scan(rsp.Data, &data); err != nil {
		return 0, fmt.Errorf("parse timestamp: %w", err)
	}
	return data.Timestamp, nil
}

func callChatWorldHistory(ctx context.Context, lobbyID int64, count int) ([]*pb.PChatMsg, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("world_history?lobby_id=%d&count=%d", lobbyID, count))
	if err != nil {
		return nil, err
	}
	var msgs []*pb.PChatMsg
	if err := gconv.Scan(rsp.Data, &msgs); err != nil {
		return nil, fmt.Errorf("parse world history: %w", err)
	}
	return msgs, nil
}

func callChatPrivateHistory(ctx context.Context, roleID, friendID int64, count int) ([]*pb.PChatMsg, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("private_history?role_id=%d&friend_id=%d&count=%d", roleID, friendID, count))
	if err != nil {
		return nil, err
	}
	var msgs []*pb.PChatMsg
	if err := gconv.Scan(rsp.Data, &msgs); err != nil {
		return nil, fmt.Errorf("parse private history: %w", err)
	}
	return msgs, nil
}

func callChatSystemHistory(ctx context.Context, count int) ([]*pb.PChatMsg, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("system_history?count=%d", count))
	if err != nil {
		return nil, err
	}
	var msgs []*pb.PChatMsg
	if err := gconv.Scan(rsp.Data, &msgs); err != nil {
		return nil, fmt.Errorf("parse system history: %w", err)
	}
	return msgs, nil
}

func callChatGuildHistory(ctx context.Context, guildID int64, count int) ([]*pb.PChatMsg, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat",
		fmt.Sprintf("guild_history?guild_id=%d&count=%d", guildID, count))
	if err != nil {
		return nil, err
	}
	var msgs []*pb.PChatMsg
	if err := gconv.Scan(rsp.Data, &msgs); err != nil {
		return nil, fmt.Errorf("parse guild history: %w", err)
	}
	return msgs, nil
}
