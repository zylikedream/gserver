package logic

import (
	"context"
	"errors"
	"reflect"
	"testing"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/pkg/gameconfig"

	proto "google.golang.org/protobuf/proto"

	"github.com/agiledragon/gomonkey/v2"
)

// ========== test setup ==========

var testCfgInited bool

func initTestGameConfig(t *testing.T) {
	t.Helper()
	if testCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()
	items := loadTestTable(t, "garden_tbitem")
	tbItem, err := gamecfg.NewGardenTbItem(items)
	if err != nil {
		t.Fatal(err)
	}

	playerLevels := loadTestTable(t, "garden_tbplayerlevel")
	tbPlayerLevel, err := gamecfg.NewGardenTbPlayerLevel(playerLevels)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{TbItem: tbItem, TbPlayerLevel: tbPlayerLevel}
	testCfgInited = true
}

func setupTestBag(t *testing.T) *RoleBag {
	t.Helper()
	initTestGameConfig(t)
	// 拦截 SendClient，避免 nil session 触发错误日志
	patch := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, _ proto.Message) {},
	)
	t.Cleanup(patch.Reset)
	main := &RoleMain{}
	basicMod := &RoleBasic{
		RoleModule:     RoleModule{Role: main},
		RoleBasicState: RoleBasicState{Level: 1},
	}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	main.Basic = basicMod
	main.Bag = bagMod
	return bagMod
}

func testGoodStack(id, num int32) *gamecfg.GardenGoodStack {
	return &gamecfg.GardenGoodStack{Id: id, Num: num}
}

func itemConfig(t *testing.T, goodID int32) *gamecfg.GardenItem {
	t.Helper()
	cfg := gameconfig.Get().TbItem.Get(goodID)
	if cfg == nil {
		t.Fatalf("item config not found: %d", goodID)
	}
	return cfg
}

// ========== classifyGoods ==========

func TestBagClassifyGoods_Nil(t *testing.T) {
	result := classifyGoods(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %v", result)
	}
}

func TestBagClassifyGoods_Empty(t *testing.T) {
	result := classifyGoods([]*gamecfg.GardenGoodStack{})
	if len(result) != 0 {
		t.Fatalf("expected empty, got %v", result)
	}
}

func TestBagClassifyGoods_Single(t *testing.T) {
	result := classifyGoods([]*gamecfg.GardenGoodStack{testGoodStack(1001, 5)})
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].GoodID != 1001 || result[0].Num != 5 {
		t.Fatalf("expected {1001,5}, got %v", result[0])
	}
}

func TestBagClassifyGoods_MergeSameID(t *testing.T) {
	result := classifyGoods([]*gamecfg.GardenGoodStack{
		testGoodStack(1001, 3),
		testGoodStack(1001, 7),
	})
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].GoodID != 1001 || result[0].Num != 10 {
		t.Fatalf("expected {1001,10}, got %v", result[0])
	}
}

func TestBagClassifyGoods_MultipleIDs(t *testing.T) {
	result := classifyGoods([]*gamecfg.GardenGoodStack{
		testGoodStack(1001, 5),
		testGoodStack(int32(GOLD_ITEM_ID), 10),
	})
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	counts := map[int]uint64{}
	for _, g := range result {
		counts[g.GoodID] = g.Num
	}
	if counts[1001] != 5 || counts[GOLD_ITEM_ID] != 10 {
		t.Fatalf("expected {1001:5, %d:10}, got %v", GOLD_ITEM_ID, counts)
	}
}

// ========== GetGood ==========

func TestBagGetGood_Exists(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 50}

	good := b.GetGood(1001)
	if good.GoodID != 1001 || good.Num != 50 {
		t.Fatalf("expected {1001,50}, got %v", good)
	}
}

func TestBagGetGood_NotExists(t *testing.T) {
	b := setupTestBag(t)

	good := b.GetGood(9999)
	if good.GoodID != 9999 || good.Num != 0 {
		t.Fatalf("expected {9999,0}, got %v", good)
	}
}

// ========== cloneGoodsMap ==========

func TestBagCloneGoodsMap_Isolation(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 10}

	clone := b.cloneGoodsMap()
	clone[1001] = bag.BagGood{GoodID: 1001, Num: 999}

	if b.Goods[1001].Num != 10 {
		t.Fatalf("original modified: expected 10, got %d", b.Goods[1001].Num)
	}
}

// ========== addSingleGood ==========

func TestBagAddSingleGood_New(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := make(GoodsMap)

	op, err := b.addSingleGood(goodsMap, bag.Good{GoodID: 1001, Num: 5})
	if err != nil {
		t.Fatal(err)
	}
	if op.PreNum != 0 || op.Num != 5 {
		t.Fatalf("expected op {0->5}, got {%d->%d}", op.PreNum, op.Num)
	}
	if goodsMap[1001].Num != 5 {
		t.Fatalf("expected map num 5, got %d", goodsMap[1001].Num)
	}
}

func TestBagAddSingleGood_Stack(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := GoodsMap{1001: {GoodID: 1001, Num: 10}}

	op, err := b.addSingleGood(goodsMap, bag.Good{GoodID: 1001, Num: 20})
	if err != nil {
		t.Fatal(err)
	}
	if op.PreNum != 10 || op.Num != 30 {
		t.Fatalf("expected op {10->30}, got {%d->%d}", op.PreNum, op.Num)
	}
}

func TestBagAddSingleGood_ExceedMaxStack(t *testing.T) {
	b := setupTestBag(t)
	maxStack := uint64(itemConfig(t, 1001).MaxStack)
	goodsMap := GoodsMap{1001: {GoodID: 1001, Num: maxStack - 1}}

	_, err := b.addSingleGood(goodsMap, bag.Good{GoodID: 1001, Num: 2})
	if !errors.Is(err, ErrGoodExceedMaxStack) {
		t.Fatalf("expected ErrGoodExceedMaxStack, got %v", err)
	}
}

func TestBagAddSingleGood_LargeStack(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := GoodsMap{GOLD_ITEM_ID: {GoodID: GOLD_ITEM_ID, Num: 1000}}

	_, err := b.addSingleGood(goodsMap, bag.Good{GoodID: GOLD_ITEM_ID, Num: 1000})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestBagAddSingleGood_ConfigNotFound(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := make(GoodsMap)

	_, err := b.addSingleGood(goodsMap, bag.Good{GoodID: 9999, Num: 1})
	if !errors.Is(err, ErrGoodConfigNotFound) {
		t.Fatalf("expected ErrGoodConfigNotFound, got %v", err)
	}
}

// ========== decSingleGood ==========

func TestBagDecSingleGood_Normal(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := GoodsMap{1001: {GoodID: 1001, Num: 50}}

	op, err := b.decSingleGood(goodsMap, bag.Good{GoodID: 1001, Num: 20})
	if err != nil {
		t.Fatal(err)
	}
	if op.PreNum != 50 || op.Num != 30 {
		t.Fatalf("expected op {50->30}, got {%d->%d}", op.PreNum, op.Num)
	}
	if goodsMap[1001].Num != 30 {
		t.Fatalf("expected map num 30, got %d", goodsMap[1001].Num)
	}
}

func TestBagDecSingleGood_ToZero(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := GoodsMap{1001: {GoodID: 1001, Num: 10}}

	op, err := b.decSingleGood(goodsMap, bag.Good{GoodID: 1001, Num: 10})
	if err != nil {
		t.Fatal(err)
	}
	if op.Num != 0 {
		t.Fatalf("expected op num 0, got %d", op.Num)
	}
	if _, exists := goodsMap[1001]; exists {
		t.Fatal("expected good deleted from map when num is 0")
	}
}

func TestBagDecSingleGood_NotEnough(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := GoodsMap{1001: {GoodID: 1001, Num: 5}}

	_, err := b.decSingleGood(goodsMap, bag.Good{GoodID: 1001, Num: 10})
	if !errors.Is(err, ErrGoodNotEnough) {
		t.Fatalf("expected ErrGoodNotEnough, got %v", err)
	}
}

func TestBagDecSingleGood_NotExists(t *testing.T) {
	b := setupTestBag(t)
	goodsMap := make(GoodsMap)

	_, err := b.decSingleGood(goodsMap, bag.Good{GoodID: 1001, Num: 1})
	if !errors.Is(err, ErrGoodNotEnough) {
		t.Fatalf("expected ErrGoodNotEnough, got %v", err)
	}
}

// ========== SaveGoods ==========

func TestBagSaveGoods_AddOnly(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if b.Goods[1001].Num != 10 {
		t.Fatalf("expected 10, got %d", b.Goods[1001].Num)
	}
	if !b.IsDirty() {
		t.Fatal("expected dirty")
	}
}

func TestBagSaveGoods_PlayerExpStored(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{testGoodStack(int32(PLAYER_EXP_ITEM_ID), 55)}, "test")
	if err != nil {
		t.Fatal(err)
	}

	if got := b.Goods[PLAYER_EXP_ITEM_ID].Num; got != 55 {
		t.Fatalf("expected exp item stored as 55, got %d", got)
	}
}

func TestBagSaveGoods_NormalAndPlayerExpAdd(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{
		testGoodStack(1001, 10),
		testGoodStack(int32(PLAYER_EXP_ITEM_ID), 20),
	}, "test")
	if err != nil {
		t.Fatal(err)
	}

	if got := b.Goods[1001].Num; got != 10 {
		t.Fatalf("expected normal item 10, got %d", got)
	}
	if got := b.Goods[PLAYER_EXP_ITEM_ID].Num; got != 20 {
		t.Fatalf("expected exp item 20, got %d", got)
	}
}

func TestBagSaveGoods_PlayerExpRetainedBeyondConfiguredMaxLevel(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{testGoodStack(int32(PLAYER_EXP_ITEM_ID), 999999)}, "test")
	if err != nil {
		t.Fatal(err)
	}

	if got := b.Goods[PLAYER_EXP_ITEM_ID].Num; got != 999999 {
		t.Fatalf("expected exp retained, got %d", got)
	}
}

func TestBagSaveGoods_RemoveOnly(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 50}
	ctx := context.Background()

	err := b.SaveGoods(ctx, []*gamecfg.GardenGoodStack{testGoodStack(1001, 20)}, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	if b.Goods[1001].Num != 30 {
		t.Fatalf("expected 30, got %d", b.Goods[1001].Num)
	}
}

func TestBagSaveGoods_RemoveAndAdd(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 50}
	b.Goods[GOLD_ITEM_ID] = bag.BagGood{GoodID: GOLD_ITEM_ID, Num: 100}
	ctx := context.Background()

	err := b.SaveGoods(ctx,
		[]*gamecfg.GardenGoodStack{testGoodStack(1001, 20)},
		[]*gamecfg.GardenGoodStack{testGoodStack(int32(GOLD_ITEM_ID), 50)},
		"trade",
	)
	if err != nil {
		t.Fatal(err)
	}
	if b.Goods[1001].Num != 30 {
		t.Fatalf("expected 1001 num 30, got %d", b.Goods[1001].Num)
	}
	if b.Goods[GOLD_ITEM_ID].Num != 150 {
		t.Fatalf("expected %d num 150, got %d", GOLD_ITEM_ID, b.Goods[GOLD_ITEM_ID].Num)
	}
}

func TestBagSaveGoods_RemoveFailed_Rollback(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 5}
	ctx := context.Background()

	err := b.SaveGoods(ctx, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, nil, "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if b.Goods[1001].Num != 5 {
		t.Fatalf("expected rollback to 5, got %d", b.Goods[1001].Num)
	}
}

func TestBagSaveGoods_AddFailed_Rollback(t *testing.T) {
	b := setupTestBag(t)
	maxStack := uint64(itemConfig(t, 1001).MaxStack)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: maxStack - 1}
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 2)}, "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if b.Goods[1001].Num != maxStack-1 {
		t.Fatalf("expected rollback to %d, got %d", maxStack-1, b.Goods[1001].Num)
	}
}

func TestBagSaveGoods_EmptyOps(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Goods) != 0 {
		t.Fatalf("expected empty goods, got %d", len(b.Goods))
	}
}

func TestBagSaveGoods_SameGoodRemoveThenAdd(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 50}
	ctx := context.Background()

	err := b.SaveGoods(ctx,
		[]*gamecfg.GardenGoodStack{testGoodStack(1001, 30)},
		[]*gamecfg.GardenGoodStack{testGoodStack(1001, 60)},
		"exchange",
	)
	if err != nil {
		t.Fatal(err)
	}
	if b.Goods[1001].Num != 80 {
		t.Fatalf("expected 50-30+60=80, got %d", b.Goods[1001].Num)
	}
}

// ========== CheckGoods ==========

func TestBagCheckGoods_Enough(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 50}

	if !b.CheckGoods([]*gamecfg.GardenGoodStack{testGoodStack(1001, 30)}) {
		t.Fatal("expected true")
	}
}

func TestBagCheckGoods_NotEnough(t *testing.T) {
	b := setupTestBag(t)
	b.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 5}

	if b.CheckGoods([]*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}) {
		t.Fatal("expected false")
	}
}

func TestBagCheckGoods_NotExists(t *testing.T) {
	b := setupTestBag(t)

	if b.CheckGoods([]*gamecfg.GardenGoodStack{testGoodStack(1001, 1)}) {
		t.Fatal("expected false")
	}
}

func TestBagCheckGoods_Nil(t *testing.T) {
	b := setupTestBag(t)

	if !b.CheckGoods(nil) {
		t.Fatal("expected true for nil input")
	}
}

// ========== MakeGoodStack ==========

func TestMakeGoodStack(t *testing.T) {
	s := bag.MakeGoodStack(1001, 50)
	if s.Id != 1001 || s.Num != 50 {
		t.Fatalf("expected {1001,50}, got {%d,%d}", s.Id, s.Num)
	}
}

// ========== SaveGoodsOpts ==========

func TestBagSaveGoods_Silent(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, "test", bag.OptSilent())
	if err != nil {
		t.Fatal(err)
	}
	if b.Goods[1001].Num != 10 {
		t.Fatalf("expected 10, got %d", b.Goods[1001].Num)
	}
	if !b.IsDirty() {
		t.Fatal("expected dirty even in silent mode")
	}
}

func TestBagSaveGoods_DefaultOpts(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if b.Goods[1001].Num != 10 {
		t.Fatalf("expected 10, got %d", b.Goods[1001].Num)
	}
}

func TestBagSaveGoods_NotifyRewardOpts(t *testing.T) {
	b := setupTestBag(t)
	ctx := context.Background()
	var sent []proto.Message
	patch := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, msg proto.Message) {
			sent = append(sent, msg)
		},
	)
	defer patch.Reset()

	err := b.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, "test", bag.OptNotifyReward())
	if err != nil {
		t.Fatal(err)
	}
	if b.Goods[1001].Num != 10 {
		t.Fatalf("expected 10, got %d", b.Goods[1001].Num)
	}
	var gotReward bool
	for _, msg := range sent {
		reward, ok := msg.(*pb.NotifyBagReward)
		if !ok {
			continue
		}
		gotReward = true
		if len(reward.Goods) != 1 {
			t.Fatalf("expected 1 reward good, got %d", len(reward.Goods))
		}
		if reward.Goods[0].PropId != 1001 || reward.Goods[0].Num != 10 {
			t.Fatalf("unexpected reward payload: %v", reward.Goods[0])
		}
	}
	if !gotReward {
		t.Fatal("expected NotifyBagReward")
	}
}
