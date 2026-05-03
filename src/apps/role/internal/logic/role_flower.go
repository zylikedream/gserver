package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"time"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"

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

func (m FlowerMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *FlowerMap) Scan(value interface{}) error {
	if value == nil {
		*m = make(FlowerMap)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("invalid type for FlowerMap")
	}
	var flowerMap map[int32]*FlowerData
	if err := json.Unmarshal(bytes, &flowerMap); err != nil {
		return err
	}
	*m = FlowerMap(flowerMap)
	return nil
}

type RoleFlowerState struct {
	RolePersistState
	Flowers FlowerMap `gorm:"column:flowers;type:jsonb"`
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

// ========== 公开方法 ==========

func (r *RoleFlower) AddFlower(flowerID int32) {
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
	now := time.Now()
	state := flower.State
	if state == int32(pb.FlowerState_FLOWER_BREEDING) && now.After(flower.StateTime) {
		state = int32(pb.FlowerState_FLOWER_BREED_DONE)
	}
	return &pb.PFlowerInfo{
		FlowerId:   flower.FlowerID,
		State:      pb.FlowerState(state),
		StateTime:  flower.StateTime.Unix(),
		Level:      flower.Level,
		BreakStage: flower.BreakStage,
	}
}

func (r *RoleFlower) ReqStartBreed(ctx context.Context, req *pb.ReqStartBreed) (*pb.RspStartBreed, error) {
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

	cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
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

	return &pb.RspStartBreed{Flower: PFlowerInfo(flower)}, nil
}

func (r *RoleFlower) ReqFinishBreed(ctx context.Context, req *pb.ReqFinishBreed) (*pb.RspFinishBreed, error) {
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

	return &pb.RspFinishBreed{Flower: PFlowerInfo(flower)}, nil
}

func (r *RoleFlower) ReqUpgradeFlower(ctx context.Context, req *pb.ReqUpgradeFlower) (*pb.RspUpgradeFlower, error) {
	flowerID := req.FlowerId

	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}

	cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
	if cfg == nil {
		return nil, errors.Errorf("flower config not found: %d", flowerID)
	}

	nextLevel := flower.Level + 1
	levelCfg := gameconfig.GameConfig().GetFlowerLevelByGroup(cfg.LevelGroup, nextLevel)
	if levelCfg == nil {
		return nil, ErrFlowerMaxLevel
	}

	// Check breakthrough gate
	nextBreak := gameconfig.GameConfig().GetFlowerBreakByGroup(cfg.LevelGroup, flower.BreakStage+1)
	if nextBreak != nil && nextLevel > nextBreak.NeedLevel {
		return nil, ErrFlowerNeedBreak
	}

	// Check and deduct resources
	coinCost := MakeGoodStack(GOLD_ITEM_ID, int(levelCfg.UpgradeCoinCost))
	essenceCost := MakeGoodStack(int(cfg.EssenceItemId), int(levelCfg.UpgradeEssenceCost))
	if !r.Role.Bag.CheckGoods([]*gamecfg.GardenGoodStack{coinCost, essenceCost}) {
		return nil, ErrGoodNotEnough
	}

	if err := r.Role.Bag.SaveGoods(ctx, []*gamecfg.GardenGoodStack{coinCost, essenceCost}, nil, "flower_upgrade"); err != nil {
		return nil, err
	}

	flower.Level = nextLevel
	r.MarkDirty()

	return &pb.RspUpgradeFlower{Flower: PFlowerInfo(flower)}, nil
}

func (r *RoleFlower) ReqBreakFlower(ctx context.Context, req *pb.ReqBreakFlower) (*pb.RspBreakFlower, error) {
	flowerID := req.FlowerId

	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}

	cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
	if cfg == nil {
		return nil, errors.Errorf("flower config not found: %d", flowerID)
	}

	nextBreakStage := flower.BreakStage + 1
	breakCfg := gameconfig.GameConfig().GetFlowerBreakByGroup(cfg.LevelGroup, nextBreakStage)
	if breakCfg == nil {
		return nil, ErrFlowerBreakMax
	}

	if flower.Level < breakCfg.NeedLevel {
		return nil, ErrFlowerBreakLevel
	}

	// TODO: Check PlayerLevelLimit when player level system is implemented

	// Build resource deduction list
	var removeGoods []*gamecfg.GardenGoodStack
	if breakCfg.CoinCost > 0 {
		removeGoods = append(removeGoods, MakeGoodStack(GOLD_ITEM_ID, int(breakCfg.CoinCost)))
	}
	if breakCfg.EssenceCost > 0 {
		removeGoods = append(removeGoods, MakeGoodStack(int(cfg.EssenceItemId), int(breakCfg.EssenceCost)))
	}
	if breakCfg.BreakItemNum > 0 {
		removeGoods = append(removeGoods, MakeGoodStack(int(breakCfg.BreakItemId), int(breakCfg.BreakItemNum)))
	}

	if !r.Role.Bag.CheckGoods(removeGoods) {
		return nil, ErrGoodNotEnough
	}
	if err := r.Role.Bag.SaveGoods(ctx, removeGoods, nil, "flower_break"); err != nil {
		return nil, err
	}

	flower.BreakStage = nextBreakStage
	r.MarkDirty()

	return &pb.RspBreakFlower{Flower: PFlowerInfo(flower)}, nil
}
