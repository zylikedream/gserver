package logic

import (
	"context"
	"math/rand"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/pkg/gameconfig"
	"gserver/src/util"
)

// ========== 数据模型 ==========

type OrderSlotData struct {
	SlotID      int32         `json:"slot_id"`
	ResidentID  int32         `json:"resident_id"`
	Demands     []bag.BagGood `json:"demands"`
	Reward      []bag.BagGood `json:"reward"`
	CooldownEnd int64         `json:"cooldown_end"`
}

type OrderSlotMap map[int32]*OrderSlotData

type Int32List []int32

type RoleResidentOrderState struct {
	RolePersistState
	Slots             OrderSlotMap `gorm:"column:slots;type:jsonb;serializer:json"`
	CompletedCount    int32        `gorm:"column:completed_count"`
	ClaimedMilestones Int32List    `gorm:"column:claimed_milestones;type:jsonb;serializer:json"`
}

func (RoleResidentOrderState) TableName() string { return "role_resident_order" }

// ========== 模块 ==========

type RoleResidentOrder struct {
	RoleModule
	RoleResidentOrderState
}

var _ IRoleModule = (*RoleResidentOrder)(nil)

func (r *RoleResidentOrder) PersistState() IPersistState {
	return &r.RoleResidentOrderState
}

func (r *RoleResidentOrder) OnCreate(ctx context.Context) {
	if r.Slots == nil {
		r.Slots = make(OrderSlotMap)
	}
	playerLevel := int32(1)
	if r.Role != nil && r.Role.Basic != nil {
		playerLevel = r.Role.Basic.Level
	}
	for _, cfg := range gameconfig.Get().TbResidentOrderSlot.GetDataList() {
		if cfg.UnlockLevel <= playerLevel {
			r.refreshSlot(cfg.Id)
		}
	}
	r.MarkDirty()
}

func (r *RoleResidentOrder) OnModInit(ctx context.Context) error {
	return nil
}

// ========== 订单生成 ==========

func (r *RoleResidentOrder) refreshSlot(slotID int32) {
	slotCfg := gameconfig.Get().TbResidentOrderSlot.Get(slotID)
	if slotCfg == nil {
		delete(r.Slots, slotID)
		return
	}
	tpl := gameconfig.Get().TbResidentOrder.Get(slotCfg.OrderId)
	if tpl == nil {
		delete(r.Slots, slotID)
		return
	}

	residentID := tpl.ResidentIds[rand.Intn(len(tpl.ResidentIds))]

	products := r.availableFlowerProducts()
	if len(products) == 0 {
		delete(r.Slots, slotID)
		return
	}

	filtered := filterKindProbs(tpl.KindProbs, len(products))
	if len(filtered) == 0 {
		delete(r.Slots, slotID)
		return
	}

	needKindCount := util.WeightedRandom(filtered)
	demands := randomDemands(products, needKindCount, tpl.NeedMin, tpl.NeedMax)

	reward := make([]bag.BagGood, len(tpl.Reward))
	for i, rw := range tpl.Reward {
		reward[i] = bag.SlackGood2BagGood(rw)
	}

	r.Slots[slotID] = &OrderSlotData{
		SlotID:      slotID,
		ResidentID:  residentID,
		Demands:     demands,
		Reward:      reward,
		CooldownEnd: time.Now().Unix() + int64(slotCfg.Cooldown),
	}
}

func (r *RoleResidentOrder) availableFlowerProducts() []int32 {
	if r.Role == nil || r.Role.Flower == nil || r.Role.Flower.Flowers == nil {
		return nil
	}
	var products []int32
	for _, flower := range r.Role.Flower.Flowers {
		if flower.State != int32(pb.FlowerState_FLOWER_HARVESTED) {
			continue
		}
		cfg := gameconfig.Get().TbFlower.Get(flower.FlowerID)
		if cfg != nil {
			products = append(products, cfg.HarvestItemId)
		}
	}
	return products
}

// ========== 随机工具 ==========

func filterKindProbs(probs []*gamecfg.GardenProbEntry, availableCount int) []*gamecfg.GardenProbEntry {
	var filtered []*gamecfg.GardenProbEntry
	for _, p := range probs {
		if int(p.Type) <= availableCount {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func randomDemands(products []int32, count int32, minNum, maxNum int32) []bag.BagGood {
	shuffled := make([]int32, len(products))
	copy(shuffled, products)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	if int(count) > len(shuffled) {
		count = int32(len(shuffled))
	}
	demands := make([]bag.BagGood, count)
	for i := int32(0); i < count; i++ {
		num := minNum
		if maxNum > minNum {
			num = minNum + rand.Int31n(maxNum-minNum+1)
		}
		demands[i] = bag.BagGood{
			GoodID: int(shuffled[i]),
			Num:    uint64(num),
		}
	}
	return demands
}

// ========== 等级解锁 ==========

func (r *RoleResidentOrder) ensureUnlockedSlots() {
	if r.Slots == nil {
		r.Slots = make(OrderSlotMap)
	}
	playerLevel := int32(1)
	if r.Role != nil && r.Role.Basic != nil {
		playerLevel = r.Role.Basic.Level
	}
	dirty := false
	for _, cfg := range gameconfig.Get().TbResidentOrderSlot.GetDataList() {
		if cfg.UnlockLevel <= playerLevel {
			if _, ok := r.Slots[cfg.Id]; !ok {
				r.refreshSlot(cfg.Id)
				dirty = true
			}
		}
	}
	if dirty {
		r.MarkDirty()
	}
}

// ========== Proto handler ==========

func (r *RoleResidentOrder) ReqResidentOrderInfo(ctx context.Context, req *pb.ReqResidentOrderInfo) (*pb.RspResidentOrderInfo, error) {
	r.ensureUnlockedSlots()
	var slots []*pb.PResidentOrderSlot
	for _, slotCfg := range gameconfig.Get().TbResidentOrderSlot.GetDataList() {
		slot := r.Slots[slotCfg.Id]
		if slot == nil {
			continue
		}
		slots = append(slots, r.toPResidentOrderSlot(slot, slotCfg))
	}
	return &pb.RspResidentOrderInfo{
		Slots:          slots,
		CompletedCount: r.CompletedCount,
		Milestones:     r.buildMilestones(),
	}, nil
}

func (r *RoleResidentOrder) ReqResidentOrderSubmit(ctx context.Context, req *pb.ReqResidentOrderSubmit) (*pb.RspResidentOrderSubmit, error) {
	r.ensureUnlockedSlots()
	slot := r.Slots[req.SlotId]
	if slot == nil {
		return nil, ErrOrderSlotCooldown
	}
	now := time.Now().Unix()
	if now < slot.CooldownEnd {
		return nil, ErrOrderSlotCooldown
	}

	demands := make([]*gamecfg.GardenGoodStack, len(slot.Demands))
	for i, d := range slot.Demands {
		demands[i] = bag.MakeGoodStack(d.GoodID, int(d.Num))
	}
	if !r.Role.Bag.CheckGoods(demands) {
		return nil, ErrOrderNotEnough
	}

	if err := r.Role.Bag.SaveGoods(ctx, demands, nil, "order_submit"); err != nil {
		return nil, err
	}

	rewards := make([]*gamecfg.GardenGoodStack, len(slot.Reward))
	for i, rw := range slot.Reward {
		rewards[i] = bag.MakeGoodStack(rw.GoodID, int(rw.Num))
	}
	if err := r.Role.Bag.SaveGoods(ctx, nil, rewards, "order_reward", bag.OptNotifyReward()); err != nil {
		return nil, err
	}

	slotCfg := gameconfig.Get().TbResidentOrderSlot.Get(slot.SlotID)

	r.refreshSlot(req.SlotId)
	r.CompletedCount++

	r.Role.PublishRoleEvent(ctx, event.EVENT_ORDER_COMPLETE, &event.OrderCompleteEventData{
		SlotID: req.SlotId,
	})

	r.MarkDirty()

	return &pb.RspResidentOrderSubmit{
		Slot:           r.toPResidentOrderSlot(r.Slots[req.SlotId], slotCfg),
		CompletedCount: r.CompletedCount,
	}, nil
}

func (r *RoleResidentOrder) ReqResidentOrderClaimMilestone(ctx context.Context, req *pb.ReqResidentOrderClaimMilestone) (*pb.RspResidentOrderClaimMilestone, error) {
	cfg := gameconfig.Get().TbResidentOrderProgressReward.Get(req.Id)
	if cfg == nil {
		return nil, ErrOrderMilestoneNotReached
	}
	if r.CompletedCount < cfg.NeedCount {
		return nil, ErrOrderMilestoneNotReached
	}
	for _, claimed := range r.ClaimedMilestones {
		if claimed == req.Id {
			return nil, ErrOrderMilestoneClaimed
		}
	}

	if err := r.Role.Bag.SaveGoods(ctx, nil, cfg.Reward, "order_milestone", bag.OptNotifyReward()); err != nil {
		return nil, err
	}

	r.ClaimedMilestones = append(r.ClaimedMilestones, req.Id)
	r.MarkDirty()
	return &pb.RspResidentOrderClaimMilestone{}, nil
}

// ========== 辅助方法 ==========

func (r *RoleResidentOrder) toPResidentOrderSlot(slot *OrderSlotData, slotCfg *gamecfg.GardenResidentOrderSlot) *pb.PResidentOrderSlot {
	if slot == nil {
		return nil
	}
	residentCfg := gameconfig.Get().TbResident.Get(slot.ResidentID)
	residentName := ""
	residentDesc := ""
	if residentCfg != nil {
		residentName = residentCfg.Name
		residentDesc = residentCfg.OrderText
	}
	demands := make([]*pb.PGoodInfo, len(slot.Demands))
	for i, d := range slot.Demands {
		demands[i] = &pb.PGoodInfo{PropId: int32(d.GoodID), Num: int64(d.Num)}
	}
	rewards := make([]*pb.PGoodInfo, len(slot.Reward))
	for i, rw := range slot.Reward {
		rewards[i] = &pb.PGoodInfo{PropId: int32(rw.GoodID), Num: int64(rw.Num)}
	}
	pos := int32(0)
	if slotCfg != nil {
		pos = slotCfg.Position
	}
	return &pb.PResidentOrderSlot{
		SlotId:       slot.SlotID,
		Position:     pos,
		ResidentId:   slot.ResidentID,
		ResidentName: residentName,
		ResidentDesc: residentDesc,
		Demands:      demands,
		Reward:       rewards,
		CoolDownEnd:  slot.CooldownEnd,
	}
}

func (r *RoleResidentOrder) buildMilestones() []*pb.PResidentOrderMilestone {
	milestones := make([]*pb.PResidentOrderMilestone, 0)
	for _, cfg := range gameconfig.Get().TbResidentOrderProgressReward.GetDataList() {
		claimed := false
		for _, c := range r.ClaimedMilestones {
			if c == cfg.Id {
				claimed = true
				break
			}
		}
		milestones = append(milestones, &pb.PResidentOrderMilestone{
			Id:        cfg.Id,
			NeedCount: cfg.NeedCount,
			Claimed:   claimed,
		})
	}
	return milestones
}
