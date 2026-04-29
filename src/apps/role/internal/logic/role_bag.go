package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/ahmetb/go-linq"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	ErrGoodNotEnough    = errors.New("good not enough")
	ErrGoodConfigNotFound = errors.New("good config not found")
	ErrGoodExceedMaxStack = errors.New("exceed max stack")
)

type GoodsMap map[int]*bag.BagGood

func (m GoodsMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *GoodsMap) Scan(value interface{}) error {
	if value == nil {
		*m = make(GoodsMap)
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("invalid type for GoodsMap")
	}

	var goodsMap map[int]*bag.BagGood
	if err := json.Unmarshal(bytes, &goodsMap); err != nil {
		return err
	}

	*m = GoodsMap(goodsMap)
	return nil
}

type RoleBagState struct {
	RolePersistState
	Goods GoodsMap `gorm:"column:goods;type:jsonb"`
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

func (r *RoleBag) addSingleGood(ctx context.Context, good bag.Good) (*bag.GoodChange, error) {
	cfg := gameconfig.GameConfig().TbItem.Get(int32(good.ID))
	if cfg == nil {
		return nil, ErrGoodConfigNotFound
	}
	have := r.Goods[good.ID]
	if have == nil {
		have = &bag.BagGood{
			PropID: good.ID,
		}
		r.Goods[good.ID] = have
	}
	newNum := have.Num + good.Num
	if cfg.MaxStack > 0 && newNum > uint64(cfg.MaxStack) {
		return nil, ErrGoodExceedMaxStack
	}
	chg := have.Update(newNum)
	r.MarkDirty()
	return chg, nil
}

func (r *RoleBag) decSingleGood(ctx context.Context, good bag.Good) (*bag.GoodChange, error) {
	have := r.Goods[good.ID]
	if have == nil || good.Num > have.Num {
		return nil, errors.Wrapf(ErrGoodNotEnough, "have:%v, need:%v", have, good)
	}
	chg := have.Update(have.Num - good.Num)
	if have.Num == 0 {
		delete(r.Goods, good.ID)
	}
	r.MarkDirty()
	return chg, nil
}

func (r *RoleBag) GetGood(propID int) bag.Good {
	good := r.Goods[propID]
	if good == nil {
		return bag.Good{ID: propID, Num: 0}
	}
	return bag.Good{ID: propID, Num: good.Num}
}

// ========== 公开接口 ==========

// SaveGoods 原子性地扣除并添加物品，reason 用于日志
func (r *RoleBag) SaveGoods(ctx context.Context, remove []bag.Good, add []bag.Good, reason string) error {
	if len(remove) == 0 && len(add) == 0 {
		return nil
	}
	remove = classifyGoods(remove)
	add = classifyGoods(add)

	// 先检查扣除物品是否足够
	for _, g := range remove {
		have := r.GetGood(g.ID)
		if have.Num < g.Num {
			return errors.Wrapf(ErrGoodNotEnough, "check failed, goodID:%d, have:%d, need:%d", g.ID, have.Num, g.Num)
		}
	}

	// 执行扣除
	var chgs []*bag.GoodChange
	for _, g := range remove {
		chg, err := r.decSingleGood(ctx, g)
		if err != nil {
			return err
		}
		chgs = append(chgs, chg)
		glog.Debug(ctx, "save_goods: remove", zap.Int("goodID", g.ID), zap.Uint64("num", g.Num), zap.String("reason", reason))
	}

	// 执行添加
	for _, g := range add {
		chg, err := r.addSingleGood(ctx, g)
		if err != nil {
			return err
		}
		chgs = append(chgs, chg)
		glog.Debug(ctx, "save_goods: add", zap.Int("goodID", g.ID), zap.Uint64("num", g.Num), zap.String("reason", reason))
	}

	// 合并变更通知客户端
	r.notifyBagUpdate(ctx, chgs)
	return nil
}

// CheckGoods 检查物品是否足够
func (r *RoleBag) CheckGoods(goodsList []bag.Good) bool {
	if len(goodsList) == 0 {
		return true
	}
	goodsList = classifyGoods(goodsList)
	for _, g := range goodsList {
		have := r.GetGood(g.ID)
		if have.Num < g.Num {
			return false
		}
	}
	return true
}

// AddGoodsStack 从配置表物品栈添加（兼容 gameconfig 接口）
func (r *RoleBag) AddGoodsStack(ctx context.Context, itemStackList []*gamecfg.GardenItemStack) error {
	if len(itemStackList) == 0 {
		return nil
	}
	goods := itemStack2Goods(itemStackList)
	return r.SaveGoods(ctx, nil, goods, "add_stack")
}

// CheckGoodsStack 从配置表物品栈检查（兼容 gameconfig 接口）
func (r *RoleBag) CheckGoodsStack(itemStackList []*gamecfg.GardenItemStack) bool {
	goods := itemStack2Goods(itemStackList)
	return r.CheckGoods(goods)
}

// DecGoodsStack 从配置表物品栈扣除（兼容 gameconfig 接口）
func (r *RoleBag) DecGoodsStack(ctx context.Context, itemStackList []*gamecfg.GardenItemStack) error {
	if len(itemStackList) == 0 {
		return nil
	}
	goods := itemStack2Goods(itemStackList)
	return r.SaveGoods(ctx, goods, nil, "dec_stack")
}

// ========== 客户端通知 ==========

func (r *RoleBag) notifyBagUpdate(ctx context.Context, chgs []*bag.GoodChange) {
	msg := &pb.NotifyBagUpdate{
		Goods: []*pb.PBagGoodUpdate{},
	}
	linq.From(chgs).Select(func(i any) any {
		return &pb.PBagGoodUpdate{
			PropId: int32(i.(*bag.GoodChange).PropID),
			PreNum: int64(i.(*bag.GoodChange).PreNum),
			Num:    int64(i.(*bag.GoodChange).Num),
		}
	}).ToSlice(&msg.Goods)
	r.Role.SendClient(ctx, msg)
}

// ========== 工具方法 ==========

func classifyGoods(goodsList []bag.Good) []bag.Good {
	result := []bag.Good{}
	linq.From(goodsList).GroupBy(
		func(it any) any { return it.(bag.Good).ID },
		func(it any) any { return it.(bag.Good).Num },
	).Select(func(i any) any {
		return bag.Good{
			ID:  i.(linq.Group).Key.(int),
			Num: linq.From(i.(linq.Group).Group).SumUInts(),
		}
	}).ToSlice(&result)
	return result
}

func itemStack2Goods(itemStackList []*gamecfg.GardenItemStack) []bag.Good {
	goods := []bag.Good{}
	linq.From(itemStackList).Select(func(obj any) any {
		return bag.Good{
			ID:  int(obj.(*gamecfg.GardenItemStack).Id),
			Num: uint64(obj.(*gamecfg.GardenItemStack).Num),
		}
	}).ToSlice(&goods)
	return goods
}

// ========== Proto Handler ==========

func (r *RoleBag) ReqBagInfo(ctx context.Context, req *pb.ReqBagInfo) (*pb.RspBagInfo, error) {
	msg := &pb.RspBagInfo{
		Goods: []*pb.PGoodInfo{},
	}
	for _, good := range r.Goods {
		msg.Goods = append(msg.Goods, &pb.PGoodInfo{
			PropId: int32(good.PropID),
			Num:    int64(good.Num),
		})
	}
	return msg, nil
}
