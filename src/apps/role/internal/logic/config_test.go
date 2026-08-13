package logic

import (
	"encoding/json"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/src/pkg/gameconfig"
	"os"
	"path/filepath"
	"testing"
)

var repoRoot string

func init() {
	// 从当前目录向上查找 go.mod 来确定仓库根目录
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			repoRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			repoRoot = "."
			break
		}
		dir = parent
	}
}

// loadTestTable 从 gameconfig/json/ 加载指定配表，支持追加额外行。
// 这样配表结构变更（如新增必填字段）后，测试自动跟随，无需手写。
func loadTestTable(t *testing.T, name string, extras ...map[string]any) []map[string]any {
	t.Helper()
	path := filepath.Join(repoRoot, "gameconfig/json/"+name+".json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load table %s: %v", name, err)
	}
	var data []map[string]any
	if err := json.Unmarshal(bytes, &data); err != nil {
		t.Fatalf("unmarshal table %s: %v", name, err)
	}
	data = append(data, extras...)
	return data
}

// testCfgInited 标记全局测试配表已完整装载。
// 统一由 initAllTestConfig 装载全部配表,避免各 init 装载子集
// 在测试乱序(-shuffle)时被 guard 跳过导致 nil 表崩溃。
var testCfgInited bool

// initAllTestConfig 装载全部测试所需配表(原子:要么全装要么不装)。
// 所有 init*TestConfig 均应调用它,保证全局配表始终完整。
func initAllTestConfig(t *testing.T) {
	t.Helper()
	if testCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()

	tables := &gamecfg.Tables{}

	// 表名 → 构造器映射(全部 13 张测试配表)
	type tbDef struct {
		file string
		new  func(rows []map[string]any) (any, error)
		set  func(tables *gamecfg.Tables, v any)
	}
	defs := []tbDef{
		{"garden_tbitem", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbItem(rows) }, func(t *gamecfg.Tables, v any) { t.TbItem = v.(*gamecfg.GardenTbItem) }},
		{"garden_tbflower", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbFlower(rows) }, func(t *gamecfg.Tables, v any) { t.TbFlower = v.(*gamecfg.GardenTbFlower) }},
		{"garden_tbflowerlevel", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbFlowerLevel(rows) }, func(t *gamecfg.Tables, v any) { t.TbFlowerLevel = v.(*gamecfg.GardenTbFlowerLevel) }},
		{"garden_tbflowerbreak", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbFlowerBreak(rows) }, func(t *gamecfg.Tables, v any) { t.TbFlowerBreak = v.(*gamecfg.GardenTbFlowerBreak) }},
		{"garden_tbgardenplot", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbGardenPlot(rows) }, func(t *gamecfg.Tables, v any) { t.TbGardenPlot = v.(*gamecfg.GardenTbGardenPlot) }},
		{"garden_tbmailconfig", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbMailConfig(rows) }, func(t *gamecfg.Tables, v any) { t.TbMailConfig = v.(*gamecfg.GardenTbMailConfig) }},
		{"garden_tbplayerlevel", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbPlayerLevel(rows) }, func(t *gamecfg.Tables, v any) { t.TbPlayerLevel = v.(*gamecfg.GardenTbPlayerLevel) }},
		{"garden_tbmaintask", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbMainTask(rows) }, func(t *gamecfg.Tables, v any) { t.TbMainTask = v.(*gamecfg.GardenTbMainTask) }},
		{"garden_tbresident", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbResident(rows) }, func(t *gamecfg.Tables, v any) { t.TbResident = v.(*gamecfg.GardenTbResident) }},
		{"garden_tbresidentorder", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbResidentOrder(rows) }, func(t *gamecfg.Tables, v any) { t.TbResidentOrder = v.(*gamecfg.GardenTbResidentOrder) }},
		{"garden_tbresidentorderprogressreward", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbResidentOrderProgressReward(rows) }, func(t *gamecfg.Tables, v any) {
			t.TbResidentOrderProgressReward = v.(*gamecfg.GardenTbResidentOrderProgressReward)
		}},
		{"garden_tbresidentorderslot", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbResidentOrderSlot(rows) }, func(t *gamecfg.Tables, v any) { t.TbResidentOrderSlot = v.(*gamecfg.GardenTbResidentOrderSlot) }},
		{"garden_tbfriendconfig", func(rows []map[string]any) (any, error) { return gamecfg.NewGardenTbFriendConfig(rows) }, func(t *gamecfg.Tables, v any) { t.TbFriendConfig = v.(*gamecfg.GardenTbFriendConfig) }},
	}
	for _, d := range defs {
		rows := loadTestTable(t, d.file)
		tb, err := d.new(rows)
		if err != nil {
			t.Fatalf("init table %s: %v", d.file, err)
		}
		d.set(tables, tb)
	}

	gc.Tables = tables
	testCfgInited = true
}
