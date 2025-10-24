package gameconfig

import (
	"context"
	"encoding/json"
	"gserver/core/gxymodule"
	"os"
)

type gameConfig struct {
	gxymodule.ModuleBase
	*Tables
}

var gameCfg *gameConfig

func OnModInit(ctx context.Context) {
	if err := gameCfg.OnInit(ctx); err != nil {
		panic(err)
	}
}

func GameConfig() *gameConfig {
	return gameCfg
}

func NewGameConfig() *gameConfig {
	gameCfg = &gameConfig{}
	return gameCfg
}

func (c *gameConfig) OnInit(ctx context.Context) error {
	if err := c.initTables(); err != nil {
		return err
	}
	return nil
}

func (gc *gameConfig) initTables() error {
	tables, err := NewTables(loader)
	if err != nil {
		return err
	}
	gc.Tables = tables
	return nil
}

func loader(file string) ([]map[string]interface{}, error) {
	if bytes, err := os.ReadFile("gameconfig/data/" + file + ".json"); err != nil {
		return nil, err
	} else {
		jsonData := make([]map[string]interface{}, 0)
		if err = json.Unmarshal(bytes, &jsonData); err != nil {
			return nil, err
		}
		return jsonData, nil
	}
}
