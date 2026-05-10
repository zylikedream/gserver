package logic

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/agiledragon/gomonkey/v2"
	proto "google.golang.org/protobuf/proto"
)

func TestOrderSlotMap_ValueAndScan(t *testing.T) {
	original := OrderSlotMap{
		1: {
			SlotID:      1,
			ResidentID:  1001,
			Demands:     []bag.BagGood{{GoodID: 2001, Num: 2}},
			Reward:      []bag.BagGood{{GoodID: 1, Num: 30}},
			CooldownEnd: 123456789,
		},
	}

	value, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}

	var scanned OrderSlotMap
	if err := scanned.Scan(value); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, scanned) {
		originalJSON, _ := json.Marshal(original)
		scannedJSON, _ := json.Marshal(scanned)
		t.Fatalf("slot map mismatch: original=%s scanned=%s", originalJSON, scannedJSON)
	}
}

func TestInt32List_ValueAndScan(t *testing.T) {
	original := Int32List{1, 2, 5}

	value, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}

	var scanned Int32List
	if err := scanned.Scan(value); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, scanned) {
		t.Fatalf("int32 list mismatch: original=%v scanned=%v", original, scanned)
	}
}

func initOrderTestConfig(t *testing.T) {
	t.Helper()
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
	plots := loadTestTable(t, "garden_tbgardenplot")
	tbPlot, err := gamecfg.NewGardenTbGardenPlot(plots)
	if err != nil {
		t.Fatal(err)
	}
	playerLevels := loadTestTable(t, "garden_tbplayerlevel")
	tbPlayerLevel, err := gamecfg.NewGardenTbPlayerLevel(playerLevels)
	if err != nil {
		t.Fatal(err)
	}
	mainTasks := loadTestTable(t, "garden_tbmaintask")
	tbMainTask, err := gamecfg.NewGardenTbMainTask(mainTasks)
	if err != nil {
		t.Fatal(err)
	}
	orderTpls := loadTestTable(t, "garden_tbresidentorder")
	tbOrder, err := gamecfg.NewGardenTbResidentOrder(orderTpls)
	if err != nil {
		t.Fatal(err)
	}
	slots := loadTestTable(t, "garden_tbresidentorderslot")
	tbSlot, err := gamecfg.NewGardenTbResidentOrderSlot(slots)
	if err != nil {
		t.Fatal(err)
	}
	residents := loadTestTable(t, "garden_tbresident")
	tbResident, err := gamecfg.NewGardenTbResident(residents)
	if err != nil {
		t.Fatal(err)
	}
	milestones := loadTestTable(t, "garden_tbresidentorderprogressreward")
	tbMilestone, err := gamecfg.NewGardenTbResidentOrderProgressReward(milestones)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{
		TbItem:                        tbItem,
		TbFlower:                      tbFlower,
		TbGardenPlot:                  tbPlot,
		TbPlayerLevel:                 tbPlayerLevel,
		TbMainTask:                    tbMainTask,
		TbResidentOrder:               tbOrder,
		TbResidentOrderSlot:           tbSlot,
		TbResident:                    tbResident,
		TbResidentOrderProgressReward: tbMilestone,
	}
}

func setupTestOrder(t *testing.T, flowerIDs ...int32) (*RoleMain, *RoleResidentOrder) {
	t.Helper()
	initOrderTestConfig(t)

	patch := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, msg proto.Message) {},
	)
	t.Cleanup(patch.Reset)

	main := &RoleMain{eventBus: event.NewEventBus()}
	basicMod := &RoleBasic{
		RoleModule:     RoleModule{Role: main},
		RoleBasicState: RoleBasicState{Level: 1},
	}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	flowerMod := &RoleFlower{
		RoleModule:      RoleModule{Role: main},
		RoleFlowerState: RoleFlowerState{Flowers: make(FlowerMap)},
	}
	orderMod := &RoleResidentOrder{
		RoleModule: RoleModule{Role: main},
	}

	main.Basic = basicMod
	main.Bag = bagMod
	main.Flower = flowerMod
	main.ResidentOrder = orderMod

	for _, fid := range flowerIDs {
		flowerMod.AddFlower(fid)
		if f, ok := flowerMod.Flowers[fid]; ok {
			f.State = int32(pb.FlowerState_FLOWER_HARVESTED)
		}
	}

	if err := orderMod.OnModInit(context.Background()); err != nil {
		t.Fatal(err)
	}
	orderMod.OnCreate(context.Background())
	return main, orderMod
}

// 获取第一张订单的需求物品ID和数量
func firstSlotDemand(t *testing.T, orderMod *RoleResidentOrder) (slotID int32, itemID, needNum int32) {
	t.Helper()
	for _, cfg := range gameconfig.GameConfig().TbResidentOrderSlot.GetDataList() {
		slot := orderMod.Slots[cfg.Id]
		if slot != nil && len(slot.Demands) > 0 {
			return slot.SlotID, int32(slot.Demands[0].GoodID), int32(slot.Demands[0].Num)
		}
	}
	t.Fatal("no slot with demands found")
	return 0, 0, 0
}

func TestOrderInfo_NewRole(t *testing.T) {
	// 没有已培育完成的花，所有点位无订单
	_, orderMod := setupTestOrder(t)

	rsp, err := orderMod.ReqResidentOrderInfo(context.Background(), &pb.ReqResidentOrderInfo{})
	if err != nil {
		t.Fatal(err)
	}

	// 没有花产品 → 所有点位都没有订单
	if len(rsp.Slots) != 0 {
		t.Fatalf("expected 0 slots, got %d", len(rsp.Slots))
	}
	if rsp.CompletedCount != 0 {
		t.Fatalf("expected completed_count 0, got %d", rsp.CompletedCount)
	}
	if len(rsp.Milestones) != 4 {
		t.Fatalf("expected 4 milestones, got %d", len(rsp.Milestones))
	}
}

func TestOrderInfo_WithFlowers(t *testing.T) {
	_, orderMod := setupTestOrder(t, 101)

	rsp, err := orderMod.ReqResidentOrderInfo(context.Background(), &pb.ReqResidentOrderInfo{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rsp.Slots) == 0 {
		t.Fatal("expected non-empty slots when flowers are available")
	}
	// 所有点位应该都有订单（有1种花，所有模板都能生成1种需求）
	for _, s := range rsp.Slots {
		if len(s.Demands) == 0 {
			t.Fatalf("slot %d has no demands", s.SlotId)
		}
		// 只有1种花可用 → 只能生成1种需求
		if len(s.Demands) != 1 {
			t.Fatalf("slot %d: expected 1 demand, got %d", s.SlotId, len(s.Demands))
		}
	}
	if rsp.CompletedCount != 0 {
		t.Fatalf("expected completed_count 0, got %d", rsp.CompletedCount)
	}
	// 验证所有里程碑状态为未领取
	for _, m := range rsp.Milestones {
		if m.Claimed {
			t.Fatalf("milestone %d should not be claimed", m.Id)
		}
	}
}

func TestSubmitOrder_Success(t *testing.T) {
	_, orderMod := setupTestOrder(t, 101)

	slotID, itemID, needNum := firstSlotDemand(t, orderMod)

	// 加足够的花产品到背包
	addGoods := []*gamecfg.GardenGoodStack{{Id: itemID, Num: needNum}}
	if err := orderMod.Role.Bag.SaveGoods(context.Background(), nil, addGoods, "test_add"); err != nil {
		t.Fatal(err)
	}

	// 提交订单
	rsp, err := orderMod.ReqResidentOrderSubmit(context.Background(), &pb.ReqResidentOrderSubmit{SlotId: slotID})
	if err != nil {
		t.Fatal(err)
	}

	if rsp.CompletedCount != 1 {
		t.Fatalf("expected completed_count 1, got %d", rsp.CompletedCount)
	}
	if rsp.Slot == nil {
		t.Fatal("expected slot in response")
	}
	// 新的订单应该已生成，且有冷却时间
	if rsp.Slot.CoolDownEnd == 0 {
		t.Fatal("expected non-zero cooldown after submit")
	}

	// 验证物品已被扣除
	has := orderMod.Role.Bag.CheckGoods(addGoods)
	if has {
		t.Fatal("goods should have been deducted")
	}

	// 验证累计完成数
	if orderMod.CompletedCount != 1 {
		t.Fatalf("expected CompletedCount 1, got %d", orderMod.CompletedCount)
	}
}

func TestSubmitOrder_Cooldown(t *testing.T) {
	_, orderMod := setupTestOrder(t, 101)

	slotID, itemID, needNum := firstSlotDemand(t, orderMod)

	addGoods := []*gamecfg.GardenGoodStack{{Id: itemID, Num: needNum * 3}}
	if err := orderMod.Role.Bag.SaveGoods(context.Background(), nil, addGoods, "test_add"); err != nil {
		t.Fatal(err)
	}

	// 第一次提交成功
	_, err := orderMod.ReqResidentOrderSubmit(context.Background(), &pb.ReqResidentOrderSubmit{SlotId: slotID})
	if err != nil {
		t.Fatal(err)
	}

	// 第二次提交应被拒绝（冷却中）
	_, err = orderMod.ReqResidentOrderSubmit(context.Background(), &pb.ReqResidentOrderSubmit{SlotId: slotID})
	if err != ErrOrderSlotCooldown {
		t.Fatalf("expected ErrOrderSlotCooldown, got %v", err)
	}
}

func TestSubmitOrder_NotEnough(t *testing.T) {
	_, orderMod := setupTestOrder(t, 101)

	slotID, _, _ := firstSlotDemand(t, orderMod)

	// 背包为空，提交应失败
	_, err := orderMod.ReqResidentOrderSubmit(context.Background(), &pb.ReqResidentOrderSubmit{SlotId: slotID})
	if err != ErrOrderNotEnough {
		t.Fatalf("expected ErrOrderNotEnough, got %v", err)
	}
}

func TestSubmitOrder_InvalidSlot(t *testing.T) {
	_, orderMod := setupTestOrder(t)

	// 不存在的 slot ID（且没有花产品，slot 已被删除）
	_, err := orderMod.ReqResidentOrderSubmit(context.Background(), &pb.ReqResidentOrderSubmit{SlotId: 999})
	if err != ErrOrderSlotCooldown {
		t.Fatalf("expected ErrOrderSlotCooldown for invalid slot, got %v", err)
	}
}

func TestClaimMilestone_Success(t *testing.T) {
	_, orderMod := setupTestOrder(t)

	orderMod.CompletedCount = 15

	rsp, err := orderMod.ReqResidentOrderClaimMilestone(context.Background(), &pb.ReqResidentOrderClaimMilestone{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rsp == nil {
		t.Fatal("expected response")
	}

	// 验证已被标记为已领取
	found := false
	for _, c := range orderMod.ClaimedMilestones {
		if c == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("milestone 1 should be marked as claimed")
	}
}

func TestClaimMilestone_NotReached(t *testing.T) {
	_, orderMod := setupTestOrder(t)

	orderMod.CompletedCount = 5

	_, err := orderMod.ReqResidentOrderClaimMilestone(context.Background(), &pb.ReqResidentOrderClaimMilestone{Id: 1})
	if err != ErrOrderMilestoneNotReached {
		t.Fatalf("expected ErrOrderMilestoneNotReached, got %v", err)
	}
}

func TestClaimMilestone_AlreadyClaimed(t *testing.T) {
	_, orderMod := setupTestOrder(t)

	orderMod.CompletedCount = 30
	orderMod.ClaimedMilestones = []int32{1}

	// 首次领取 milestone 2 应成功
	_, err := orderMod.ReqResidentOrderClaimMilestone(context.Background(), &pb.ReqResidentOrderClaimMilestone{Id: 2})
	if err != nil {
		t.Fatal(err)
	}

	// 再次领取 milestone 1 应被拒绝
	_, err = orderMod.ReqResidentOrderClaimMilestone(context.Background(), &pb.ReqResidentOrderClaimMilestone{Id: 1})
	if err != ErrOrderMilestoneClaimed {
		t.Fatalf("expected ErrOrderMilestoneClaimed, got %v", err)
	}
}

func TestOrderGeneration_FilterKindProbs_OneFlower(t *testing.T) {
	// 只有1种花 → 所有订单只能生成1种需求
	_, orderMod := setupTestOrder(t, 101)

	for slotID, slot := range orderMod.Slots {
		if len(slot.Demands) != 1 {
			t.Fatalf("slot %d with 1 flower: expected 1 demand, got %d", slotID, len(slot.Demands))
		}
	}
}

func TestOrderGeneration_FilterKindProbs_MultipleFlowers(t *testing.T) {
	// 有3种花 → 模板 1002 和 1003 有机会生成2种需求
	_, orderMod := setupTestOrder(t, 101, 102, 103)

	for slotID, slot := range orderMod.Slots {
		if len(slot.Demands) < 1 || len(slot.Demands) > 2 {
			t.Fatalf("slot %d: unexpected demand count %d", slotID, len(slot.Demands))
		}
		// 验证需求物品各不相同
		seen := make(map[int32]bool)
		for _, d := range slot.Demands {
			if seen[int32(d.GoodID)] {
				t.Fatalf("slot %d: duplicate item %d in demands", slotID, int32(d.GoodID))
			}
			seen[int32(d.GoodID)] = true
		}
	}
}

func TestOrderGeneration_ResidentFromConfig(t *testing.T) {
	_, orderMod := setupTestOrder(t, 101)

	for slotID, slot := range orderMod.Slots {
		// 验证居民存在于模板配置中
		slotCfg := gameconfig.GameConfig().TbResidentOrderSlot.Get(slotID)
		if slotCfg == nil {
			continue
		}
		tpl := gameconfig.GameConfig().TbResidentOrder.Get(slotCfg.OrderId)
		if tpl == nil {
			continue
		}
		found := false
		for _, rid := range tpl.ResidentIds {
			if rid == slot.ResidentID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("slot %d: resident %d not in template %d pool", slotID, slot.ResidentID, tpl.Id)
		}
	}
}

func TestOrderCompleteEvent(t *testing.T) {
	_, orderMod := setupTestOrder(t, 101)

	slotID, itemID, needNum := firstSlotDemand(t, orderMod)

	// 监听事件
	eventCh := make(chan event.EventParam, 1)
	orderMod.Role.SubscribeRoleEvent(event.EVENT_ORDER_COMPLETE, func(param event.EventParam) {
		eventCh <- param
	})

	addGoods := []*gamecfg.GardenGoodStack{{Id: itemID, Num: needNum}}
	if err := orderMod.Role.Bag.SaveGoods(context.Background(), nil, addGoods, "test_add"); err != nil {
		t.Fatal(err)
	}

	if _, err := orderMod.ReqResidentOrderSubmit(context.Background(), &pb.ReqResidentOrderSubmit{SlotId: slotID}); err != nil {
		t.Fatal(err)
	}

	select {
	case param := <-eventCh:
		data, ok := param.Data.(*event.OrderCompleteEventData)
		if !ok {
			t.Fatal("expected OrderCompleteEventData")
		}
		if data.SlotID != slotID {
			t.Fatalf("expected slotID %d, got %d", slotID, data.SlotID)
		}
	default:
		t.Fatal("expected EVENT_ORDER_COMPLETE to be published")
	}
}

func TestSlotLocking_Level1(t *testing.T) {
	// 等级1 → 只解锁 unlock_level <= 1 的 slot
	_, orderMod := setupTestOrder(t, 101)

	activeCount := 0
	for _, cfg := range gameconfig.GameConfig().TbResidentOrderSlot.GetDataList() {
		if orderMod.Slots[cfg.Id] != nil {
			activeCount++
		}
	}
	if activeCount != 2 {
		t.Fatalf("level 1: expected 2 active slots, got %d", activeCount)
	}
}

func TestSlotLocking_NoBasic(t *testing.T) {
	// Basic 模块不存在时，默认等级1（仍需要有花产品才能生成订单）
	initOrderTestConfig(t)

	main := &RoleMain{eventBus: event.NewEventBus()}
	fakeFlower := &RoleFlower{
		RoleModule:      RoleModule{Role: main},
		RoleFlowerState: RoleFlowerState{Flowers: make(FlowerMap)},
	}
	fakeFlower.AddFlower(101)
	fakeFlower.Flowers[101].State = int32(pb.FlowerState_FLOWER_HARVESTED)
	main.Flower = fakeFlower

	orderMod := &RoleResidentOrder{
		RoleModule: RoleModule{Role: main},
	}
	main.ResidentOrder = orderMod
	orderMod.OnCreate(context.Background())

	if len(orderMod.Slots) != 2 {
		t.Fatalf("no Basic/level1: expected 2 active slots, got %d", len(orderMod.Slots))
	}
}

func TestSlotLocking_LevelUp(t *testing.T) {
	_, orderMod := setupTestOrder(t, 101)

	checkActive := func(expected int) map[int32]bool {
		active := make(map[int32]bool)
		for _, cfg := range gameconfig.GameConfig().TbResidentOrderSlot.GetDataList() {
			if orderMod.Slots[cfg.Id] != nil {
				active[cfg.Id] = true
			}
		}
		if len(active) != expected {
			t.Fatalf("expected %d active slots, got %v", expected, active)
		}
		return active
	}

	// 等级1：2个slot
	checkActive(2)

	// 升级到3：新增 slot 3 (unlock=2) 和 slot 4 (unlock=3)
	orderMod.Role.Basic.Level = 3
	orderMod.ensureUnlockedSlots()
	active := checkActive(4)
	for _, id := range []int32{1, 2, 3, 4} {
		if !active[id] {
			t.Fatalf("slot %d should be active at level 3", id)
		}
	}

	// 升级到5：新增 slot 5 (unlock=5)
	orderMod.Role.Basic.Level = 5
	orderMod.ensureUnlockedSlots()
	active = checkActive(5)
	for _, id := range []int32{1, 2, 3, 4, 5} {
		if !active[id] {
			t.Fatalf("slot %d should be active at level 5", id)
		}
	}

	// 升级但无新slot解锁：数量不变
	orderMod.Role.Basic.Level = 10
	orderMod.ensureUnlockedSlots()
	checkActive(5)
}

func TestSlotLocking_NoNewUnlock(t *testing.T) {
	// 等级不变：再次调用 ensureUnlockedSlots 不应新增
	_, orderMod := setupTestOrder(t, 101)

	orderMod.ensureUnlockedSlots()

	activeCount := 0
	for _, cfg := range gameconfig.GameConfig().TbResidentOrderSlot.GetDataList() {
		if orderMod.Slots[cfg.Id] != nil {
			activeCount++
		}
	}
	// 等级1：2个slot，调用 ensureUnlockedSlots 后仍为2（已存在，不重复创建）
	if activeCount != 2 {
		t.Fatalf("expected 2 active slots (no new unlock), got %d", activeCount)
	}
}
