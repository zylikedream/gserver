package logic

import (
	"context"

	// "gserver/gameconfig"
	gameconfig "gserver/gameconfig/src"
	"gserver/service/role/internal/logic/bag"

	"gserver/protocol/pb"

	"github.com/ahmetb/go-linq"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	ErrItemAddNoGrid        = errors.New("bag full no grid to add item")
	ErrItemDecItemNotEnough = errors.New("dec item not enough")
)

type RoleBag struct {
	RoleModule `bson:"inline"`
	Items      map[int]*bag.BagItem     `bson:"items"`
	Currencies map[int]*bag.BagCurrency `bson:"currencies"`
	GridUse    int                      `bson:"grid_use"`
}

func NewRoleBag() *RoleBag {
	return &RoleBag{
		Items:      make(map[int]*bag.BagItem),
		Currencies: make(map[int]*bag.BagCurrency),
	}
}

func (r *RoleBag) AddSingleItem(ctx context.Context, item bag.Item) (*bag.ItemChange, error) {
	itemTable := gameconfig.GameConfig().TbItem
	itemconf := itemTable.Get(int32(item.ID))
	have := r.Items[item.ID]
	newGrid := (have.Num + item.Num - 1) / uint64(itemconf.MaxOverlap)
	gridAdd := int(newGrid - have.Grid)
	if newGrid > have.Grid && r.isGridFull(gridAdd) {
		return nil, errors.Wrapf(ErrItemAddNoGrid, "item_num:%d grid_use:%d ", item.Num, r.GridUse)
	}
	if itemconf.AutoUse {
		// todo 自动使用物品
		return nil, nil
	}
	chg := have.Update(item.ID, have.Num+item.Num, newGrid)
	r.GridUse += gridAdd
	return chg, nil
}

func (r *RoleBag) GetItem(propid int) bag.Item {
	bagItem := r.Items[propid]
	return bag.Item{
		ID:  propid,
		Num: bagItem.Num,
	}
}

func (r *RoleBag) isGridFull(add int) bool {
	bagMaxGrid := gameconfig.GameConfig().TbGlobal.Get().MaxGrid
	return int32(r.GridUse+add) > bagMaxGrid
}

func (r *RoleBag) DecSingleItem(ctx context.Context, item bag.Item) (*bag.ItemChange, error) {
	have := r.Items[item.ID]
	if item.Num > have.Num {
		return nil, errors.Wrapf(ErrItemDecItemNotEnough, "have:%v, need:%v", have, item)
	}
	itemTable := gameconfig.GameConfig().TbItem
	itemconf := itemTable.Get(int32(item.ID))
	newGrid := (have.Num - item.Num - 1) / uint64(itemconf.MaxOverlap)
	gridDec := int(newGrid - have.Grid)

	chg := have.Update(item.ID, have.Num-item.Num, newGrid)

	if newGrid == 0 {
		delete(r.Items, item.ID)
	}
	r.GridUse -= gridDec
	return chg, nil
}

func (r *RoleBag) AddItemRc(ctx context.Context, itemRcList []*gameconfig.ItemItemRC) error {
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
	var chgs []*bag.ItemChange
	itemList = r.ClassifyItemList(itemList)
	for _, item := range itemList {
		if chg, err := r.AddSingleItem(ctx, item); err != nil {
			// todo 格子满了的处理
			return err
		} else {
			chgs = append(chgs, chg)
		}
	}
	r.notifyItemUpdate(chgs)
	glog.Debug(ctx, "add item success", zap.Any("item", itemList), zap.Any("chgs", chgs))
	return nil
}

func (r *RoleBag) CheckItemRc(ctx context.Context, itemRcList []*gameconfig.ItemItemRC) bool {
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

func (r *RoleBag) DecItemRC(ctx context.Context, itemRcList []*gameconfig.ItemItemRC) error {
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
	var chgs []*bag.ItemChange
	for _, item := range itemList {
		if chg, err := r.DecSingleItem(ctx, item); err != nil {
			return err
		} else {
			chgs = append(chgs, chg)
		}
	}
	r.notifyItemUpdate(chgs)
	glog.Debug(ctx, "dec item success", zap.Any("item", itemList), zap.Any("griddec ", chgs))
	return nil
}

func (r *RoleBag) notifyItemUpdate(chgs []*bag.ItemChange) {
	// sess := ctx.GetSession()
	msg := &pb.NotifyItemUpdate{
		Items: []*pb.PItemUpdate{},
	}
	linq.From(chgs).Select(func(i any) any {
		return &pb.PItemUpdate{
			PropId: int32(i.(*bag.ItemChange).PropID),
			Num:    int64(i.(*bag.ItemChange).Num),
		}
	}).ToSlice(&msg.Items)
	r.Role.SendClient(msg)
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

func (r *RoleBag) ItemRC2Item(itemRcList []*gameconfig.ItemItemRC) ([]bag.Item, error) {
	items := []bag.Item{}
	linq.From(itemRcList).Select(func(obj any) any {
		return bag.Item{
			ID:  int(obj.(*gameconfig.ItemItemRC).Id),
			Num: uint64(obj.(*gameconfig.ItemItemRC).Num),
		}
	}).ToSlice(&items)
	return items, nil
}

func (r *RoleBag) ReqBagInfo(ctx context.Context, req *pb.ReqBagInfo) (*pb.RspBagInfo, error) {
	msg := &pb.RspBagInfo{
		Items: []*pb.PItemInfo{},
	}
	linq.From(r.Items).Select(func(i any) any {
		return &pb.PItemInfo{
			PropId: int32(i.(*bag.BagItem).PropID),
			Num:    int64(i.(*bag.BagItem).Num),
		}
	}).ToSlice(&msg.Items)
	linq.From(r.Currencies).Select(func(i any) any {
		return &pb.PCurrencyInfo{
			Id:  int32(i.(*bag.BagCurrency).PropID),
			Num: int64(i.(*bag.BagCurrency).Num),
		}
	}).ToSlice(&msg.Currencys)
	return msg, nil
}
