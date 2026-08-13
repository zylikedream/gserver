package gameconfig

import (
	"context"
	"encoding/json"
	"os"
	"sort"

	"gserver/core/gxymodule"
	gamecfg "gserver/gameconfig/gosrc"
)

type GameConfig struct {
	gxymodule.ModuleBase
	*gamecfg.Tables
}

var gameCfg *GameConfig

// Get 返回全局配表实例(进程内只读单例,由模块启动时 New 初始化)。
func Get() *GameConfig {
	return gameCfg
}

func NewGameConfig() *GameConfig {
	gameCfg = &GameConfig{}
	return gameCfg
}

func (c *GameConfig) OnModInit(ctx context.Context) error {
	return c.initTables()
}

func (gc *GameConfig) initTables() error {
	tables, err := gamecfg.NewTables(loader)
	if err != nil {
		panic(err)
	}
	gc.Tables = tables
	return nil
}

func (gc *GameConfig) GetFlowerLevelByGroup(levelGroup int32, level int32) *gamecfg.GardenFlowerLevel {
	for _, v := range gc.TbFlowerLevel.GetDataList() {
		if v.LevelGroup == levelGroup && v.Level == level {
			return v
		}
	}
	return nil
}

func (gc *GameConfig) GetFlowerBreakByGroup(levelGroup int32, breakStage int32) *gamecfg.GardenFlowerBreak {
	for _, v := range gc.TbFlowerBreak.GetDataList() {
		if v.LevelGroup == levelGroup && v.BreakStage == breakStage {
			return v
		}
	}
	return nil
}

func (gc *GameConfig) GetPlayerLevelByTotalExp(totalExp int64) *gamecfg.GardenPlayerLevel {
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

func (gc *GameConfig) GetMaxPlayerLevel() *gamecfg.GardenPlayerLevel {
	levels := gc.sortedPlayerLevels()
	if len(levels) == 0 {
		return nil
	}
	return levels[len(levels)-1]
}

func (gc *GameConfig) GetNextPlayerLevel(level int32) *gamecfg.GardenPlayerLevel {
	levels := gc.sortedPlayerLevels()
	for _, cfg := range levels {
		if cfg.Level > level {
			return cfg
		}
	}
	return nil
}

func (gc *GameConfig) GetPlayerLevelUnlockDescs(oldLevel int32, newLevel int32) []string {
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

func (gc *GameConfig) GetFirstMainTask() *gamecfg.GardenMainTask {
	tasks := gc.sortedMainTasks()
	for _, cfg := range tasks {
		if cfg.PreTaskId == 0 {
			return cfg
		}
	}
	return nil
}

func (gc *GameConfig) GetNextMainTask(taskID int32) *gamecfg.GardenMainTask {
	tasks := gc.sortedMainTasks()
	for _, cfg := range tasks {
		if cfg.PreTaskId == taskID {
			return cfg
		}
	}
	return nil
}

func (gc *GameConfig) sortedPlayerLevels() []*gamecfg.GardenPlayerLevel {
	if gc == nil || gc.TbPlayerLevel == nil {
		return nil
	}
	levels := append([]*gamecfg.GardenPlayerLevel(nil), gc.TbPlayerLevel.GetDataList()...)
	sort.Slice(levels, func(i, j int) bool {
		return levels[i].Level < levels[j].Level
	})
	return levels
}

func (gc *GameConfig) sortedMainTasks() []*gamecfg.GardenMainTask {
	if gc == nil || gc.TbMainTask == nil {
		return nil
	}
	tasks := append([]*gamecfg.GardenMainTask(nil), gc.TbMainTask.GetDataList()...)
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Chapter != tasks[j].Chapter {
			return tasks[i].Chapter < tasks[j].Chapter
		}
		if tasks[i].Sort != tasks[j].Sort {
			return tasks[i].Sort < tasks[j].Sort
		}
		return tasks[i].Id < tasks[j].Id
	})
	return tasks
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
