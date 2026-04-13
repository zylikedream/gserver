package bag

import "time"

type BagItem struct {
	PropID     int       `db:"prop_id"`
	Num        uint64    `db:"num"`
	Grid       uint64    `db:"grid"` // 占用的格子数
	UpdateTime time.Time `db:"update_time"`
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

func (it *BagItem) Update(propID int, Num uint64, Grid uint64) *ItemChange {
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
