package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"time"

	"gserver/gameconfig"
	"gserver/protocol/pb"

	"github.com/pkg/errors"
)

var (
	ErrFlowerLocked       = errors.New("flower not unlocked")
	ErrFlowerBreedBusy    = errors.New("another flower is breeding")
	ErrFlowerNotBreeding  = errors.New("flower is not breeding")
	ErrFlowerNotBreedDone = errors.New("breed not finished yet")
)

// ========== 数据模型 ==========

type FlowerData struct {
	FlowerID  int32     `json:"flower_id"`
	State     int32     `json:"state"`
	StateTime time.Time `json:"state_time"`
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
		FlowerID:  flowerID,
		State:     int32(pb.FlowerState_FLOWER_UNLOCKED),
		StateTime: time.Now(),
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
		FlowerId:  flower.FlowerID,
		State:     pb.FlowerState(state),
		StateTime: flower.StateTime.Unix(),
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
