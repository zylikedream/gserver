package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/core/gxytimer"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role"
	"gserver/src/lib/rolelib"
	"gserver/src/pkg/gameconfig"

	"gorm.io/gorm"
	"gserver/src/util"

	"github.com/gogf/gf/v2/util/gconv"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm/clause"
)

const MaxLogCount = 100

type GuildActor struct {
	gxymodule.ModuleBase
	*gxyactor.ActorBase
	GuildID int64
	Data    *Guild

	db  *gorm.DB
	cfg *gameconfig.GameConfig
}

func NewGuildActor() *GuildActor {
	ctx := gxylog.NewContext(context.Background(), "guild")
	g := &GuildActor{db: gxypgx.DB(), cfg: gameconfig.Get()}
	g.ActorBase = gxyactor.NewActorBase(ctx, g, "guild")
	return g
}

// ===== IActor 接口 =====

func (g *GuildActor) Init(ctx context.Context, args []any) error {
	if len(args) != 1 {
		return errors.New("guild actor init args error")
	}
	g.GuildID = gconv.Int64(args[0])
	if g.GuildID <= 0 {
		return errors.New("guild actor init args error")
	}
	return nil
}

func (g *GuildActor) DelayInit(ctx context.Context) error {
	g.Data = &Guild{}
	if err := g.db.First(g.Data, g.GuildID).Error; err != nil {
		return err
	}

	g.Timer().AddTick(ctx, &gxytimer.Tick{Name: "guild_save", Interval: 600 * time.Second}, g.TickSave)
	g.Timer().AddCron(ctx, gxytimer.DayRefresh, g.onDayRefresh)

	gxylog.Info(ctx, "guild actor started", gxylog.Num("guildID", g.GuildID), gxylog.Num("members", len(g.Data.Members)), gxylog.Num("applies", len(g.Data.ApplyList)))
	return nil
}

func (g *GuildActor) Terminate(ctx context.Context, err error) {
	g.StopModule(ctx)
}

func (g *GuildActor) HandleMessage(ctx context.Context, msg any) error {
	_, err := g.AutoHandleMsg(ctx, msg)
	return err
}

// ===== Module 生命周期 =====

func (g *GuildActor) OnModStop(ctx context.Context) error {
	g.save(ctx)
	gxylog.Info(ctx, "guild actor stopped", gxylog.Num("guildID", g.GuildID))
	return nil
}

func (g *GuildActor) save(ctx context.Context) {
	if g.Data == nil {
		return
	}
	g.db.Save(g.Data)
}

func (g *GuildActor) TickSave(ctx context.Context, _ gxytimer.TimerActiveInfo) {
	g.save(ctx)
}

func (g *GuildActor) onDayRefresh(ctx context.Context, _ gxytimer.TimerActiveInfo) {
	// 清理过期申请
	now := time.Now()
	valid := make([]*GuildApply, 0, len(g.Data.ApplyList))
	for _, a := range g.Data.ApplyList {
		if a.Status == 0 {
			if a.ExpireAt.After(now) {
				valid = append(valid, a)
			}
		} else {
			valid = append(valid, a)
		}
	}
	g.Data.ApplyList = valid
}

// ===== 日志 =====

func (g *GuildActor) addLog(ctx context.Context, content string) {
	entry := &GuildLog{Content: content, CreatedAt: time.Now()}
	g.Data.Logs = append(g.Data.Logs, entry)
	if len(g.Data.Logs) > MaxLogCount {
		g.Data.Logs = g.Data.Logs[len(g.Data.Logs)-MaxLogCount:]
	}
	g.db.Save(g.Data)
}

// ===== 通知（携带数据） =====

func (g *GuildActor) notifyPlayer(ctx context.Context, roleID int64, msg proto.Message) {
	rolelib.PublishRoleNotify(ctx, roleID, msg)
}

func (g *GuildActor) notifyGuildInfo(ctx context.Context, exclude ...int64) {
	msg := g.buildNotifyGuildInfo(ctx)
	excludeSet := toSet(exclude)
	for _, m := range g.Data.Members {
		if _, ok := excludeSet[m.RoleID]; ok {
			continue
		}
		// 填充 self 字段
		msg.Self = g.buildPGuildMember(ctx, m)
		g.notifyPlayer(ctx, m.RoleID, msg)
	}
}

func (g *GuildActor) notifyGuildBasic(ctx context.Context) {
	msg := g.buildNotifyGuildBasic(ctx)
	for _, m := range g.Data.Members {
		g.notifyPlayer(ctx, m.RoleID, msg)
	}
}

func (g *GuildActor) notifyApplyUpdate(ctx context.Context) {
	msg := g.buildNotifyGuildApply(ctx)
	for _, m := range g.Data.Members {
		if m.Position > 2 {
			continue // 只有会长(1)/副会长(2)
		}
		g.notifyPlayer(ctx, m.RoleID, msg)
	}
}

func (g *GuildActor) buildNotifyGuildInfo(ctx context.Context) *pb.NotifyGuildInfo {
	guild := g.Data
	levelCfg := getLevelConfig(g.cfg, guild.Level)
	memberLimit := int32(30)
	if levelCfg != nil {
		memberLimit = levelCfg.MemberLimit
	}
	return &pb.NotifyGuildInfo{
		Guild: &pb.PGuildBasic{
			Id: guild.ID, Name: guild.Name, Level: guild.Level,
			Icon: guild.Icon, Declaration: guild.Declaration,
			Announcement: guild.Announcement,
			NeedApproval: guild.NeedApproval,
			MemberCount:  guild.MemberCount, MemberLimit: memberLimit,
			LeaderId: guild.LeaderID, CreatedAt: guild.CreatedAt.Unix(),
		},
		Members: g.buildMemberList(ctx),
	}
}

func (g *GuildActor) buildNotifyGuildBasic(_ context.Context) *pb.NotifyGuildBasic {
	guild := g.Data
	levelCfg := getLevelConfig(g.cfg, guild.Level)
	memberLimit := int32(30)
	if levelCfg != nil {
		memberLimit = levelCfg.MemberLimit
	}
	return &pb.NotifyGuildBasic{
		Guild: &pb.PGuildBasic{
			Id: guild.ID, Name: guild.Name, Level: guild.Level,
			Icon: guild.Icon, Declaration: guild.Declaration,
			Announcement: guild.Announcement,
			NeedApproval: guild.NeedApproval,
			MemberCount:  guild.MemberCount, MemberLimit: memberLimit,
			LeaderId: guild.LeaderID, CreatedAt: guild.CreatedAt.Unix(),
		},
	}
}

func (g *GuildActor) buildNotifyGuildApply(ctx context.Context) *pb.NotifyGuildApply {
	pending := g.getPendingApplies()
	result := make([]*pb.PGuildApply, 0, len(pending))
	for _, a := range pending {
		pub := role.GetRolePublic(ctx, a.RoleID)
		result = append(result, &pb.PGuildApply{
			ApplyId: a.ID, GuildId: g.GuildID,
			PlayerInfo: pub, CreatedAt: a.CreatedAt.Unix(),
		})
	}
	return &pb.NotifyGuildApply{Applies: result}
}

func (g *GuildActor) buildMemberList(ctx context.Context) []*pb.PGuildMember {
	result := make([]*pb.PGuildMember, 0, len(g.Data.Members))
	for _, m := range g.Data.Members {
		result = append(result, g.buildPGuildMember(ctx, m))
	}
	return result
}

func (g *GuildActor) buildPGuildMember(ctx context.Context, m *GuildMember) *pb.PGuildMember {
	pub := role.GetRolePublic(ctx, m.RoleID)
	if pub == nil {
		pub = &pb.PRolePublic{RoleId: m.RoleID}
	}
	return &pb.PGuildMember{
		PlayerInfo: pub,
		Position:   pb.EGuildPosition(m.Position),
		JoinedAt:   m.JoinedAt,
	}
}

// ===== 权限检查 =====

func (g *GuildActor) getMember(roleID int64) *GuildMember {
	for _, m := range g.Data.Members {
		if m.RoleID == roleID {
			return m
		}
	}
	return nil
}

func (g *GuildActor) canApprove(operatorID int64) bool {
	m := g.getMember(operatorID)
	return m != nil && m.Position <= 2
}

func toSet(ids []int64) map[int64]struct{} {
	s := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		s[id] = struct{}{}
	}
	return s
}

// getPendingApplies 返回所有待审核的申请（status=0）
func (g *GuildActor) getPendingApplies() []*GuildApply {
	result := make([]*GuildApply, 0, len(g.Data.ApplyList))
	for _, a := range g.Data.ApplyList {
		if a.Status == 0 {
			result = append(result, a)
		}
	}
	return result
}

func getLevelConfig(cfg *gameconfig.GameConfig, level int32) *gamecfg.GardenGuildLevel {
	return cfg.TbGuildLevel.Get(level)
}

// ===== 错误变量 =====

var (
	ErrPlayerAlreadyInGuild    = errors.New("该玩家已加入公会")
	ErrGuildFull               = errors.New("公会人数已满")
	ErrPermissionDenied        = errors.New("权限不足")
	ErrGuildNotFound           = errors.New("公会不存在")
	ErrCannotKickLeader        = errors.New("不能踢出会长")
	ErrCannotKickViceLeader    = errors.New("不能操作同级副会长")
	ErrCannotTransferToSelf    = errors.New("不能转让给自己")
	ErrGuildHasMembers         = errors.New("公会还有其他成员，请先转让会长")
	ErrApplyExpired            = errors.New("申请已过期或不存在")
	ErrPositionLimitReached    = errors.New("副会长数量已达上限")
	ErrMemberNotFound          = errors.New("成员不存在")
	ErrCannotSetPositionToSelf = errors.New("不能设置自己的职位")
	ErrInvalidPosition         = errors.New("无效的职位")
)

// nextApplyID 生成自增 apply_id（基于现有最大 ID + 1）
func (g *GuildActor) nextApplyID() int64 {
	var maxID int64
	for _, a := range g.Data.ApplyList {
		if a.ID > maxID {
			maxID = a.ID
		}
	}
	return maxID + 1
}

// ActorCreateGuild — 由 HTTP create handler 调用
func (g *GuildActor) ActorCreateGuild(ctx context.Context, msg *pb.ActorCreateGuild) error {
	g.addLog(ctx, "公会创建成功")
	return nil
}

// ---- 加入公会（内部根据 NeedApproval 分流） ----

// ApplyGuild — 申请加入公会
func (g *GuildActor) ApplyGuild(ctx context.Context, req *pb.ReqGuildApply) (*pb.RspGuildApply, error) {
	// 检查是否已有待审核申请
	for _, a := range g.Data.ApplyList {
		if a.RoleID == req.RoleId && a.Status == 0 {
			return nil, errors.New("你已申请过该公会")
		}
	}
	if g.Data.NeedApproval {
		return g.createApply(ctx, req)
	}
	return g.joinDirect(ctx, req)
}

// createApply — 创建申请
func (g *GuildActor) createApply(ctx context.Context, req *pb.ReqGuildApply) (*pb.RspGuildApply, error) {
	cfg := g.cfg.TbGuildConfig.Get()
	if cfg == nil {
		return nil, errors.New("公会配置未找到")
	}

	apply := &GuildApply{
		ID:        g.nextApplyID(),
		RoleID:    req.RoleId,
		Status:    0,
		CreatedAt: time.Now(),
		ExpireAt:  time.Now().Add(time.Duration(cfg.ApplyExpireHours) * time.Hour),
	}
	g.Data.ApplyList = append(g.Data.ApplyList, apply)
	g.notifyApplyUpdate(ctx)
	return &pb.RspGuildApply{}, nil
}

// addMember — 原子操作：核验成员上限 → 原子门 → 追加成员 → 通知
func (g *GuildActor) addMember(ctx context.Context, roleID int64) error {
	levelCfg := g.cfg.TbGuildLevel.Get(g.Data.Level)
	if levelCfg == nil {
		return errors.New("公会等级配置未找到")
	}
	if len(g.Data.Members) >= int(levelCfg.MemberLimit) {
		return ErrGuildFull
	}

	// 原子门：INSERT OR UPDATE，WHERE guild_id=0 确保只对无公会玩家生效
	result := g.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "role_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"guild_id": g.GuildID,
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Table: "role_guild", Name: "guild_id"}, Value: 0},
		}},
	}).Create(&GuildRoleState{
		RoleID:  roleID,
		GuildID: g.GuildID,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrPlayerAlreadyInGuild
	}

	member := &GuildMember{RoleID: roleID, Position: int32(gamecfg.GardenEGuildPosition_MEMBER), JoinedAt: time.Now().Unix()}
	g.Data.Members = append(g.Data.Members, member)
	g.Data.MemberCount = int32(len(g.Data.Members))

	g.addLog(ctx, fmt.Sprintf("玩家 %d 加入公会", roleID))
	return nil
}

// joinDirect — 直接加入（免审批）
func (g *GuildActor) joinDirect(ctx context.Context, req *pb.ReqGuildApply) (*pb.RspGuildApply, error) {
	if err := g.addMember(ctx, req.RoleId); err != nil {
		return nil, err
	}
	g.notifyGuildInfo(ctx) // 通知全部成员（含新成员）
	return &pb.RspGuildApply{}, nil
}

// ---- 申请列表 ----

// GetGuildApplyList — 从内存返回（带 PRolePublic 信息）
func (g *GuildActor) GetGuildApplyList(ctx context.Context, req *pb.ReqGuildApplyList) (*pb.RspGuildApplyList, error) {
	pending := g.getPendingApplies()
	result := make([]*pb.PGuildApply, 0, len(pending))
	for _, a := range pending {
		pub := role.GetRolePublic(ctx, a.RoleID)
		result = append(result, &pb.PGuildApply{
			ApplyId: a.ID, GuildId: g.GuildID,
			PlayerInfo: pub, CreatedAt: a.CreatedAt.Unix(),
		})
	}
	return &pb.RspGuildApplyList{Applies: result}, nil
}

// ---- 审批 ----

func (g *GuildActor) ApproveApply(ctx context.Context, operatorID int64, req *pb.ReqGuildApproveApply) error {
	if !g.canApprove(operatorID) {
		return ErrPermissionDenied
	}

	// 支持批量审批
	for _, applyID := range req.ApplyIds {
		if err := g.processSingleApply(ctx, applyID, req.Approve); err != nil {
			return err
		}
	}

	g.notifyApplyUpdate(ctx)
	if req.Approve {
		g.notifyGuildInfo(ctx)
	}
	return nil
}

// processSingleApply 处理单个申请审批
func (g *GuildActor) processSingleApply(ctx context.Context, applyID int64, approve bool) error {
	var apply *GuildApply
	for _, a := range g.getPendingApplies() {
		if a.ID == applyID {
			apply = a
			break
		}
	}
	if apply == nil {
		return ErrApplyExpired
	}

	// 拒绝：仅更新状态
	if !approve {
		apply.Status = 2
		return nil
	}

	// 同意
	apply.Status = 1
	if err := g.addMember(ctx, apply.RoleID); err != nil {
		return err
	}
	return nil
}

// ---- 踢出 ----

func (g *GuildActor) KickMember(ctx context.Context, operatorID int64, req *pb.ReqGuildKickMember) error {
	if !g.canKick(operatorID, req.TargetId) {
		return ErrPermissionDenied
	}

	g.Data.Members = removeMember(g.Data.Members, req.TargetId)
	g.Data.MemberCount = int32(len(g.Data.Members))

	g.db.Model(&GuildRoleState{}).
		Where("role_id = ?", req.TargetId).Update("guild_id", 0)

	g.notifyPlayer(ctx, req.TargetId, &pb.NotifyGuildKicked{Reason: req.Reason})
	g.notifyGuildInfo(ctx, req.TargetId)

	g.addLog(ctx, fmt.Sprintf("玩家 %d 被 %d 踢出公会", req.TargetId, operatorID))
	return nil
}

// ---- 辅助 ----

func removeMember(members []*GuildMember, roleID int64) []*GuildMember {
	for i, m := range members {
		if m.RoleID == roleID {
			return append(members[:i], members[i+1:]...)
		}
	}
	return members
}

func (g *GuildActor) canKick(operatorID, targetID int64) bool {
	if operatorID == targetID {
		return false
	}
	op := g.getMember(operatorID)
	target := g.getMember(targetID)
	if op == nil || target == nil {
		return false
	}
	// 只有会长(1)可以踢副会长(2)，会长和副会长都可以踢成员(3)
	if target.Position == int32(gamecfg.GardenEGuildPosition_LEADER) {
		return false
	}
	if target.Position == int32(gamecfg.GardenEGuildPosition_VICE_LEADER) && op.Position != int32(gamecfg.GardenEGuildPosition_LEADER) {
		return false
	}
	return op.Position <= target.Position
}

// ===== Actor 消息处理器 =====

// GuildInfo — 获取公会大厅信息
func (g *GuildActor) GuildInfo(ctx context.Context, req *pb.ReqGuildInfo) (*pb.RspGuildInfo, error) {
	msg := g.buildNotifyGuildInfo(ctx)
	return &pb.RspGuildInfo{
		Guild:   msg.Guild,
		Members: msg.Members,
		Logs:    g.buildLogList(),
	}, nil
}

// GuildLogs — 获取公会日志
func (g *GuildActor) GuildLogs(ctx context.Context, _ *pb.ReqGuildLogs) (*pb.RspGuildLogs, error) {
	return &pb.RspGuildLogs{
		Logs: g.buildLogList(),
	}, nil
}

// HandleApproveApply — 审批加入（actor 消息路由）
func (g *GuildActor) HandleApproveApply(ctx context.Context, req *pb.ReqGuildApproveApply) (*pb.RspGuildApproveApply, error) {
	if err := g.ApproveApply(ctx, req.RoleId, req); err != nil {
		return nil, err
	}
	return &pb.RspGuildApproveApply{}, nil
}

// HandleKickMember — 踢出成员（actor 消息路由）
func (g *GuildActor) HandleKickMember(ctx context.Context, req *pb.ReqGuildKickMember) (*pb.RspGuildKickMember, error) {
	if err := g.KickMember(ctx, req.RoleId, req); err != nil {
		return nil, err
	}
	return &pb.RspGuildKickMember{}, nil
}

// SetPosition — 设置成员职位（会长操作）
func (g *GuildActor) SetPosition(ctx context.Context, req *pb.ReqGuildSetPosition) (*pb.RspGuildSetPosition, error) {
	if req.RoleId == req.TargetId {
		return nil, ErrCannotSetPositionToSelf
	}
	op := g.getMember(req.RoleId)
	if op == nil || op.Position >= req.Position {
		return nil, ErrPermissionDenied
	}
	target := g.getMember(req.TargetId)
	if target == nil {
		return nil, ErrMemberNotFound
	}
	validPostions := []int32{int32(gamecfg.GardenEGuildPosition_VICE_LEADER), int32(gamecfg.GardenEGuildPosition_MEMBER)}
	if !util.ListMember(validPostions, req.Position) {
		return nil, ErrInvalidPosition
	}

	target.Position = req.Position
	g.notifyGuildInfo(ctx)
	return &pb.RspGuildSetPosition{}, nil
}

// TransferLeader — 转让会长
func (g *GuildActor) TransferLeader(ctx context.Context, req *pb.ReqGuildTransferLeader) (*pb.RspGuildTransferLeader, error) {
	op := g.getMember(req.RoleId)
	if op == nil || op.Position != int32(gamecfg.GardenEGuildPosition_LEADER) {
		return nil, ErrPermissionDenied
	}
	if req.TargetId == req.RoleId {
		return nil, ErrCannotTransferToSelf
	}
	target := g.getMember(req.TargetId)
	if target == nil {
		return nil, errors.New("目标成员不存在")
	}
	op.Position = int32(gamecfg.GardenEGuildPosition_MEMBER)
	target.Position = int32(gamecfg.GardenEGuildPosition_LEADER)
	g.Data.LeaderID = req.TargetId
	g.notifyGuildInfo(ctx)
	g.addLog(ctx, fmt.Sprintf("会长转让给 %d", req.TargetId))
	return &pb.RspGuildTransferLeader{}, nil
}

// UpdateGuildInfo — 修改公会信息
func (g *GuildActor) UpdateGuildInfo(ctx context.Context, req *pb.ReqGuildUpdateInfo) (*pb.RspGuildUpdateInfo, error) {
	op := g.getMember(req.RoleId)
	if op == nil || op.Position > int32(gamecfg.GardenEGuildPosition_VICE_LEADER) {
		return nil, ErrPermissionDenied
	}
	if req.Declaration != "" {
		g.Data.Declaration = req.Declaration
	}
	if req.Announcement != "" {
		g.Data.Announcement = req.Announcement
	}
	g.Data.NeedApproval = req.NeedApproval
	g.notifyGuildBasic(ctx)
	return &pb.RspGuildUpdateInfo{}, nil
}

// LeaveGuild — 退出公会
func (g *GuildActor) LeaveGuild(ctx context.Context, req *pb.ReqGuildLeave) (*pb.RspGuildLeave, error) {
	op := g.getMember(req.RoleId)
	if op == nil {
		return nil, errors.New("你不在该公会中")
	}
	if op.Position == int32(gamecfg.GardenEGuildPosition_LEADER) {
		return nil, errors.New("会长不能退出公会，请先转让会长")
	}
	g.Data.Members = removeMember(g.Data.Members, req.RoleId)
	g.Data.MemberCount = int32(len(g.Data.Members))
	g.db.Model(&GuildRoleState{}).
		Where("role_id = ?", req.RoleId).Update("guild_id", 0)
	g.notifyGuildInfo(ctx)
	g.addLog(ctx, fmt.Sprintf("玩家 %d 退出公会", req.RoleId))
	return &pb.RspGuildLeave{}, nil
}

// DisbandGuild — 解散公会
func (g *GuildActor) DisbandGuild(ctx context.Context, req *pb.ReqGuildDisband) (*pb.RspGuildDisband, error) {
	op := g.getMember(req.RoleId)
	if op == nil || op.Position != int32(gamecfg.GardenEGuildPosition_LEADER) {
		return nil, ErrPermissionDenied
	}
	if len(g.Data.Members) > 1 {
		return nil, ErrGuildHasMembers
	}
	// 通知成员
	for _, m := range g.Data.Members {
		g.notifyPlayer(ctx, m.RoleID, &pb.NotifyGuildKicked{Reason: "公会已解散"})
	}
	// 删除 guild 记录
	g.db.Delete(g.Data)
	// 清理所有 role_guild
	g.db.Model(&GuildRoleState{}).
		Where("guild_id = ?", g.GuildID).Update("guild_id", 0)
	// 停止 actor
	g.Stop(nil)
	return &pb.RspGuildDisband{}, nil
}

// buildLogList — 构建公会日志列表
func (g *GuildActor) buildLogList() []*pb.PGuildLog {
	result := make([]*pb.PGuildLog, 0, len(g.Data.Logs))
	for _, l := range g.Data.Logs {
		result = append(result, &pb.PGuildLog{
			Content: l.Content, CreatedAt: l.CreatedAt.Unix(),
		})
	}
	return result
}
