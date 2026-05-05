package bag

import (
	gamecfg "gserver/gameconfig/gosrc"
	"time"
)

// Type constants
const (
	GoodTypeItem     = 0
	GoodTypeCurrency = 1
)

type BagGood struct {
	GoodID     int `json:"good_id"`
	Num        uint64
	UpdateTime time.Time `json:"update_time"`
}

type GoodOp struct {
	GoodID int
	PreNum uint64
	Num    uint64
	Reson  string
}

type GoodUpdate struct {
	GoodID    int
	PreNum    uint64 // 变更前的数量
	Num       uint64 // 变更后的数量
	RemoveNum uint64 // 减少的数量
	AddNum    uint64 // 增加的数量
	Reason    string // 变更原因
}

// Good 物品（包含 item 和 currency）
type Good struct {
	GoodID int
	Num    uint64
}

type SaveGoodsOpts struct {
	NotifyChange bool
	NotifyReward bool
}

func DefaultSaveGoodsOpts() SaveGoodsOpts {
	return SaveGoodsOpts{NotifyChange: true}
}

func OptSilent() SaveGoodsOpts {
	return SaveGoodsOpts{}
}

func OptNotifyReward() SaveGoodsOpts {
	return SaveGoodsOpts{NotifyChange: true, NotifyReward: true}
}

func (g *BagGood) Update(num uint64) GoodOp {
	Op := GoodOp{
		GoodID: g.GoodID,
		PreNum: g.Num,
		Num:    num,
	}
	g.Num = num
	g.UpdateTime = time.Now()
	return Op
}

func SlackGood2BagGood(stack *gamecfg.GardenGoodStack) BagGood {
	return BagGood{
		GoodID:     int(stack.Id),
		Num:        uint64(stack.Num),
		UpdateTime: time.Now(),
	}
}
