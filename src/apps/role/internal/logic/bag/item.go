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

type GoodChange struct {
	PropID int
	PreNum uint64
	Num    uint64
}

// Good 物品（包含 item 和 currency）
type Good struct {
	ID  int
	Num uint64
}

func (g *BagGood) Update(num uint64) *GoodChange {
	chg := &GoodChange{
		PropID: g.PropID,
		PreNum: g.Num,
		Num:    num,
	}
	g.Num = num
	g.UpdateTime = time.Now()
	return chg
}
