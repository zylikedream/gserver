package logic

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/pkg/gameconfig"

	"github.com/agiledragon/gomonkey/v2"
	proto "google.golang.org/protobuf/proto"
)

// ========== test setup ==========

var flowerCfgInited bool

const (
	flowerTestID      int32 = 101
	flowerTestOtherID int32 = 102
)

func initFlowerTestConfig(t *testing.T) {
	t.Helper()
	if flowerCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()

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

	gc.Tables = &gamecfg.Tables{
		TbItem: tbItem, TbFlower: tbFlower,
		TbFlowerLevel: tbFlowerLevel, TbFlowerBreak: tbFlowerBreak,
		TbPlayerLevel: tbPlayerLevel,
	}
	flowerCfgInited = true
}

func setupTestFlower(t *testing.T) *RoleFlower {
	t.Helper()
	initFlowerTestConfig(t)

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
	main.Basic = basicMod
	main.Bag = bagMod
	main.Flower = flowerMod
	return flowerMod
}

func setupTestFlowerWithMaterials(t *testing.T) *RoleFlower {
	t.Helper()
	f := setupTestFlower(t)
	for _, cost := range flowerConfig(t, flowerTestID).BreedCost {
		addBagGood(f, int(cost.Id), 100)
	}
	return f
}

func setupTestFlowerWithEssence(t *testing.T) *RoleFlower {
	t.Helper()
	f := setupTestFlowerWithMaterials(t)
	cfg := flowerConfig(t, flowerTestID)
	addBagGood(f, int(cfg.EssenceItemId), 100000)
	addBagGood(f, GOLD_ITEM_ID, 100000)
	if breakCfg := flowerBreakConfig(t, cfg.LevelGroup, 1); breakCfg != nil && breakCfg.BreakItemNum > 0 {
		addBagGood(f, int(breakCfg.BreakItemId), 100000)
	}
	return f
}

func addBagGood(f *RoleFlower, goodID int, num uint64) {
	good := f.Role.Bag.Goods[goodID]
	good.GoodID = goodID
	good.Num += num
	f.Role.Bag.Goods[goodID] = good
}

func goodNum(f *RoleFlower, goodID int) uint64 {
	return f.Role.Bag.Goods[goodID].Num
}

func flowerConfig(t *testing.T, flowerID int32) *gamecfg.GardenFlower {
	t.Helper()
	cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
	if cfg == nil {
		t.Fatalf("flower config not found: %d", flowerID)
	}
	return cfg
}

func flowerLevelConfig(t *testing.T, levelGroup int32, level int32) *gamecfg.GardenFlowerLevel {
	t.Helper()
	cfg := gameconfig.GameConfig().GetFlowerLevelByGroup(levelGroup, level)
	if cfg == nil {
		t.Fatalf("flower level config not found: group=%d level=%d", levelGroup, level)
	}
	return cfg
}

func flowerBreakConfig(t *testing.T, levelGroup int32, breakStage int32) *gamecfg.GardenFlowerBreak {
	t.Helper()
	cfg := gameconfig.GameConfig().GetFlowerBreakByGroup(levelGroup, breakStage)
	if cfg == nil {
		t.Fatalf("flower break config not found: group=%d break_stage=%d", levelGroup, breakStage)
	}
	return cfg
}

func maxFlowerLevel(t *testing.T, levelGroup int32) int32 {
	t.Helper()
	var maxLevel int32
	for _, cfg := range gameconfig.GameConfig().TbFlowerLevel.GetDataList() {
		if cfg.LevelGroup == levelGroup && cfg.Level > maxLevel {
			maxLevel = cfg.Level
		}
	}
	if maxLevel == 0 {
		t.Fatalf("no flower levels found: group=%d", levelGroup)
	}
	return maxLevel
}

// ========== UnlockFlower ==========

func TestFlowerUnlock(t *testing.T) {
	f := setupTestFlower(t)

	f.AddFlower(context.Background(), flowerTestID)

	fd, ok := f.Flowers[flowerTestID]
	if !ok {
		t.Fatalf("expected flower %d in map", flowerTestID)
	}
	if fd.FlowerID != flowerTestID {
		t.Fatalf("expected flower_id %d, got %d", flowerTestID, fd.FlowerID)
	}
	if fd.State != int32(pb.FlowerState_FLOWER_UNLOCKED) {
		t.Fatalf("expected UNLOCKED, got %d", fd.State)
	}
	if !f.IsDirty() {
		t.Fatal("expected dirty")
	}
}

// ========== FindBreeding ==========

func TestFindBreeding_None(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(context.Background(), flowerTestID)

	if found := f.FindBreeding(); found != nil {
		t.Fatalf("expected nil, got %v", found)
	}
}

func TestFindBreeding_OneBreeding(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(context.Background(), flowerTestID)
	f.Flowers[flowerTestID].State = int32(pb.FlowerState_FLOWER_BREEDING)

	found := f.FindBreeding()
	if found == nil {
		t.Fatal("expected non-nil")
	}
	if found.FlowerID != flowerTestID {
		t.Fatalf("expected flower %d, got %d", flowerTestID, found.FlowerID)
	}
}

// ========== StartBreed ==========

func TestStartBreed_Success(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(context.Background(), flowerTestID)
	cfg := flowerConfig(t, flowerTestID)
	before := map[int]uint64{}
	for _, cost := range cfg.BreedCost {
		before[int(cost.Id)] = goodNum(f, int(cost.Id))
	}

	rsp, err := f.ReqFlowerStartBreed(context.Background(), &pb.ReqFlowerStartBreed{FlowerId: flowerTestID})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil {
		t.Fatal("expected non-nil response")
	}

	fd := f.Flowers[flowerTestID]
	if fd.State != int32(pb.FlowerState_FLOWER_BREEDING) {
		t.Fatalf("expected BREEDING, got %d", fd.State)
	}

	for _, cost := range cfg.BreedCost {
		got := goodNum(f, int(cost.Id))
		want := before[int(cost.Id)] - uint64(cost.Num)
		if got != want {
			t.Fatalf("expected good %d num %d, got %d", cost.Id, want, got)
		}
	}
	if !f.IsDirty() {
		t.Fatal("expected dirty")
	}
}

func TestStartBreed_NotUnlocked(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)

	_, err := f.ReqFlowerStartBreed(context.Background(), &pb.ReqFlowerStartBreed{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

func TestStartBreed_AlreadyBreeding(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(context.Background(), flowerTestID)
	f.AddFlower(context.Background(), flowerTestOtherID)
	f.Flowers[flowerTestID].State = int32(pb.FlowerState_FLOWER_BREEDING)

	_, err := f.ReqFlowerStartBreed(context.Background(), &pb.ReqFlowerStartBreed{FlowerId: flowerTestOtherID})
	if !errors.Is(err, ErrFlowerBreedBusy) {
		t.Fatalf("expected ErrFlowerBreedBusy, got %v", err)
	}
}

func TestStartBreed_MaterialNotEnough(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(context.Background(), flowerTestID)

	_, err := f.ReqFlowerStartBreed(context.Background(), &pb.ReqFlowerStartBreed{FlowerId: flowerTestID})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ========== FinishBreed ==========

func TestFinishBreed_Success(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(context.Background(), flowerTestID)
	f.Flowers[flowerTestID].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[flowerTestID].StateTime = time.Now().Add(-1 * time.Hour) // past

	rsp, err := f.ReqFlowerFinishBreed(context.Background(), &pb.ReqFlowerFinishBreed{FlowerId: flowerTestID})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil {
		t.Fatal("expected non-nil response")
	}

	fd := f.Flowers[flowerTestID]
	if fd.State != int32(pb.FlowerState_FLOWER_HARVESTED) {
		t.Fatalf("expected HARVESTED, got %d", fd.State)
	}
}

func TestFinishBreed_NotBreeding(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(context.Background(), flowerTestID)
	// status stays UNLOCKED

	_, err := f.ReqFlowerFinishBreed(context.Background(), &pb.ReqFlowerFinishBreed{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerNotBreedDone) {
		t.Fatalf("expected ErrFlowerNotBreedDone, got %v", err)
	}
}

func TestFinishBreed_NotDone(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(context.Background(), flowerTestID)
	f.Flowers[flowerTestID].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[flowerTestID].StateTime = time.Now().Add(1 * time.Hour) // future

	_, err := f.ReqFlowerFinishBreed(context.Background(), &pb.ReqFlowerFinishBreed{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerNotBreedDone) {
		t.Fatalf("expected ErrFlowerNotDone, got %v", err)
	}
}

func TestFinishBreed_NotUnlocked(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)

	_, err := f.ReqFlowerFinishBreed(context.Background(), &pb.ReqFlowerFinishBreed{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

// ========== BreedInfo ==========

func TestBreedInfo_BreedDone(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(context.Background(), flowerTestID)
	f.Flowers[flowerTestID].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[flowerTestID].StateTime = time.Now().Add(-1 * time.Hour) // past

	rsp, err := f.ReqFlowerInfo(context.Background(), &pb.ReqFlowerInfo{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rsp.Flowers) != 1 {
		t.Fatalf("expected 1 flower, got %d", len(rsp.Flowers))
	}
	if rsp.Flowers[0].State != pb.FlowerState_FLOWER_BREED_DONE {
		t.Fatalf("expected BREED_DONE, got %v", rsp.Flowers[0].State)
	}
}

func TestBreedInfo_StillBreeding(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(context.Background(), flowerTestID)
	f.Flowers[flowerTestID].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[flowerTestID].StateTime = time.Now().Add(1 * time.Hour) // future

	rsp, err := f.ReqFlowerInfo(context.Background(), &pb.ReqFlowerInfo{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rsp.Flowers) != 1 {
		t.Fatalf("expected 1 flower, got %d", len(rsp.Flowers))
	}
	if rsp.Flowers[0].State != pb.FlowerState_FLOWER_BREEDING {
		t.Fatalf("expected BREEDING, got %v", rsp.Flowers[0].State)
	}
}

func TestBreedInfo_Empty(t *testing.T) {
	f := setupTestFlower(t)

	rsp, err := f.ReqFlowerInfo(context.Background(), &pb.ReqFlowerInfo{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rsp.Flowers) != 0 {
		t.Fatalf("expected 0 flowers, got %d", len(rsp.Flowers))
	}
}

// ========== FlowerMap Scan/Value ==========

func TestFlowerMap_ScanNil(t *testing.T) {
	var m FlowerMap
	if err := m.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestFlowerMap_ValueAndScan(t *testing.T) {
	original := FlowerMap{
		101: {FlowerID: 101, State: 1, StateTime: time.Unix(1700000000, 0), Level: 1, BreakStage: 0},
		102: {FlowerID: 102, State: 2, StateTime: time.Unix(1700001000, 0), Level: 1, BreakStage: 0},
	}

	val, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}

	var restored FlowerMap
	if err := restored.Scan(val); err != nil {
		t.Fatal(err)
	}

	if len(restored) != 2 {
		t.Fatalf("expected 2, got %d", len(restored))
	}
	if restored[101].FlowerID != 101 || restored[101].State != 1 {
		t.Fatalf("unexpected restored[101]: %v", restored[101])
	}
	if restored[102].FlowerID != 102 || restored[102].State != 2 {
		t.Fatalf("unexpected restored[102]: %v", restored[102])
	}
}

// ========== UpgradeFlower ==========

func TestUpgradeFlower_Success(t *testing.T) {
	f := setupTestFlowerWithEssence(t)
	f.AddFlower(context.Background(), flowerTestID)
	flower := f.Flowers[flowerTestID]
	flower.State = int32(pb.FlowerState_FLOWER_HARVESTED)
	flower.StateTime = time.Now()

	cfg := flowerConfig(t, flowerTestID)
	levelCfg := flowerLevelConfig(t, cfg.LevelGroup, 2)
	essenceID := int(cfg.EssenceItemId)
	essenceBefore := goodNum(f, essenceID)
	goldBefore := goodNum(f, GOLD_ITEM_ID)

	rsp, err := f.ReqFlowerUpgrade(context.Background(), &pb.ReqFlowerUpgrade{FlowerId: flowerTestID})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil {
		t.Fatal("expected non-nil response")
	}

	fd := f.Flowers[flowerTestID]
	if fd.Level != 2 {
		t.Fatalf("expected level 2, got %d", fd.Level)
	}
	if got, want := goodNum(f, essenceID), essenceBefore-uint64(levelCfg.UpgradeEssenceCost); got != want {
		t.Fatalf("expected essence %d, got %d", want, got)
	}
	if got, want := goodNum(f, GOLD_ITEM_ID), goldBefore-uint64(levelCfg.UpgradeCoinCost); got != want {
		t.Fatalf("expected gold %d, got %d", want, got)
	}
}

func TestUpgradeFlower_MaxLevel(t *testing.T) {
	f := setupTestFlowerWithEssence(t)
	f.AddFlower(context.Background(), flowerTestID)
	flower := f.Flowers[flowerTestID]
	flower.State = int32(pb.FlowerState_FLOWER_HARVESTED)
	flower.StateTime = time.Now()

	cfg := flowerConfig(t, flowerTestID)
	f.Flowers[flowerTestID].Level = maxFlowerLevel(t, cfg.LevelGroup)

	_, err := f.ReqFlowerUpgrade(context.Background(), &pb.ReqFlowerUpgrade{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerMaxLevel) {
		t.Fatalf("expected ErrFlowerMaxLevel, got %v", err)
	}
}

func TestUpgradeFlower_NeedBreak(t *testing.T) {
	f := setupTestFlowerWithEssence(t)
	f.AddFlower(context.Background(), flowerTestID)
	flower := f.Flowers[flowerTestID]
	flower.State = int32(pb.FlowerState_FLOWER_HARVESTED)
	flower.StateTime = time.Now()

	cfg := flowerConfig(t, flowerTestID)
	breakCfg := flowerBreakConfig(t, cfg.LevelGroup, 1)
	flowerLevelConfig(t, cfg.LevelGroup, breakCfg.NeedLevel+1)
	f.Flowers[flowerTestID].Level = breakCfg.NeedLevel

	_, err := f.ReqFlowerUpgrade(context.Background(), &pb.ReqFlowerUpgrade{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerNeedBreak) {
		t.Fatalf("expected ErrFlowerNeedBreak, got %v", err)
	}
}

func TestUpgradeFlower_NotUnlocked(t *testing.T) {
	f := setupTestFlowerWithEssence(t)

	_, err := f.ReqFlowerUpgrade(context.Background(), &pb.ReqFlowerUpgrade{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

// ========== BreakFlower ==========

func TestBreakFlower_Success(t *testing.T) {
	f := setupTestFlowerWithEssence(t)
	f.AddFlower(context.Background(), flowerTestID)
	cfg := flowerConfig(t, flowerTestID)
	breakCfg := flowerBreakConfig(t, cfg.LevelGroup, 1)
	f.Flowers[flowerTestID].Level = breakCfg.NeedLevel

	rsp, err := f.ReqFlowerBreak(context.Background(), &pb.ReqFlowerBreak{FlowerId: flowerTestID})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil {
		t.Fatal("expected non-nil response")
	}

	fd := f.Flowers[flowerTestID]
	if fd.BreakStage != 1 {
		t.Fatalf("expected break_stage 1, got %d", fd.BreakStage)
	}
}

func TestBreakFlower_PlayerLevelNotEnough(t *testing.T) {
	f := setupTestFlowerWithEssence(t)
	f.AddFlower(context.Background(), flowerTestID)
	cfg := flowerConfig(t, flowerTestID)
	breakCfg := flowerBreakConfig(t, cfg.LevelGroup, 1)
	f.Role.Basic.Level = breakCfg.PlayerLevelLimit - 1
	f.Flowers[flowerTestID].Level = breakCfg.NeedLevel

	_, err := f.ReqFlowerBreak(context.Background(), &pb.ReqFlowerBreak{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerBreakPlayerLevel) {
		t.Fatalf("expected ErrFlowerBreakPlayerLevel, got %v", err)
	}
}

func TestBreakFlower_LevelNotEnough(t *testing.T) {
	f := setupTestFlowerWithEssence(t)
	f.AddFlower(context.Background(), flowerTestID)
	cfg := flowerConfig(t, flowerTestID)
	breakCfg := flowerBreakConfig(t, cfg.LevelGroup, 1)
	f.Flowers[flowerTestID].Level = breakCfg.NeedLevel - 1

	_, err := f.ReqFlowerBreak(context.Background(), &pb.ReqFlowerBreak{FlowerId: flowerTestID})
	if !errors.Is(err, ErrFlowerBreakLevel) {
		t.Fatalf("expected ErrFlowerBreakLevel, got %v", err)
	}
}
