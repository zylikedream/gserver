package bag

import "time"

// Type constants
const (
    GoodTypeItem     = 0
    GoodTypeCurrency = 1
)

type BagGood struct {
    Type       int       `db:"type"`       // 0=Item, 1=Currency
    PropID     int       `db:"prop_id"`
    Num        uint64    `db:"num"`
    UpdateTime time.Time `db:"update_time"`
}

type BagChange struct {
    PropID  int
    PreNum uint64
    Num    uint64
}

// Item 类型别名，保持向后兼容
type Item struct {
    ID  int `bson:"id" copier:"Id"`
    Num uint64 `bson:"num"`
}

func (g *BagGood) Update(num uint64) *BagChange {
    chg := &BagChange{
        PropID:  g.PropID,
        PreNum: g.Num,
        Num:    num,
    }
    g.Num = num
    g.UpdateTime = time.Now()
    return chg
}
