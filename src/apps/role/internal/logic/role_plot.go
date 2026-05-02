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
	ErrPlotLocked      = errors.New("plot not unlocked")
	ErrPlotNotEmpty    = errors.New("plot is not empty")
	ErrPlotNotPlanted  = errors.New("plot is not planted")
	ErrPlotNotGrowing  = errors.New("plot is not growing")
	ErrPlotNotReady    = errors.New("plot not ready for harvest")
	ErrPlotHarvestable = errors.New("plot is harvestable, harvest first")
)

// ========== 数据模型 ==========

type PlotData struct {
	PlotID       int32     `json:"plot_id"`
	FlowerID     int32     `json:"flower_id"`
	State        int32     `json:"state"`
	HarvestCount int32     `json:"harvest_count"`
	StateTime    time.Time `json:"state_time"`
}

type PlotMap map[int32]*PlotData

func (m PlotMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *PlotMap) Scan(value interface{}) error {
	if value == nil {
		*m = make(PlotMap)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("invalid type for PlotMap")
	}
	var plotMap map[int32]*PlotData
	if err := json.Unmarshal(bytes, &plotMap); err != nil {
		return err
	}
	*m = PlotMap(plotMap)
	return nil
}

type RolePlotState struct {
	RolePersistState
	Plots PlotMap `gorm:"column:plots;type:jsonb"`
}

func (RolePlotState) TableName() string { return "role_plot" }

// ========== 模块 ==========

type RolePlot struct {
	RoleModule
	RolePlotState
}

var _ IRoleModule = (*RolePlot)(nil)

func (r *RolePlot) PersistState() IPersistState {
	return &r.RolePlotState
}

func (r *RolePlot) OnModInit(ctx context.Context) error {
	if r.Plots == nil {
		r.Plots = make(PlotMap)
	}
	return nil
}

// ========== 公开方法 ==========

func (r *RolePlot) UnlockPlot(plotID int32) {
	r.Plots[plotID] = &PlotData{
		PlotID: plotID,
	}
	r.MarkDirty()
}

// ========== 辅助方法 ==========

// getPlotState 返回地块实际状态，GROWING 到时间后转为 HARVESTABLE
func getPlotState(plot *PlotData) int32 {
	state := plot.State
	if state == int32(pb.PlotState_PLOT_GROWING) && time.Now().After(plot.StateTime) {
		state = int32(pb.PlotState_PLOT_HARVESTABLE)
	}
	return state
}

// pPlotInfo 将 PlotData 转为 proto
func pPlotInfo(plot *PlotData) *pb.PPlotInfo {
	return &pb.PPlotInfo{
		PlotId:       plot.PlotID,
		FlowerId:     plot.FlowerID,
		State:        pb.PlotState(getPlotState(plot)),
		HarvestCount: plot.HarvestCount,
		StateTime:    plot.StateTime.Unix(),
	}
}

// ========== Proto Handler ==========

func (r *RolePlot) ReqPlotInfo(ctx context.Context, req *pb.ReqPlotInfo) (*pb.RspPlotInfo, error) {
	rsp := &pb.RspPlotInfo{Plots: []*pb.PPlotInfo{}}
	for _, plot := range r.Plots {
		rsp.Plots = append(rsp.Plots, pPlotInfo(plot))
	}
	return rsp, nil
}

func (r *RolePlot) ReqUnlockPlot(ctx context.Context, req *pb.ReqUnlockPlot) (*pb.RspUnlockPlot, error) {
	plotID := req.PlotId

	cfg := gameconfig.GameConfig().TbGardenPlot.Get(plotID)
	if cfg == nil {
		return nil, errors.Errorf("plot config not found: %d", plotID)
	}

	if _, ok := r.Plots[plotID]; ok {
		return nil, ErrPlotNotEmpty
	}

	if !r.Role.Bag.CheckGoods(cfg.Cost) {
		return nil, ErrGoodNotEnough
	}
	if err := r.Role.Bag.SaveGoods(ctx, cfg.Cost, nil, "unlock_plot"); err != nil {
		return nil, err
	}

	r.UnlockPlot(plotID)

	return &pb.RspUnlockPlot{Plot: pPlotInfo(r.Plots[plotID])}, nil
}

func (r *RolePlot) ReqPlantFlower(ctx context.Context, req *pb.ReqPlantFlower) (*pb.RspPlantFlower, error) {
	flowerID := req.FlowerId

	// 检查花朵是否已解锁
	flower, ok := r.Role.Flower.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}

	// 检查花朵是否已收获
	if flower.State != int32(pb.FlowerState_FLOWER_HARVESTED) {
		return nil, ErrFlowerLocked
	}

	// 校验所有地块状态
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		if getPlotState(plot) != int32(pb.PlotState_PLOT_EMPTY) {
			return nil, ErrPlotNotEmpty
		}
	}

	// 种植
	for _, plotID := range req.PlotIds {
		plot := r.Plots[plotID]
		plot.FlowerID = flowerID
		plot.State = int32(pb.PlotState_PLOT_PLANTED)
	}
	r.MarkDirty()

	rsp := &pb.RspPlantFlower{Plots: []*pb.PPlotInfo{}}
	for _, plotID := range req.PlotIds {
		rsp.Plots = append(rsp.Plots, pPlotInfo(r.Plots[plotID]))
	}
	return rsp, nil
}

func (r *RolePlot) ReqWaterFlower(ctx context.Context, req *pb.ReqWaterFlower) (*pb.RspWaterFlower, error) {
	// 先校验所有地块状态，并计算总水滴消耗
	var totalWaterCost int32
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		if getPlotState(plot) != int32(pb.PlotState_PLOT_PLANTED) {
			return nil, ErrPlotNotPlanted
		}
		flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
		if flowerCfg == nil {
			return nil, errors.Errorf("flower config not found: %d", plot.FlowerID)
		}
		totalWaterCost += flowerCfg.WaterCost
	}

	// 扣除水滴
	if totalWaterCost > 0 {
		waterCost := MakeGoodStack(WATER_ITEM_ID, int(totalWaterCost))
		if !r.Role.Bag.CheckGoods([]*gamecfg.GardenGoodStack{waterCost}) {
			return nil, ErrGoodNotEnough
		}
		if err := r.Role.Bag.SaveGoods(ctx, []*gamecfg.GardenGoodStack{waterCost}, nil, "water_flower"); err != nil {
			return nil, err
		}
	}

	// 浇水：进入 GROWING 状态
	now := time.Now()
	for _, plotID := range req.PlotIds {
		plot := r.Plots[plotID]
		flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
		plot.State = int32(pb.PlotState_PLOT_GROWING)
		plot.StateTime = now.Add(time.Duration(flowerCfg.GrowTime) * time.Second)
	}
	r.MarkDirty()

	rsp := &pb.RspWaterFlower{Plots: []*pb.PPlotInfo{}}
	for _, plotID := range req.PlotIds {
		rsp.Plots = append(rsp.Plots, pPlotInfo(r.Plots[plotID]))
	}
	return rsp, nil
}

func (r *RolePlot) ReqHarvestFlower(ctx context.Context, req *pb.ReqHarvestFlower) (*pb.RspHarvestFlower, error) {
	// 校验所有地块状态，并收集收获物品
	var harvestItems []*gamecfg.GardenGoodStack
	now := time.Now()
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		state := getPlotState(plot)
		if state != int32(pb.PlotState_PLOT_HARVESTABLE) {
			return nil, ErrPlotNotReady
		}
		_ = state // 使用 plot 的原始 state 判断
		if plot.State != int32(pb.PlotState_PLOT_GROWING) || !now.After(plot.StateTime) {
			return nil, ErrPlotNotReady
		}

		flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
		if flowerCfg == nil {
			return nil, errors.Errorf("flower config not found: %d", plot.FlowerID)
		}

		harvestItems = append(harvestItems, MakeGoodStack(int(flowerCfg.HarvestItemId), int(flowerCfg.HarvestNum)))
	}

	// 添加收获物品到背包
	if len(harvestItems) > 0 {
		if err := r.Role.Bag.SaveGoods(ctx, nil, harvestItems, "harvest_flower"); err != nil {
			return nil, err
		}
	}

	// 更新地块状态
	for _, plotID := range req.PlotIds {
		plot := r.Plots[plotID]
		flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
		plot.HarvestCount++
		if plot.HarvestCount >= flowerCfg.HarvestTimes {
			// 收获完毕，重置为空地
			plot.FlowerID = 0
			plot.State = int32(pb.PlotState_PLOT_EMPTY)
			plot.HarvestCount = 0
			plot.StateTime = time.Time{}
		} else {
			// 还有收获次数，继续生长
			plot.StateTime = now.Add(time.Duration(flowerCfg.HarvestInterval) * time.Second)
		}
	}
	r.MarkDirty()

	rsp := &pb.RspHarvestFlower{Plots: []*pb.PPlotInfo{}}
	for _, plotID := range req.PlotIds {
		rsp.Plots = append(rsp.Plots, pPlotInfo(r.Plots[plotID]))
	}
	return rsp, nil
}

func (r *RolePlot) ReqRemovePlant(ctx context.Context, req *pb.ReqRemovePlant) (*pb.RspRemovePlant, error) {
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		state := getPlotState(plot)
		if state == int32(pb.PlotState_PLOT_HARVESTABLE) {
			return nil, ErrPlotHarvestable
		}
		if plot.State != int32(pb.PlotState_PLOT_PLANTED) && plot.State != int32(pb.PlotState_PLOT_GROWING) {
			return nil, ErrPlotNotPlanted
		}
	}

	// 重置地块为空地
	for _, plotID := range req.PlotIds {
		plot := r.Plots[plotID]
		plot.FlowerID = 0
		plot.State = int32(pb.PlotState_PLOT_EMPTY)
		plot.HarvestCount = 0
		plot.StateTime = time.Time{}
	}
	r.MarkDirty()

	rsp := &pb.RspRemovePlant{Plots: []*pb.PPlotInfo{}}
	for _, plotID := range req.PlotIds {
		rsp.Plots = append(rsp.Plots, pPlotInfo(r.Plots[plotID]))
	}
	return rsp, nil
}
