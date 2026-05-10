package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	"gserver/protocol/pb"
	"gserver/src/apps/chat"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/os/glog"
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

func (r *RoleChat) OnModStart(ctx context.Context) error {
	if r.lastLobbyID > 0 {
		return nil
	}
	_, err := r.joinWorldChannel(ctx)
	if err != nil {
		glog.Warningf(ctx, "join world channel failed: %v", err)
	}
	return nil
}

func (r *RoleChat) OnCreate(ctx context.Context) {}

func (r *RoleChat) OnModStop(ctx context.Context) error {
	r.leaveWorldChannel(ctx)
	return nil
}

// ===== Proto Handlers =====

func (r *RoleChat) joinWorldChannel(ctx context.Context) (gxyactor.PID, error) {
	lobbyID, err := callChatJoinLobby(ctx, r.RoleID)
	if err != nil {
		return nil, err
	}
	r.lastLobbyID = lobbyID
	channel, err := r.JoinChannel(pb.ChannelType_CHANNEL_TYPE_WORLD, lobbyID)
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func (r *RoleChat) JoinChannel(channelType pb.ChannelType, channelID int64) (gxyactor.PID, error) {
	if channelID < 0 {
		return nil, errors.New("channelID 不能小于 0")
	}
	channel, err := lib.GetChannelActor(int32(channelType), channelID)
	if err != nil {
		return nil, err
	}
	self := r.Role.Self()
	r.Role.Send(channel, &pb.ChannelRegisterMsg{
		RoleId: r.RoleID,
		Pid: &pb.ActorPid{
			Address: self.Address,
			Id:      self.Id,
		},
		ChannelType: int32(channelType),
		ChannelId:   channelID,
	})
	return channel, nil
}

func (r *RoleChat) ReqChatInit(ctx context.Context, req *pb.ReqChatInit) (*pb.RspChatInit, error) {
	channel, err := r.joinWorldChannel(ctx)
	if err != nil {
		return nil, err
	}
	history, err := r.Role.Call(channel, &pb.ReqChatChannelHistory{
		ChannelType: pb.ChannelType_CHANNEL_TYPE_WORLD,
		ChannelId:   r.lastLobbyID,
		Count:       50,
	}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	rsp := &pb.RspChatInit{
		WorldMessages: history.(*pb.RspChatChannelHistory).Messages,
	}
	return rsp, nil
}

func (r *RoleChat) ReqChatSendChannel(ctx context.Context, req *pb.ReqChatSendChannel) (*pb.RspChatSendChannel, error) {
	var channelID int64
	switch req.ChannelType {
	case pb.ChannelType_CHANNEL_TYPE_WORLD:
		if r.lastLobbyID == 0 {
			return nil, errors.New("聊天未初始化")
		}
		channelID = r.lastLobbyID
		cfg := chat.GetConfig()
		if time.Since(r.lastWorldChatTime) < time.Duration(cfg.WorldCooldown)*time.Second {
			return nil, ErrChatCooldown
		}
		r.lastWorldChatTime = time.Now()
	case pb.ChannelType_CHANNEL_TYPE_GUILD:
		if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
			return nil, errors.New("你没有加入公会")
		}
		channelID = r.Role.Guild.GuildID
	default:
		return nil, errors.New("不支持的频道类型")
	}
	if err := validateChatMsg(req.Content, chat.GetConfig().MsgMaxLength); err != nil {
		return nil, err
	}
	channelType := int32(req.ChannelType)
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
	return &pb.RspChatSendChannel{}, nil
}

func (r *RoleChat) ReqChatChannelHistory(ctx context.Context, req *pb.ReqChatChannelHistory) (*pb.RspChatChannelHistory, error) {
	var channelID int64
	switch req.ChannelType {
	case pb.ChannelType_CHANNEL_TYPE_WORLD:
		if r.lastLobbyID == 0 {
			return nil, errors.New("聊天未初始化")
		}
		channelID = r.lastLobbyID
	case pb.ChannelType_CHANNEL_TYPE_GUILD:
		if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
			return nil, errors.New("你没有加入公会")
		}
		channelID = r.Role.Guild.GuildID
	default:
		return nil, errors.New("不支持的频道类型")
	}
	channelType := int32(req.ChannelType)
	pid, err := lib.GetChannelActor(channelType, channelID)
	if err != nil {
		return nil, fmt.Errorf("获取频道 actor 失败: %w", err)
	}
	rsp, err := r.Role.Call(pid, &pb.ReqChatChannelHistory{
		ChannelType: pb.ChannelType(channelType),
		ChannelId:   channelID,
		Count:       req.Count,
	}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspChatChannelHistory), nil
}

func (r *RoleChat) ReqChatSendPrivate(ctx context.Context, req *pb.ReqChatSendPrivate) (*pb.RspChatSendPrivate, error) {
	cfg := chat.GetConfig()

	if err := validateChatMsg(req.Content, cfg.MsgMaxLength); err != nil {
		return nil, err
	}

	if !isFriend(ctx, r.RoleID, req.TargetId) {
		return nil, ErrChatNotFriend
	}

	ts, err := callChatStorePrivate(ctx, r.Role.Public.GetRolePublic(ctx),
		req.TargetId, strings.TrimSpace(req.Content))
	if err != nil {
		return nil, err
	}

	// 通知目标角色
	if targetPid, err := lib.GetRoleActor(req.TargetId, false); err == nil {
		r.Role.Send(targetPid, &pb.NotifyChatPrivate{
			Message: &pb.PChatMsg{
				Sender:    r.Role.Public.GetRolePublic(ctx),
				Content:   strings.TrimSpace(req.Content),
				Timestamp: ts,
			},
		})
	}

	return &pb.RspChatSendPrivate{}, nil
}

func (r *RoleChat) ReqChatPrivateHistory(ctx context.Context, req *pb.ReqChatPrivateHistory) (*pb.RspChatPrivateHistory, error) {
	count := int(req.Count)
	if count <= 0 {
		count = 50
	}
	msgs, err := callChatPrivateHistory(ctx, r.RoleID, req.FriendId, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspChatPrivateHistory{Messages: msgs}, nil
}

func (r *RoleChat) ReqChatSystemHistory(ctx context.Context, req *pb.ReqChatSystemHistory) (*pb.RspChatSystemHistory, error) {
	count := int(req.Count)
	if count <= 0 {
		count = chat.GetConfig().SystemMsgKeep
	}
	msgs, err := callChatSystemHistory(ctx, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspChatSystemHistory{Messages: msgs}, nil
}

// ===== Internal =====

func (r *RoleChat) leaveWorldChannel(ctx context.Context) {
	// 从频道 actor 注销
	err := callChatLeaveLobby(ctx, r.RoleID, r.lastLobbyID)
	if err != nil {
		glog.Warningf(ctx, "leaveChannel: leave lobby failederr=%v", err)
	}
	r.LeaveChannel(ctx, pb.ChannelType_CHANNEL_TYPE_WORLD, r.lastLobbyID)
	r.lastLobbyID = 0
}

func (r *RoleChat) LeaveChannel(ctx context.Context, channelType pb.ChannelType, channelID int64) {
	if channelID < 0 {
		return
	}
	pid, err := lib.GetChannelActor(int32(channelType), channelID, false)
	if err != nil {
		glog.Warningf(ctx, "leaveChannel: get actor failed, channelType=%d, channelID=%d, err=%v", channelType, channelID, err)
		return
	}
	r.Role.Send(pid, &pb.ChannelUnregisterMsg{
		RoleId:      r.RoleID,
		ChannelType: int32(channelType),
		ChannelId:   channelID,
	})
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
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat-http",
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
	_, err := gxyhttp.HttpSystem().PostService(ctx, "chat-http",
		fmt.Sprintf("leave_lobby?role_id=%d&lobby_id=%d", roleID, lobbyID))
	return err
}

func callChatStorePrivate(ctx context.Context, sender *pb.PRolePublic, targetID int64, content string) (int64, error) {
	sj, _ := json.Marshal(sender)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat-http",
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
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat-http",
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
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "chat-http",
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
