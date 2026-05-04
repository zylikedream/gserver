package logic

import (
	"context"
	"fmt"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"time"
)

type RoleBasicState struct {
	RolePersistState
	RoleName string    `gorm:"column:role_name"`
	Head     string    `gorm:"column:head"`
	LoginTm  time.Time `gorm:"column:login_tm"`
	LogoutTm time.Time `gorm:"column:logout_tm"`
	CreateTm time.Time `gorm:"column:create_tm"` // 创角时间
	VipLv    int       `gorm:"column:vip_lv"`
	Level    int32     `gorm:"column:level;default:1"`
}

func (RoleBasicState) TableName() string { return "role_basic" }

func (r *RoleBasicState) GetIndexes() []string {
	return []string{"update_at"}
}

type RoleBasic struct {
	RoleModule
	RoleBasicState
}

var _ IRoleModule = (*RoleBasic)(nil)

func (r *RoleBasic) PersistState() IPersistState {
	return &r.RoleBasicState
}

func (r *RoleBasic) OnModInit(ctx context.Context) error {
	exp := r.getPlayerExp()
	r.RefreshLevelByExp(ctx, exp, exp, "module_init")
	return nil
}

func (r *RoleBasic) ReqBasicSetName(ctx context.Context, req *pb.ReqBasicSetName) (*pb.RspBasicSetName, error) {
	if !r.isNameValid(req.Name) {
		return nil, fmt.Errorf("name unvalid:%s", req.Name)
	}
	rsp := &pb.RspBasicSetName{
		Name: req.Name,
	}
	r.RoleName = req.Name
	r.MarkDirty()
	return rsp, nil
}

func (r *RoleBasic) ReqBasicInfo(ctx context.Context, req *pb.ReqBasicInfo) (*pb.RspBasicInfo, error) {
	return &pb.RspBasicInfo{
		RoleId:   r.RoleID,
		Name:     r.RoleName,
		CreateTm: r.CreateTm.Unix(),
		Head:     r.Head,
		Level:    r.Level,
	}, nil
}

func (r *RoleBasic) ReqBasicSetHead(ctx context.Context, req *pb.ReqBasicSetHead) (*pb.RspBasicSetHead, error) {
	rsp := &pb.RspBasicSetHead{
		Head: req.Head,
	}
	r.Head = req.Head
	r.MarkDirty()
	return rsp, nil
}

// --------------------proto handlers end-------------

func (r *RoleBasic) isNameValid(string) bool {
	return true
}

func (r *RoleBasic) RefreshLevelByExp(ctx context.Context, oldExp int64, newExp int64, reason string) (int32, int32) {
	oldLevel := r.Level
	newLevel := r.getLevelByExp(newExp)
	if newLevel <= 0 {
		newLevel = 1
	}
	if r.Level != newLevel {
		r.Level = newLevel
		r.MarkDirty()
		if newLevel > oldLevel {
			r.notifyRoleLevelUp(ctx, oldLevel, newLevel, oldExp, newExp)
		}
	}
	return oldLevel, newLevel
}

func (r *RoleBasic) getLevelByExp(Exp int64) int32 {
	gc := gameconfig.GameConfig()
	if gc == nil {
		return 1
	}
	cfg := gc.GetPlayerLevelByTotalExp(Exp)
	if cfg == nil {
		return 1
	}
	return cfg.Level
}

func (r *RoleBasic) getPlayerExp() int64 {
	if r.Role == nil || r.Role.Bag == nil {
		return 0
	}
	exp := r.Role.Bag.GetGood(PLAYER_EXP_ITEM_ID).Num
	return int64(exp)
}

func (r *RoleBasic) notifyRoleLevelUp(ctx context.Context, oldLevel int32, newLevel int32, oldExp int64, newExp int64) {
	if r.Role == nil {
		return
	}
	gc := gameconfig.GameConfig()
	var unlockDesc []string
	if gc != nil {
		unlockDesc = gc.GetPlayerLevelUnlockDescs(oldLevel, newLevel)
	}
	r.Role.SendClient(ctx, &pb.NotifyRoleLevelUp{
		OldLevel:   oldLevel,
		NewLevel:   newLevel,
		OldExp:     oldExp,
		NewExp:     newExp,
		UnlockDesc: unlockDesc,
	})
}
