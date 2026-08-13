package logic

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/protocol/pb"
	"gserver/src/lib/guildlib"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/util/gconv"
	"google.golang.org/protobuf/proto"
)

// ===== 子模块 =====

type RoleGuild struct {
	RoleModule
	GuildID       int64
	lastChannelID int64
}

var _ IRoleModule = (*RoleGuild)(nil)

func (r *RoleGuild) PersistState() IPersistState { return nil }

func (r *RoleGuild) OnModInit(ctx context.Context) error { return nil }

func (r *RoleGuild) OnModStart(ctx context.Context) error {
	return r.ReloadGuildID(ctx)
}

func (r *RoleGuild) OnCreate(ctx context.Context) {}

func (r *RoleGuild) AfterLogin(ctx context.Context) {
	if r.lastChannelID > 0 {
		return
	}
	r.JoinGuildChannel(ctx)
}

func (r *RoleGuild) JoinGuildChannel(ctx context.Context) {
	if r.GuildID <= 0 {
		return
	}
	if _, err := r.Role.Chat.JoinChannel(ctx, int32(gamecfg.GardenEChatChannelType_GUILD), r.GuildID); err != nil {
		gxylog.Error(ctx, "加入公会聊天失败", gxylog.Err(err))
	} else {
		r.lastChannelID = r.GuildID
	}
}

func (r *RoleGuild) OnModStop(ctx context.Context) error {
	r.leaveGuildChannel(ctx)
	return nil
}

func (r *RoleGuild) leaveGuildChannel(ctx context.Context) {
	if r.lastChannelID > 0 {
		r.Role.Chat.LeaveChannel(ctx, int32(gamecfg.GardenEChatChannelType_GUILD), r.lastChannelID)
		r.lastChannelID = 0
	}
}

func (r *RoleGuild) SetGuildID(ctx context.Context, guildID int64) {
	if r.GuildID == guildID {
		return
	}
	r.leaveGuildChannel(ctx)
	r.GuildID = guildID
	r.JoinGuildChannel(ctx)
}

func (r *RoleGuild) ReloadGuildID(ctx context.Context) error {
	guildID, err := guildlib.GetGuildIDByRoleID(ctx, r.RoleID)
	if err != nil {
		return err
	}
	r.SetGuildID(ctx, guildID)
	return nil
}

// ===== HTTP helpers =====

func callGuildCreate(ctx context.Context, leaderID int64, name, declaration, icon string, needApproval bool) (int64, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "guild-http",
		fmt.Sprintf("create?leader_id=%d&name=%s&declaration=%s&icon=%s&need_approval=%t",
			leaderID, url.QueryEscape(name), url.QueryEscape(declaration), url.QueryEscape(icon), needApproval))
	if err != nil {
		return 0, err
	}
	data := struct {
		GuildID int64 `json:"guild_id"`
	}{}
	if err := gconv.Scan(rsp.Data, &data); err != nil {
		return 0, fmt.Errorf("parse guild_id: %w", err)
	}
	return data.GuildID, nil
}

func callGuildSearch(ctx context.Context, keyword string) ([]*pb.PGuildBasic, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "guild-http",
		fmt.Sprintf("search?keyword=%s", url.QueryEscape(keyword)))
	if err != nil {
		return nil, err
	}
	var guilds []*pb.PGuildBasic
	if err := gconv.Scan(rsp.Data, &guilds); err != nil {
		return nil, fmt.Errorf("parse search result: %w", err)
	}
	return guilds, nil
}

// ===== Proto Handlers =====

func (r *RoleGuild) ReqGuildCreate(ctx context.Context, req *pb.ReqGuildCreate) (*pb.RspGuildCreate, error) {
	guildCfg := r.Cfg().TbGuildConfig.Get()
	if guildCfg == nil {
		return nil, fmt.Errorf("公会配置未找到")
	}
	if r.Role.Basic.Level < guildCfg.UnlockLevel {
		return nil, fmt.Errorf("等级不足，需要 Lv%d", guildCfg.UnlockLevel)
	}
	if r.GuildID > 0 {
		return nil, fmt.Errorf("你已加入公会")
	}
	if !r.Role.Bag.CheckGoods(guildCfg.CreateCost) {
		return nil, fmt.Errorf("创建公会消耗不足")
	}

	guildID, err := callGuildCreate(ctx, r.RoleID, req.Name, req.Declaration, req.Icon, req.NeedApproval)
	if err != nil {
		return nil, err
	}
	// 扣除公会创建消耗
	if err := r.Role.Bag.SaveGoods(ctx, guildCfg.CreateCost, nil, "create_guild"); err != nil {
		return nil, err
	}

	r.SetGuildID(ctx, guildID)
	return &pb.RspGuildCreate{GuildId: guildID}, nil
}

func (r *RoleGuild) ReqGuildSearch(ctx context.Context, req *pb.ReqGuildSearch) (*pb.RspGuildSearch, error) {
	guilds, err := callGuildSearch(ctx, req.Keyword)
	if err != nil {
		return nil, err
	}
	return &pb.RspGuildSearch{Guilds: guilds}, nil
}

func (r *RoleGuild) withGuildActor(ctx context.Context, req proto.Message) (any, error) {
	pid, err := lib.GetGuildActor(ctx, r.GuildID)
	if err != nil {
		return nil, fmt.Errorf("获取公会 actor 失败: %w", err)
	}
	return gxyactor.Call(ctx, pid, req, 10*time.Second)
}

func (r *RoleGuild) ReqGuildApply(ctx context.Context, req *pb.ReqGuildApply) (*pb.RspGuildApply, error) {
	pid, err := lib.GetGuildActor(ctx, req.GuildId)
	if err != nil {
		return nil, fmt.Errorf("获取公会 actor 失败: %w", err)
	}
	rsp, err := gxyactor.Call(ctx, pid, &pb.ReqGuildApply{RoleId: r.RoleID, GuildId: req.GuildId}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	// GuildID 由 NotifyGuildInfo handler 更新（addMember 成功后推送）
	return rsp.(*pb.RspGuildApply), nil
}

func (r *RoleGuild) requireGuild() error {
	if r.GuildID == 0 {
		return fmt.Errorf("你没有加入公会")
	}
	return nil
}

func (r *RoleGuild) ReqGuildInfo(ctx context.Context, req *pb.ReqGuildInfo) (*pb.RspGuildInfo, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildInfo), nil
}

func (r *RoleGuild) ReqGuildLogs(ctx context.Context, req *pb.ReqGuildLogs) (*pb.RspGuildLogs, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildLogs), nil
}

func (r *RoleGuild) ReqGuildApplyList(ctx context.Context, req *pb.ReqGuildApplyList) (*pb.RspGuildApplyList, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildApplyList), nil
}

func (r *RoleGuild) ReqGuildApproveApply(ctx context.Context, req *pb.ReqGuildApproveApply) (*pb.RspGuildApproveApply, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildApproveApply), nil
}

func (r *RoleGuild) ReqGuildKickMember(ctx context.Context, req *pb.ReqGuildKickMember) (*pb.RspGuildKickMember, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildKickMember), nil
}

func (r *RoleGuild) ReqGuildSetPosition(ctx context.Context, req *pb.ReqGuildSetPosition) (*pb.RspGuildSetPosition, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildSetPosition), nil
}

func (r *RoleGuild) ReqGuildTransferLeader(ctx context.Context, req *pb.ReqGuildTransferLeader) (*pb.RspGuildTransferLeader, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildTransferLeader), nil
}

func (r *RoleGuild) ReqGuildUpdateInfo(ctx context.Context, req *pb.ReqGuildUpdateInfo) (*pb.RspGuildUpdateInfo, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildUpdateInfo), nil
}

func (r *RoleGuild) ReqGuildLeave(ctx context.Context, req *pb.ReqGuildLeave) (*pb.RspGuildLeave, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	r.SetGuildID(ctx, 0)
	return rsp.(*pb.RspGuildLeave), nil
}

func (r *RoleGuild) ReqGuildDisband(ctx context.Context, req *pb.ReqGuildDisband) (*pb.RspGuildDisband, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	r.SetGuildID(ctx, 0)
	return rsp.(*pb.RspGuildDisband), nil
}
