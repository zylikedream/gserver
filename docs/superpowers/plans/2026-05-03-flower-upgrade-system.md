# 鲜花升级系统实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现鲜花升级功能，包括鲜花升级、突破、收获结算改造

**Architecture:** 在现有 `RoleFlower` 模块的 `FlowerData` 上扩展 `Level`/`BreakStage` 字段；新增 `UpgradeFlower`/`BreakFlower` 两个 Proto Handler；修改 `RolePlot.HarvestFlower` 使其按等级加成结算并掉落专属精华；新增 `TbFlowerLevel`/`TbFlowerBreak` 配置表查询

**Tech Stack:** Go, protoactor-go, protobuf, GORM/PostgreSQL (jsonb), gameconfig

---

### Task 1: 更新配置表结构

**Files:**
- Modify: `gameconfig/gosrc/garden.Flower.go` — 新增 `LevelGroup` 字段
- Modify: `gameconfig/gosrc/garden.FlowerLevel.go` — 主键从 `FlowerId` 改为 `LevelGroup`
- Modify: `gameconfig/gosrc/garden.FlowerBreak.go` — 主键从 `FlowerId` 改为 `LevelGroup`
- Modify: `gameconfig/gosrc/Tables.go` — (自动更新)

- [ ] **Step 1: 在 `GardenFlower` 结构体中新增 `LevelGroup int32` 字段**

```go
type GardenFlower struct {
    Id int32
    Name string
    Quality GardenEItemQuality
    BreedTime int32
    BreedCost []*GardenGoodStack
    GrowTime int32
    HarvestInterval int32
    HarvestTimes int32
    HarvestItemId int32
    HarvestNum int32
    WaterCost int32
    EssenceItemId int32
    EssenceDropRate int32
    EssenceDropNum int32
    LevelGroup int32  // 新增
}
```

同时更新 `NewGardenFlower` 解析函数，添加 `level_group` JSON 字段读取。

- [ ] **Step 2: 更新 `GardenFlowerLevel` — 将 `FlowerId` 改为 `LevelGroup`**

```go
type GardenFlowerLevel struct {
    Id int32
    LevelGroup int32       // 原 FlowerId → LevelGroup
    Level int32
    UpgradeCoinCost int32
    UpgradeEssenceCost int32
    HarvestNumAdd int32
    HarvestTimesAdd int32
    HarvestIntervalReduce int32
    EssenceDropRateAdd int32
    EssenceDropNumAdd int32
}
```

更新 `NewGardenFlowerLevel` 中的 JSON 解析字段名。

- [ ] **Step 3: 更新 `GardenFlowerBreak` — 将 `FlowerId` 改为 `LevelGroup`**

```go
type GardenFlowerBreak struct {
    Id int32
    LevelGroup int32         // 原 FlowerId → LevelGroup
    BreakStage int32
    NeedLevel int32
    CoinCost int32
    EssenceCost int32
    BreakItemId int32
    BreakItemNum int32
    PlayerLevelLimit int32
}
```

更新 `NewGardenFlowerBreak` 中的 JSON 解析字段名。

- [ ] **Step 4: 验证 build 通过**

Run: `go build ./...`
Expected: success

- [ ] **Step 5: 提交**

```bash
git add gameconfig/gosrc/garden.Flower.go gameconfig/gosrc/garden.FlowerLevel.go gameconfig/gosrc/garden.FlowerBreak.go
git commit -m "feat: 鲜花升级配置表字段更新 - flower 加 level_group, level/break 改主键"
```

---

### Task 2: Proto 消息扩展

**Files:**
- Modify: `protocol/client/flower.proto` — 新增升级/突破消息，扩展 PFlowerInfo

- [ ] **Step 1: 在 `PFlowerInfo` 中新增 `level` 和 `break_stage` 字段**

```protobuf
message PFlowerInfo {
    int32 flower_id = 1;
    FlowerState state = 2;
    int64 state_time = 3;
    int32 level = 4;        // 当前等级
    int32 break_stage = 5;  // 突破阶段
}
```

- [ ] **Step 2: 在 proto 文件中追加升级/突破消息**

```protobuf
message ReqUpgradeFlower {
    option (msg_id) = 23007;
    int32 flower_id = 1;
}

message RspUpgradeFlower {
    option (msg_id) = 23008;
    PFlowerInfo flower = 1;
}

message ReqBreakFlower {
    option (msg_id) = 23009;
    int32 flower_id = 1;
}

message RspBreakFlower {
    option (msg_id) = 23010;
    PFlowerInfo flower = 1;
}
```

- [ ] **Step 3: 生成 pb go 代码**

Run: `make pb`
Expected: `protocol/pb/*.proto.go` 中生成新的 message struct

- [ ] **Step 4: 提交**

```bash
git add protocol/
git commit -m "feat: 鲜花升级 proto 定义 - PFlowerInfo 扩展 level/break_stage，新增 UpgradeFlower/BreakFlower"
```

---

### Task 3: FlowerData 扩展 + 错误码

**Files:**
- Modify: `src/apps/role/internal/logic/role_flower.go` — `FlowerData` 新增字段
- Modify: `src/apps/role/internal/logic/const.go` — 新增错误码

- [ ] **Step 1: `FlowerData` 新增 `Level` 和 `BreakStage` 字段**

```go
type FlowerData struct {
    FlowerID   int32     `json:"flower_id"`
    State      int32     `json:"state"`
    StateTime  time.Time `json:"state_time"`
    Level      int32     `json:"level"`       // 当前等级，默认 1
    BreakStage int32     `json:"break_stage"` // 突破阶段，0=未突破，1=已突破
}
```

- [ ] **Step 2: `const.go` 新增错误码**

```go
var (
    ErrVersionConflict       = errors.New("optimistic lock version conflict")
    ErrFlowerMaxLevel        = errors.New("flower already at max level")
    ErrFlowerNeedBreak       = errors.New("flower needs breakthrough first")
    ErrFlowerBreakMax        = errors.New("flower already at max break stage")
    ErrFlowerBreakLevel      = errors.New("flower level not enough for breakthrough")
    ErrFlowerBreakPlayerLevel = errors.New("player level not enough for breakthrough")
)
```

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_flower.go src/apps/role/internal/logic/const.go
git commit -m "feat: FlowerData 新增 Level/BreakStage 字段及升级错误码"
```

---

### Task 4: 配置查询辅助方法

`TbFlowerLevel` 和 `TbFlowerBreak` 当前以自增 `Id` 作为 map key。需要在 `gameconfig` 层增加按 `(LevelGroup, Level)` / `(LevelGroup, BreakStage)` 查找的辅助方法。

**Files:**
- Modify: `gameconfig/gameconfig.go` — 新增查询方法

- [ ] **Step 1: 在 `gameconfig/gameconfig.go` 中新增辅助方法**

```go
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
```

- [ ] **Step 2: 验证 build 通过**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: 提交**

```bash
git add gameconfig/gameconfig.go
git commit -m "feat: 添加 TbFlowerLevel/TbFlowerBreak 按复合键查询方法"
```

---

### Task 5: 实现 UpgradeFlower / BreakFlower

**Files:**
- Modify: `src/apps/role/internal/logic/role_flower.go` — 新增 `ReqUpgradeFlower` / `ReqBreakFlower`、公开方法 `GetFlowerLevel`、更新 `PFlowerInfo`
- Modify: `src/apps/role/internal/logic/role_main.go` — `roleModules` 引用保持不变（已有 `Flower` 字段）

- [ ] **Step 1: 更新 `PFlowerInfo` 返回 level 和 break_stage**

```go
func PFlowerInfo(flower *FlowerData) *pb.PFlowerInfo {
    now := time.Now()
    state := flower.State
    if state == int32(pb.FlowerState_FLOWER_BREEDING) && now.After(flower.StateTime) {
        state = int32(pb.FlowerState_FLOWER_BREED_DONE)
    }
    return &pb.PFlowerInfo{
        FlowerId:   flower.FlowerID,
        State:      pb.FlowerState(state),
        StateTime:  flower.StateTime.Unix(),
        Level:      flower.Level,
        BreakStage: flower.BreakStage,
    }
}
```

- [ ] **Step 2: 添加公开方法 `GetFlowerLevel`**

```go
// GetFlowerLevel 返回花的等级信息，供 RolePlot 查询
func (r *RoleFlower) GetFlowerLevel(flowerID int32) (level int32, breakStage int32) {
    flower, ok := r.Flowers[flowerID]
    if !ok {
        return 1, 0 // 花不存在时返回默认值
    }
    return flower.Level, flower.BreakStage
}
```

- [ ] **Step 3: 实现 `ReqUpgradeFlower`**

```go
func (r *RoleFlower) ReqUpgradeFlower(ctx context.Context, req *pb.ReqUpgradeFlower) (*pb.RspUpgradeFlower, error) {
    flowerID := req.FlowerId

    flower, ok := r.Flowers[flowerID]
    if !ok {
        return nil, ErrFlowerLocked
    }

    cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
    if cfg == nil {
        return nil, errors.Errorf("flower config not found: %d", flowerID)
    }

    // 查找下一级配置
    nextLevel := flower.Level + 1
    levelCfg := gameconfig.GameConfig().GetFlowerLevelByGroup(cfg.LevelGroup, nextLevel)
    if levelCfg == nil {
        return nil, ErrFlowerMaxLevel
    }

    // 校验突破门槛：查找 {level_group, break_stage+1}
    nextBreak := gameconfig.GameConfig().GetFlowerBreakByGroup(cfg.LevelGroup, flower.BreakStage+1)
    if nextBreak != nil && nextLevel >= nextBreak.NeedLevel {
        return nil, ErrFlowerNeedBreak
    }

    // 校验消耗
    coinCost := MakeGoodStack(GOLD_ITEM_ID, int(levelCfg.UpgradeCoinCost))
    essenceCost := MakeGoodStack(int(cfg.EssenceItemId), int(levelCfg.UpgradeEssenceCost))
    if !r.Role.Bag.CheckGoods([]*gamecfg.GardenGoodStack{coinCost, essenceCost}) {
        return nil, ErrGoodNotEnough
    }

    // 扣资源
    removeGoods := []*gamecfg.GardenGoodStack{coinCost, essenceCost}
    if err := r.Role.Bag.SaveGoods(ctx, removeGoods, nil, "flower_upgrade"); err != nil {
        return nil, err
    }

    // 升级
    flower.Level = nextLevel
    r.MarkDirty()

    return &pb.RspUpgradeFlower{Flower: PFlowerInfo(flower)}, nil
}
```

- [ ] **Step 4: 实现 `ReqBreakFlower`**

```go
func (r *RoleFlower) ReqBreakFlower(ctx context.Context, req *pb.ReqBreakFlower) (*pb.RspBreakFlower, error) {
    flowerID := req.FlowerId

    flower, ok := r.Flowers[flowerID]
    if !ok {
        return nil, ErrFlowerLocked
    }

    cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
    if cfg == nil {
        return nil, errors.Errorf("flower config not found: %d", flowerID)
    }

    // 查找下一次突破配置
    nextBreakStage := flower.BreakStage + 1
    breakCfg := gameconfig.GameConfig().GetFlowerBreakByGroup(cfg.LevelGroup, nextBreakStage)
    if breakCfg == nil {
        return nil, ErrFlowerBreakMax
    }

    // 校验等级
    if flower.Level < breakCfg.NeedLevel {
        return nil, ErrFlowerBreakLevel
    }

    // 校验玩家等级 (通过 RoleMain)
    // playerLevel := r.Role.GetPlayerLevel()  // 待确认玩家等级获取方式

    // 校验消耗
    coinCost := MakeGoodStack(GOLD_ITEM_ID, int(breakCfg.CoinCost))
    essenceCost := MakeGoodStack(int(cfg.EssenceItemId), int(breakCfg.EssenceCost))
    breakMaterial := MakeGoodStack(int(breakCfg.BreakItemId), int(breakCfg.BreakItemNum))

    var removeGoods []*gamecfg.GardenGoodStack
    if breakCfg.CoinCost > 0 {
        removeGoods = append(removeGoods, coinCost)
    }
    if breakCfg.EssenceCost > 0 {
        removeGoods = append(removeGoods, essenceCost)
    }
    if breakCfg.BreakItemNum > 0 {
        removeGoods = append(removeGoods, breakMaterial)
    }

    if !r.Role.Bag.CheckGoods(removeGoods) {
        return nil, ErrGoodNotEnough
    }
    if err := r.Role.Bag.SaveGoods(ctx, removeGoods, nil, "flower_break"); err != nil {
        return nil, err
    }

    // 突破
    flower.BreakStage = nextBreakStage
    r.MarkDirty()

    return &pb.RspBreakFlower{Flower: PFlowerInfo(flower)}, nil
}
```

- [ ] **Step 5: 验证 build 通过**

Run: `go build ./...`
Expected: success

- [ ] **Step 6: 提交**

```bash
git add src/apps/role/internal/logic/role_flower.go
git commit -m "feat: 实现 ReqUpgradeFlower / ReqBreakFlower"
```

---

### Task 6: 收获结算改造

**Files:**
- Modify: `src/apps/role/internal/logic/role_plot.go` — `ReqHarvestFlower` 增加等级加成和精华掉落

- [ ] **Step 1: 重写 `ReqHarvestFlower`，加入等级加成结算和精华掉落**

```go
func (r *RolePlot) ReqHarvestFlower(ctx context.Context, req *pb.ReqHarvestFlower) (*pb.RspHarvestFlower, error) {
    var harvestItems []*gamecfg.GardenGoodStack
    var essenceItems []*gamecfg.GardenGoodStack
    now := time.Now()

    // 第一轮：校验所有地块状态
    for _, plotID := range req.PlotIds {
        plot, ok := r.Plots[plotID]
        if !ok {
            return nil, ErrPlotLocked
        }
        state := getPlotState(plot)
        if state != int32(pb.PlotState_PLOT_HARVESTABLE) {
            return nil, ErrPlotNotReady
        }
        if plot.State != int32(pb.PlotState_PLOT_GROWING) || !now.After(plot.StateTime) {
            return nil, ErrPlotNotReady
        }
    }

    // 第二轮：计算收获产出
    for _, plotID := range req.PlotIds {
        plot := r.Plots[plotID]
        flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
        if flowerCfg == nil {
            return nil, errors.Errorf("flower config not found: %d", plot.FlowerID)
        }

        // 读鲜花升级加成
        level, _ := r.Role.Flower.GetFlowerLevel(plot.FlowerID)
        levelCfg := gameconfig.GameConfig().GetFlowerLevelByGroup(flowerCfg.LevelGroup, level)

        // 计算最终收获值
        finalNum := flowerCfg.HarvestNum
        if levelCfg != nil {
            finalNum += levelCfg.HarvestNumAdd
        }

        harvestItems = append(harvestItems, MakeGoodStack(int(flowerCfg.HarvestItemId), int(finalNum)))

        // 精华掉落判定
        if flowerCfg.EssenceItemId > 0 {
            dropRate := flowerCfg.EssenceDropRate
            if levelCfg != nil {
                dropRate += levelCfg.EssenceDropRateAdd
            }
            // 简单概率判定：rand.Intn(10000) < dropRate (假设万分比)
            if dropRate > 0 && rand.Intn(10000) < int(dropRate) {
                dropNum := flowerCfg.EssenceDropNum
                if levelCfg != nil && levelCfg.EssenceDropNumAdd > 0 {
                    dropNum += levelCfg.EssenceDropNumAdd
                }
                essenceItems = append(essenceItems, MakeGoodStack(int(flowerCfg.EssenceItemId), int(dropNum)))
            }
        }
    }

    // 发放花产品
    if len(harvestItems) > 0 {
        if err := r.Role.Bag.SaveGoods(ctx, nil, harvestItems, "harvest_flower"); err != nil {
            return nil, err
        }
    }

    // 发放专属精华
    if len(essenceItems) > 0 {
        if err := r.Role.Bag.SaveGoods(ctx, nil, essenceItems, "harvest_essence"); err != nil {
            return nil, err
        }
    }

    // 更新地块状态
    for _, plotID := range req.PlotIds {
        plot := r.Plots[plotID]
        flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)

        // 读取升级后的 harvest_times
        level, _ := r.Role.Flower.GetFlowerLevel(plot.FlowerID)
        levelCfg := gameconfig.GameConfig().GetFlowerLevelByGroup(flowerCfg.LevelGroup, level)

        finalTimes := flowerCfg.HarvestTimes
        if levelCfg != nil {
            finalTimes += levelCfg.HarvestTimesAdd
        }

        plot.HarvestCount++
        if plot.HarvestCount >= finalTimes {
            // 收获完毕，重置为空地
            plot.FlowerID = 0
            plot.State = int32(pb.PlotState_PLOT_EMPTY)
            plot.HarvestCount = 0
            plot.StateTime = time.Time{}
        } else {
            // 计算最终收获间隔
            finalInterval := flowerCfg.HarvestInterval
            if levelCfg != nil {
                finalInterval -= levelCfg.HarvestIntervalReduce
            }
            if finalInterval < 1 {
                finalInterval = 1 // 最小间隔保护
            }
            plot.StateTime = now.Add(time.Duration(finalInterval) * time.Second)
        }
    }
    r.MarkDirty()

    rsp := &pb.RspHarvestFlower{Plots: []*pb.PPlotInfo{}}
    for _, plotID := range req.PlotIds {
        rsp.Plots = append(rsp.Plots, pPlotInfo(r.Plots[plotID]))
    }
    return rsp, nil
}
```

注意：需要在 `role_plot.go` 的 import 中添加 `"math/rand"`。

- [ ] **Step 2: 验证 build 通过**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_plot.go
git commit -m "feat: 收获结算加入等级加成和专属精华掉落"
```

---

### Task 7: GM 命令

**Files:**
- Modify: `src/apps/role/internal/logic/role_gm.go` — 新增 `add_flower_level` GM 命令用于测试

- [ ] **Step 1: 添加 GM 命令 `add_flower_level`**

在 `role_gm.go` 中新增方法（参考现有 `add_flower` 命令的模式）：

```go
// 用法: add_flower_level [花ID] [等级]
// 示例: add_flower_level 101 10
func (r *RoleGM) add_flower_level(flowerID int32, level int32) (string, error) {
    flower, ok := r.Role.Flower.Flowers[flowerID]
    if !ok {
        return "", fmt.Errorf("flower %d not unlocked", flowerID)
    }
    if level < 1 {
        return "", fmt.Errorf("level must be >= 1")
    }
    flower.Level = level
    r.Role.Flower.MarkDirty()
    return fmt.Sprintf("flower %d level set to %d", flowerID, level), nil
}
```

- [ ] **Step 2: 提交**

```bash
git add src/apps/role/internal/logic/role_gm.go
git commit -m "feat: 添加 add_flower_level GM 命令"
```

---

### Task 8: 单元测试

**Files:**
- Modify: `src/apps/role/internal/logic/role_flower_test.go` — 新增升级/突破测试
- Modify: `src/apps/role/internal/logic/role_plot_test.go` — 更新收获测试（验证加成生效）

- [ ] **Step 1: `role_flower_test.go` — 升级/突破测试**

在 `initFlowerTestConfig` 中更新花配置，添加 `level_group`、精华字段：

```go
flowers := []map[string]interface{}{
    {
        "id": float64(101), "name": "rose", "quality": float64(1),
        "breed_time": float64(10),
        "breed_cost": []interface{}{...},
        "grow_time": float64(60), "harvest_interval": float64(30),
        "harvest_times": float64(3), "harvest_item_id": float64(10001),
        "harvest_num": float64(2), "water_cost": float64(5),
        "essence_item_id": float64(5001), "essence_drop_rate": float64(5000),
        "essence_drop_num": float64(1), "level_group": float64(1),
    },
    {
        "id": float64(102), "name": "sunflower", "quality": float64(1),
        "breed_time": float64(20),
        "breed_cost": []interface{}{...},
        "grow_time": float64(120), "harvest_interval": float64(60),
        "harvest_times": float64(2), "harvest_item_id": float64(10002),
        "harvest_num": float64(1), "water_cost": float64(3),
        "essence_item_id": float64(5002), "essence_drop_rate": float64(3000),
        "essence_drop_num": float64(1), "level_group": float64(1),
    },
}
```

在 `initFlowerTestConfig` 中初始化 `TbFlowerLevel` 和 `TbFlowerBreak`：

```go
// TbFlowerLevel: level_group=1, Lv1~Lv5 的配置
levels := []map[string]interface{}{
    {"id": float64(1), "level_group": float64(1), "level": float64(1),
     "upgrade_coin_cost": float64(0), "upgrade_essence_cost": float64(0),
     "harvest_num_add": float64(0), "harvest_times_add": float64(0),
     "harvest_interval_reduce": float64(0), "essence_drop_rate_add": float64(0),
     "essence_drop_num_add": float64(0)},
    {"id": float64(2), "level_group": float64(1), "level": float64(2),
     "upgrade_coin_cost": float64(100), "upgrade_essence_cost": float64(2),
     "harvest_num_add": float64(0), "harvest_times_add": float64(0),
     "harvest_interval_reduce": float64(5), "essence_drop_rate_add": float64(500),
     "essence_drop_num_add": float64(0)},
    {"id": float64(3), "level_group": float64(1), "level": float64(3),
     "upgrade_coin_cost": float64(200), "upgrade_essence_cost": float64(3),
     "harvest_num_add": float64(0), "harvest_times_add": float64(0),
     "harvest_interval_reduce": float64(10), "essence_drop_rate_add": float64(1000),
     "essence_drop_num_add": float64(0)},
}
tbFlowerLevel, err := gamecfg.NewGardenTbFlowerLevel(levels)

// TbFlowerBreak: level_group=1, break_stage=1, need_level=3
breaks := []map[string]interface{}{
    {"id": float64(1), "level_group": float64(1), "break_stage": float64(1),
     "need_level": float64(3),
     "coin_cost": float64(500), "essence_cost": float64(5),
     "break_item_id": float64(6001), "break_item_num": float64(1),
     "player_level_limit": float64(0)},
}
tbFlowerBreak, err := gamecfg.NewGardenTbFlowerBreak(breaks)
```

在 `initFlowerTestConfig` 中把 `TbFlowerLevel` 和 `TbFlowerBreak` 注入 Tables：

```go
gc.Tables = &gamecfg.Tables{
    TbItem: tbItem, TbFlower: tbFlower,
    TbFlowerLevel: tbFlowerLevel, TbFlowerBreak: tbFlowerBreak,
}
```

添加升级测试：

```go
// ========== UpgradeFlower ==========

func TestUpgradeFlower_Success(t *testing.T) {
    f := setupTestFlowerWithMaterials(t)
    f.AddFlower(101)
    f.Role.Bag.Goods[5001] = bag.BagGood{GoodID: 5001, Num: 10} // essence
    f.Role.Bag.Goods[GOLD_ITEM_ID] = bag.BagGood{GoodID: GOLD_ITEM_ID, Num: 1000}

    rsp, err := f.ReqUpgradeFlower(context.Background(), &pb.ReqUpgradeFlower{FlowerId: 101})
    if err != nil {
        t.Fatal(err)
    }
    if rsp == nil {
        t.Fatal("expected non-nil response")
    }

    fd := f.Flowers[101]
    if fd.Level != 2 {
        t.Fatalf("expected level 2, got %d", fd.Level)
    }
    // 验证扣资源：essence 10-2=8
    if f.Role.Bag.Goods[5001].Num != 8 {
        t.Fatalf("expected essence 8, got %d", f.Role.Bag.Goods[5001].Num)
    }
}

func TestUpgradeFlower_MaxLevel(t *testing.T) {
    f := setupTestFlowerWithMaterials(t)
    f.AddFlower(101)
    f.Flowers[101].Level = 3 // level_group=1 最高配到 Lv3

    _, err := f.ReqUpgradeFlower(context.Background(), &pb.ReqUpgradeFlower{FlowerId: 101})
    if !errors.Is(err, ErrFlowerMaxLevel) {
        t.Fatalf("expected ErrFlowerMaxLevel, got %v", err)
    }
}

func TestUpgradeFlower_NeedBreak(t *testing.T) {
    f := setupTestFlowerWithMaterials(t)
    f.AddFlower(101)
    f.Flowers[101].Level = 2 // 尝试升到 Lv3，但 Lv3 >= need_level(3) 需先突破
    f.Role.Bag.Goods[5001] = bag.BagGood{GoodID: 5001, Num: 10}
    f.Role.Bag.Goods[GOLD_ITEM_ID] = bag.BagGood{GoodID: GOLD_ITEM_ID, Num: 1000}

    _, err := f.ReqUpgradeFlower(context.Background(), &pb.ReqUpgradeFlower{FlowerId: 101})
    if !errors.Is(err, ErrFlowerNeedBreak) {
        t.Fatalf("expected ErrFlowerNeedBreak, got %v", err)
    }
}

func TestUpgradeFlower_NotUnlocked(t *testing.T) {
    f := setupTestFlowerWithMaterials(t)

    _, err := f.ReqUpgradeFlower(context.Background(), &pb.ReqUpgradeFlower{FlowerId: 101})
    if !errors.Is(err, ErrFlowerLocked) {
        t.Fatalf("expected ErrFlowerLocked, got %v", err)
    }
}

// ========== BreakFlower ==========

func TestBreakFlower_Success(t *testing.T) {
    f := setupTestFlowerWithMaterials(t)
    f.AddFlower(101)
    f.Flowers[101].Level = 3
    // 准备突破消耗
    f.Role.Bag.Goods[5001] = bag.BagGood{GoodID: 5001, Num: 10}  // essence
    f.Role.Bag.Goods[GOLD_ITEM_ID] = bag.BagGood{GoodID: GOLD_ITEM_ID, Num: 1000}
    f.Role.Bag.Goods[6001] = bag.BagGood{GoodID: 6001, Num: 5}   // break material

    rsp, err := f.ReqBreakFlower(context.Background(), &pb.ReqBreakFlower{FlowerId: 101})
    if err != nil {
        t.Fatal(err)
    }
    if rsp == nil {
        t.Fatal("expected non-nil response")
    }

    fd := f.Flowers[101]
    if fd.BreakStage != 1 {
        t.Fatalf("expected break_stage 1, got %d", fd.BreakStage)
    }
}

func TestBreakFlower_LevelNotEnough(t *testing.T) {
    f := setupTestFlowerWithMaterials(t)
    f.AddFlower(101)
    // level=1, need_level=3

    _, err := f.ReqBreakFlower(context.Background(), &pb.ReqBreakFlower{FlowerId: 101})
    if !errors.Is(err, ErrFlowerBreakLevel) {
        t.Fatalf("expected ErrFlowerBreakLevel, got %v", err)
    }
}
```

- [ ] **Step 2: 运行测试确认通过**

Run: `go test ./src/apps/role/internal/logic/ -run TestUpgradeFlower -v`
Expected: PASS

Run: `go test ./src/apps/role/internal/logic/ -run TestBreakFlower -v`
Expected: PASS

Run: `go test ./src/apps/role/internal/logic/ -run TestFlower -v`
Expected: ALL PASS（确保不影响现有培育测试）

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_flower_test.go
git commit -m "test: 添加鲜花升级/突破单元测试"
```

---

### Task 9: 文档更新

**Files:**
- Modify: `docs/system/flower.md` — 补充升级系统文档

- [ ] **Step 1: 更新 `docs/system/flower.md`**

在现有文档末尾追加升级系统章节（或生成独立文档）。补充内容：

```
## 鲜花升级系统

详见 [设计 spec](../superpowers/specs/2026-05-03-flower-upgrade-system-design.md)。

### 数据模型扩展

`FlowerData` 新增：
- `level` (int32)：当前等级，默认 1
- `break_stage` (int32)：突破阶段，0=未突破

### Proto 接口扩展

| 消息 | ID | 说明 |
|------|----|------|
| ReqUpgradeFlower / RspUpgradeFlower | 23007-23008 | 升级鲜花 |
| ReqBreakFlower / RspBreakFlower | 23009-23010 | 突破 |

`PFlowerInfo` 扩展 `level`、`break_stage` 字段。

### 升级流程

1. 校验花已解锁
2. 通过 TbFlower.level_group 查找升级配置
3. 校验是否被突破门槛拦住
4. 扣金币 + 专属精华 → level++

### 突破流程

1. 查找 TbFlowerBreak 配置
2. 校验等级和玩家等级
3. 扣金币 + 专属精华 + 突破材料 → break_stage++

### 收获结算变化

- 花产品数量 = 基础 + 等级加成
- 收获次数 = 基础 + 等级加成
- 收获间隔 = 基础 - 等级缩减（最小 1s）
- 额外按概率掉落专属精华
```

- [ ] **Step 2: 提交**

```bash
git add docs/system/flower.md
git commit -m "docs: 鲜花升级系统文档更新"
```

---

### Task 10: 边界情况处理 — 缺少 `GOLD_ITEM_ID`

当前代码中 `const.go` 没有 `GOLD_ITEM_ID` 常量，需要确认金币的物品 ID。

- [ ] **Step 1: 检查金币物品 ID**

查看 `gameconfig` 中的 TbItem 配置或现有代码中的金币引用。

Run: `grep -rn "GOLD\|gold\|金币" src/apps/role/internal/logic/ --include="*.go" | head -10`

如果不存在，在 `const.go` 中定义：

```go
var GOLD_ITEM_ID = 1  // 或其他实际配置的物品 ID
```
