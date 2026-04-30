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

var flowerCfgInited bool

func initFlowerTestConfig(t *testing.T) {
	t.Helper()
	if flowerCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()

	// TbItem: soil=1001, fertilizer=2001, seed=101 (flower products used as seeds)
	items := []map[string]interface{}{
		{"id": float64(1001), "name": "soil", "desc": "", "major_type": float64(2),
			"sub_type": float64(10), "quality": float64(1), "price": float64(10),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(2001), "name": "fertilizer", "desc": "", "major_type": float64(2),
			"sub_type": float64(11), "quality": float64(1), "price": float64(20),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(101), "name": "rose_seed", "desc": "", "major_type": float64(2),
			"sub_type": float64(20), "quality": float64(1), "price": float64(50),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(102), "name": "sunflower_seed", "desc": "", "major_type": float64(2),
			"sub_type": float64(20), "quality": float64(1), "price": float64(50),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
	}
	tbItem, err := gamecfg.NewGardenTbItem(items)
	if err != nil {
		t.Fatal(err)
	}

	// TbFlower: rose=101 breed_time=10 cost=[soil*2,fertilizer*1], sunflower=102 breed_time=20 cost=[soil*1]
	flowers := []map[string]interface{}{
		{
			"id": float64(101), "name": "rose", "quality": float64(1),
			"breed_time": float64(10),
			"breed_cost": []interface{}{
				map[string]interface{}{"id": float64(1001), "num": float64(2)},
				map[string]interface{}{"id": float64(2001), "num": float64(1)},
			},
		},
		{
			"id": float64(102), "name": "sunflower", "quality": float64(1),
			"breed_time": float64(20),
			"breed_cost": []interface{}{
				map[string]interface{}{"id": float64(1001), "num": float64(1)},
			},
		},
	}
	tbFlower, err := gamecfg.NewGardenTbFlower(flowers)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{TbItem: tbItem, TbFlower: tbFlower}
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
	bagMod := &RoleBag{
		RoleModule:   RoleModule{Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	flowerMod := &RoleFlower{
		RoleModule:      RoleModule{Role: main},
		RoleFlowerState: RoleFlowerState{Flowers: make(FlowerMap)},
	}
	main.Bag = bagMod
	main.Flower = flowerMod
	return flowerMod
}

func setupTestFlowerWithMaterials(t *testing.T) *RoleFlower {
	t.Helper()
	f := setupTestFlower(t)
	f.Role.Bag.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 100}
	f.Role.Bag.Goods[2001] = bag.BagGood{GoodID: 2001, Num: 100}
	return f
}

// ========== UnlockFlower ==========

func TestFlowerUnlock(t *testing.T) {
	f := setupTestFlower(t)

	f.AddFlower(101)

	fd, ok := f.Flowers[101]
	if !ok {
		t.Fatal("expected flower 101 in map")
	}
	if fd.FlowerID != 101 {
		t.Fatalf("expected flower_id 101, got %d", fd.FlowerID)
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
	f.AddFlower(101)

	if found := f.FindBreeding(); found != nil {
		t.Fatalf("expected nil, got %v", found)
	}
}

func TestFindBreeding_OneBreeding(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(101)
	f.Flowers[101].State = int32(pb.FlowerState_FLOWER_BREEDING)

	found := f.FindBreeding()
	if found == nil {
		t.Fatal("expected non-nil")
	}
	if found.FlowerID != 101 {
		t.Fatalf("expected flower 101, got %d", found.FlowerID)
	}
}

// ========== StartBreed ==========

func TestStartBreed_Success(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(101)

	rsp, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 101})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil {
		t.Fatal("expected non-nil response")
	}

	fd := f.Flowers[101]
	if fd.State != int32(pb.FlowerState_FLOWER_BREEDING) {
		t.Fatalf("expected BREEDING, got %d", fd.State)
	}

	// soil: 100 - 2 = 98, fertilizer: 100 - 1 = 99
	if f.Role.Bag.Goods[1001].Num != 98 {
		t.Fatalf("expected soil 98, got %d", f.Role.Bag.Goods[1001].Num)
	}
	if f.Role.Bag.Goods[2001].Num != 99 {
		t.Fatalf("expected fertilizer 99, got %d", f.Role.Bag.Goods[2001].Num)
	}
	if !f.IsDirty() {
		t.Fatal("expected dirty")
	}
}

func TestStartBreed_NotUnlocked(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)

	_, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 101})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

func TestStartBreed_AlreadyBreeding(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(101)
	f.AddFlower(102)
	f.Flowers[101].State = int32(pb.FlowerState_FLOWER_BREEDING)

	_, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 102})
	if !errors.Is(err, ErrFlowerBreedBusy) {
		t.Fatalf("expected ErrFlowerBreedBusy, got %v", err)
	}
}

func TestStartBreed_MaterialNotEnough(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(101)

	_, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 101})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ========== FinishBreed ==========

func TestFinishBreed_Success(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(101)
	f.Flowers[101].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(-1 * time.Hour) // past

	rsp, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 101})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil {
		t.Fatal("expected non-nil response")
	}

	fd := f.Flowers[101]
	if fd.State != int32(pb.FlowerState_FLOWER_HARVESTED) {
		t.Fatalf("expected HARVESTED, got %d", fd.State)
	}
}

func TestFinishBreed_NotBreeding(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(101)
	// status stays UNLOCKED

	_, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 101})
	if !errors.Is(err, ErrFlowerNotBreedDone) {
		t.Fatalf("expected ErrFlowerNotBreedDone, got %v", err)
	}
}

func TestFinishBreed_NotDone(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.AddFlower(101)
	f.Flowers[101].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(1 * time.Hour) // future

	_, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 101})
	if !errors.Is(err, ErrFlowerNotBreedDone) {
		t.Fatalf("expected ErrFlowerNotDone, got %v", err)
	}
}

func TestFinishBreed_NotUnlocked(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)

	_, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 101})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

// ========== BreedInfo ==========

func TestBreedInfo_BreedDone(t *testing.T) {
	f := setupTestFlower(t)
	f.AddFlower(101)
	f.Flowers[101].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(-1 * time.Hour) // past

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
	f.AddFlower(101)
	f.Flowers[101].State = int32(pb.FlowerState_FLOWER_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(1 * time.Hour) // future

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
		101: {FlowerID: 101, State: 1, StateTime: time.Unix(1700000000, 0)},
		102: {FlowerID: 102, State: 2, StateTime: time.Unix(1700001000, 0)},
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
