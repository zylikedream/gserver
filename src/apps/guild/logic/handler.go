package logic

import (
	"context"
	"strconv"
	"time"

	"gserver/core/gxyhttp"
	"gserver/core/gxypgx"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/lib"
	"gserver/src/pkg/gameconfig"

	"gorm.io/gorm"

	"github.com/gogf/gf/v2/frame/g"
)

type GuildHandler struct {
	g.Meta `method:"POST"`
	db     *gorm.DB
	cfg    *gameconfig.GameConfig
}

// NewGuildHandler 构造注入依赖(组装根)。
func NewGuildHandler() *GuildHandler {
	return &GuildHandler{db: gxypgx.DB(), cfg: gameconfig.Get()}
}

// ===== 创建公会 =====

type CreateGuildReq struct {
	g.Meta       `path:"/create"`
	LeaderID     int64  `p:"leader_id" v:"required"`
	Name         string `p:"name" v:"required"`
	Declaration  string `p:"declaration"`
	Icon         string `p:"icon"`
	NeedApproval bool   `p:"need_approval"`
}

func (h *GuildHandler) Create(ctx context.Context, req *CreateGuildReq) (any, error) {
	// 检查名称唯一性
	var count int64
	h.db.Model(&Guild{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, gxyhttp.NewErrCode(1, "公会名称已存在")
	}

	// 写入公会记录（JSONB 列直接初始化 Members/Logs）
	guild := &Guild{
		Name: req.Name, Level: 1, LeaderID: req.LeaderID,
		Declaration: req.Declaration, Icon: req.Icon,
		NeedApproval: req.NeedApproval, MemberCount: 1,
		Members: []*GuildMember{
			{RoleID: req.LeaderID, Position: int32(gamecfg.GardenEGuildPosition_LEADER), JoinedAt: time.Now().Unix()},
		},
		Logs: []*GuildLog{
			{Content: "公会创建成功", CreatedAt: time.Now()},
		},
	}
	if err := h.db.Create(guild).Error; err != nil {
		return nil, gxyhttp.NewErrCode(1, "创建公会失败: "+err.Error())
	}

	// 更新 role_guild
	if err := h.db.Exec(
		"INSERT INTO role_guild (role_id, guild_id) VALUES (?, ?) ON CONFLICT (role_id) DO UPDATE SET guild_id = ?",
		req.LeaderID, guild.ID, guild.ID,
	).Error; err != nil {
		h.db.Delete(guild)
		return nil, gxyhttp.NewErrCode(1, "创建公会失败: "+err.Error())
	}

	// 激活 guild actor（DelayInit 从 DB 加载）
	_, err := lib.GetGuildActor(ctx, guild.ID)
	if err != nil {
		h.db.Delete(guild)
		h.db.Delete(GuildRoleState{GuildID: guild.ID})
		return nil, err
	}

	return map[string]int64{"guild_id": guild.ID}, nil
}

// ===== 搜索公会 =====

type SearchGuildReq struct {
	g.Meta  `path:"/search"`
	Keyword string `p:"keyword"`
}

func (h *GuildHandler) Search(ctx context.Context, req *SearchGuildReq) (any, error) {
	var guilds []Guild
	if id, err := strconv.ParseInt(req.Keyword, 10, 64); err == nil {
		h.db.Where("id = ?", id).Find(&guilds)
	} else {
		h.db.Where("name LIKE ?", "%"+req.Keyword+"%").Limit(20).Find(&guilds)
	}

	result := make([]*pb.PGuildBasic, 0, len(guilds))
	for _, guild := range guilds {
		cfg := h.cfg.TbGuildLevel.Get(guild.Level)
		memberLimit := int32(30)
		if cfg != nil {
			memberLimit = cfg.MemberLimit
		}
		result = append(result, &pb.PGuildBasic{
			Id: guild.ID, Name: guild.Name, Level: guild.Level,
			Icon: guild.Icon, Declaration: guild.Declaration,
			NeedApproval: guild.NeedApproval,
			MemberCount:  guild.MemberCount, MemberLimit: memberLimit,
			LeaderId: guild.LeaderID, CreatedAt: guild.CreatedAt.Unix(),
		})
	}
	return result, nil
}
