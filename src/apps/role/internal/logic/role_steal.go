package logic

import (
	"context"
	"errors"
	"time"

	"gserver/core/gxypgx"
	"gserver/gameconfig"
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

func (r *RolePlot) ReqFriendPlotInfo(ctx context.Context, req *pb.ReqFriendPlotInfo) (*pb.RspFriendPlotInfo, error) {
	friendID := req.FriendId

	if !isFriend(ctx, r.RoleID, friendID) {
		return nil, ErrNotFriend
	}

	// Read target's role_plot row from DB
	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	err := gxypgx.DB().WithContext(ctx).Table("role_plot").
		Where("role_id = ?", friendID).First(&row).Error
	if err != nil {
		return &pb.RspFriendPlotInfo{}, nil
	}

	cfg := gameconfig.GameConfig().TbFriendConfig.Get()
	dailyCount, _ := countDailySteal(ctx, r.RoleID, friendID)

	rsp := &pb.RspFriendPlotInfo{
		StealLimit: cfg.StealPerFriendDailyLimit,
		StealUsed:  int32(dailyCount),
	}

	for _, plot := range row.Plots {
		state := getPlotState(plot)
		info := &pb.PPlotInfo{
			PlotId:       plot.PlotID,
			FlowerId:     plot.FlowerID,
			State:        pb.PlotState(state),
			HarvestCount: plot.HarvestCount,
			StateTime:    plot.StateTime.Unix(),
		}

		if state == int32(pb.PlotState_PLOT_HARVESTABLE) {
			stolen, _ := countPlotStolen(ctx, friendID, plot.PlotID)
			if stolen < int64(cfg.FlowerMaxBeStolenTimes) && dailyCount < int64(cfg.StealPerFriendDailyLimit) {
				info.CanSteal = true
			}
		}

		rsp.Plots = append(rsp.Plots, info)
	}

	return rsp, nil
}

func (r *RolePlot) ReqStealFlower(ctx context.Context, req *pb.ReqStealFlower) (*pb.RspStealFlower, error) {
	friendID := req.FriendId
	plotID := req.PlotId
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()

	if !isFriend(ctx, r.RoleID, friendID) {
		return &pb.RspStealFlower{Success: false, Error: ErrNotFriend.Error()}, nil
	}

	// FOR UPDATE lock target's role_plot row
	tx := gxypgx.DB().WithContext(ctx).Begin()
	defer tx.Rollback()

	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	err := tx.Table("role_plot").
		Set("gorm:query_option", "FOR UPDATE").
		Where("role_id = ?", friendID).First(&row).Error
	if err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常，请稍后重试"}, nil
	}

	// Check plot exists and is harvestable
	plot, ok := row.Plots[plotID]
	if !ok {
		return &pb.RspStealFlower{Success: false, Error: ErrStealLocked.Error()}, nil
	}
	state := getPlotState(plot)
	if state != int32(pb.PlotState_PLOT_HARVESTABLE) {
		return &pb.RspStealFlower{Success: false, Error: ErrStealNotHarvestable.Error()}, nil
	}

	// Check per-flower stolen count
	stolenCount, err := countPlotStolen(ctx, friendID, plotID)
	if err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}
	if stolenCount >= int64(cfg.FlowerMaxBeStolenTimes) {
		return &pb.RspStealFlower{Success: false, Error: ErrStealFlowerFull.Error()}, nil
	}

	// Check daily steal count
	dailyCount, err := countDailySteal(ctx, r.RoleID, friendID)
	if err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}
	if dailyCount >= int64(cfg.StealPerFriendDailyLimit) {
		return &pb.RspStealFlower{Success: false, Error: ErrStealDailyFull.Error()}, nil
	}

	// Insert steal_record
	if err := tx.Create(&StealRecord{
		OwnerID:   friendID,
		PlotID:    plotID,
		StealerID: r.RoleID,
		FlowerID:  plot.FlowerID,
		StealTime: time.Now(),
	}).Error; err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}

	if err := tx.Commit().Error; err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}

	// Give reward
	flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
	if flowerCfg == nil {
		return &pb.RspStealFlower{Success: false, Error: "配置异常"}, nil
	}
	rewardNum := int(cfg.StealRewardNum)

	if err := r.Role.Bag.SaveGoods(ctx, nil,
		[]*gamecfg.GardenGoodStack{bag.MakeGoodStack(int(flowerCfg.HarvestItemId), rewardNum)},
		"steal_flower"); err != nil {
		return &pb.RspStealFlower{Success: false, Error: "背包异常"}, nil
	}

	return &pb.RspStealFlower{
		Success: true,
		Rewards: []*pb.PGoodInfo{
			{PropId: flowerCfg.HarvestItemId, Num: int64(rewardNum)},
		},
	}, nil
}

func isFriend(ctx context.Context, myID, targetID int64) bool {
	rel, _ := getRelation(ctx, myID, targetID)
	return rel == relationFriend
}
