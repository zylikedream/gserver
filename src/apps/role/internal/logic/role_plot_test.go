package logic

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gserver/src/pkg/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/agiledragon/gomonkey/v2"
	proto "google.golang.org/protobuf/proto"
)

// ========== test setup ==========

var plotCfgInited bool

const (
	plotTestID      int32 = 1
	plotTestFlower  int32 = 101
	plotOtherFlower int32 = 102
)

func initPlotTestConfig(t *testing.T) {
	t.Helper()
	if plotCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()

	// 物品配表从 gameconfig/json/ 加载，自动跟随配表结构变更
	items := loadTestTable(t, "garden_tbitem")
	tbItem, err := gamecfg.NewGardenTbItem(items)
	if err != nil {
		t.Fatal(err)
	}

	flowers := loadTestTable(t, "garden_tbflower")
	tbFlower, err := gamecfg.NewGardenTbFlower(flowers)
	if err != nil {
		t.Fatal(err)
	}

	// TbGardenPlot 从配表加载
	plots := loadTestTable(t, "garden_tbgardenplot")
	tbGardenPlot, err := gamecfg.NewGardenTbGardenPlot(plots)
	if err != nil {
		t.Fatal(err)
	}

	levels := loadTestTable(t, "garden_tbflowerlevel")
	tbFlowerLevel, err := gamecfg.NewGardenTbFlowerLevel(levels)
	if err != nil {
		t.Fatal(err)
	}

	breaks := loadTestTable(t, "garden_tbflowerbreak")
	tbFlowerBreak, err := gamecfg.NewGardenTbFlowerBreak(breaks)
	if err != nil {
		t.Fatal(err)
	}

	playerLevels := loadTestTable(t, "garden_tbplayerlevel")
	tbPlayerLevel, err := gamecfg.NewGardenTbPlayerLevel(playerLevels)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{TbItem: tbItem, TbFlower: tbFlower, TbGardenPlot: tbGardenPlot,
		TbFlowerLevel: tbFlowerLevel, TbFlowerBreak: tbFlowerBreak, TbPlayerLevel: tbPlayerLevel}
	plotCfgInited = true
}

func setupTestPlot(t *testing.T) *RolePlot {
	t.Helper()
	initPlotTestConfig(t)

	patch := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, _ proto.Message) {},
	)
	t.Cleanup(patch.Reset)

	main := &RoleMain{}
	basicMod := &RoleBasic{
		RoleModule:     RoleModule{Role: main},
		RoleBasicState: RoleBasicState{Level: 20},
	}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	flowerMod := &RoleFlower{
		RoleModule:      RoleModule{Role: main},
		RoleFlowerState: RoleFlowerState{Flowers: make(FlowerMap)},
	}
	plotMod := &RolePlot{
		RoleModule:    RoleModule{Role: main},
		RolePlotState: RolePlotState{Plots: make(PlotMap)},
	}
	main.Basic = basicMod
	main.Bag = bagMod
	main.Flower = flowerMod
	main.Plot = plotMod
	return plotMod
}

func setupTestPlotWithMaterials(t *testing.T) *RolePlot {
	t.Helper()
	p := setupTestPlot(t)
	// 水滴
	p.Role.Bag.Goods[WATER_ITEM_ID] = bag.BagGood{GoodID: WATER_ITEM_ID, Num: 100}
	// 解锁花并设为已收获状态（可种植）
	p.Role.Flower.AddFlower(plotTestFlower)
	p.Role.Flower.Flowers[plotTestFlower].State = int32(pb.FlowerState_FLOWER_HARVESTED)
	p.Role.Flower.AddFlower(plotOtherFlower)
	p.Role.Flower.Flowers[plotOtherFlower].State = int32(pb.FlowerState_FLOWER_HARVESTED)
	return p
}

func plotFlowerConfig(t *testing.T, flowerID int32) *gamecfg.GardenFlower {
	t.Helper()
	cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
	if cfg == nil {
		t.Fatalf("flower config not found: %d", flowerID)
	}
	return cfg
}

func plotLevelConfig(t *testing.T, levelGroup int32, level int32) *gamecfg.GardenFlowerLevel {
	t.Helper()
	cfg := gameconfig.GameConfig().GetFlowerLevelByGroup(levelGroup, level)
	if cfg == nil {
		t.Fatalf("flower level config not found: group=%d level=%d", levelGroup, level)
	}
	return cfg
}

func plotHarvestNum(t *testing.T, flowerID int32, level int32) int32 {
	t.Helper()
	cfg := plotFlowerConfig(t, flowerID)
	levelCfg := plotLevelConfig(t, cfg.LevelGroup, level)
	return cfg.HarvestNum + levelCfg.HarvestNumAdd
}

func plotHarvestTimes(t *testing.T, flowerID int32, level int32) int32 {
	t.Helper()
	cfg := plotFlowerConfig(t, flowerID)
	levelCfg := plotLevelConfig(t, cfg.LevelGroup, level)
	return cfg.HarvestTimes + levelCfg.HarvestTimesAdd
}

// ========== PlotMap Scan/Value ==========

func TestPlotMap_ScanNil(t *testing.T) {
	var m PlotMap
	if err := m.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestPlotMap_ValueAndScan(t *testing.T) {
	original := PlotMap{
		1: {PlotID: 1, FlowerID: 101, State: 1, HarvestCount: 0, StateTime: time.Unix(1700000000, 0)},
		2: {PlotID: 2, State: 0},
	}

	val, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}

	var restored PlotMap
	if err := restored.Scan(val); err != nil {
		t.Fatal(err)
	}

	if len(restored) != 2 {
		t.Fatalf("expected 2, got %d", len(restored))
	}
	if restored[1].FlowerID != 101 || restored[1].State != 1 {
		t.Fatalf("unexpected restored[1]: %v", restored[1])
	}
	if restored[2].State != 0 {
		t.Fatalf("unexpected restored[2]: %v", restored[2])
	}
}

// ========== UnlockPlot ==========

func TestUnlockPlot_Success(t *testing.T) {
	p := setupTestPlot(t)

	p.UnlockPlot(plotTestID)

	plot, ok := p.Plots[plotTestID]
	if !ok {
		t.Fatalf("expected plot %d in map", plotTestID)
	}
	if plot.State != int32(pb.PlotState_PLOT_EMPTY) {
		t.Fatalf("expected EMPTY, got %d", plot.State)
	}
	if !p.IsDirty() {
		t.Fatal("expected dirty")
	}
}

func TestReqPlotUnlock_PlayerLevelNotEnough(t *testing.T) {
	p := setupTestPlot(t)
	cfg := gameconfig.GameConfig().TbGardenPlot.Get(13)
	for _, cost := range cfg.Cost {
		p.Role.Bag.Goods[int(cost.Id)] = bag.BagGood{GoodID: int(cost.Id), Num: uint64(cost.Num)}
	}
	p.Role.Basic.Level = cfg.UnlockLevel - 1

	_, err := p.ReqPlotUnlock(context.Background(), &pb.ReqPlotUnlock{PlotId: 13})
	if !errors.Is(err, ErrPlayerLevelNotEnough) {
		t.Fatalf("expected ErrPlayerLevelNotEnough, got %v", err)
	}
}

func TestReqPlotUnlock_WithRequiredPlayerLevel(t *testing.T) {
	p := setupTestPlot(t)
	cfg := gameconfig.GameConfig().TbGardenPlot.Get(13)
	for _, cost := range cfg.Cost {
		p.Role.Bag.Goods[int(cost.Id)] = bag.BagGood{GoodID: int(cost.Id), Num: uint64(cost.Num)}
	}
	p.Role.Basic.Level = cfg.UnlockLevel

	rsp, err := p.ReqPlotUnlock(context.Background(), &pb.ReqPlotUnlock{PlotId: 13})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil || rsp.Plot == nil || rsp.Plot.PlotId != 13 {
		t.Fatalf("unexpected response: %v", rsp)
	}
}

// ========== PlantFlower ==========

func TestPlantFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)

	rsp, err := p.ReqPlotPlant(context.Background(), &pb.ReqPlotPlant{
		PlotIds:  []int32{plotTestID},
		FlowerId: plotTestFlower,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Plots) != 1 {
		t.Fatalf("expected 1 plot, got %d", len(rsp.Plots))
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_PLANTED {
		t.Fatalf("expected PLANTED, got %v", rsp.Plots[0].State)
	}
	if p.Plots[plotTestID].FlowerID != plotTestFlower {
		t.Fatalf("expected flower %d, got %d", plotTestFlower, p.Plots[plotTestID].FlowerID)
	}
}

func TestPlantFlower_NotUnlocked(t *testing.T) {
	p := setupTestPlotWithMaterials(t)

	_, err := p.ReqPlotPlant(context.Background(), &pb.ReqPlotPlant{
		PlotIds:  []int32{plotTestID},
		FlowerId: plotTestFlower,
	})
	if !errors.Is(err, ErrPlotLocked) {
		t.Fatalf("expected ErrPlotLocked, got %v", err)
	}
}

func TestPlantFlower_FlowerNotBred(t *testing.T) {
	p := setupTestPlot(t)
	p.UnlockPlot(plotTestID)

	_, err := p.ReqPlotPlant(context.Background(), &pb.ReqPlotPlant{
		PlotIds:  []int32{plotTestID},
		FlowerId: 999,
	})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

func TestPlantFlower_NotEmpty(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_PLANTED)

	_, err := p.ReqPlotPlant(context.Background(), &pb.ReqPlotPlant{
		PlotIds:  []int32{plotTestID},
		FlowerId: plotTestFlower,
	})
	if !errors.Is(err, ErrPlotNotEmpty) {
		t.Fatalf("expected ErrPlotNotEmpty, got %v", err)
	}
}

// ========== WaterFlower ==========

func TestWaterFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].FlowerID = plotTestFlower
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_PLANTED)
	cfg := plotFlowerConfig(t, plotTestFlower)
	waterBefore := p.Role.Bag.Goods[WATER_ITEM_ID].Num

	rsp, err := p.ReqPlotWater(context.Background(), &pb.ReqPlotWater{PlotIds: []int32{plotTestID}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_GROWING {
		t.Fatalf("expected GROWING, got %v", rsp.Plots[0].State)
	}
	if got, want := p.Role.Bag.Goods[WATER_ITEM_ID].Num, waterBefore-uint64(cfg.WaterCost); got != want {
		t.Fatalf("expected water %d, got %d", want, got)
	}
	if !p.IsDirty() {
		t.Fatal("expected dirty")
	}
}

func TestWaterFlower_NotPlanted(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)

	_, err := p.ReqPlotWater(context.Background(), &pb.ReqPlotWater{PlotIds: []int32{plotTestID}})
	if !errors.Is(err, ErrPlotNotPlanted) {
		t.Fatalf("expected ErrPlotNotPlanted, got %v", err)
	}
}

// ========== HarvestFlower ==========

func TestHarvestFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].FlowerID = plotTestFlower
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[plotTestID].StateTime = time.Now().Add(-1 * time.Hour) // past, ready to harvest
	cfg := plotFlowerConfig(t, plotTestFlower)
	wantHarvestNum := plotHarvestNum(t, plotTestFlower, 1)

	rsp, err := p.ReqPlotHarvest(context.Background(), &pb.ReqPlotHarvest{PlotIds: []int32{plotTestID}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].HarvestCount != 1 {
		t.Fatalf("expected harvest_count 1, got %d", rsp.Plots[0].HarvestCount)
	}
	if p.Plots[plotTestID].State != int32(pb.PlotState_PLOT_GROWING) {
		t.Fatalf("expected GROWING, got %d", p.Plots[plotTestID].State)
	}
	if got := p.Role.Bag.Goods[int(cfg.HarvestItemId)].Num; got != uint64(wantHarvestNum) {
		t.Fatalf("expected harvest item %d num %d, got %d", cfg.HarvestItemId, wantHarvestNum, got)
	}
}

func TestHarvestFlower_LastHarvest(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].FlowerID = plotTestFlower
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[plotTestID].HarvestCount = plotHarvestTimes(t, plotTestFlower, 1) - 1
	p.Plots[plotTestID].StateTime = time.Now().Add(-1 * time.Hour)

	_, err := p.ReqPlotHarvest(context.Background(), &pb.ReqPlotHarvest{PlotIds: []int32{plotTestID}})
	if err != nil {
		t.Fatal(err)
	}
	// should be EMPTY
	if p.Plots[plotTestID].State != int32(pb.PlotState_PLOT_EMPTY) {
		t.Fatalf("expected EMPTY, got %d", p.Plots[plotTestID].State)
	}
	if p.Plots[plotTestID].FlowerID != 0 {
		t.Fatalf("expected flower_id 0, got %d", p.Plots[plotTestID].FlowerID)
	}
}

func TestHarvestFlower_NotReady(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[plotTestID].StateTime = time.Now().Add(1 * time.Hour) // future, not ready

	_, err := p.ReqPlotHarvest(context.Background(), &pb.ReqPlotHarvest{PlotIds: []int32{plotTestID}})
	if !errors.Is(err, ErrPlotNotReady) {
		t.Fatalf("expected ErrPlotNotReady, got %v", err)
	}
}

// ========== RemovePlant ==========

func TestRemovePlant_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].FlowerID = plotTestFlower
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_PLANTED)

	rsp, err := p.ReqPlotRemove(context.Background(), &pb.ReqPlotRemove{PlotIds: []int32{plotTestID}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_EMPTY {
		t.Fatalf("expected EMPTY, got %v", rsp.Plots[0].State)
	}
}

func TestRemovePlant_Harvestable(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].FlowerID = plotTestFlower
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[plotTestID].StateTime = time.Now().Add(-1 * time.Hour) // past, harvestable

	_, err := p.ReqPlotRemove(context.Background(), &pb.ReqPlotRemove{PlotIds: []int32{plotTestID}})
	if !errors.Is(err, ErrPlotHarvestable) {
		t.Fatalf("expected ErrPlotHarvestable, got %v", err)
	}
}

// ========== PlotInfo ==========

func TestPlotInfo_Harvestable(t *testing.T) {
	p := setupTestPlot(t)
	p.UnlockPlot(plotTestID)
	p.Plots[plotTestID].FlowerID = plotTestFlower
	p.Plots[plotTestID].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[plotTestID].StateTime = time.Now().Add(-1 * time.Hour) // past

	rsp, err := p.ReqPlotInfo(context.Background(), &pb.ReqPlotInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_HARVESTABLE {
		t.Fatalf("expected HARVESTABLE, got %v", rsp.Plots[0].State)
	}
}

func TestPlotInfo_Empty(t *testing.T) {
	p := setupTestPlot(t)

	rsp, err := p.ReqPlotInfo(context.Background(), &pb.ReqPlotInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Plots) != 0 {
		t.Fatalf("expected 0 plots, got %d", len(rsp.Plots))
	}
}
