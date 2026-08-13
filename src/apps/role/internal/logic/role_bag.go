package logic

import (
	"context"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"

	"gserver/core/gxylog"

	"github.com/pkg/errors"
)

var (
	ErrGoodNotEnough      = errors.New("good not enough")
	ErrGoodConfigNotFound = errors.New("good config not found")
	ErrGoodExceedMaxStack = errors.New("exceed max stack")
)

type GoodsMap map[int]bag.BagGood

type RoleBagState struct {
	RolePersistState
	Goods GoodsMap `gorm:"column:goods;type:jsonb;serializer:json"`
}

func (RoleBagState) TableName() string { return "role_bag" }

func (r *RoleBagState) GetIndexes() []string {
	return []string{"update_at"}
}

type RoleBag struct {
	RoleModule
	RoleBagState
}

var _ IRoleModule = (*RoleBag)(nil)

func (r *RoleBag) PersistState() IPersistState {
	return &r.RoleBagState
}

func (r *RoleBag) OnModInit(ctx context.Context) error {
	return nil
}

// ========== 单个物品操作（内部使用） ==========
func (r *RoleBag) addGoods(goodsMap GoodsMap, goods []bag.Good, reason string) ([]bag.GoodOp, error) {
	var ops []bag.GoodOp
	for _, g := range goods {
		chg, err := r.addSingleGood(goodsMap, g)
		if err != nil {
			return nil, err
		}
		chg.Reson = reason
		ops = append(ops, chg)
	}
	return ops, nil
}

func (r *RoleBag) addSingleGood(goodsMap GoodsMap, good bag.Good) (bag.GoodOp, error) {
	cfg := r.Cfg().TbItem.Get(int32(good.GoodID))
	if cfg == nil {
		return bag.GoodOp{}, ErrGoodConfigNotFound
	}
	have := goodsMap[good.GoodID]
	if have.GoodID == 0 {
		have = bag.BagGood{
			GoodID: good.GoodID,
		}
	}
	newNum := have.Num + good.Num
	if cfg.MaxStack > 0 && newNum > uint64(cfg.MaxStack) {
		return bag.GoodOp{}, ErrGoodExceedMaxStack
	}
	op := have.Update(newNum)
	goodsMap[good.GoodID] = have
	return op, nil
}

func (r *RoleBag) decGoods(goodsMap GoodsMap, goods []bag.Good, reason string) ([]bag.GoodOp, error) {
	var ops []bag.GoodOp
	for _, g := range goods {
		chg, err := r.decSingleGood(goodsMap, g)
		if err != nil {
			return nil, err
		}
		chg.Reson = reason
		ops = append(ops, chg)
	}
	return ops, nil
}

func (r *RoleBag) decSingleGood(goodsMap GoodsMap, good bag.Good) (bag.GoodOp, error) {
	have := goodsMap[good.GoodID]
	if have.GoodID == 0 || good.Num > have.Num {
		return bag.GoodOp{}, errors.Wrapf(ErrGoodNotEnough, "have:%v, need:%v", have, good)
	}
	op := have.Update(have.Num - good.Num)
	goodsMap[good.GoodID] = have
	if have.Num == 0 {
		delete(goodsMap, good.GoodID)
	}
	return op, nil
}

func (r *RoleBag) GetGood(GoodID int) bag.Good {
	good := r.Goods[GoodID]
	if good.GoodID == 0 {
		return bag.Good{GoodID: GoodID, Num: 0}
	}
	return bag.Good{GoodID: GoodID, Num: good.Num}
}

func (r *RoleBag) cloneGoodsMap() GoodsMap {
	// 复制物品列表
	clone := make(GoodsMap)
	for prop, good := range r.Goods {
		clone[prop] = good
	}
	return clone
}

func (r *RoleBag) saveGoodsMap(goodsMap GoodsMap) {
	r.Goods = goodsMap
	r.MarkDirty()
}

// ========== 公开接口 ==========

// SaveGoods 原子性地扣除并添加物品，reason 用于日志
func (r *RoleBag) SaveGoods(ctx context.Context, removeGoods []*gamecfg.GardenGoodStack, addGoods []*gamecfg.GardenGoodStack, reason string, opts0 ...bag.SaveGoodsOpts) error {
	opts := bag.DefaultSaveGoodsOpts()
	if len(opts0) > 0 {
		opts = opts0[0]
	}
	remove := classifyGoods(removeGoods)
	add := classifyGoods(addGoods)

	if len(remove) == 0 && len(add) == 0 {
		return nil
	}

	goodsMap := r.cloneGoodsMap()
	// 执行扣除
	removeOps, err := r.decGoods(goodsMap, remove, reason)
	if err != nil {
		return errors.Wrapf(err, "save good failed, err")
	}
	// 执行添加
	addOps, err := r.addGoods(goodsMap, add, reason)
	if err != nil {
		return errors.Wrapf(err, "save good failed, err")
	}

	r.saveGoodsMap(goodsMap)
	// 合并变更
	ops := append(removeOps, addOps...)

	r.onBagChange(ctx, ops, reason, opts)

	r.saveGoodOps(ctx, ops)
	return nil
}

// CheckGoods 检查物品是否足够
func (r *RoleBag) CheckGoods(goodsStack []*gamecfg.GardenGoodStack) bool {
	goods := classifyGoods(goodsStack)
	for _, g := range goods {
		have := r.GetGood(g.GoodID)
		if have.Num < g.Num {
			return false
		}
	}
	return true
}

func (r *RoleBag) onBagChange(ctx context.Context, ops []bag.GoodOp, reason string, opts bag.SaveGoodsOpts) {
	updates := map[int]bag.GoodUpdate{}
	// 合并RemoveGoods和AddGoods的ID,他们可能有重复的
	// 经过合并上每个物品ID最多出现两次并且一定是一次扣除一次添加
	for _, op := range ops {
		update := updates[op.GoodID]
		if update.GoodID == 0 {
			update.GoodID = op.GoodID
			update.Reason = reason
			update.PreNum = op.PreNum
			update.Num = op.Num
		} else { // 已存在，更新当前数量(不更新之前数量)
			update.Num = op.Num
		}
		if op.Num > op.PreNum {
			update.AddNum = op.Num - op.PreNum
		} else if op.Num < op.PreNum {
			update.RemoveNum = op.PreNum - op.Num
		}
		updates[op.GoodID] = update
	}

	r.notifyBagUpdate(ctx, updates, opts)
	r.notifyBagReward(ctx, updates, opts)
	r.onGoodUpdateEvent(ctx, updates)
}

// 通知客户端背包变更
func (r *RoleBag) notifyBagUpdate(ctx context.Context, updates map[int]bag.GoodUpdate, opts bag.SaveGoodsOpts) {
	r.notifyGoodUpdate(ctx, updates, opts)
}

func (r *RoleBag) notifyBagReward(ctx context.Context, updates map[int]bag.GoodUpdate, opts bag.SaveGoodsOpts) {
	if !opts.NotifyReward || len(updates) == 0 {
		return
	}
	msg := &pb.NotifyBagReward{Goods: make([]*pb.PGoodInfo, 0, len(updates))}
	for _, update := range updates {
		if update.AddNum == 0 {
			continue
		}
		msg.Goods = append(msg.Goods, &pb.PGoodInfo{
			PropId: int32(update.GoodID),
			Num:    int64(update.AddNum),
		})
	}
	if len(msg.Goods) == 0 {
		return
	}
	r.Role.SendClient(ctx, msg)
}

func (r *RoleBag) notifyGoodUpdate(ctx context.Context, updates map[int]bag.GoodUpdate, opts bag.SaveGoodsOpts) {
	if !opts.NotifyChange {
		return
	}
	msg := &pb.NotifyBagUpdate{
		Goods: []*pb.PBagGoodUpdate{},
	}
	for _, update := range updates {
		msg.Goods = append(msg.Goods, &pb.PBagGoodUpdate{
			PropId: int32(update.GoodID),
			Num:    int64(update.Num),
			PreNum: int64(update.PreNum),
		})
	}
	r.Role.SendClient(ctx, msg)
}

// 通知物品变更事件
func (r *RoleBag) onGoodUpdateEvent(ctx context.Context, updates map[int]bag.GoodUpdate) {
	if r.Role == nil {
		return
	}
	changes := make([]event.GoodChange, 0, len(updates))
	for _, update := range updates {
		changes = append(changes, event.GoodChange{
			GoodID:    update.GoodID,
			PreNum:    update.PreNum,
			Num:       update.Num,
			AddNum:    update.AddNum,
			RemoveNum: update.RemoveNum,
			Reason:    update.Reason,
		})
	}
	r.Role.PublishRoleEvent(ctx, event.EVENT_GOOD_CHANGE, event.GoodChangeEventData{Changes: changes})
}

// 保存物品变更操作
func (r *RoleBag) saveGoodOps(ctx context.Context, ops []bag.GoodOp) {
	for _, op := range ops {
		if op.Num > op.PreNum {
			gxylog.Debug(ctx, "add good", gxylog.Num("id", op.GoodID), gxylog.Num("num", op.Num), gxylog.Str("reason", op.Reson))
		} else if op.Num < op.PreNum {
			gxylog.Debug(ctx, "dec good", gxylog.Num("id", op.GoodID), gxylog.Num("num", op.Num), gxylog.Str("reason", op.Reson))
		}
	}
}

// ========== 工具方法 ==========
func classifyGoods(goodsList []*gamecfg.GardenGoodStack) []bag.Good {
	if goodsList == nil {
		return []bag.Good{}
	}
	result := []bag.Good{}
	// 使用map按ID分组并累加数量
	groups := make(map[int]uint64)
	for _, g := range goodsList {
		groups[int(g.Id)] += uint64(g.Num)
	}
	// 转换为结果列表
	for id, num := range groups {
		if num > 0 {
			result = append(result, bag.Good{GoodID: id, Num: num})
		}
	}
	return result
}

// ========== Proto Handler ==========

func (r *RoleBag) ReqBagInfo(ctx context.Context, req *pb.ReqBagInfo) (*pb.RspBagInfo, error) {
	msg := &pb.RspBagInfo{
		Goods: []*pb.PGoodInfo{},
	}
	for _, good := range r.Goods {
		msg.Goods = append(msg.Goods, &pb.PGoodInfo{
			PropId: int32(good.GoodID),
			Num:    int64(good.Num),
		})
	}
	return msg, nil
}
