package logic

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/agiledragon/gomonkey/v2"
	proto "google.golang.org/protobuf/proto"
)

// ========== test setup ==========

var plotCfgInited bool

func initPlotTestConfig(t *testing.T) {
	t.Helper()
	if plotCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()

	// TbFlower: rose=101, grow_time=60, harvest_interval=30, harvest_times=3, harvest_item_id=10001, harvest_num=2, water_cost=5
	flowers := []map[string]interface{}{
		{
			"id": float64(101), "name": "rose", "quality": float64(1),
			"breed_time": float64(10), "breed_cost": []interface{}{},
			"grow_time": float64(60), "harvest_interval": float64(30),
			"harvest_times": float64(3), "harvest_item_id": float64(10001),
			"harvest_num": float64(2), "water_cost": float64(5),
		},
		{
			"id": float64(102), "name": "sunflower", "quality": float64(1),
			"breed_time": float64(20), "breed_cost": []interface{}{},
			"grow_time": float64(120), "harvest_interval": float64(60),
			"harvest_times": float64(2), "harvest_item_id": float64(10002),
			"harvest_num": float64(1), "water_cost": float64(3),
		},
	}
	tbFlower, err := gamecfg.NewGardenTbFlower(flowers)
	if err != nil {
		t.Fatal(err)
	}

	// TbGardenPlot: 1-12 free
	plots := make([]map[string]interface{}, 12)
	for i := 0; i < 12; i++ {
		plots[i] = map[string]interface{}{
			"id": float64(i + 1), "unlock_level": float64(0), "cost": []interface{}{},
		}
	}
	tbGardenPlot, err := gamecfg.NewGardenTbGardenPlot(plots)
	if err != nil {
		t.Fatal(err)
	}

	// TbItem: water_drop=3001, harvest products=10001,10002
	items := []map[string]interface{}{
		{"id": float64(3001), "name": "water_drop", "desc": "", "major_type": float64(2),
			"sub_type": float64(12), "quality": float64(1), "price": float64(5),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(10001), "name": "rose_petal", "desc": "", "major_type": float64(2),
			"sub_type": float64(80), "quality": float64(1), "price": float64(10),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(10002), "name": "sunflower_petal", "desc": "", "major_type": float64(2),
			"sub_type": float64(80), "quality": float64(1), "price": float64(10),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
	}
	tbItem, err := gamecfg.NewGardenTbItem(items)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{TbItem: tbItem, TbFlower: tbFlower, TbGardenPlot: tbGardenPlot}
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
	main.Bag = bagMod
	main.Flower = flowerMod
	main.Plot = plotMod
	return plotMod
}

func setupTestPlotWithMaterials(t *testing.T) *RolePlot {
	t.Helper()
	p := setupTestPlot(t)
	// 水滴
	p.Role.Bag.Goods[3001] = bag.BagGood{GoodID: 3001, Num: 100}
	// 解锁花
	p.Role.Flower.AddFlower(101)
	p.Role.Flower.AddFlower(102)
	return p
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

	p.UnlockPlot(1)

	plot, ok := p.Plots[1]
	if !ok {
		t.Fatal("expected plot 1 in map")
	}
	if plot.State != int32(pb.PlotState_PLOT_EMPTY) {
		t.Fatalf("expected EMPTY, got %d", plot.State)
	}
	if !p.IsDirty() {
		t.Fatal("expected dirty")
	}
}

// ========== PlantFlower ==========

func TestPlantFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)

	rsp, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 101,
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
	if p.Plots[1].FlowerID != 101 {
		t.Fatalf("expected flower 101, got %d", p.Plots[1].FlowerID)
	}
}

func TestPlantFlower_NotUnlocked(t *testing.T) {
	p := setupTestPlotWithMaterials(t)

	_, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 101,
	})
	if !errors.Is(err, ErrPlotLocked) {
		t.Fatalf("expected ErrPlotLocked, got %v", err)
	}
}

func TestPlantFlower_FlowerNotBred(t *testing.T) {
	p := setupTestPlot(t)
	p.UnlockPlot(1)

	_, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 999,
	})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

func TestPlantFlower_NotEmpty(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].State = int32(pb.PlotState_PLOT_PLANTED)

	_, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 101,
	})
	if !errors.Is(err, ErrPlotNotEmpty) {
		t.Fatalf("expected ErrPlotNotEmpty, got %v", err)
	}
}

// ========== WaterFlower ==========

func TestWaterFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_PLANTED)

	rsp, err := p.ReqWaterFlower(context.Background(), &pb.ReqWaterFlower{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_GROWING {
		t.Fatalf("expected GROWING, got %v", rsp.Plots[0].State)
	}
	// water 100 - 5 = 95
	if p.Role.Bag.Goods[3001].Num != 95 {
		t.Fatalf("expected water 95, got %d", p.Role.Bag.Goods[3001].Num)
	}
	if !p.IsDirty() {
		t.Fatal("expected dirty")
	}
}

func TestWaterFlower_NotPlanted(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)

	_, err := p.ReqWaterFlower(context.Background(), &pb.ReqWaterFlower{PlotIds: []int32{1}})
	if !errors.Is(err, ErrPlotNotPlanted) {
		t.Fatalf("expected ErrPlotNotPlanted, got %v", err)
	}
}

// ========== HarvestFlower ==========

func TestHarvestFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour) // past, ready to harvest

	rsp, err := p.ReqHarvestFlower(context.Background(), &pb.ReqHarvestFlower{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].HarvestCount != 1 {
		t.Fatalf("expected harvest_count 1, got %d", rsp.Plots[0].HarvestCount)
	}
	// harvest_times=3, so still GROWING after first harvest
	if p.Plots[1].State != int32(pb.PlotState_PLOT_GROWING) {
		t.Fatalf("expected GROWING, got %d", p.Plots[1].State)
	}
	// got 2x rose_petal (10001)
	if p.Role.Bag.Goods[10001].Num != 2 {
		t.Fatalf("expected 2 petals, got %d", p.Role.Bag.Goods[10001].Num)
	}
}

func TestHarvestFlower_LastHarvest(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].HarvestCount = 2 // harvest_times=3, this is the last
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour)

	_, err := p.ReqHarvestFlower(context.Background(), &pb.ReqHarvestFlower{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	// should be EMPTY
	if p.Plots[1].State != int32(pb.PlotState_PLOT_EMPTY) {
		t.Fatalf("expected EMPTY, got %d", p.Plots[1].State)
	}
	if p.Plots[1].FlowerID != 0 {
		t.Fatalf("expected flower_id 0, got %d", p.Plots[1].FlowerID)
	}
}

func TestHarvestFlower_NotReady(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(1 * time.Hour) // future, not ready

	_, err := p.ReqHarvestFlower(context.Background(), &pb.ReqHarvestFlower{PlotIds: []int32{1}})
	if !errors.Is(err, ErrPlotNotReady) {
		t.Fatalf("expected ErrPlotNotReady, got %v", err)
	}
}

// ========== RemovePlant ==========

func TestRemovePlant_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_PLANTED)

	rsp, err := p.ReqRemovePlant(context.Background(), &pb.ReqRemovePlant{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_EMPTY {
		t.Fatalf("expected EMPTY, got %v", rsp.Plots[0].State)
	}
}

func TestRemovePlant_Harvestable(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour) // past, harvestable

	_, err := p.ReqRemovePlant(context.Background(), &pb.ReqRemovePlant{PlotIds: []int32{1}})
	if !errors.Is(err, ErrPlotHarvestable) {
		t.Fatalf("expected ErrPlotHarvestable, got %v", err)
	}
}

// ========== PlotInfo ==========

func TestPlotInfo_Harvestable(t *testing.T) {
	p := setupTestPlot(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour) // past

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
