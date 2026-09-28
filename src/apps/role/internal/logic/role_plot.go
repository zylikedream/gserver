package logic

import (
	"context"
	"math/rand"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/cockroachdb/errors"
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

type RolePlotState struct {
	RolePersistState
	Plots PlotMap `gorm:"column:plots;type:jsonb;serializer:json"`
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

func (r *RolePlot) OnCreate(ctx context.Context) {
	// 遍历配置表，初始化所有地块为锁定状态
	level := r.Role.Basic.Level
	for _, cfg := range r.Cfg().TbGardenPlot.GetDataList() {
		if level >= cfg.UnlockLevel {
			r.UnlockPlot(cfg.Id)
		}
	}
}

// ========== 持久化 ==========

func (r *RolePlot) refreshPlot(ctx context.Context) error {
	r.MarkDirty()
	publishRolePlotSnapshot(ctx, r.RoleID, r.Plots)
	return nil
}

// ========== 公开方法 ==========

func (r *RolePlot) UnlockPlot(plotID int32) {
	r.Plots[plotID] = &PlotData{
		PlotID: plotID,
	}
	r.MarkDirty()
}

func (r *RolePlot) AfterLogin(ctx context.Context) {
	publishRolePlotSnapshot(ctx, r.RoleID, r.Plots)
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

func (r *RolePlot) ReqPlotUnlock(ctx context.Context, req *pb.ReqPlotUnlock) (*pb.RspPlotUnlock, error) {
	plotID := req.PlotId

	cfg := r.Cfg().TbGardenPlot.Get(plotID)
	if cfg == nil {
		return nil, errors.Errorf("plot config not found: %d", plotID)
	}

	if _, ok := r.Plots[plotID]; ok {
		return nil, errors.WithStack(ErrPlotNotEmpty)
	}

	if r.Role.Basic.Level < cfg.UnlockLevel {
		return nil, errors.WithStack(ErrPlayerLevelNotEnough)
	}

	if !r.Role.Bag.CheckGoods(cfg.Cost) {
		return nil, errors.WithStack(ErrGoodNotEnough)
	}
	if err := r.Role.Bag.SaveGoods(ctx, cfg.Cost, nil, "unlock_plot"); err != nil {
		return nil, err
	}

	r.UnlockPlot(plotID)
	if err := r.refreshPlot(ctx); err != nil {
		return nil, err
	}
	r.Role.PublishRoleEvent(ctx, event.EVENT_UNLOCK_PLOT, event.UnlockPlotEventData{PlotID: plotID})

	return &pb.RspPlotUnlock{Plot: pPlotInfo(r.Plots[plotID])}, nil
}

func (r *RolePlot) ReqPlotPlant(ctx context.Context, req *pb.ReqPlotPlant) (*pb.RspPlotPlant, error) {
	flowerID := req.FlowerId

	// 检查花朵是否已解锁
	flower, ok := r.Role.Flower.Flowers[flowerID]
	if !ok {
		return nil, errors.WithStack(ErrFlowerLocked)
	}

	// 检查花朵是否已收获
	if flower.State != int32(pb.FlowerState_FLOWER_HARVESTED) {
		return nil, errors.WithStack(ErrFlowerLocked)
	}

	// 校验所有地块状态
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, errors.WithStack(ErrPlotLocked)
		}
		if getPlotState(plot) != int32(pb.PlotState_PLOT_EMPTY) {
			return nil, errors.WithStack(ErrPlotNotEmpty)
		}
	}

	// 种植
	for _, plotID := range req.PlotIds {
		plot := r.Plots[plotID]
		plot.FlowerID = flowerID
		plot.State = int32(pb.PlotState_PLOT_PLANTED)
	}
	if err := r.refreshPlot(ctx); err != nil {
		return nil, err
	}
	r.Role.PublishRoleEvent(ctx, event.EVENT_PLANT_FLOWER, event.PlantFlowerEventData{
		FlowerID: flowerID,
		PlotIDs:  append([]int32(nil), req.PlotIds...),
	})

	rsp := &pb.RspPlotPlant{Plots: []*pb.PPlotInfo{}}
	for _, plotID := range req.PlotIds {
		rsp.Plots = append(rsp.Plots, pPlotInfo(r.Plots[plotID]))
	}
	return rsp, nil
}

func (r *RolePlot) ReqPlotWater(ctx context.Context, req *pb.ReqPlotWater) (*pb.RspPlotWater, error) {
	// 先校验所有地块状态，并计算总水滴消耗
	var totalWaterCost int32
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, errors.WithStack(ErrPlotLocked)
		}
		if getPlotState(plot) != int32(pb.PlotState_PLOT_PLANTED) {
			return nil, errors.WithStack(ErrPlotNotPlanted)
		}
		flowerCfg := r.Cfg().TbFlower.Get(plot.FlowerID)
		if flowerCfg == nil {
			return nil, errors.Errorf("flower config not found: %d", plot.FlowerID)
		}
		totalWaterCost += flowerCfg.WaterCost
	}

	// 扣除水滴
	if totalWaterCost > 0 {
		waterCost := bag.MakeGoodStack(WATER_ITEM_ID, int(totalWaterCost))
		if !r.Role.Bag.CheckGoods([]*gamecfg.GardenGoodStack{waterCost}) {
			return nil, errors.WithStack(ErrGoodNotEnough)
		}
		if err := r.Role.Bag.SaveGoods(ctx, []*gamecfg.GardenGoodStack{waterCost}, nil, "water_flower"); err != nil {
			return nil, err
		}
	}

	// 浇水：进入 GROWING 状态
	now := time.Now()
	for _, plotID := range req.PlotIds {
		plot := r.Plots[plotID]
		flowerCfg := r.Cfg().TbFlower.Get(plot.FlowerID)
		plot.State = int32(pb.PlotState_PLOT_GROWING)
		plot.StateTime = now.Add(time.Duration(flowerCfg.GrowTime) * time.Second)
	}
	if err := r.refreshPlot(ctx); err != nil {
		return nil, err
	}
	r.Role.PublishRoleEvent(ctx, event.EVENT_WATER_FLOWER, event.WaterFlowerEventData{
		PlotIDs: append([]int32(nil), req.PlotIds...),
	})

	rsp := &pb.RspPlotWater{Plots: plotInfos(r.Plots, req.PlotIds)}
	return rsp, nil
}

// harvestResult 是一次收获的产出,在收获流程各阶段间传递。
type harvestResult struct {
	items   []*gamecfg.GardenGoodStack // 收获的作物
	essence []*gamecfg.GardenGoodStack // 掉落的精华
	flowers []event.HarvestFlowerItem  // 事件用的收获记录
}

func (r *RolePlot) ReqPlotHarvest(ctx context.Context, req *pb.ReqPlotHarvest) (*pb.RspPlotHarvest, error) {
	var rsp *pb.RspPlotHarvest
	err := withPlotLocks(ctx, r.RoleID, req.PlotIds, func() error {
		now := time.Now()
		if err := r.validateHarvestable(req.PlotIds, now); err != nil {
			return err
		}
		result, err := r.computeHarvest(ctx, req.PlotIds)
		if err != nil {
			return err
		}
		if err := r.saveHarvest(ctx, result); err != nil {
			return err
		}
		r.advancePlots(ctx, req.PlotIds, now)
		if err := r.refreshPlot(ctx); err != nil {
			return err
		}
		r.Role.PublishRoleEvent(ctx, event.EVENT_HARVEST_FLOWER, event.HarvestFlowerEventData{
			Items:   append([]*gamecfg.GardenGoodStack(nil), result.items...),
			Flowers: result.flowers,
		})
		rsp = &pb.RspPlotHarvest{Plots: plotInfos(r.Plots, req.PlotIds)}
		return nil
	})
	return rsp, err
}

// validateHarvestable 校验所有地块均可收获。
func (r *RolePlot) validateHarvestable(plotIDs []int32, now time.Time) error {
	for _, plotID := range plotIDs {
		plot, ok := r.Plots[plotID]
		if !ok {
			return errors.WithStack(ErrPlotLocked)
		}
		if getPlotState(plot) != int32(pb.PlotState_PLOT_HARVESTABLE) {
			return errors.WithStack(ErrPlotNotReady)
		}
		if plot.State != int32(pb.PlotState_PLOT_GROWING) || !now.After(plot.StateTime) {
			return errors.WithStack(ErrPlotNotReady)
		}
	}
	return nil
}

// computeHarvest 计算各地块收获的作物与精华掉落(只读,不写)。
func (r *RolePlot) computeHarvest(ctx context.Context, plotIDs []int32) (harvestResult, error) {
	var result harvestResult
	for _, plotID := range plotIDs {
		plot := r.Plots[plotID]
		flowerCfg := r.Cfg().TbFlower.Get(plot.FlowerID)
		if flowerCfg == nil {
			return harvestResult{}, errors.Errorf("flower config not found: %d", plot.FlowerID)
		}

		level, _ := r.Role.Flower.GetFlowerLevel(plot.FlowerID)
		levelCfg := r.Cfg().GetFlowerLevelByGroup(flowerCfg.LevelGroup, level)

		finalNum := flowerCfg.HarvestNum
		if levelCfg != nil {
			finalNum += levelCfg.HarvestNumAdd
		}

		stolenCount, _ := countPlotStolen(ctx, r.DB(), r.RoleID, plotID)
		minKeep := r.Cfg().TbFriendConfig.Get().OwnerMinKeepNum
		if int64(finalNum)-stolenCount > int64(minKeep) {
			finalNum = finalNum - int32(stolenCount)
		} else {
			finalNum = minKeep
		}

		result.items = append(result.items, bag.MakeGoodStack(int(flowerCfg.HarvestItemId), int(finalNum)))
		result.flowers = append(result.flowers, event.HarvestFlowerItem{
			FlowerID:   plot.FlowerID,
			PlotID:     plotID,
			HarvestNum: finalNum,
		})

		if flowerCfg.EssenceItemId > 0 {
			dropRate := flowerCfg.EssenceDropRate
			if levelCfg != nil {
				dropRate += levelCfg.EssenceDropRateAdd
			}
			if dropRate > 0 && rand.Intn(10000) < int(dropRate) {
				dropNum := flowerCfg.EssenceDropNum
				if levelCfg != nil {
					dropNum += levelCfg.EssenceDropNumAdd
				}
				result.essence = append(result.essence, bag.MakeGoodStack(int(flowerCfg.EssenceItemId), int(dropNum)))
			}
		}
	}
	return result, nil
}

// saveHarvest 落库收获产出。
func (r *RolePlot) saveHarvest(ctx context.Context, result harvestResult) error {
	if len(result.items) > 0 {
		if err := r.Role.Bag.SaveGoods(ctx, nil, result.items, "harvest_flower"); err != nil {
			return err
		}
	}
	if len(result.essence) > 0 {
		if err := r.Role.Bag.SaveGoods(ctx, nil, result.essence, "harvest_essence"); err != nil {
			return err
		}
	}
	return nil
}

// advancePlots 推进各地块的收获计数与状态:收获次数用尽则清空,否则进入下一轮生长间隔。
func (r *RolePlot) advancePlots(ctx context.Context, plotIDs []int32, now time.Time) {
	for _, plotID := range plotIDs {
		plot := r.Plots[plotID]
		flowerCfg := r.Cfg().TbFlower.Get(plot.FlowerID)

		level, _ := r.Role.Flower.GetFlowerLevel(plot.FlowerID)
		levelCfg := r.Cfg().GetFlowerLevelByGroup(flowerCfg.LevelGroup, level)

		finalTimes := flowerCfg.HarvestTimes
		if levelCfg != nil {
			finalTimes += levelCfg.HarvestTimesAdd
		}

		plot.HarvestCount++
		if plot.HarvestCount >= finalTimes {
			plot.FlowerID = 0
			plot.State = int32(pb.PlotState_PLOT_EMPTY)
			plot.HarvestCount = 0
			plot.StateTime = time.Time{}
			_ = deletePlotStealRecords(ctx, r.DB(), r.RoleID, plotID)
			continue
		}
		finalInterval := flowerCfg.HarvestInterval
		if levelCfg != nil {
			finalInterval -= levelCfg.HarvestIntervalReduce
		}
		if finalInterval < 1 {
			finalInterval = 1
		}
		plot.StateTime = now.Add(time.Duration(finalInterval) * time.Second)
	}
}

// plotInfos 按 plotIDs 构造地块信息列表。
func plotInfos(plots PlotMap, plotIDs []int32) []*pb.PPlotInfo {
	infos := make([]*pb.PPlotInfo, 0, len(plotIDs))
	for _, plotID := range plotIDs {
		infos = append(infos, pPlotInfo(plots[plotID]))
	}
	return infos
}

func (r *RolePlot) ReqPlotRemove(ctx context.Context, req *pb.ReqPlotRemove) (*pb.RspPlotRemove, error) {
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, errors.WithStack(ErrPlotLocked)
		}
		state := getPlotState(plot)
		if state == int32(pb.PlotState_PLOT_HARVESTABLE) {
			return nil, errors.WithStack(ErrPlotHarvestable)
		}
		if plot.State != int32(pb.PlotState_PLOT_PLANTED) && plot.State != int32(pb.PlotState_PLOT_GROWING) {
			return nil, errors.WithStack(ErrPlotNotPlanted)
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
	if err := r.refreshPlot(ctx); err != nil {
		return nil, err
	}

	rsp := &pb.RspPlotRemove{Plots: []*pb.PPlotInfo{}}
	for _, plotID := range req.PlotIds {
		rsp.Plots = append(rsp.Plots, pPlotInfo(r.Plots[plotID]))
	}
	return rsp, nil
}
