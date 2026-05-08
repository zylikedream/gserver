package logic

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"gserver/core/gxyhttp"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/apps/chat"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/util/gconv"
	"google.golang.org/protobuf/proto"
)

// ===== 持久化状态 =====

type RoleGuildState struct {
	RolePersistState
	GuildID int64 `gorm:"column:guild_id"` // 0=无公会
}

func (RoleGuildState) TableName() string { return "role_guild" }

// ===== 子模块 =====

type RoleGuild struct {
	RoleModule
	RoleGuildState
}

var _ IRoleModule = (*RoleGuild)(nil)

func (r *RoleGuild) PersistState() IPersistState { return &r.RoleGuildState }

func (r *RoleGuild) OnModInit(ctx context.Context) error { return nil }

func (r *RoleGuild) OnCreate(ctx context.Context) {}

func (r *RoleGuild) OnModStart(ctx context.Context) error {
	if r.GuildID > 0 {
		chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())
	}
	return nil
}

// ===== HTTP helpers =====

func callGuildCreate(ctx context.Context, leaderID int64, name, declaration, icon string, needApproval bool) (int64, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "guild",
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
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "guild",
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

func (r *RoleGuild) ReqCreateGuild(ctx context.Context, req *pb.ReqCreateGuild) (*pb.RspCreateGuild, error) {
	basic := r.Role.GetBasic()
	guildCfg := gameconfig.GameConfig().TbGuildConfig.Get()
	if guildCfg == nil {
		return nil, fmt.Errorf("公会配置未找到")
	}
	if basic.Level < guildCfg.UnlockLevel {
		return nil, fmt.Errorf("等级不足，需要 Lv%d", guildCfg.UnlockLevel)
	}
	if r.GuildID > 0 {
		return nil, fmt.Errorf("你已加入公会")
	}
	if !r.Role.GetBag().CheckGoods(guildCfg.CreateCost) {
		return nil, fmt.Errorf("创建公会消耗不足")
	}

	guildID, err := callGuildCreate(ctx, r.RoleID, req.Name, req.Declaration, req.Icon, req.NeedApproval)
	if err != nil {
		return nil, err
	}
	// 扣除公会创建消耗
	if err := r.Role.GetBag().SaveGoods(ctx, nil, guildCfg.CreateCost, "create_guild"); err != nil {
		return nil, err
	}

	r.GuildID = guildID
	chat.RegisterRoleGuildChat(r.RoleID, guildID, r.Role.Self())
	return &pb.RspCreateGuild{GuildId: guildID}, nil
}

func (r *RoleGuild) ReqSearchGuild(ctx context.Context, req *pb.ReqSearchGuild) (*pb.RspSearchGuild, error) {
	guilds, err := callGuildSearch(ctx, req.Keyword)
	if err != nil {
		return nil, err
	}
	return &pb.RspSearchGuild{Guilds: guilds}, nil
}

func (r *RoleGuild) withGuildActor(ctx context.Context, req proto.Message) (any, error) {
	pid, err := lib.GetGuildActor(r.GuildID)
	if err != nil {
		return nil, fmt.Errorf("获取公会 actor 失败: %w", err)
	}
	return r.Role.Call(pid, req, 10*time.Second)
}

func (r *RoleGuild) ReqApplyGuild(ctx context.Context, req *pb.ReqApplyGuild) (*pb.RspApplyGuild, error) {
	pid, err := lib.GetGuildActor(req.GuildId)
	if err != nil {
		return nil, fmt.Errorf("获取公会 actor 失败: %w", err)
	}
	rsp, err := r.Role.Call(pid, &pb.ReqApplyGuild{RoleId: r.RoleID, GuildId: req.GuildId}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	r.GuildID = req.GuildId
	chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())
	return rsp.(*pb.RspApplyGuild), nil
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

func (r *RoleGuild) ReqApproveApply(ctx context.Context, req *pb.ReqApproveApply) (*pb.RspApproveApply, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspApproveApply), nil
}

func (r *RoleGuild) ReqKickMember(ctx context.Context, req *pb.ReqKickMember) (*pb.RspKickMember, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspKickMember), nil
}

func (r *RoleGuild) ReqSetViceLeader(ctx context.Context, req *pb.ReqSetViceLeader) (*pb.RspSetViceLeader, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspSetViceLeader), nil
}

func (r *RoleGuild) ReqTransferLeader(ctx context.Context, req *pb.ReqTransferLeader) (*pb.RspTransferLeader, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspTransferLeader), nil
}

func (r *RoleGuild) ReqUpdateGuildInfo(ctx context.Context, req *pb.ReqUpdateGuildInfo) (*pb.RspUpdateGuildInfo, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspUpdateGuildInfo), nil
}

func (r *RoleGuild) ReqLeaveGuild(ctx context.Context, req *pb.ReqLeaveGuild) (*pb.RspLeaveGuild, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	r.GuildID = 0
	chat.UnregisterRoleGuildChat(r.RoleID)
	return rsp.(*pb.RspLeaveGuild), nil
}

func (r *RoleGuild) ReqDisbandGuild(ctx context.Context, req *pb.ReqDisbandGuild) (*pb.RspDisbandGuild, error) {
	if err := r.requireGuild(); err != nil {
		return nil, err
	}
	req.RoleId = r.RoleID
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	r.GuildID = 0
	chat.UnregisterRoleGuildChat(r.RoleID)
	return rsp.(*pb.RspDisbandGuild), nil
}
