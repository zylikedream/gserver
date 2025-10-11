package bag

import "time"

type BagCurrency struct {
	PropID     int       `bson:"prop_id"`
	Num        uint64    `bson:"num"`
	UpdateTime time.Time `bson:"update_time"`
}

type Currency struct {
	ID  int    `bson:"id" copier:"Id"`
	Num uint64 `bson:"num"`
}

type CurrencyChange struct {
	PropID int
	PreNum uint64
	Num    uint64
}

func (it *BagCurrency) Update(propID int, Num uint64) *CurrencyChange {
	chg := &CurrencyChange{
		PropID: propID,
		PreNum: it.Num,
		Num:    Num,
	}

	it.PropID = propID
	it.Num = Num
	it.UpdateTime = time.Now()

	return chg
}
