package logic

import (
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/pkg/gameconfig"
	"time"
)

func RoleTaskEventTypes() []event.EventType {
	return []event.EventType{
		event.EVENT_GOOD_CHANGE,
		event.EVENT_PLAYER_LEVEL,
		event.EVENT_BREED_START,
		event.EVENT_BREED_FINISH,
		event.EVENT_PLANT_FLOWER,
		event.EVENT_WATER_FLOWER,
		event.EVENT_HARVEST_FLOWER,
		event.EVENT_UNLOCK_PLOT,
		event.EVENT_FLOWER_LEVEL,
	}
}

func CalcCurrentStateProgress(role *RoleMain, currentProgress int32, targetType gamecfg.GardenETaskTargetType, targetParam int32) int32 {
	switch targetType {
	case gamecfg.GardenETaskTargetType_OWN_ITEM:
		return int32(role.Bag.GetGood(int(targetParam)).Num)
	case gamecfg.GardenETaskTargetType_BREED_FINISH:
		return getBreedFinishProgress(role, targetParam)
	case gamecfg.GardenETaskTargetType_PLAYER_LEVEL:
		return role.Basic.Level
	case gamecfg.GardenETaskTargetType_UNLOCK_PLOT:
		return getUnlockPlotProgress(role, targetParam)
	case gamecfg.GardenETaskTargetType_FLOWER_LEVEL:
		return getFlowerLevelProgress(role, targetParam)
	default:
		return currentProgress
	}
}

func CalcEventProgressAdd(role *RoleMain, currentProgress int32, targetType gamecfg.GardenETaskTargetType, targetParam int32, param event.EventParam) int32 {
	if param.EType != EventTypeByTaskTarget(targetType) {
		return 0
	}
	switch targetType {
	case gamecfg.GardenETaskTargetType_BREED_START:
		return breedStartProgressAdd(param, targetParam)
	case gamecfg.GardenETaskTargetType_BREED_FINISH:
		return breedFinishProgressAdd(param, targetParam)
	case gamecfg.GardenETaskTargetType_PLANT_FLOWER:
		return plantFlowerProgressAdd(param, targetParam)
	case gamecfg.GardenETaskTargetType_WATER_FLOWER:
		return waterFlowerProgressAdd(param)
	case gamecfg.GardenETaskTargetType_HARVEST_FLOWER:
		return harvestFlowerProgressAdd(param, targetParam)
	case gamecfg.GardenETaskTargetType_GET_ITEM:
		return goodChangeProgressAdd(role, param, targetParam)
	case gamecfg.GardenETaskTargetType_PLAYER_LEVEL:
		return playerLevelProgressAdd(param, currentProgress)
	case gamecfg.GardenETaskTargetType_UNLOCK_PLOT:
		return unlockPlotProgressAdd(param, targetParam)
	case gamecfg.GardenETaskTargetType_FLOWER_LEVEL:
		return flowerLevelProgressAdd(param, currentProgress, targetParam)
	}
	return 0
}

func breedStartProgressAdd(param event.EventParam, targetParam int32) int32 {
	data, ok := param.Data.(event.BreedStartEventData)
	if !ok || !MatchTaskParam(targetParam, data.FlowerID) {
		return 0
	}
	return 1
}

func breedFinishProgressAdd(param event.EventParam, targetParam int32) int32 {
	data, ok := param.Data.(event.BreedFinishEventData)
	if !ok || !MatchTaskParam(targetParam, data.FlowerID) {
		return 0
	}
	return 1
}

func plantFlowerProgressAdd(param event.EventParam, targetParam int32) int32 {
	data, ok := param.Data.(event.PlantFlowerEventData)
	if !ok || !MatchTaskParam(targetParam, data.FlowerID) {
		return 0
	}
	return int32(len(data.PlotIDs))
}

func waterFlowerProgressAdd(param event.EventParam) int32 {
	data, ok := param.Data.(event.WaterFlowerEventData)
	if !ok {
		return 0
	}
	return int32(len(data.PlotIDs))
}

func harvestFlowerProgressAdd(param event.EventParam, targetParam int32) int32 {
	data, ok := param.Data.(event.HarvestFlowerEventData)
	if !ok {
		return 0
	}
	var count int32
	for _, item := range data.Flowers {
		if MatchTaskParam(targetParam, item.FlowerID) {
			count++
		}
	}
	return count
}

func goodChangeProgressAdd(role *RoleMain, param event.EventParam, targetParam int32) int32 {
	data, ok := param.Data.(event.GoodChangeEventData)
	if !ok {
		return 0
	}
	return getItemProgressAdd(role, role.Cfg(), targetParam, data)
}

func playerLevelProgressAdd(param event.EventParam, currentProgress int32) int32 {
	data, ok := param.Data.(event.PlayerLevelEventData)
	if !ok {
		return 0
	}
	if data.NewLevel > currentProgress {
		return data.NewLevel - currentProgress
	}
	return 0
}

func unlockPlotProgressAdd(param event.EventParam, targetParam int32) int32 {
	data, ok := param.Data.(event.UnlockPlotEventData)
	if !ok || !MatchTaskParam(targetParam, data.PlotID) {
		return 0
	}
	return 1
}

func flowerLevelProgressAdd(param event.EventParam, currentProgress int32, targetParam int32) int32 {
	data, ok := param.Data.(event.FlowerLevelEventData)
	if !ok || !MatchTaskParam(targetParam, data.FlowerID) {
		return 0
	}
	if data.NewLevel > currentProgress {
		return data.NewLevel - currentProgress
	}
	return 0
}

func MatchTaskParam(targetParam int32, value int32) bool {
	return targetParam == 0 || targetParam == value
}

func EventTypeByTaskTarget(targetType gamecfg.GardenETaskTargetType) event.EventType {
	switch targetType {
	case gamecfg.GardenETaskTargetType_BREED_START:
		return event.EVENT_BREED_START
	case gamecfg.GardenETaskTargetType_BREED_FINISH:
		return event.EVENT_BREED_FINISH
	case gamecfg.GardenETaskTargetType_PLANT_FLOWER:
		return event.EVENT_PLANT_FLOWER
	case gamecfg.GardenETaskTargetType_WATER_FLOWER:
		return event.EVENT_WATER_FLOWER
	case gamecfg.GardenETaskTargetType_HARVEST_FLOWER:
		return event.EVENT_HARVEST_FLOWER
	case gamecfg.GardenETaskTargetType_GET_ITEM, gamecfg.GardenETaskTargetType_OWN_ITEM:
		return event.EVENT_GOOD_CHANGE
	case gamecfg.GardenETaskTargetType_PLAYER_LEVEL:
		return event.EVENT_PLAYER_LEVEL
	case gamecfg.GardenETaskTargetType_UNLOCK_PLOT:
		return event.EVENT_UNLOCK_PLOT
	case gamecfg.GardenETaskTargetType_FLOWER_LEVEL:
		return event.EVENT_FLOWER_LEVEL
	default:
		return ""
	}
}

func getUnlockPlotProgress(role *RoleMain, plotID int32) int32 {
	if role == nil || role.Plot == nil {
		return 0
	}
	if plotID > 0 {
		if _, ok := role.Plot.Plots[plotID]; ok {
			return 1
		}
		return 0
	}
	return int32(len(role.Plot.Plots))
}

func getFlowerLevelProgress(role *RoleMain, flowerID int32) int32 {
	if role == nil || role.Flower == nil {
		return 0
	}
	if flowerID > 0 {
		level, _ := role.Flower.GetFlowerLevel(flowerID)
		return level
	}
	var maxLevel int32
	for _, flower := range role.Flower.Flowers {
		if flower.Level > maxLevel {
			maxLevel = flower.Level
		}
	}
	return maxLevel
}

func getBreedFinishProgress(role *RoleMain, flowerID int32) int32 {
	if role == nil || role.Flower == nil {
		return 0
	}
	now := time.Now()
	if flowerID > 0 {
		flower, ok := role.Flower.Flowers[flowerID]
		if !ok {
			return 0
		}
		if isBreedFinishedState(flower, now) {
			return 1
		}
		return 0
	}
	var count int32
	for _, flower := range role.Flower.Flowers {
		if isBreedFinishedState(flower, now) {
			count++
		}
	}
	return count
}

func isBreedFinishedState(flower *FlowerData, now time.Time) bool {
	if flower == nil {
		return false
	}
	state := getFlowerDisplayState(flower, now)
	return state == int32(pb.FlowerState_FLOWER_BREED_DONE) || state == int32(pb.FlowerState_FLOWER_HARVESTED)
}

func getItemProgressAdd(role *RoleMain, cfg *gameconfig.GameConfig, targetParam int32, data event.GoodChangeEventData) int32 {
	if role == nil {
		return 0
	}
	var total uint64
	for _, change := range data.Changes {
		if change.AddNum == 0 {
			continue
		}
		if targetParam > 0 {
			if change.GoodID == int(targetParam) {
				total += change.AddNum
			}
			continue
		}
		if isFlowerProduct(role, cfg, int32(change.GoodID)) {
			total += change.AddNum
		}
	}
	return int32(total)
}

func isFlowerProduct(role *RoleMain, cfg *gameconfig.GameConfig, goodID int32) bool {
	if role == nil || cfg == nil || cfg.TbFlower == nil {
		return false
	}
	for _, fc := range cfg.TbFlower.GetDataList() {
		if fc.HarvestItemId == goodID {
			return true
		}
	}
	return false
}
