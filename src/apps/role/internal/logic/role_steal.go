package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gserver/core/gxypgx"
	"gorm.io/gorm/clause"
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

// ========== 每日偷取计数模型 ==========

type DailyStealEntry struct {
	FriendID int64 `json:"friend_id"`
	Count    int32 `json:"count"`
}

// DailyStealMap 存储玩家对所有好友的每日偷取次数
// key: friendID
type DailyStealMap map[int64]*DailyStealEntry

func (m DailyStealMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *DailyStealMap) Scan(value any) error {
	if value == nil {
		*m = make(DailyStealMap)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("invalid type for DailyStealMap")
	}
	var dm DailyStealMap
	if err := json.Unmarshal(bytes, &dm); err != nil {
		return err
	}
	*m = dm
	return nil
}

type RoleStealState struct {
	RolePersistState
	DailySteals DailyStealMap `gorm:"column:daily_steals;type:jsonb"`
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

func (r *RoleSteal) ReqFriendPlotInfo(ctx context.Context, req *pb.ReqFriendPlotInfo) (*pb.RspFriendPlotInfo, error) {
	friendID := req.FriendId

	if !isFriend(ctx, r.RoleID, friendID) {
		return nil, ErrNotFriend
	}

	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	err := gxypgx.DB().WithContext(ctx).Table("role_plot").
		Where("role_id = ?", friendID).First(&row).Error
	if err != nil {
		return &pb.RspFriendPlotInfo{}, nil
	}

	cfg := gameconfig.GameConfig().TbFriendConfig.Get()
	dailyCount := r.getDailyCount(friendID)

	rsp := &pb.RspFriendPlotInfo{
		StealLimit: cfg.StealPerFriendDailyLimit,
		StealUsed:  dailyCount,
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
			if stolen < int64(cfg.FlowerMaxBeStolenTimes) && dailyCount < cfg.StealPerFriendDailyLimit && !hasStealRecord(ctx, r.RoleID, friendID, plot.PlotID) {
				info.CanSteal = true
			}
		}

		rsp.Plots = append(rsp.Plots, info)
	}

	return rsp, nil
}

func (r *RoleSteal) ReqStealFlower(ctx context.Context, req *pb.ReqStealFlower) (*pb.RspStealFlower, error) {
	friendID := req.FriendId
	plotID := req.PlotId
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()

	if !isFriend(ctx, r.RoleID, friendID) {
		return nil, ErrNotFriend
	}

	dailyCount := r.getDailyCount(friendID)
	if dailyCount >= cfg.StealPerFriendDailyLimit {
		return nil, ErrStealDailyFull
	}

	// FOR UPDATE lock target's role_plot row
	tx := gxypgx.DB().WithContext(ctx).Begin()
	defer tx.Rollback()

	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Table("role_plot").
		Where("role_id = ?", friendID).First(&row).Error
	if err != nil {
		return nil, err
	}

	plot, ok := row.Plots[plotID]
	if !ok {
		return nil, ErrStealLocked
	}
	state := getPlotState(plot)
	if state != int32(pb.PlotState_PLOT_HARVESTABLE) {
		return nil, ErrStealNotHarvestable
	}

	// Check per-flower total stolen count
	stolenCount, err := countPlotStolen(ctx, friendID, plotID)
	if err != nil {
		return nil, err
	}
	if stolenCount >= int64(cfg.FlowerMaxBeStolenTimes) {
		return nil, ErrStealFlowerFull
	}

	// Check if already stolen this plot this cycle
	alreadyStolen := hasStealRecord(ctx, r.RoleID, friendID, plotID)
	if alreadyStolen {
		return nil, ErrStealLocked
	}

	// Insert steal_record
	if err := tx.Create(&StealRecord{
		OwnerID:   friendID,
		PlotID:    plotID,
		StealerID: r.RoleID,
		FlowerID:  plot.FlowerID,
		StealTime: time.Now(),
	}).Error; err != nil {
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	r.incDailyCount(friendID)

	// Give reward
	flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
	if flowerCfg == nil {
		return nil, errors.New("flower config not found")
	}
	rewardNum := int(cfg.StealRewardNum)

	if err := r.Role.Bag.SaveGoods(ctx, nil,
		[]*gamecfg.GardenGoodStack{bag.MakeGoodStack(int(flowerCfg.HarvestItemId), rewardNum)}, "steal_flower"); err != nil {
		return nil, err
	}

	return &pb.RspStealFlower{
		Success: true,
		Rewards: []*pb.PGoodInfo{
			{PropId: flowerCfg.HarvestItemId, Num: int64(rewardNum)},
		},
	}, nil
}
