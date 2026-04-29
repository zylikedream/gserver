package bag

import "time"

// Type constants
const (
	GoodTypeItem     = 0
	GoodTypeCurrency = 1
)

type BagGood struct {
	PropID     int `json:"prop_id"`
	Num        uint64
	UpdateTime time.Time `json:"update_time"`
}

type BagChange struct {
	PropID int
	PreNum uint64
	Num    uint64
}

// Item 类型别名，保持向后兼容
type Item struct {
	ID  int
	Num uint64
}

func (g *BagGood) Update(num uint64) *BagChange {
	chg := &BagChange{
		PropID: g.PropID,
		PreNum: g.Num,
		Num:    num,
	}
	g.Num = num
	g.UpdateTime = time.Now()
	return chg
}
