package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"time"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/pkg/errors"
)

var (
	ErrFlowerLocked      = errors.New("flower not unlocked")
	ErrFlowerBreedBusy   = errors.New("another flower is breeding")
	ErrFlowerNotBreeding = errors.New("flower is not breeding")
	ErrFlowerNotDone     = errors.New("breed not finished yet")
)

// ========== 数据模型 ==========

type FlowerData struct {
	FlowerID  int32     `json:"flower_id"`
	Status    int32     `json:"status"`
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

func (r *RoleFlower) UnlockFlower(flowerID int32) {
	r.Flowers[flowerID] = &FlowerData{
		FlowerID: flowerID,
		Status:   int32(pb.FlowerStatus_UNLOCKED),
	}
	r.MarkDirty()
}

func (r *RoleFlower) FindBreeding() *FlowerData {
	for _, f := range r.Flowers {
		if f.Status == int32(pb.FlowerStatus_BREEDING) {
			return f
		}
	}
	return nil
}

// ========== Proto Handler ==========

func (r *RoleFlower) ReqBreedInfo(ctx context.Context, req *pb.ReqBreedInfo) (*pb.RspBreedInfo, error) {
	now := time.Now()
	rsp := &pb.RspBreedInfo{Flowers: []*pb.PFlowerState{}}
	for _, f := range r.Flowers {
		status := f.Status
		if status == int32(pb.FlowerStatus_BREEDING) && now.After(f.StateTime) {
			status = int32(pb.FlowerStatus_BREED_DONE)
		}
		rsp.Flowers = append(rsp.Flowers, &pb.PFlowerState{
			FlowerId:  f.FlowerID,
			Status:    pb.FlowerStatus(status),
			StateTime: f.StateTime.Unix(),
		})
	}
	return rsp, nil
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
		return nil, bag.ErrGoodNotEnough
	}
	if err := r.Role.Bag.SaveGoods(ctx, cfg.BreedCost, nil, "breed"); err != nil {
		return nil, err
	}

	flower.Status = int32(pb.FlowerStatus_BREEDING)
	flower.StateTime = time.Now().Add(time.Duration(cfg.BreedTime) * time.Second)
	r.MarkDirty()

	return &pb.RspStartBreed{}, nil
}

func (r *RoleFlower) ReqFinishBreed(ctx context.Context, req *pb.ReqFinishBreed) (*pb.RspFinishBreed, error) {
	flowerID := req.FlowerId

	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}
	if flower.Status != int32(pb.FlowerStatus_BREEDING) {
		return nil, ErrFlowerNotBreeding
	}

	if time.Now().Before(flower.StateTime) {
		return nil, ErrFlowerNotDone
	}

	if err := r.Role.Bag.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{
		MakeGoodStack(int(flowerID), 1),
	}, "breed"); err != nil {
		return nil, err
	}

	flower.Status = int32(pb.FlowerStatus_HARVESTED)
	r.MarkDirty()

	return &pb.RspFinishBreed{}, nil
}
