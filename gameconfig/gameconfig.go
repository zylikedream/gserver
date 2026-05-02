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
