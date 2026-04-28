package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"

	cfg "gserver/gameconfig/src"
	"gserver/src/apps/role/internal/logic/bag"

	"gserver/protocol/pb"

	"github.com/ahmetb/go-linq"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	ErrItemDecItemNotEnough = errors.New("dec item not enough")
)

type GoodsMap map[int]*bag.BagGood

// Value 实现 driver.Valuer 接口，将 map 转换为 JSON 字节
func (m GoodsMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan 实现 sql.Scanner 接口，将 JSON 字节转换为 map
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

func (r *RoleBag) AddSingleItem(ctx context.Context, item bag.Item) (*bag.BagChange, error) {
	have := r.Goods[item.ID]
	if have == nil {
		have = &bag.BagGood{
			Type:   bag.GoodTypeItem,
			PropID: item.ID,
		}
		r.Goods[item.ID] = have
	}
	chg := have.Update(have.Num + item.Num)
	r.MarkDirty()
	return chg, nil
}

func (r *RoleBag) GetItem(propID int) bag.Item {
	good := r.Goods[propID]
	if good == nil {
		return bag.Item{ID: propID, Num: 0}
	}
	return bag.Item{ID: propID, Num: good.Num}
}

func (r *RoleBag) DecSingleItem(ctx context.Context, item bag.Item) (*bag.BagChange, error) {
	have := r.Goods[item.ID]
	if have == nil || item.Num > have.Num {
		return nil, errors.Wrapf(ErrItemDecItemNotEnough, "have:%v, need:%v", have, item)
	}
	chg := have.Update(have.Num - item.Num)
	if have.Num == 0 {
		delete(r.Goods, item.ID)
	}
	r.MarkDirty()
	return chg, nil
}

func (r *RoleBag) AddItemRc(ctx context.Context, itemRcList []*cfg.ItemItemRC) error {
	if len(itemRcList) == 0 {
		return nil
	}
	items, err := r.ItemRC2Item(itemRcList)
	if err != nil {
		return err
	}
	return r.AddItem(ctx, items)
}

func (r *RoleBag) AddItem(ctx context.Context, itemList []bag.Item) error {
	var chgs []*bag.BagChange
	itemList = r.ClassifyItemList(itemList)
	for _, item := range itemList {
		if chg, err := r.AddSingleItem(ctx, item); err != nil {
			// todo 格子满了的处理
			return err
		} else {
			chgs = append(chgs, chg)
		}
	}
	r.notifyBagUpdate(ctx, chgs)
	glog.Debug(ctx, "add item success", zap.Any("item", itemList), zap.Any("chgs", chgs))
	return nil
}

func (r *RoleBag) CheckItemRc(ctx context.Context, itemRcList []*cfg.ItemItemRC) bool {
	items, err := r.ItemRC2Item(itemRcList)
	if err != nil {
		return false
	}
	return r.CheckItem(ctx, items)
}

func (r *RoleBag) CheckItem(ctx context.Context, itemList []bag.Item) bool {
	if len(itemList) == 0 {
		return true
	}
	itemList = r.ClassifyItemList(itemList)
	return linq.From(itemList).All(func(i any) bool {
		item := i.(bag.Item)
		have := r.GetItem(item.ID)
		return have.Num >= item.Num
	})
}

func (r *RoleBag) DecItemRC(ctx context.Context, itemRcList []*cfg.ItemItemRC) error {
	items, err := r.ItemRC2Item(itemRcList)
	if err != nil {
		return err
	}
	return r.DecItem(ctx, items)
}

func (r *RoleBag) DecItem(ctx context.Context, itemList []bag.Item) error {
	if len(itemList) == 0 {
		return nil
	}
	itemList = r.ClassifyItemList(itemList)
	var chgs []*bag.BagChange
	for _, item := range itemList {
		if chg, err := r.DecSingleItem(ctx, item); err != nil {
			return err
		} else {
			chgs = append(chgs, chg)
		}
	}
	r.notifyBagUpdate(ctx, chgs)
	glog.Debug(ctx, "dec item success", zap.Any("item", itemList), zap.Any("griddec ", chgs))
	return nil
}

func (r *RoleBag) notifyBagUpdate(ctx context.Context, chgs []*bag.BagChange) {
	msg := &pb.NotifyBagUpdate{
		Goods: []*pb.PBagGoodUpdate{},
	}
	linq.From(chgs).Select(func(i any) any {
		return &pb.PBagGoodUpdate{
			PropId: int32(i.(*bag.BagChange).PropID),
			PreNum: int64(i.(*bag.BagChange).PreNum),
			Num:    int64(i.(*bag.BagChange).Num),
		}
	}).ToSlice(&msg.Goods)
	r.Role.SendClient(ctx, msg)
}

func (r *RoleBag) ClassifyItemList(itemList []bag.Item) []bag.Item {
	classifyItemList := []bag.Item{}
	linq.From(itemList).GroupBy(
		func(it any) any { return it.(bag.Item).ID },
		func(it any) any { return it.(bag.Item).Num },
	).Select(func(i any) any {
		return bag.Item{
			ID:  i.(linq.Group).Key.(int),
			Num: linq.From(i.(linq.Group).Group).SumUInts(),
		}
	}).ToSlice(&classifyItemList)
	return classifyItemList
}

func (r *RoleBag) ItemRC2Item(itemRcList []*cfg.ItemItemRC) ([]bag.Item, error) {
	items := []bag.Item{}
	linq.From(itemRcList).Select(func(obj any) any {
		return bag.Item{
			ID:  int(obj.(*cfg.ItemItemRC).Id),
			Num: uint64(obj.(*cfg.ItemItemRC).Num),
		}
	}).ToSlice(&items)
	return items, nil
}

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
