package gameconfig

import (
	"context"
	"encoding/json"
	"gserver/core/gxymodule"
	gamecfg "gserver/gameconfig/gosrc"
	"os"
	"sort"
)

type gameConfig struct {
	gxymodule.ModuleBase
	*gamecfg.Tables
}

var gameCfg *gameConfig

func GameConfig() *gameConfig {
	return gameCfg
}

func NewGameConfig() *gameConfig {
	gameCfg = &gameConfig{}
	return gameCfg
}

func (c *gameConfig) OnModInit(ctx context.Context) error {
	return c.initTables()
}

func (gc *gameConfig) initTables() error {
	tables, err := gamecfg.NewTables(loader)
	if err != nil {
		panic(err)
	}
	gc.Tables = tables
	return nil
}

// GetFlowerLevelByGroup 按成长组和等级查找升级配置
func (gc *gameConfig) GetFlowerLevelByGroup(levelGroup int32, level int32) *gamecfg.GardenFlowerLevel {
	for _, v := range gc.TbFlowerLevel.GetDataList() {
		if v.LevelGroup == levelGroup && v.Level == level {
			return v
		}
	}
	return nil
}

// GetFlowerBreakByGroup 按成长组和突破阶段查找突破配置
func (gc *gameConfig) GetFlowerBreakByGroup(levelGroup int32, breakStage int32) *gamecfg.GardenFlowerBreak {
	for _, v := range gc.TbFlowerBreak.GetDataList() {
		if v.LevelGroup == levelGroup && v.BreakStage == breakStage {
			return v
		}
	}
	return nil
}

func (gc *gameConfig) GetPlayerLevelByTotalExp(totalExp int64) *gamecfg.GardenPlayerLevel {
	if gc == nil || gc.TbPlayerLevel == nil {
		return nil
	}
	levels := gc.sortedPlayerLevels()
	var result *gamecfg.GardenPlayerLevel
	for _, cfg := range levels {
		if int64(cfg.TotalExp) > totalExp {
			break
		}
		result = cfg
	}
	return result
}

func (gc *gameConfig) GetMaxPlayerLevel() *gamecfg.GardenPlayerLevel {
	levels := gc.sortedPlayerLevels()
	if len(levels) == 0 {
		return nil
	}
	return levels[len(levels)-1]
}

func (gc *gameConfig) GetNextPlayerLevel(level int32) *gamecfg.GardenPlayerLevel {
	levels := gc.sortedPlayerLevels()
	for _, cfg := range levels {
		if cfg.Level > level {
			return cfg
		}
	}
	return nil
}

func (gc *gameConfig) GetPlayerLevelUnlockDescs(oldLevel int32, newLevel int32) []string {
	if newLevel <= oldLevel {
		return nil
	}
	var descs []string
	for _, cfg := range gc.sortedPlayerLevels() {
		if cfg.Level > oldLevel && cfg.Level <= newLevel && cfg.UnlockDesc != "" {
			descs = append(descs, cfg.UnlockDesc)
		}
	}
	return descs
}

func (gc *gameConfig) sortedPlayerLevels() []*gamecfg.GardenPlayerLevel {
	if gc == nil || gc.TbPlayerLevel == nil {
		return nil
	}
	levels := append([]*gamecfg.GardenPlayerLevel(nil), gc.TbPlayerLevel.GetDataList()...)
	sort.Slice(levels, func(i, j int) bool {
		return levels[i].Level < levels[j].Level
	})
	return levels
}

func loader(file string) ([]map[string]interface{}, error) {
	if bytes, err := os.ReadFile("gameconfig/json/" + file + ".json"); err != nil {
		return nil, err
	} else {
		jsonData := make([]map[string]interface{}, 0)
		if err = json.Unmarshal(bytes, &jsonData); err != nil {
			return nil, err
		}
		return jsonData, nil
	}
}
