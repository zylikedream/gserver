package logic

import (
	"context"
	"math/rand"
	"time"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"
)

// ========== 数据模型 ==========

type OrderSlotData struct {
	SlotID      int32         `json:"slot_id"`
	ResidentID  int32         `json:"resident_id"`
	Demands     []bag.BagGood `json:"demands"`
	Reward      []bag.BagGood `json:"reward"`
	CooldownEnd int64         `json:"cooldown_end"`
}

type RoleResidentOrderState struct {
	RolePersistState
	Slots             map[int32]*OrderSlotData `gorm:"column:slots;type:jsonb"`
	CompletedCount    int32                    `gorm:"column:completed_count"`
	ClaimedMilestones []int32                  `gorm:"column:claimed_milestones;type:jsonb"`
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
		r.Slots = make(map[int32]*OrderSlotData)
	}
	for _, cfg := range gameconfig.GameConfig().TbResidentOrderSlot.GetDataList() {
		r.refreshSlot(cfg.Id)
	}
	r.MarkDirty()
}

func (r *RoleResidentOrder) OnModInit(ctx context.Context) error {
	if r.Slots == nil {
		r.Slots = make(map[int32]*OrderSlotData)
	}
	if len(r.Slots) == 0 {
		for _, cfg := range gameconfig.GameConfig().TbResidentOrderSlot.GetDataList() {
			r.refreshSlot(cfg.Id)
		}
		r.MarkDirty()
	}
	return nil
}

// ========== 订单生成 ==========

func (r *RoleResidentOrder) refreshSlot(slotID int32) {
	slotCfg := gameconfig.GameConfig().TbResidentOrderSlot.Get(slotID)
	if slotCfg == nil {
		delete(r.Slots, slotID)
		return
	}
	tpl := gameconfig.GameConfig().TbResidentOrder.Get(slotCfg.OrderId)
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

	needKindCount := weightedRandom(filtered)
	demands := randomDemands(products, needKindCount, tpl.NeedMin, tpl.NeedMax)

	reward := make([]bag.BagGood, len(tpl.Reward))
	for i, rw := range tpl.Reward {
		reward[i] = bag.SlackGood2BagGood(rw)
	}

	r.Slots[slotID] = &OrderSlotData{
		SlotID:     slotID,
		ResidentID: residentID,
		Demands:    demands,
		Reward:     reward,
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
		cfg := gameconfig.GameConfig().TbFlower.Get(flower.FlowerID)
		if cfg != nil {
			products = append(products, cfg.HarvestItemId)
		}
	}
	return products
}

// ========== 随机工具 ==========

func filterKindProbs(probs []*gamecfg.GardenResidentOrderKindProb, availableCount int) []*gamecfg.GardenResidentOrderKindProb {
	var filtered []*gamecfg.GardenResidentOrderKindProb
	for _, p := range probs {
		if int(p.Type) <= availableCount {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func weightedRandom(probs []*gamecfg.GardenResidentOrderKindProb) int32 {
	total := int32(0)
	for _, p := range probs {
		total += p.Prob
	}
	r := rand.Int31n(total)
	cumulative := int32(0)
	for _, p := range probs {
		cumulative += p.Prob
		if r < cumulative {
			return p.Type
		}
	}
	return probs[len(probs)-1].Type
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

// ========== Proto handler ==========

func (r *RoleResidentOrder) ReqOrderInfo(ctx context.Context, req *pb.ReqOrderInfo) (*pb.RspOrderInfo, error) {
	var slots []*pb.POrderSlot
	for _, slotCfg := range gameconfig.GameConfig().TbResidentOrderSlot.GetDataList() {
		slot := r.Slots[slotCfg.Id]
		if slot == nil {
			continue
		}
		slots = append(slots, r.toPOrderSlot(slot, slotCfg))
	}
	return &pb.RspOrderInfo{
		Slots:          slots,
		CompletedCount: r.CompletedCount,
		Milestones:     r.buildMilestones(),
	}, nil
}

func (r *RoleResidentOrder) ReqSubmitOrder(ctx context.Context, req *pb.ReqSubmitOrder) (*pb.RspSubmitOrder, error) {
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
		demands[i] = &gamecfg.GardenGoodStack{Id: int32(d.GoodID), Num: int32(d.Num)}
	}
	if !r.Role.Bag.CheckGoods(demands) {
		return nil, ErrOrderNotEnough
	}

	if err := r.Role.Bag.SaveGoods(ctx, demands, nil, "order_submit"); err != nil {
		return nil, err
	}

	rewards := make([]*gamecfg.GardenGoodStack, len(slot.Reward))
	for i, rw := range slot.Reward {
		rewards[i] = &gamecfg.GardenGoodStack{Id: int32(rw.GoodID), Num: int32(rw.Num)}
	}
	if err := r.Role.Bag.SaveGoods(ctx, nil, rewards, "order_reward", bag.OptNotifyReward()); err != nil {
		return nil, err
	}

	slotCfg := gameconfig.GameConfig().TbResidentOrderSlot.Get(slot.SlotID)

	r.refreshSlot(req.SlotId)

	newSlot := r.Slots[req.SlotId]
	if slotCfg != nil && newSlot != nil {
		newSlot.CooldownEnd = now + int64(slotCfg.Cooldown)
	}

	r.CompletedCount++

	r.Role.PublishRoleEvent(event.EVENT_ORDER_COMPLETE, &event.OrderCompleteEventData{
		SlotID: req.SlotId,
	})

	r.MarkDirty()

	return &pb.RspSubmitOrder{
		Slot:           r.toPOrderSlot(r.Slots[req.SlotId], slotCfg),
		CompletedCount: r.CompletedCount,
	}, nil
}

func (r *RoleResidentOrder) ReqClaimOrderMilestone(ctx context.Context, req *pb.ReqClaimOrderMilestone) (*pb.RspClaimOrderMilestone, error) {
	cfg := gameconfig.GameConfig().TbResidentOrderProgressReward.Get(req.Id)
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

	rewards := make([]*gamecfg.GardenGoodStack, len(cfg.Reward))
	for i, rw := range cfg.Reward {
		rewards[i] = &gamecfg.GardenGoodStack{Id: rw.Id, Num: rw.Num}
	}
	if err := r.Role.Bag.SaveGoods(ctx, nil, rewards, "order_milestone", bag.OptNotifyReward()); err != nil {
		return nil, err
	}

	r.ClaimedMilestones = append(r.ClaimedMilestones, req.Id)
	r.MarkDirty()
	return &pb.RspClaimOrderMilestone{}, nil
}

// ========== 辅助方法 ==========

func (r *RoleResidentOrder) toPOrderSlot(slot *OrderSlotData, slotCfg *gamecfg.GardenResidentOrderSlot) *pb.POrderSlot {
	if slot == nil {
		return nil
	}
	residentCfg := gameconfig.GameConfig().TbResident.Get(slot.ResidentID)
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
	return &pb.POrderSlot{
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

func (r *RoleResidentOrder) buildMilestones() []*pb.PMilestoneInfo {
	milestones := make([]*pb.PMilestoneInfo, 0)
	for _, cfg := range gameconfig.GameConfig().TbResidentOrderProgressReward.GetDataList() {
		claimed := false
		for _, c := range r.ClaimedMilestones {
			if c == cfg.Id {
				claimed = true
				break
			}
		}
		milestones = append(milestones, &pb.PMilestoneInfo{
			Id:        cfg.Id,
			NeedCount: cfg.NeedCount,
			Claimed:   claimed,
		})
	}
	return milestones
}
