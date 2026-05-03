package gameconfig

import (
	"context"
	"encoding/json"
	"gserver/core/gxymodule"
	gamecfg "gserver/gameconfig/gosrc"
	"os"
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
