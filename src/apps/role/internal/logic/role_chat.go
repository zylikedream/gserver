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
	"gserver/src/lib"

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
	var worldMsgs []*pb.PChatMsg

	// 注册到世界频道 actor，并从 ChannelActor 拉取聊天历史
	pid, perr := lib.GetChannelActor(1, lobbyID)
	if perr == nil {
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
		// 拉取世界聊天历史
		if rsp, e := r.Role.Call(pid, &pb.ReqChannelHistory{
			ChannelType: 1,
			ChannelId:   lobbyID,
			Count:       int32(cfg.WorldMsgKeep),
		}, 10*time.Second); e == nil {
			if h, ok := rsp.(*pb.RspChannelHistory); ok {
				worldMsgs = h.Messages
			}
		}
	}

	systemMsgs, _ := callChatSystemHistory(ctx, cfg.SystemMsgKeep)

	chat.RegisterLocalRole(r.RoleID, lobbyID, r.Role.Self())

	return &pb.RspChatInit{
		LobbyId:        int32(lobbyID),
		WorldMessages:  worldMsgs,
		SystemMessages: systemMsgs,
	}, nil
}

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

// ===== Internal =====

func (r *RoleChat) chatLeave(ctx context.Context) {
	if r.lastLobbyID > 0 {
		// 从世界频道 actor 注销
		if pid, err := lib.GetChannelActor(1, r.lastLobbyID, false); err == nil {
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

// ===== HTTP helpers (私聊/系统消息保留) =====

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
