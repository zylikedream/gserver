package logic

import (
	"context"
	"time"

	"gserver/core/gxylog"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/pkg/gameconfig"

	"github.com/pkg/errors"
)

var (
	ErrFlowerLocked       = errors.New("flower not unlocked")
	ErrFlowerBreedBusy    = errors.New("another flower is breeding")
	ErrFlowerWrongState   = errors.New("flower is at wrong state")
	ErrFlowerNotBreedDone = errors.New("breed not finished yet")
)

// ========== 数据模型 ==========

type FlowerData struct {
	FlowerID   int32     `json:"flower_id"`
	State      int32     `json:"state"`
	StateTime  time.Time `json:"state_time"`
	Level      int32     `json:"level"`       // 当前等级，默认 1
	BreakStage int32     `json:"break_stage"` // 突破阶段，0=未突破，1=已突破
}

type FlowerMap map[int32]*FlowerData

type RoleFlowerState struct {
	RolePersistState
	Flowers FlowerMap `gorm:"column:flowers;type:jsonb;serializer:json"`
}

func (RoleFlowerState) TableName() string { return "role_flower" }

// ========== 模块 ==========

type RoleFlower struct {
	RoleModule
	RoleFlowerState
}

var _ IRoleModule = (*RoleFlower)(nil)

func (r *RoleFlower) PersistState() IPersistState {
	return &r.RoleFlowerState
}

func (r *RoleFlower) OnModInit(ctx context.Context) error {
	if r.Flowers == nil {
		r.Flowers = make(FlowerMap)
	}
	return nil
}

func (r *RoleFlower) OnModStart(ctx context.Context) error {
	r.Role.eventBus.Subscribe(event.EVENT_GOOD_CHANGE, r.onGoodChangeEvent)
	return nil
}

func (r *RoleFlower) onGoodChangeEvent(ctx context.Context, param event.EventParam) {
	data, ok := param.Data.(event.GoodChangeEventData)
	if !ok {
		return
	}
	for _, change := range data.Changes {
		item := gameconfig.Get().TbItem.Get(int32(change.GoodID))
		if item == nil || item.SubType != gamecfg.GardenEItemSubType_SEED {
			continue
		}
		flowerID := int32(change.GoodID)
		r.AddFlower(ctx, flowerID)

	}
}

// ========== 公开方法 ==========

func (r *RoleFlower) AddFlower(ctx context.Context, flowerID int32) {
	if _, ok := r.Flowers[flowerID]; ok {
		gxylog.Warn(ctx, "flower already added", gxylog.Num("flowerID", int64(flowerID)))
		return
	}
	r.Flowers[flowerID] = &FlowerData{
		FlowerID:   flowerID,
		State:      int32(pb.FlowerState_FLOWER_UNLOCKED),
		StateTime:  time.Now(),
		Level:      1,
		BreakStage: 0,
	}
	r.MarkDirty()
}

func (r *RoleFlower) FindBreeding() *FlowerData {
	for _, f := range r.Flowers {
		if f.State == int32(pb.FlowerState_FLOWER_BREEDING) {
			return f
		}
	}
	return nil
}

// GetFlowerLevel 返回花的等级信息，供 RolePlot 查询
func (r *RoleFlower) GetFlowerLevel(flowerID int32) (level int32, breakStage int32) {
	flower, ok := r.Flowers[flowerID]
	if !ok {
		return 1, 0
	}
	// Legacy data without level defaults to 1
	if flower.Level == 0 {
		return 1, flower.BreakStage
	}
	return flower.Level, flower.BreakStage
}

// ========== Proto Handler ==========

func (r *RoleFlower) ReqFlowerInfo(ctx context.Context, req *pb.ReqFlowerInfo) (*pb.RspFlowerInfo, error) {
	rsp := &pb.RspFlowerInfo{Flowers: []*pb.PFlowerInfo{}}
	for _, f := range r.Flowers {
		rsp.Flowers = append(rsp.Flowers, PFlowerInfo(f))
	}
	return rsp, nil
}

func PFlowerInfo(flower *FlowerData) *pb.PFlowerInfo {
	state := getFlowerDisplayState(flower, time.Now())
	return &pb.PFlowerInfo{
		FlowerId:   flower.FlowerID,
		State:      pb.FlowerState(state),
		StateTime:  flower.StateTime.Unix(),
		Level:      flower.Level,
		BreakStage: flower.BreakStage,
	}
}

func getFlowerDisplayState(flower *FlowerData, now time.Time) int32 {
	state := flower.State
	if state == int32(pb.FlowerState_FLOWER_BREEDING) && now.After(flower.StateTime) {
		return int32(pb.FlowerState_FLOWER_BREED_DONE)
	}
	return state
}

func (r *RoleFlower) ReqFlowerStartBreed(ctx context.Context, req *pb.ReqFlowerStartBreed) (*pb.RspFlowerStartBreed, error) {
	flowerID := req.FlowerId

	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}

	if r.FindBreeding() != nil {
		return nil, ErrFlowerBreedBusy
	}
	// 只有unlock状态的可以烘焙
	if flower.State != int32(pb.FlowerState_FLOWER_UNLOCKED) {
		return nil, ErrFlowerWrongState
	}

	cfg := gameconfig.Get().TbFlower.Get(flowerID)
	if cfg == nil {
		return nil, errors.Errorf("flower config not found: %d", flowerID)
	}

	if !r.Role.Bag.CheckGoods(cfg.BreedCost) {
		return nil, ErrGoodNotEnough
	}
	if err := r.Role.Bag.SaveGoods(ctx, cfg.BreedCost, nil, "breed"); err != nil {
		return nil, err
	}

	flower.State = int32(pb.FlowerState_FLOWER_BREEDING)
	flower.StateTime = time.Now().Add(time.Duration(cfg.BreedTime) * time.Second)
	r.MarkDirty()
	r.Role.PublishRoleEvent(ctx, event.EVENT_BREED_START, event.BreedStartEventData{FlowerID: flowerID})

	return &pb.RspFlowerStartBreed{Flower: PFlowerInfo(flower)}, nil
}

func (r *RoleFlower) ReqFlowerFinishBreed(ctx context.Context, req *pb.ReqFlowerFinishBreed) (*pb.RspFlowerFinishBreed, error) {
	flowerID := req.FlowerId

	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}
	if !(flower.State == int32(pb.FlowerState_FLOWER_BREEDING) && time.Now().After(flower.StateTime)) {
		return nil, ErrFlowerNotBreedDone
	}

	flower.State = int32(pb.FlowerState_FLOWER_HARVESTED)
	flower.StateTime = time.Now()
	r.MarkDirty()
	r.Role.PublishRoleEvent(ctx, event.EVENT_BREED_FINISH, event.BreedFinishEventData{FlowerID: flowerID})

	return &pb.RspFlowerFinishBreed{Flower: PFlowerInfo(flower)}, nil
}

func (r *RoleFlower) ReqFlowerUpgrade(ctx context.Context, req *pb.ReqFlowerUpgrade) (*pb.RspFlowerUpgrade, error) {
	flowerID := req.FlowerId

	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}

	cfg := gameconfig.Get().TbFlower.Get(flowerID)
	if cfg == nil {
		return nil, errors.Errorf("flower config not found: %d", flowerID)
	}

	// if flower is not harvested, return error
	if flower.State != int32(pb.FlowerState_FLOWER_HARVESTED) {
		return nil, ErrFlowerWrongState
	}

	nextLevel := flower.Level + 1
	levelCfg := gameconfig.Get().GetFlowerLevelByGroup(cfg.LevelGroup, nextLevel)
	if levelCfg == nil {
		return nil, ErrFlowerMaxLevel
	}

	// 不能超过玩家等级
	if r.Role.Basic.Level < nextLevel {
		return nil, ErrPlayerLevelNotEnough
	}

	// Check breakthrough gate
	nextBreak := gameconfig.Get().GetFlowerBreakByGroup(cfg.LevelGroup, flower.BreakStage+1)
	if nextBreak != nil && nextLevel > nextBreak.NeedLevel {
		return nil, ErrFlowerNeedBreak
	}

	// Check and deduct resources
	coinCost := bag.MakeGoodStack(GOLD_ITEM_ID, int(levelCfg.UpgradeCoinCost))
	essenceCost := bag.MakeGoodStack(int(cfg.EssenceItemId), int(levelCfg.UpgradeEssenceCost))
	totalCost := []*gamecfg.GardenGoodStack{
		coinCost,
		essenceCost,
	}
	if !r.Role.Bag.CheckGoods(totalCost) {
		return nil, ErrGoodNotEnough
	}

	if err := r.Role.Bag.SaveGoods(ctx, totalCost, nil, "flower_upgrade"); err != nil {
		return nil, err
	}

	oldLevel := flower.Level
	flower.Level = nextLevel
	r.MarkDirty()
	r.Role.PublishRoleEvent(ctx, event.EVENT_FLOWER_LEVEL, event.FlowerLevelEventData{
		FlowerID: flowerID,
		OldLevel: oldLevel,
		NewLevel: nextLevel,
	})

	return &pb.RspFlowerUpgrade{Flower: PFlowerInfo(flower)}, nil
}

func (r *RoleFlower) ReqFlowerBreak(ctx context.Context, req *pb.ReqFlowerBreak) (*pb.RspFlowerBreak, error) {
	flowerID := req.FlowerId

	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}

	cfg := gameconfig.Get().TbFlower.Get(flowerID)
	if cfg == nil {
		return nil, errors.Errorf("flower config not found: %d", flowerID)
	}

	nextBreakStage := flower.BreakStage + 1
	breakCfg := gameconfig.Get().GetFlowerBreakByGroup(cfg.LevelGroup, nextBreakStage)
	if breakCfg == nil {
		return nil, ErrFlowerBreakMax
	}

	if flower.Level < breakCfg.NeedLevel {
		return nil, ErrFlowerBreakLevel
	}

	if r.Role.Basic.Level < breakCfg.PlayerLevelLimit {
		return nil, ErrFlowerBreakPlayerLevel
	}

	// Build resource deduction list
	var removeGoods []*gamecfg.GardenGoodStack
	if breakCfg.CoinCost > 0 {
		removeGoods = append(removeGoods, bag.MakeGoodStack(GOLD_ITEM_ID, int(breakCfg.CoinCost)))
	}
	if breakCfg.EssenceCost > 0 {
		removeGoods = append(removeGoods, bag.MakeGoodStack(int(cfg.EssenceItemId), int(breakCfg.EssenceCost)))
	}
	if breakCfg.BreakItemNum > 0 {
		removeGoods = append(removeGoods, bag.MakeGoodStack(int(breakCfg.BreakItemId), int(breakCfg.BreakItemNum)))
	}

	if !r.Role.Bag.CheckGoods(removeGoods) {
		return nil, ErrGoodNotEnough
	}
	if err := r.Role.Bag.SaveGoods(ctx, removeGoods, nil, "flower_break"); err != nil {
		return nil, err
	}

	flower.BreakStage = nextBreakStage
	r.MarkDirty()

	return &pb.RspFlowerBreak{Flower: PFlowerInfo(flower)}, nil
}
