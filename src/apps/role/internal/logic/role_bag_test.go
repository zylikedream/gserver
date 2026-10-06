package logic

import (
	"context"
	"errors"
	"testing"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/pkg/gameconfig"

	"ergo.services/ergo/testing/unit"
	proto "google.golang.org/protobuf/proto"
)

// ========== test setup ==========

func initTestGameConfig(t *testing.T) {
	t.Helper()
	initAllTestConfig(t)
}

func setupTestBag(t *testing.T) *RoleBag {
	t.Helper()
	initTestGameConfig(t)
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

// setupNotifyingBag 起一个带真实 session 的 role actor,返回的 Subject 用于断言
// SaveGoods 发给客户端的消息。opts 相关的用例必须走这里,否则 SendClient 直接返回。
func setupNotifyingBag(t *testing.T) (*RoleBag, *unit.Subject) {
	t.Helper()
	role, subj, _ := spawnTestRole(t, 1001)
	b := &RoleBag{
		RoleModule:   RoleModule{Role: role, RoleID: role.RoleID},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	role.Bag = b
	return b, subj
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

// classifyGoods 的分组契约:同 ID 必须累加,不同 ID 必须各留一条,
// nil/空输入返回空列表而不是 nil。
func TestBagClassifyGoods_MergeSameID(t *testing.T) {
	tests := []struct {
		name  string
		input []*gamecfg.GardenGoodStack
		want  map[int]uint64
	}{
		{"nil", nil, map[int]uint64{}},
		{"empty", []*gamecfg.GardenGoodStack{}, map[int]uint64{}},
		{"single", []*gamecfg.GardenGoodStack{testGoodStack(1001, 5)}, map[int]uint64{1001: 5}},
		{"merge same id", []*gamecfg.GardenGoodStack{
			testGoodStack(1001, 3),
			testGoodStack(1001, 7),
		}, map[int]uint64{1001: 10}},
		{"multiple ids", []*gamecfg.GardenGoodStack{
			testGoodStack(1001, 5),
			testGoodStack(int32(GOLD_ITEM_ID), 10),
		}, map[int]uint64{1001: 5, GOLD_ITEM_ID: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyGoods(tt.input)
			if len(result) != len(tt.want) {
				t.Fatalf("expected %d goods, got %d: %v", len(tt.want), len(result), result)
			}
			got := map[int]uint64{}
			for _, g := range result {
				got[g.GoodID] = g.Num
			}
			for id, wantNum := range tt.want {
				if got[id] != wantNum {
					t.Fatalf("good %d: expected num %d, got %d", id, wantNum, got[id])
				}
			}
		})
	}
}

// ========== GetGood ==========

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

// addSingleGood 的三条契约:新物品落库为 {0->N},已有物品累加为 {N->N+M},
// 超过配置 MaxStack 必须拒绝且不写回 map。
func TestBagAddSingleGood_ExceedMaxStack(t *testing.T) {
	maxStack := uint64(itemConfig(t, 1001).MaxStack)
	tests := []struct {
		name     string
		goodsMap GoodsMap
		add      bag.Good
		wantPre  uint64
		wantNum  uint64
		wantErr  error
	}{
		{"new", make(GoodsMap), bag.Good{GoodID: 1001, Num: 5}, 0, 5, nil},
		{"stack", GoodsMap{1001: {GoodID: 1001, Num: 10}}, bag.Good{GoodID: 1001, Num: 20}, 10, 30, nil},
		{"exceed max stack", GoodsMap{1001: {GoodID: 1001, Num: maxStack - 1}}, bag.Good{GoodID: 1001, Num: 2}, 0, 0, ErrGoodExceedMaxStack},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := setupTestBag(t)

			op, err := b.addSingleGood(tt.goodsMap, tt.add)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected err %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr != nil {
				// 拒绝时 map 必须保持原状,不能留下半写状态
				if got := tt.goodsMap[1001].Num; got != maxStack-1 {
					t.Fatalf("expected goodsMap untouched at %d, got %d", maxStack-1, got)
				}
				return
			}
			if op.PreNum != tt.wantPre || op.Num != tt.wantNum {
				t.Fatalf("expected op {%d->%d}, got {%d->%d}", tt.wantPre, tt.wantNum, op.PreNum, op.Num)
			}
			if got := tt.goodsMap[1001].Num; got != tt.wantNum {
				t.Fatalf("expected map num %d, got %d", tt.wantNum, got)
			}
		})
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

// decSingleGood 的扣除契约:够扣时按 扣前->扣后 记录,
// 扣到 0 必须把条目从 map 删除(不留 Num=0 的僵尸行),
// 持有量不足和完全不存在都归为同一个 ErrGoodNotEnough。
func TestBagDecSingleGood_ToZero(t *testing.T) {
	tests := []struct {
		name     string
		goodsMap GoodsMap
		dec      bag.Good
		wantPre  uint64
		wantNum  uint64
		wantErr  error
		wantGone bool
	}{
		{"normal", GoodsMap{1001: {GoodID: 1001, Num: 50}}, bag.Good{GoodID: 1001, Num: 20}, 50, 30, nil, false},
		{"to zero", GoodsMap{1001: {GoodID: 1001, Num: 10}}, bag.Good{GoodID: 1001, Num: 10}, 10, 0, nil, true},
		{"not exists", make(GoodsMap), bag.Good{GoodID: 1001, Num: 1}, 0, 0, ErrGoodNotEnough, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := setupTestBag(t)

			op, err := b.decSingleGood(tt.goodsMap, tt.dec)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected err %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr != nil {
				if _, exists := tt.goodsMap[1001]; exists {
					t.Fatal("expected goodsMap untouched on rejection")
				}
				return
			}
			if op.PreNum != tt.wantPre || op.Num != tt.wantNum {
				t.Fatalf("expected op {%d->%d}, got {%d->%d}", tt.wantPre, tt.wantNum, op.PreNum, op.Num)
			}
			_, exists := tt.goodsMap[1001]
			if exists == tt.wantGone {
				t.Fatalf("expected exists=%v, got %v", !tt.wantGone, exists)
			}
		})
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

// ========== SaveGoods ==========

// SaveGoods 的落库契约:扣除与添加可以在同一次调用里组合,
// 普通物品与玩家经验物品走同一条背包路径(经验不特殊处理)。
func TestBagSaveGoods_AddOnly(t *testing.T) {
	tests := []struct {
		name   string
		goods  GoodsMap
		remove []*gamecfg.GardenGoodStack
		add    []*gamecfg.GardenGoodStack
		want   map[int]uint64
	}{
		{"add only", nil, nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, map[int]uint64{1001: 10}},
		{"player exp stored", nil, nil, []*gamecfg.GardenGoodStack{testGoodStack(int32(PLAYER_EXP_ITEM_ID), 55)}, map[int]uint64{PLAYER_EXP_ITEM_ID: 55}},
		{"normal and player exp add", nil, nil, []*gamecfg.GardenGoodStack{
			testGoodStack(1001, 10),
			testGoodStack(int32(PLAYER_EXP_ITEM_ID), 20),
		}, map[int]uint64{1001: 10, PLAYER_EXP_ITEM_ID: 20}},
		{"remove only", GoodsMap{1001: {GoodID: 1001, Num: 50}}, []*gamecfg.GardenGoodStack{testGoodStack(1001, 20)}, nil, map[int]uint64{1001: 30}},
		{"remove and add", GoodsMap{
			1001:         {GoodID: 1001, Num: 50},
			GOLD_ITEM_ID: {GoodID: GOLD_ITEM_ID, Num: 100},
		}, []*gamecfg.GardenGoodStack{testGoodStack(1001, 20)}, []*gamecfg.GardenGoodStack{
			testGoodStack(int32(GOLD_ITEM_ID), 50),
		}, map[int]uint64{1001: 30, GOLD_ITEM_ID: 150}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := setupTestBag(t)
			for id, g := range tt.goods {
				b.Goods[id] = g
			}

			if err := b.SaveGoods(context.Background(), tt.remove, tt.add, "test"); err != nil {
				t.Fatal(err)
			}
			if len(b.Goods) != len(tt.want) {
				t.Fatalf("expected %d goods, got %d", len(tt.want), len(b.Goods))
			}
			for id, wantNum := range tt.want {
				if got := b.Goods[id].Num; got != wantNum {
					t.Fatalf("good %d: expected num %d, got %d", id, wantNum, got)
				}
			}
			if !b.IsDirty() {
				t.Fatal("expected dirty")
			}
		})
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

// CheckGoods 的判定契约:逐项比较持有量,任一项不足即 false,
// nil 请求表示"无要求"必须是 true。
func TestBagCheckGoods_NotEnough(t *testing.T) {
	tests := []struct {
		name  string
		goods GoodsMap
		check []*gamecfg.GardenGoodStack
		want  bool
	}{
		{"enough", GoodsMap{1001: {GoodID: 1001, Num: 50}}, []*gamecfg.GardenGoodStack{testGoodStack(1001, 30)}, true},
		{"not enough", GoodsMap{1001: {GoodID: 1001, Num: 5}}, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, false},
		{"not exists", nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 1)}, false},
		{"nil", nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := setupTestBag(t)
			for id, g := range tt.goods {
				b.Goods[id] = g
			}

			if got := b.CheckGoods(tt.check); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

// ========== SaveGoodsOpts ==========

// opts 门控:OptNotifyReward 额外弹奖励,OptSilent 两种通知都不发
// (但数据仍落库并标脏)。这是静默模式唯一的负向用例。
func TestBagSaveGoods_NotifyRewardOpts(t *testing.T) {
	t.Run("notify reward", func(t *testing.T) {
		b, subj := setupNotifyingBag(t)

		err := b.SaveGoods(context.Background(), nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, "test", bag.OptNotifyReward())
		if err != nil {
			t.Fatal(err)
		}
		if b.Goods[1001].Num != 10 {
			t.Fatalf("expected 10, got %d", b.Goods[1001].Num)
		}
		subj.ShouldSend().Where(clientMsgMatcher(func(m proto.Message) bool {
			reward, ok := m.(*pb.NotifyBagReward)
			if !ok || len(reward.Goods) != 1 {
				return false
			}
			return reward.Goods[0].PropId == 1001 && reward.Goods[0].Num == 10
		})).Assert()
	})

	t.Run("silent", func(t *testing.T) {
		b, subj := setupNotifyingBag(t)

		err := b.SaveGoods(context.Background(), nil, []*gamecfg.GardenGoodStack{testGoodStack(1001, 10)}, "test", bag.OptSilent())
		if err != nil {
			t.Fatal(err)
		}
		if b.Goods[1001].Num != 10 {
			t.Fatalf("expected 10, got %d", b.Goods[1001].Num)
		}
		if !b.IsDirty() {
			t.Fatal("expected dirty even in silent mode")
		}
		subj.ShouldSend().Where(clientMsgMatcher(func(m proto.Message) bool {
			_, ok := m.(*pb.NotifyBagUpdate)
			return ok
		})).None().Assert()
		subj.ShouldSend().Where(clientMsgMatcher(func(m proto.Message) bool {
			_, ok := m.(*pb.NotifyBagReward)
			return ok
		})).None().Assert()
	})
}
