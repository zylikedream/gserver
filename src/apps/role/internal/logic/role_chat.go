package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/protocol/pb"
	"gserver/src/lib"
	"gserver/src/lib/rolelib"

	gamecfg "gserver/gameconfig/gosrc"

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

func (r *RoleChat) AfterLogin(ctx context.Context) {
	if r.lastLobbyID > 0 {
		return
	}
	_, err := r.joinWorldChannel(ctx)
	if err != nil {
		gxylog.Warn(ctx, "join world channel failed", gxylog.Err(err))
	}
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
	channel, err := r.JoinChannel(ctx, int32(gamecfg.GardenEChatChannelType_WORLD), lobbyID)
	if err != nil {
		return nil, err
	}
	r.lastLobbyID = lobbyID
	return channel, nil
}

func (r *RoleChat) JoinChannel(ctx context.Context, channelType int32, channelID int64) (gxyactor.PID, error) {
	if channelID < 0 {
		return nil, errors.New("channelID 不能小于 0")
	}
	channel, err := lib.GetChannelActor(ctx, channelType, channelID)
	if err != nil {
		return nil, err
	}
	self := r.Role.Self()
	gxyactor.Send(ctx, channel, &pb.ChannelRegisterMsg{
		RoleId: r.RoleID,
		Pid: &pb.ActorPid{
			Address: self.Address,
			Id:      self.Id,
		},
		ChannelType: channelType,
		ChannelId:   channelID,
	})
	return channel, nil
}

func (r *RoleChat) ReqChatInit(ctx context.Context, req *pb.ReqChatInit) (*pb.RspChatInit, error) {
	if r.lastLobbyID == 0 {
		_, err := r.joinWorldChannel(ctx)
		if err != nil {
			return nil, err
		}
	}
	var worldMessages []*pb.PChatMsg
	worldHistory, err := r.ReqChatChannelHistory(ctx, &pb.ReqChatChannelHistory{
		ChannelType: int32(gamecfg.GardenEChatChannelType_WORLD),
		ChannelId:   r.lastLobbyID,
		Count:       50,
	})
	if err == nil {
		worldMessages = worldHistory.Messages
	}
	systemMessages, err := callChatSystemHistory(ctx, 50)
	rsp := &pb.RspChatInit{
		WorldMessages:  worldMessages,
		SystemMessages: systemMessages,
		LobbyId:        int32(r.lastLobbyID),
	}
	return rsp, nil
}

func (r *RoleChat) ReqChatSendChannel(ctx context.Context, req *pb.ReqChatSendChannel) (*pb.RspChatSendChannel, error) {
	var channelID int64
	switch req.ChannelType {
	case int32(gamecfg.GardenEChatChannelType_WORLD):
		if r.lastLobbyID == 0 {
			return nil, errors.New("聊天未初始化")
		}
		channelID = r.lastLobbyID
		if time.Since(r.lastWorldChatTime) < time.Duration(r.Cfg().TbChatChannel.Get(1).Cooldown)*time.Second {
			return nil, errors.WithStack(ErrChatCooldown)
		}
		r.lastWorldChatTime = time.Now()
	case int32(gamecfg.GardenEChatChannelType_GUILD):
		if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
			return nil, errors.New("你没有加入公会")
		}
		channelID = r.Role.Guild.GuildID
	default:
		return nil, errors.New("不支持的频道类型")
	}
	if err := validateChatMsg(req.Content, int(r.Cfg().TbChatChannel.Get(1).MessageLimit)); err != nil {
		return nil, err
	}
	channelType := req.ChannelType
	pid, err := lib.GetChannelActor(ctx, channelType, channelID)
	if err != nil {
		return nil, errors.Wrap(err, "获取频道 actor 失败")
	}
	err = gxyactor.Send(ctx, pid, &pb.ReqChannelSend{
		ChannelType: channelType,
		ChannelId:   channelID,
		SenderId:    r.RoleID,
		Content:     strings.TrimSpace(req.Content),
	})
	if err != nil {
		return nil, err
	}
	return &pb.RspChatSendChannel{}, nil
}

func (r *RoleChat) ReqChatChannelHistory(ctx context.Context, req *pb.ReqChatChannelHistory) (*pb.RspChatChannelHistory, error) {
	var channelID int64
	switch req.ChannelType {
	case int32(gamecfg.GardenEChatChannelType_WORLD):
		if r.lastLobbyID == 0 {
			return nil, errors.New("聊天未初始化")
		}
		channelID = r.lastLobbyID
	case int32(gamecfg.GardenEChatChannelType_GUILD):
		if r.Role.Guild == nil || r.Role.Guild.GuildID == 0 {
			return nil, errors.New("你没有加入公会")
		}
		channelID = r.Role.Guild.GuildID
	default:
		return nil, errors.New("不支持的频道类型")
	}
	channelType := req.ChannelType
	pid, err := lib.GetChannelActor(ctx, channelType, channelID)
	if err != nil {
		return nil, errors.Wrap(err, "获取频道 actor 失败")
	}
	rsp, err := gxyactor.Call(ctx, pid, &pb.ReqChatChannelHistory{
		ChannelType: channelType,
		ChannelId:   channelID,
		Count:       req.Count,
	}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspChatChannelHistory), nil
}

func (r *RoleChat) ReqChatSendPrivate(ctx context.Context, req *pb.ReqChatSendPrivate) (*pb.RspChatSendPrivate, error) {
	if err := validateChatMsg(req.Content, int(r.Cfg().TbChatChannel.Get(2).MessageLimit)); err != nil {
		return nil, err
	}

	if !isFriend(ctx, r.DB(), r.RoleID, req.TargetId) {
		return nil, errors.WithStack(ErrChatNotFriend)
	}

	ts, err := callChatStorePrivate(ctx, r.Role.Public.GetRolePublic(ctx),
		req.TargetId, strings.TrimSpace(req.Content))
	if err != nil {
		return nil, err
	}

	// 私聊正文已由 chat-http 持久化；在线通知走 RoleNotify，避免 role 节点互相直连。
	if err := rolelib.PublishRoleNotify(ctx, req.TargetId, &pb.NotifyChatPrivate{
		Message: &pb.PChatMsg{
			Sender:    r.Role.Public.GetRolePublic(ctx),
			Content:   strings.TrimSpace(req.Content),
			Timestamp: ts,
		},
	}); err != nil {
		gxylog.Warn(ctx, "publish private chat notify failed", gxylog.Num("target", req.TargetId), gxylog.Err(err))
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
		count = int(r.Cfg().TbChatChannel.Get(3).HistoryLimit)
	}
	msgs, err := callChatSystemHistory(ctx, count)
	if err != nil {
		return nil, err
	}
	return &pb.RspChatSystemHistory{Messages: msgs}, nil
}

// ===== Internal =====

func (r *RoleChat) leaveWorldChannel(ctx context.Context) {
	if r.lastLobbyID == 0 {
		return
	}
	// 从频道 actor 注销
	err := callChatLeaveLobby(ctx, r.RoleID, r.lastLobbyID)
	if err != nil {
		gxylog.Warn(ctx, "leaveChannel: leave lobby failed", gxylog.Err(err))
	}
	r.LeaveChannel(ctx, int32(gamecfg.GardenEChatChannelType_WORLD), r.lastLobbyID)
	r.lastLobbyID = 0
}

func (r *RoleChat) LeaveChannel(ctx context.Context, channelType int32, channelID int64) {
	if channelID < 0 {
		return
	}
	pid, err := lib.GetChannelActor(ctx, channelType, channelID)
	if err != nil {
		gxylog.Warn(ctx, "leaveChannel: get actor failed", gxylog.Num("channelType", channelType), gxylog.Num("channelID", channelID), gxylog.Err(err))
		return
	}
	gxyactor.Send(ctx, pid, &pb.ChannelUnregisterMsg{
		RoleId:      r.RoleID,
		ChannelType: channelType,
		ChannelId:   channelID,
	})
}

func validateChatMsg(content string, maxLen int) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return errors.WithStack(ErrChatMsgEmpty)
	}
	if len([]rune(trimmed)) > maxLen {
		return errors.WithStack(ErrChatMsgTooLong)
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
		return 0, errors.Wrap(err, "parse lobby_id")
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
		return 0, errors.Wrap(err, "parse timestamp")
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
		return nil, errors.Wrap(err, "parse private history")
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
		return nil, errors.Wrap(err, "parse system history")
	}
	return msgs, nil
}
