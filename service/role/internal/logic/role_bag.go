package logic

import (
	"context"
	"time"

	// "gserver/gameconfig"
	gameconfig "gserver/gameconfig/src"

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

type bagItem struct {
	PropID     int       `bson:"prop_id"`
	Num        uint64    `bson:"num"`
	Grid       uint64    `bson:"grid"` // 占用的格子数
	UpdateTime time.Time `bson:"update_time"`
}

type Item struct {
	ID  int    `bson:"id" copier:"Id"`
	Num uint64 `bson:"num"`
}

type ItemChange struct {
	PropID  int
	PreNum  uint64
	Num     uint64
	PreGrid uint64
	Grid    uint64
}

func (it *bagItem) update(propID int, Num uint64, Grid uint64) *ItemChange {
	chg := &ItemChange{
		PropID:  propID,
		PreNum:  it.Num,
		Num:     Num,
		PreGrid: it.Grid,
		Grid:    Grid,
	}

	it.PropID = propID
	it.Num = Num
	it.Grid = Grid
	it.UpdateTime = time.Now()

	return chg
}

type RoleBag struct {
	RoleModule
	RoleID  int64           `bson:"role_id"`
	Items   map[int]bagItem `bson:"items"`
	GridUse int             `bson:"grid_use"`
}

func NewRoleBag() *RoleBag {
	return &RoleBag{
		Items: make(map[int]bagItem),
	}
}

func (r *RoleBag) OnInit(ctx context.Context) error {
	return nil
}

func (r *RoleBag) AddSingleItem(ctx context.Context, item Item) (*ItemChange, error) {
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
	chg := have.update(item.ID, have.Num+item.Num, newGrid)
	r.GridUse += gridAdd
	return chg, nil
}

func (r *RoleBag) GetItem(propid int) Item {
	bagItem := r.Items[propid]
	return Item{
		ID:  propid,
		Num: bagItem.Num,
	}
}

func (r *RoleBag) isGridFull(add int) bool {
	bagMaxGrid := gameconfig.GameConfig().TbGlobal.Get().MaxGrid
	return int32(r.GridUse+add) > bagMaxGrid
}

func (r *RoleBag) DecSingleItem(ctx context.Context, item Item) (*ItemChange, error) {
	have := r.Items[item.ID]
	if item.Num > have.Num {
		return nil, errors.Wrapf(ErrItemDecItemNotEnough, "have:%v, need:%v", have, item)
	}
	itemTable := gameconfig.GameConfig().TbItem
	itemconf := itemTable.Get(int32(item.ID))
	newGrid := (have.Num - item.Num - 1) / uint64(itemconf.MaxOverlap)
	gridDec := int(newGrid - have.Grid)

	chg := have.update(item.ID, have.Num-item.Num, newGrid)

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

func (r *RoleBag) AddItem(ctx context.Context, itemList []Item) error {
	var chgs []*ItemChange
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

func (r *RoleBag) CheckItem(ctx context.Context, itemList []Item) bool {
	if len(itemList) == 0 {
		return true
	}
	itemList = r.ClassifyItemList(itemList)
	return linq.From(itemList).All(func(i interface{}) bool {
		item := i.(Item)
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

func (r *RoleBag) DecItem(ctx context.Context, itemList []Item) error {
	if len(itemList) == 0 {
		return nil
	}
	itemList = r.ClassifyItemList(itemList)
	var chgs []*ItemChange
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

func (r *RoleBag) notifyItemUpdate(chgs []*ItemChange) {
	// sess := ctx.GetSession()
	msg := pb.NotifyItemUpdate{
		Items: []*pb.PItemUpdate{},
	}
	linq.From(chgs).SelectT(func(i interface{}) interface{} {
		return &pb.PItemUpdate{
			PropId: int32(i.(*ItemChange).PropID),
			Num:    int64(i.(*ItemChange).Num),
		}
	}).ToSlice(&msg.Items)
	// if err := sess.Send(msg); err != nil {
	// 	s.logger.Error("notify item update failed", zap.Any("msg", msg))
	// }
}

func (r *RoleBag) ClassifyItemList(itemList []Item) []Item {
	classifyItemList := []Item{}
	linq.From(itemList).GroupBy(
		func(it interface{}) interface{} { return it.(Item).ID },
		func(it interface{}) interface{} { return it.(Item).Num },
	).Select(func(i interface{}) interface{} {
		return Item{
			ID:  i.(linq.Group).Key.(int),
			Num: linq.From(i.(linq.Group).Group).SumUInts(),
		}
	}).ToSlice(&classifyItemList)
	return classifyItemList
}

func (r *RoleBag) ItemRC2Item(itemRcList []*gameconfig.ItemItemRC) ([]Item, error) {
	items := []Item{}
	linq.From(itemRcList).Select(func(obj interface{}) interface{} {
		return Item{
			ID:  int(obj.(*gameconfig.ItemItemRC).Id),
			Num: uint64(obj.(*gameconfig.ItemItemRC).Num),
		}
	}).ToSlice(&items)
	return items, nil
}
