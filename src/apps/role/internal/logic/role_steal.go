package logic

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"
)

var (
	ErrNotFriend           = errors.New("对方不是你的好友")
	ErrStealNotHarvestable = errors.New("该鲜花尚未成熟")
	ErrStealFlowerFull     = errors.New("该鲜花已被摘取完毕")
	ErrStealDailyFull      = errors.New("今日对该好友的摘取次数已用完")
	ErrStealLocked         = errors.New("该鲜花已无法摘取")
)

// ========== 每日偷取计数模型 ==========

type DailyStealEntry struct {
	FriendID int64 `json:"friend_id"`
	Count    int32 `json:"count"`
}

// DailyStealMap 存储玩家对所有好友的每日偷取次数
// key: friendID
type DailyStealMap map[int64]*DailyStealEntry

type RoleStealState struct {
	RolePersistState
	DailySteals DailyStealMap `gorm:"column:daily_steals;type:jsonb;serializer:json"`
	StealDate   string        `gorm:"column:steal_date"`
}

func (RoleStealState) TableName() string { return "role_steal" }

// ========== 模块 ==========

type RoleSteal struct {
	RoleModule
	RoleStealState
}

var _ IRoleModule = (*RoleSteal)(nil)

func (r *RoleSteal) PersistState() IPersistState {
	return &r.RoleStealState
}

func (r *RoleSteal) OnModInit(ctx context.Context) error {
	if r.DailySteals == nil {
		r.DailySteals = make(DailyStealMap)
	}
	return nil
}

func (r *RoleSteal) OnCreate(ctx context.Context) {
	r.StealDate = todayStr()
}

func (r *RoleSteal) getDailySteal() DailyStealMap {
	if r.DailySteals == nil {
		r.DailySteals = make(DailyStealMap)
	}
	if r.StealDate != todayStr() {
		r.DailySteals = make(DailyStealMap)
		r.StealDate = todayStr()
		r.MarkDirty()
	}
	return r.DailySteals
}

func (r *RoleSteal) getDailyCount(friendID int64) int32 {
	dailySteals := r.getDailySteal()
	if e, ok := dailySteals[friendID]; ok {
		return e.Count
	}
	return 0
}

func (r *RoleSteal) incDailyCount(friendID int64) {
	dailySteals := r.getDailySteal()
	e, ok := dailySteals[friendID]
	if !ok {
		e = &DailyStealEntry{FriendID: friendID}
		dailySteals[friendID] = e
	}
	e.Count++
	r.MarkDirty()
}

func todayStr() string {
	return time.Now().Format("2006-01-02")
}

// ========== Proto Handler ==========

func (r *RoleSteal) ReqPlotFriendInfo(ctx context.Context, req *pb.ReqPlotFriendInfo) (*pb.RspPlotFriendInfo, error) {
	friendID := req.FriendId

	if !isFriend(ctx, r.DB(), r.RoleID, friendID) {
		return nil, errors.WithStack(ErrNotFriend)
	}

	plots, ok := getRolePlotSnapshot(ctx, r.Deps(), friendID)
	if !ok {
		return &pb.RspPlotFriendInfo{}, nil
	}

	cfg := r.Cfg().TbFriendConfig.Get()
	dailyCount := r.getDailyCount(friendID)

	rsp := &pb.RspPlotFriendInfo{
		StealLimit: cfg.StealPerFriendDailyLimit,
		StealUsed:  dailyCount,
	}

	for _, plot := range plots {
		state := getPlotState(plot)
		info := &pb.PPlotInfo{
			PlotId:       plot.PlotID,
			FlowerId:     plot.FlowerID,
			State:        pb.PlotState(state),
			HarvestCount: plot.HarvestCount,
			StateTime:    plot.StateTime.Unix(),
		}

		if state == int32(pb.PlotState_PLOT_HARVESTABLE) {
			stolen, _ := countPlotStolen(ctx, r.DB(), friendID, plot.PlotID)
			if stolen < int64(cfg.FlowerMaxBeStolenTimes) && dailyCount < cfg.StealPerFriendDailyLimit && !hasStealRecord(ctx, r.DB(), r.RoleID, friendID, plot.PlotID) {
				info.CanSteal = true
			}
		}

		rsp.Plots = append(rsp.Plots, info)
	}

	return rsp, nil
}

func (r *RoleSteal) ReqPlotSteal(ctx context.Context, req *pb.ReqPlotSteal) (*pb.RspPlotSteal, error) {
	friendID := req.FriendId
	plotID := req.PlotId
	cfg := r.Cfg().TbFriendConfig.Get()

	if !isFriend(ctx, r.DB(), r.RoleID, friendID) {
		return nil, errors.WithStack(ErrNotFriend)
	}
	if r.getDailyCount(friendID) >= cfg.StealPerFriendDailyLimit {
		return nil, errors.WithStack(ErrStealDailyFull)
	}

	var rsp *pb.RspPlotSteal
	err := withPlotLocks(ctx, friendID, []int32{plotID}, func() error {
		var err error
		rsp, err = r.steal(ctx, friendID, plotID)
		return err
	})
	return rsp, err
}

// steal 在持有对端地块锁的前提下执行一次偷取:校验可偷 → 记录 → 发奖。
func (r *RoleSteal) steal(ctx context.Context, friendID int64, plotID int32) (*pb.RspPlotSteal, error) {
	cfg := r.Cfg().TbFriendConfig.Get()
	plots, ok := getRolePlotSnapshot(ctx, r.Deps(), friendID)
	if !ok {
		return nil, errors.WithStack(ErrStealLocked)
	}
	plot, ok := plots[plotID]
	if !ok {
		return nil, errors.WithStack(ErrStealLocked)
	}
	if getPlotState(plot) != int32(pb.PlotState_PLOT_HARVESTABLE) {
		return nil, errors.WithStack(ErrStealNotHarvestable)
	}

	stolenCount, err := countPlotStolen(ctx, r.DB(), friendID, plotID)
	if err != nil {
		return nil, err
	}
	if stolenCount >= int64(cfg.FlowerMaxBeStolenTimes) {
		return nil, errors.WithStack(ErrStealFlowerFull)
	}
	if hasStealRecord(ctx, r.DB(), r.RoleID, friendID, plotID) {
		return nil, errors.WithStack(ErrStealLocked)
	}

	if err := createStealRecord(ctx, r.DB(), &StealRecord{
		OwnerID:   friendID,
		PlotID:    plotID,
		StealerID: r.RoleID,
		FlowerID:  plot.FlowerID,
		StealTime: time.Now(),
	}); err != nil {
		return nil, err
	}
	r.incDailyCount(friendID)

	flowerCfg := r.Cfg().TbFlower.Get(plot.FlowerID)
	if flowerCfg == nil {
		return nil, errors.New("flower config not found")
	}
	rewardNum := int(cfg.StealRewardNum)
	if err := r.Role.Bag.SaveGoods(ctx, nil,
		[]*gamecfg.GardenGoodStack{bag.MakeGoodStack(int(flowerCfg.HarvestItemId), rewardNum)}, "steal_flower"); err != nil {
		return nil, err
	}
	return &pb.RspPlotSteal{
		Success: true,
		Rewards: []*pb.PGoodInfo{{PropId: flowerCfg.HarvestItemId, Num: int64(rewardNum)}},
	}, nil
}
