package event

import gamecfg "gserver/gameconfig/gosrc"

const (
	EVENT_GOOD_CHANGE    EventType = "good_change"
	EVENT_PLAYER_LEVEL   EventType = "player_level"
	EVENT_BREED_START    EventType = "breed_start"
	EVENT_BREED_FINISH   EventType = "breed_finish"
	EVENT_PLANT_FLOWER   EventType = "plant_flower"
	EVENT_WATER_FLOWER   EventType = "water_flower"
	EVENT_HARVEST_FLOWER EventType = "harvest_flower"
	EVENT_UNLOCK_PLOT    EventType = "unlock_plot"
	EVENT_FLOWER_LEVEL   EventType = "flower_level"
	EVENT_ORDER_COMPLETE EventType = "order_complete"
)

type GoodChange struct {
	GoodID    int
	PreNum    uint64
	Num       uint64
	AddNum    uint64
	RemoveNum uint64
	Reason    string
}

type GoodChangeEventData struct {
	Changes []GoodChange
}

type PlayerLevelEventData struct {
	OldLevel int32
	NewLevel int32
	OldExp   int64
	NewExp   int64
	Reason   string
}

type BreedStartEventData struct {
	FlowerID int32
}

type BreedFinishEventData struct {
	FlowerID int32
}

type PlantFlowerEventData struct {
	FlowerID int32
	PlotIDs  []int32
}

type WaterFlowerEventData struct {
	PlotIDs []int32
}

type HarvestFlowerItem struct {
	FlowerID   int32
	PlotID     int32
	HarvestNum int32
}

type HarvestFlowerEventData struct {
	Items   []*gamecfg.GardenGoodStack
	Flowers []HarvestFlowerItem
}

type UnlockPlotEventData struct {
	PlotID int32
}

type FlowerLevelEventData struct {
	FlowerID int32
	OldLevel int32
	NewLevel int32
}

type OrderCompleteEventData struct {
	SlotID int32
}
