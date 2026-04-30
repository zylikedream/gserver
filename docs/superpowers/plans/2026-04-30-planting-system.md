# 种植系统 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 RolePlot 模块，管理 72 个地块的解锁、种植、浇水、收获、移除功能。

**Architecture:** 新建 RolePlot 模块平级于 RoleFlower，PlotMap jsonb 单行存储。Proto 追加在 flower.proto 中（24001-24012）。gameconfig 需新增 TbGardenPlot 表（72 条数据）和 TbFlower 种植字段。被动检查模式同培育系统。

**Tech Stack:** Go 1.25, protobuf, GORM (jsonb), gomonkey (测试 mock)

---

## File Structure

| 文件 | 操作 | 说明 |
|------|------|------|
| `gameconfig/gosrc/garden.GardenPlot.go` | 新建 | GardenPlot 结构体 + NewGardenPlot |
| `gameconfig/gosrc/garden.TbGardenPlot.go` | 新建 | TbGardenPlot 表 + Get(key) |
| `gameconfig/gosrc/Tables.go` | 修改 | 加 TbGardenPlot 字段和加载 |
| `gameconfig/gosrc/garden.Flower.go` | 修改 | 加 grow_time 等种植字段 |
| `gameconfig/json/garden_tbgardenplot.json` | 新建 | 地块配置 JSON（72 条） |
| `protocol/client/flower.proto` | 修改 | 追加 PlotState 枚举和 12 个消息 |
| `protocol/pb/flower.pb.go` | 重新生成 | `make pb` + `make pbids` |
| `src/apps/role/internal/logic/role_plot.go` | 新建 | RolePlot 模块主逻辑 |
| `src/apps/role/internal/logic/role_plot_test.go` | 新建 | 单元测试 |
| `src/apps/role/internal/logic/role_main.go` | 修改 | roleModules 加 Plot |
| `src/apps/role/internal/logic/role_schema.go` | 修改 | AutoMigrate 加 RolePlotState |
| `src/apps/role/internal/logic/role_gm.go` | 修改 | 加 unlock_plot GM 命令 |
| `docs/system/plot.md` | 新建 | 系统文档 |

---

### Task 1: Proto 定义 + 生成

**Files:**
- Modify: `protocol/client/flower.proto`

- [ ] **Step 1: 追加 Proto 定义**

在 `flower.proto` 末尾追加 PlotState 枚举和 12 个消息（24001-24012）：

```protobuf
enum PlotState {
    PLOT_EMPTY       = 0;
    PLOT_PLANTED     = 1;
    PLOT_GROWING     = 2;
    PLOT_HARVESTABLE = 3;
}

message PPlotInfo {
    int32 plot_id       = 1;
    int32 flower_id     = 2;
    PlotState state     = 3;
    int32 harvest_count = 4;
    int64 state_time    = 5;
}

message ReqPlotInfo {
    option (msg_id) = 24001;
}

message RspPlotInfo {
    option (msg_id) = 24002;
    repeated PPlotInfo plots = 1;
}

message ReqUnlockPlot {
    option (msg_id) = 24003;
    int32 plot_id = 1;
}

message RspUnlockPlot {
    option (msg_id) = 24004;
    PPlotInfo plot = 1;
}

message ReqPlantFlower {
    option (msg_id) = 24005;
    repeated int32 plot_ids = 1;
    int32 flower_id = 2;
}

message RspPlantFlower {
    option (msg_id) = 24006;
    repeated PPlotInfo plots = 1;
}

message ReqWaterFlower {
    option (msg_id) = 24007;
    repeated int32 plot_ids = 1;
}

message RspWaterFlower {
    option (msg_id) = 24008;
    repeated PPlotInfo plots = 1;
}

message ReqHarvestFlower {
    option (msg_id) = 24009;
    repeated int32 plot_ids = 1;
}

message RspHarvestFlower {
    option (msg_id) = 24010;
    repeated PPlotInfo plots = 1;
}

message ReqRemovePlant {
    option (msg_id) = 24011;
    repeated int32 plot_ids = 1;
}

message RspRemovePlant {
    option (msg_id) = 24012;
    repeated PPlotInfo plots = 1;
}
```

- [ ] **Step 2: 生成 protobuf**

Run: `make pb && make pbids`
Expected: 输出包含 `flower.proto 24012`，无错误。

- [ ] **Step 3: 提交**

```bash
git add protocol/client/flower.proto protocol/pb/
git commit -m "feat(plot): 添加种植系统 proto 定义 24001-24012"
```

---

### Task 2: gameconfig TbGardenPlot + TbFlower 种植字段

**Files:**
- Create: `gameconfig/gosrc/garden.GardenPlot.go`
- Create: `gameconfig/gosrc/garden.TbGardenPlot.go`
- Create: `gameconfig/json/garden_tbgardenplot.json`
- Modify: `gameconfig/gosrc/garden.Flower.go`
- Modify: `gameconfig/gosrc/Tables.go`

- [ ] **Step 1: 创建 GardenPlot 结构体**

创建 `gameconfig/gosrc/garden.GardenPlot.go`：

```go
package gamecfg;

import "errors"

type GardenPlot struct {
    Id           int32
    UnlockLevel  int32
    Cost         []*GardenGoodStack
}

const TypeId_GardenPlot = -1234567890

func (*GardenPlot) GetTypeId() int32 {
    return -1234567890
}

func NewGardenPlot(_buf map[string]interface{}) (_v *GardenPlot, err error) {
    _v = &GardenPlot{}
    { var _ok_ bool; var __json_id__ interface{}; if __json_id__, _ok_ = _buf["id"]; !_ok_ || __json_id__ == nil { err = errors.New("id error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_id__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.Id = __x__ }}
    { var _ok_ bool; var __json_unlock_level__ interface{}; if __json_unlock_level__, _ok_ = _buf["unlock_level"]; !_ok_ || __json_unlock_level__ == nil { err = errors.New("unlock_level error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_unlock_level__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.UnlockLevel = __x__ }}
    { var _ok_ bool; var __json_cost__ interface{}; if __json_cost__, _ok_ = _buf["cost"]; !_ok_ || __json_cost__ == nil { _v.Cost = nil } else { var __x__ []*GardenGoodStack;  {
                    var _arr0_ []interface{}
                    var _ok0_ bool
                    if _arr0_, _ok0_ = (__json_cost__).([]interface{}); !_ok0_ { err = errors.New("__x__ error"); return }

                    __x__ = make([]*GardenGoodStack, 0, len(_arr0_))

                    for _, _e0_ := range _arr0_ {
                        var _list_v0_ *GardenGoodStack
                        { var _ok_ bool; var _x_ map[string]interface{}; if _x_, _ok_ = _e0_.(map[string]interface{}); !_ok_ { err = errors.New("_list_v0_ error"); return }; if _list_v0_, err = NewGardenGoodStack(_x_); err != nil { return } }
                        __x__ = append(__x__, _list_v0_)
                    }
                }
    ; _v.Cost = __x__ }}
    return
}
```

- [ ] **Step 2: 创建 TbGardenPlot 表**

创建 `gameconfig/gosrc/garden.TbGardenPlot.go`：

```go
package gamecfg;

type GardenTbGardenPlot struct {
    _dataMap map[int32]*GardenPlot
    _dataList []*GardenPlot
}

func NewGardenTbGardenPlot(_buf []map[string]interface{}) (*GardenTbGardenPlot, error) {
    _dataList := make([]*GardenPlot, 0, len(_buf))
    dataMap := make(map[int32]*GardenPlot)

    for _, _ele_ := range _buf {
        if _v, err2 := NewGardenPlot(_ele_); err2 != nil {
            return nil, err2
        } else {
            _dataList = append(_dataList, _v)
            dataMap[_v.Id] = _v
        }
    }
    return &GardenTbGardenPlot{_dataList:_dataList, _dataMap:dataMap}, nil
}

func (table *GardenTbGardenPlot) GetDataMap() map[int32]*GardenPlot {
    return table._dataMap
}

func (table *GardenTbGardenPlot) GetDataList() []*GardenPlot {
    return table._dataList
}

func (table *GardenTbGardenPlot) Get(key int32) *GardenPlot {
    return table._dataMap[key]
}
```

- [ ] **Step 3: 创建配置 JSON**

创建 `gameconfig/json/garden_tbgardenplot.json`，72 条数据。地块 1-12 解锁等级 0 免费无消耗，13-24 等级 5 花费 5000 金币，25-36 等级 10 花费 20000 金币，37-48 等级 20 花费 100000 金币，49-60 等级 30 花费 50 钻石，61-72 等级 40 花费 200 钻石。金币物品 ID 假设为 9001，钻石物品 ID 假设为 9002（需确认实际 ID）：

```json
[
  {"id": 1, "unlock_level": 0, "cost": []},
  {"id": 2, "unlock_level": 0, "cost": []},
  ...
  {"id": 12, "unlock_level": 0, "cost": []},
  {"id": 13, "unlock_level": 5, "cost": [{"id": 9001, "num": 5000}]},
  ...
  {"id": 24, "unlock_level": 5, "cost": [{"id": 9001, "num": 5000}]},
  {"id": 25, "unlock_level": 10, "cost": [{"id": 9001, "num": 20000}]},
  ...
  {"id": 36, "unlock_level": 10, "cost": [{"id": 9001, "num": 20000}]},
  {"id": 37, "unlock_level": 20, "cost": [{"id": 9001, "num": 100000}]},
  ...
  {"id": 48, "unlock_level": 20, "cost": [{"id": 9001, "num": 100000}]},
  {"id": 49, "unlock_level": 30, "cost": [{"id": 9002, "num": 50}]},
  ...
  {"id": 60, "unlock_level": 30, "cost": [{"id": 9002, "num": 50}]},
  {"id": 61, "unlock_level": 40, "cost": [{"id": 9002, "num": 200}]},
  ...
  {"id": 72, "unlock_level": 40, "cost": [{"id": 9002, "num": 200}]}
]
```

实际实现时需要生成完整 72 条 JSON（每组 12 条相同配置）。

- [ ] **Step 4: 修改 Tables.go 注册 TbGardenPlot**

在 `gameconfig/gosrc/Tables.go` 的 `Tables` 结构体加字段，`NewTables` 加加载逻辑：

```go
// Tables 结构体加：
TbGardenPlot *GardenTbGardenPlot

// NewTables 函数末尾 return 前加：
if buf, err = loader("garden_tbgardenplot") ; err != nil {
    return nil, err
}
if tables.TbGardenPlot, err = NewGardenTbGardenPlot(buf) ; err != nil {
    return nil, err
}
```

- [ ] **Step 5: 给 GardenFlower 加种植字段**

在 `gameconfig/gosrc/garden.Flower.go` 的 `GardenFlower` 结构体追加字段，在 `NewGardenFlower` 函数追加解析：

```go
// 结构体加：
GrowTime        int32
HarvestInterval int32
HarvestTimes    int32
HarvestItemId   int32
HarvestNum      int32
WaterCost       int32
```

```go
// NewGardenFlower 追加解析（每个字段格式同 BreedTime）：
{ var _ok_ bool; var __json_grow_time__ interface{}; if __json_grow_time__, _ok_ = _buf["grow_time"]; !_ok_ || __json_grow_time__ == nil { err = errors.New("grow_time error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_grow_time__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.GrowTime = __x__ }}
{ var _ok_ bool; var __json_harvest_interval__ interface{}; if __json_harvest_interval__, _ok_ = _buf["harvest_interval"]; !_ok_ || __json_harvest_interval__ == nil { err = errors.New("harvest_interval error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_harvest_interval__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.HarvestInterval = __x__ }}
{ var _ok_ bool; var __json_harvest_times__ interface{}; if __json_harvest_times__, _ok_ = _buf["harvest_times"]; !_ok_ || __json_harvest_times__ == nil { err = errors.New("harvest_times error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_harvest_times__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.HarvestTimes = __x__ }}
{ var _ok_ bool; var __json_harvest_item_id__ interface{}; if __json_harvest_item_id__, _ok_ = _buf["harvest_item_id"]; !_ok_ || __json_harvest_item_id__ == nil { err = errors.New("harvest_item_id error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_harvest_item_id__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.HarvestItemId = __x__ }}
{ var _ok_ bool; var __json_harvest_num__ interface{}; if __json_harvest_num__, _ok_ = _buf["harvest_num"]; !_ok_ || __json_harvest_num__ == nil { err = errors.New("harvest_num error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_harvest_num__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.HarvestNum = __x__ }}
{ var _ok_ bool; var __json_water_cost__ interface{}; if __json_water_cost__, _ok_ = _buf["water_cost"]; !_ok_ || __json_water_cost__ == nil { err = errors.New("water_cost error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_water_cost__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.WaterCost = __x__ }}
```

- [ ] **Step 6: 编译验证**

Run: `go build ./... 2>&1`
Expected: 无错误。

- [ ] **Step 7: 提交**

```bash
git add gameconfig/
git commit -m "feat(plot): 新增 TbGardenPlot 配置表和 TbFlower 种植字段"
```

---

### Task 3: RolePlot 模块骨架 + 核心逻辑

**Files:**
- Create: `src/apps/role/internal/logic/role_plot.go`

- [ ] **Step 1: 创建 role_plot.go**

```go
package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gserver/gameconfig"
	"gserver/protocol/pb"

	"github.com/pkg/errors"
)

var (
	ErrPlotLocked      = errors.New("plot not unlocked")
	ErrPlotNotEmpty    = errors.New("plot is not empty")
	ErrPlotNotPlanted  = errors.New("plot is not planted")
	ErrPlotNotGrowing  = errors.New("plot is not growing")
	ErrPlotNotReady    = errors.New("plot not ready for harvest")
	ErrPlotHarvestable = errors.New("plot is harvestable, harvest first")
)

// ========== 数据模型 ==========

type PlotData struct {
	PlotID       int32     `json:"plot_id"`
	FlowerID     int32     `json:"flower_id"`
	State        int32     `json:"state"`
	HarvestCount int32     `json:"harvest_count"`
	StateTime    time.Time `json:"state_time"`
}

type PlotMap map[int32]*PlotData

func (m PlotMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *PlotMap) Scan(value interface{}) error {
	if value == nil {
		*m = make(PlotMap)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("invalid type for PlotMap")
	}
	var plotMap map[int32]*PlotData
	if err := json.Unmarshal(bytes, &plotMap); err != nil {
		return err
	}
	*m = PlotMap(plotMap)
	return nil
}

type RolePlotState struct {
	RolePersistState
	Plots PlotMap `gorm:"column:plots;type:jsonb"`
}

func (RolePlotState) TableName() string { return "role_plot" }

// ========== 模块 ==========

type RolePlot struct {
	RoleModule
	RolePlotState
}

var _ IRoleModule = (*RolePlot)(nil)

func (r *RolePlot) PersistState() IPersistState {
	return &r.RolePlotState
}

func (r *RolePlot) OnModInit(ctx context.Context) error {
	if r.Plots == nil {
		r.Plots = make(PlotMap)
	}
	return nil
}

// ========== 公开方法 ==========

func (r *RolePlot) UnlockPlot(plotID int32) {
	r.Plots[plotID] = &PlotData{
		PlotID: plotID,
	}
	r.MarkDirty()
}

func (r *RolePlot) getPlotState(plot *PlotData) int32 {
	state := plot.State
	if state == int32(pb.PlotState_PLOT_GROWING) && time.Now().After(plot.StateTime) {
		state = int32(pb.PlotState_PLOT_HARVESTABLE)
	}
	return state
}

func (r *RolePlot) pPlotInfo(plot *PlotData) *pb.PPlotInfo {
	return &pb.PPlotInfo{
		PlotId:       plot.PlotID,
		FlowerId:     plot.FlowerID,
		State:        pb.PlotState(r.getPlotState(plot)),
		HarvestCount: plot.HarvestCount,
		StateTime:    plot.StateTime.Unix(),
	}
}

// ========== Proto Handler ==========

func (r *RolePlot) ReqPlotInfo(ctx context.Context, req *pb.ReqPlotInfo) (*pb.RspPlotInfo, error) {
	rsp := &pb.RspPlotInfo{Plots: []*pb.PPlotInfo{}}
	for _, p := range r.Plots {
		rsp.Plots = append(rsp.Plots, r.pPlotInfo(p))
	}
	return rsp, nil
}

func (r *RolePlot) ReqUnlockPlot(ctx context.Context, req *pb.ReqUnlockPlot) (*pb.RspUnlockPlot, error) {
	plotID := req.PlotId

	cfg := gameconfig.GameConfig().TbGardenPlot.Get(plotID)
	if cfg == nil {
		return nil, errors.Errorf("plot config not found: %d", plotID)
	}

	if _, ok := r.Plots[plotID]; ok {
		return nil, ErrPlotLocked
	}

	// TODO: 检查玩家等级（需 Role.Basic 接口）
	if cfg.Cost != nil && len(cfg.Cost) > 0 {
		if !r.Role.Bag.CheckGoods(cfg.Cost) {
			return nil, ErrGoodNotEnough
		}
		if err := r.Role.Bag.SaveGoods(ctx, cfg.Cost, nil, "unlock_plot"); err != nil {
			return nil, err
		}
	}

	r.UnlockPlot(plotID)

	return &pb.RspUnlockPlot{Plot: r.pPlotInfo(r.Plots[plotID])}, nil
}

func (r *RolePlot) ReqPlantFlower(ctx context.Context, req *pb.ReqPlantFlower) (*pb.RspPlantFlower, error) {
	flowerID := req.FlowerId

	if _, ok := r.Role.Flower.Flowers[flowerID]; !ok {
		return nil, ErrFlowerLocked
	}

	updated := make([]*pb.PPlotInfo, 0, len(req.PlotIds))
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		if plot.State != int32(pb.PlotState_PLOT_EMPTY) {
			return nil, ErrPlotNotEmpty
		}

		plot.FlowerID = flowerID
		plot.State = int32(pb.PlotState_PLOT_PLANTED)
		updated = append(updated, r.pPlotInfo(plot))
	}
	r.MarkDirty()

	return &pb.RspPlantFlower{Plots: updated}, nil
}

func (r *RolePlot) ReqWaterFlower(ctx context.Context, req *pb.ReqWaterFlower) (*pb.RspWaterFlower, error) {
	// 先校验所有地块状态，再统一扣水滴
	waterCosts := make(map[int32]int32) // flowerID -> count
	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		if plot.State != int32(pb.PlotState_PLOT_PLANTED) {
			return nil, ErrPlotNotPlanted
		}
		waterCosts[plot.FlowerID]++
	}

	// 汇总水滴消耗
	var totalCost []*gamecfg.GardenGoodStack
	// 水滴物品 ID 固定为 WATER_DROP，暂用常量；后续可配置
	waterDropID := int32(3001)
	totalWater := int32(0)
	for fid, cnt := range waterCosts {
		cfg := gameconfig.GameConfig().TbFlower.Get(fid)
		if cfg == nil {
			return nil, errors.Errorf("flower config not found: %d", fid)
		}
		totalWater += cfg.WaterCost * cnt
	}
	if totalWater > 0 {
		cost := []*gamecfg.GardenGoodStack{{Id: waterDropID, Num: totalWater}}
		if !r.Role.Bag.CheckGoods(cost) {
			return nil, ErrGoodNotEnough
		}
		if err := r.Role.Bag.SaveGoods(ctx, cost, nil, "water"); err != nil {
			return nil, err
		}
	}

	updated := make([]*pb.PPlotInfo, 0, len(req.PlotIds))
	for _, plotID := range req.PlotIds {
		plot := r.Plots[plotID]
		cfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
		plot.State = int32(pb.PlotState_PLOT_GROWING)
		plot.StateTime = time.Now().Add(time.Duration(cfg.GrowTime) * time.Second)
		updated = append(updated, r.pPlotInfo(plot))
	}
	r.MarkDirty()

	return &pb.RspWaterFlower{Plots: updated}, nil
}

func (r *RolePlot) ReqHarvestFlower(ctx context.Context, req *pb.ReqHarvestFlower) (*pb.RspHarvestFlower, error) {
	var totalAdd []*gamecfg.GardenGoodStack
	updated := make([]*pb.PPlotInfo, 0, len(req.PlotIds))

	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		if !(plot.State == int32(pb.PlotState_PLOT_GROWING) && time.Now().After(plot.StateTime)) {
			return nil, ErrPlotNotReady
		}

		cfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
		if cfg == nil {
			return nil, errors.Errorf("flower config not found: %d", plot.FlowerID)
		}

		plot.HarvestCount++

		if plot.HarvestCount < cfg.HarvestTimes {
			plot.State = int32(pb.PlotState_PLOT_GROWING)
			plot.StateTime = time.Now().Add(time.Duration(cfg.HarvestInterval) * time.Second)
		} else {
			// 达到收获上限，回到空地块
			plot.State = int32(pb.PlotState_PLOT_EMPTY)
			plot.FlowerID = 0
			plot.HarvestCount = 0
			plot.StateTime = time.Time{}
		}

		totalAdd = append(totalAdd, &gamecfg.GardenGoodStack{Id: cfg.HarvestItemId, Num: cfg.HarvestNum})
		updated = append(updated, r.pPlotInfo(plot))
	}

	if len(totalAdd) > 0 {
		if err := r.Role.Bag.SaveGoods(ctx, nil, totalAdd, "harvest"); err != nil {
			return nil, err
		}
	}
	r.MarkDirty()

	return &pb.RspHarvestFlower{Plots: updated}, nil
}

func (r *RolePlot) ReqRemovePlant(ctx context.Context, req *pb.ReqRemovePlant) (*pb.RspRemovePlant, error) {
	updated := make([]*pb.PPlotInfo, 0, len(req.PlotIds))

	for _, plotID := range req.PlotIds {
		plot, ok := r.Plots[plotID]
		if !ok {
			return nil, ErrPlotLocked
		}
		// 可收获状态不能移除，需先收获
		if plot.State == int32(pb.PlotState_PLOT_GROWING) && time.Now().After(plot.StateTime) {
			return nil, ErrPlotHarvestable
		}
		if plot.State != int32(pb.PlotState_PLOT_PLANTED) && plot.State != int32(pb.PlotState_PLOT_GROWING) {
			return nil, ErrPlotNotPlanted
		}

		plot.State = int32(pb.PlotState_PLOT_EMPTY)
		plot.FlowerID = 0
		plot.HarvestCount = 0
		plot.StateTime = time.Time{}
		updated = append(updated, r.pPlotInfo(plot))
	}
	r.MarkDirty()

	return &pb.RspRemovePlant{Plots: updated}, nil
}
```

- [ ] **Step 2: 编译验证**

Run: `go build ./src/apps/role/... 2>&1`

注意：此时还未注册到 roleModules，编译应通过（只要 import 正确）。

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_plot.go
git commit -m "feat(plot): RolePlot 模块骨架和核心逻辑"
```

---

### Task 4: 注册 RolePlot 到 roleModules + schema

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go`
- Modify: `src/apps/role/internal/logic/role_schema.go`

- [ ] **Step 1: roleModules 加 Plot 字段**

在 `role_main.go` 的 `roleModules` 结构体加 `Plot *RolePlot`：

```go
type roleModules struct {
	Bag    *RoleBag
	Basic  *RoleBasic
	Public *RolePublic
	Extra  *RoleExtra
	Flower *RoleFlower
	Plot   *RolePlot
	GM     *RoleGM
}
```

- [ ] **Step 2: schema 加 RolePlotState**

在 `role_schema.go` 的 AutoMigrate 加 `&RolePlotState{}`：

```go
if err := db.AutoMigrate(
	&RoleAccount{},
	&RoleBasicState{},
	&RoleBagState{},
	&RoleExtraPersistState{},
	&RolePublicState{},
	&RoleFlowerState{},
	&RolePlotState{},
); err != nil {
```

- [ ] **Step 3: 编译验证**

Run: `go build ./src/apps/role/... 2>&1`
Expected: 无错误。

- [ ] **Step 4: 提交**

```bash
git add src/apps/role/internal/logic/role_main.go src/apps/role/internal/logic/role_schema.go
git commit -m "feat(plot): 注册 RolePlot 到 roleModules 和 schema"
```

---

### Task 5: GM 命令

**Files:**
- Modify: `src/apps/role/internal/logic/role_gm.go`

- [ ] **Step 1: 添加 unlock_plot GM 命令**

在 `role_gm.go` 末尾（FinishBreedGM 之后）追加：

```go
// UnlockPlot 解锁地块
// 用法: unlock_plot [地块ID]
// 示例: unlock_plot 1
func (r *RoleGM) UnlockPlot(plotID int) error {
	r.Role.Plot.UnlockPlot(int32(plotID))
	return nil
}
```

- [ ] **Step 2: 编译验证**

Run: `go build ./src/apps/role/... 2>&1`

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_gm.go
git commit -m "feat(plot): 添加 unlock_plot GM 命令"
```

---

### Task 6: 单元测试

**Files:**
- Create: `src/apps/role/internal/logic/role_plot_test.go`

- [ ] **Step 1: 编写测试 setup 和 PlotMap 序列化测试**

```go
package logic

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"

	"github.com/agiledragon/gomonkey/v2"
	proto "google.golang.org/protobuf/proto"
)

// ========== test setup ==========

var plotCfgInited bool

func initPlotTestConfig(t *testing.T) {
	t.Helper()
	if plotCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()

	// TbFlower: rose=101, grow_time=60, harvest_interval=30, harvest_times=3, harvest_item_id=10001, harvest_num=2, water_cost=5
	flowers := []map[string]interface{}{
		{
			"id": float64(101), "name": "rose", "quality": float64(1),
			"breed_time": float64(10), "breed_cost": []interface{}{},
			"grow_time": float64(60), "harvest_interval": float64(30),
			"harvest_times": float64(3), "harvest_item_id": float64(10001),
			"harvest_num": float64(2), "water_cost": float64(5),
		},
		{
			"id": float64(102), "name": "sunflower", "quality": float64(1),
			"breed_time": float64(20), "breed_cost": []interface{}{},
			"grow_time": float64(120), "harvest_interval": float64(60),
			"harvest_times": float64(2), "harvest_item_id": float64(10002),
			"harvest_num": float64(1), "water_cost": float64(3),
		},
	}
	tbFlower, err := gamecfg.NewGardenTbFlower(flowers)
	if err != nil {
		t.Fatal(err)
	}

	// TbGardenPlot: 1-12 free, 13+ locked for testing
	plots := make([]map[string]interface{}, 12)
	for i := 0; i < 12; i++ {
		plots[i] = map[string]interface{}{
			"id": float64(i + 1), "unlock_level": float64(0), "cost": []interface{}{},
		}
	}
	tbGardenPlot, err := gamecfg.NewGardenTbGardenPlot(plots)
	if err != nil {
		t.Fatal(err)
	}

	// TbItem: water_drop=3001, harvest products=10001,10002
	items := []map[string]interface{}{
		{"id": float64(3001), "name": "water_drop", "desc": "", "major_type": float64(2),
			"sub_type": float64(12), "quality": float64(1), "price": float64(5),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(10001), "name": "rose_petal", "desc": "", "major_type": float64(2),
			"sub_type": float64(80), "quality": float64(1), "price": float64(10),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(10002), "name": "sunflower_petal", "desc": "", "major_type": float64(2),
			"sub_type": float64(80), "quality": float64(1), "price": float64(10),
			"max_stack": float64(999), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
	}
	tbItem, err := gamecfg.NewGardenTbItem(items)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{TbItem: tbItem, TbFlower: tbFlower, TbGardenPlot: tbGardenPlot}
	plotCfgInited = true
}

func setupTestPlot(t *testing.T) *RolePlot {
	t.Helper()
	initPlotTestConfig(t)

	patch := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, _ proto.Message) {},
	)
	t.Cleanup(patch.Reset)

	main := &RoleMain{}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	flowerMod := &RoleFlower{
		RoleModule:      RoleModule{Role: main},
		RoleFlowerState: RoleFlowerState{Flowers: make(FlowerMap)},
	}
	plotMod := &RolePlot{
		RoleModule:    RoleModule{Role: main},
		RolePlotState: RolePlotState{Plots: make(PlotMap)},
	}
	main.Bag = bagMod
	main.Flower = flowerMod
	main.Plot = plotMod
	return plotMod
}

func setupTestPlotWithMaterials(t *testing.T) *RolePlot {
	t.Helper()
	p := setupTestPlot(t)
	// 水滴
	p.Role.Bag.Goods[3001] = BagGood{GoodID: 3001, Num: 100}
	// 解锁花
	p.Role.Flower.AddFlower(101)
	p.Role.Flower.AddFlower(102)
	return p
}

// ========== PlotMap Scan/Value ==========

func TestPlotMap_ScanNil(t *testing.T) {
	var m PlotMap
	if err := m.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestPlotMap_ValueAndScan(t *testing.T) {
	original := PlotMap{
		1: {PlotID: 1, FlowerID: 101, State: 1, HarvestCount: 0, StateTime: time.Unix(1700000000, 0)},
		2: {PlotID: 2, State: 0},
	}

	val, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}

	var restored PlotMap
	if err := restored.Scan(val); err != nil {
		t.Fatal(err)
	}

	if len(restored) != 2 {
		t.Fatalf("expected 2, got %d", len(restored))
	}
	if restored[1].FlowerID != 101 || restored[1].State != 1 {
		t.Fatalf("unexpected restored[1]: %v", restored[1])
	}
	if restored[2].State != 0 {
		t.Fatalf("unexpected restored[2]: %v", restored[2])
	}
}

// ========== UnlockPlot ==========

func TestUnlockPlot_Success(t *testing.T) {
	p := setupTestPlot(t)

	p.UnlockPlot(1)

	plot, ok := p.Plots[1]
	if !ok {
		t.Fatal("expected plot 1 in map")
	}
	if plot.State != int32(pb.PlotState_PLOT_EMPTY) {
		t.Fatalf("expected EMPTY, got %d", plot.State)
	}
	if !p.IsDirty() {
		t.Fatal("expected dirty")
	}
}

// ========== PlantFlower ==========

func TestPlantFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)

	rsp, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 101,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Plots) != 1 {
		t.Fatalf("expected 1 plot, got %d", len(rsp.Plots))
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_PLANTED {
		t.Fatalf("expected PLANTED, got %v", rsp.Plots[0].State)
	}
	if p.Plots[1].FlowerID != 101 {
		t.Fatalf("expected flower 101, got %d", p.Plots[1].FlowerID)
	}
}

func TestPlantFlower_NotUnlocked(t *testing.T) {
	p := setupTestPlotWithMaterials(t)

	_, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 101,
	})
	if !errors.Is(err, ErrPlotLocked) {
		t.Fatalf("expected ErrPlotLocked, got %v", err)
	}
}

func TestPlantFlower_FlowerNotBred(t *testing.T) {
	p := setupTestPlot(t)
	p.UnlockPlot(1)

	_, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 999,
	})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

func TestPlantFlower_NotEmpty(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].State = int32(pb.PlotState_PLOT_PLANTED)

	_, err := p.ReqPlantFlower(context.Background(), &pb.ReqPlantFlower{
		PlotIds:  []int32{1},
		FlowerId: 101,
	})
	if !errors.Is(err, ErrPlotNotEmpty) {
		t.Fatalf("expected ErrPlotNotEmpty, got %v", err)
	}
}

// ========== WaterFlower ==========

func TestWaterFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_PLANTED)

	rsp, err := p.ReqWaterFlower(context.Background(), &pb.ReqWaterFlower{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_GROWING {
		t.Fatalf("expected GROWING, got %v", rsp.Plots[0].State)
	}
	// soil 100 - 5 = 95
	if p.Role.Bag.Goods[3001].Num != 95 {
		t.Fatalf("expected water 95, got %d", p.Role.Bag.Goods[3001].Num)
	}
	if !p.IsDirty() {
		t.Fatal("expected dirty")
	}
}

func TestWaterFlower_NotPlanted(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)

	_, err := p.ReqWaterFlower(context.Background(), &pb.ReqWaterFlower{PlotIds: []int32{1}})
	if !errors.Is(err, ErrPlotNotPlanted) {
		t.Fatalf("expected ErrPlotNotPlanted, got %v", err)
	}
}

// ========== HarvestFlower ==========

func TestHarvestFlower_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour) // past

	rsp, err := p.ReqHarvestFlower(context.Background(), &pb.ReqHarvestFlower{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].HarvestCount != 1 {
		t.Fatalf("expected harvest_count 1, got %d", rsp.Plots[0].HarvestCount)
	}
	// harvest_times=3, should still be GROWING
	if p.Plots[1].State != int32(pb.PlotState_PLOT_GROWING) {
		t.Fatalf("expected GROWING, got %d", p.Plots[1].State)
	}
	// got 2x rose_petal (10001)
	if p.Role.Bag.Goods[10001].Num != 2 {
		t.Fatalf("expected 2 petals, got %d", p.Role.Bag.Goods[10001].Num)
	}
}

func TestHarvestFlower_LastHarvest(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].HarvestCount = 2 // harvest_times=3, this is the last
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour)

	_, err := p.ReqHarvestFlower(context.Background(), &pb.ReqHarvestFlower{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	// should be EMPTY
	if p.Plots[1].State != int32(pb.PlotState_PLOT_EMPTY) {
		t.Fatalf("expected EMPTY, got %d", p.Plots[1].State)
	}
	if p.Plots[1].FlowerID != 0 {
		t.Fatalf("expected flower_id 0, got %d", p.Plots[1].FlowerID)
	}
}

func TestHarvestFlower_NotReady(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(1 * time.Hour) // future

	_, err := p.ReqHarvestFlower(context.Background(), &pb.ReqHarvestFlower{PlotIds: []int32{1}})
	if !errors.Is(err, ErrPlotNotReady) {
		t.Fatalf("expected ErrPlotNotReady, got %v", err)
	}
}

// ========== RemovePlant ==========

func TestRemovePlant_Success(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_PLANTED)

	rsp, err := p.ReqRemovePlant(context.Background(), &pb.ReqRemovePlant{PlotIds: []int32{1}})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_EMPTY {
		t.Fatalf("expected EMPTY, got %v", rsp.Plots[0].State)
	}
}

func TestRemovePlant_Harvestable(t *testing.T) {
	p := setupTestPlotWithMaterials(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour) // past, harvestable

	_, err := p.ReqRemovePlant(context.Background(), &pb.ReqRemovePlant{PlotIds: []int32{1}})
	if !errors.Is(err, ErrPlotHarvestable) {
		t.Fatalf("expected ErrPlotHarvestable, got %v", err)
	}
}

// ========== ReqPlotInfo ==========

func TestPlotInfo_Harvestable(t *testing.T) {
	p := setupTestPlot(t)
	p.UnlockPlot(1)
	p.Plots[1].FlowerID = 101
	p.Plots[1].State = int32(pb.PlotState_PLOT_GROWING)
	p.Plots[1].StateTime = time.Now().Add(-1 * time.Hour)

	rsp, err := p.ReqPlotInfo(context.Background(), &pb.ReqPlotInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_HARVESTABLE {
		t.Fatalf("expected HARVESTABLE, got %v", rsp.Plots[0].State)
	}
}

func TestPlotInfo_Empty(t *testing.T) {
	p := setupTestPlot(t)

	rsp, err := p.ReqPlotInfo(context.Background(), &pb.ReqPlotInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Plots) != 0 {
		t.Fatalf("expected 0 plots, got %d", len(rsp.Plots))
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `go test ./src/apps/role/internal/logic/ -run "TestPlot|TestPlant|TestWater|TestHarvest|TestRemove" -v -count=1 2>&1`
Expected: 全部 PASS。

- [ ] **Step 3: 运行全部测试确保无回归**

Run: `go test ./src/apps/role/internal/logic/ -v -count=1 2>&1 | tail -30`
Expected: 所有测试 PASS（flower + plot + bag）。

- [ ] **Step 4: 提交**

```bash
git add src/apps/role/internal/logic/role_plot_test.go
git commit -m "test(plot): 添加 RolePlot 单元测试"
```

---

### Task 7: 系统文档

**Files:**
- Create: `docs/system/plot.md`

- [ ] **Step 1: 编写系统文档**

参考 `docs/system/flower.md` 格式，创建 `docs/system/plot.md`，包含：数据结构、状态流转、配置表、Proto 接口、核心逻辑、GM 命令、错误码、代码位置、设计决策。

- [ ] **Step 2: 提交**

```bash
git add docs/system/plot.md
git commit -m "docs: 添加种植系统文档"
```
