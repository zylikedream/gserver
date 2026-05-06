# Steal-Flower System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现偷花系统 — 玩家可以查看好友花园并偷取成熟鲜花，获得花产物。

**Architecture:** steal_record 辅助表记录偷取行为，偷花者 actor 直接读 DB 获取目标地块状态（FOR UPDATE 锁防并发），收获时查询 steal_record 扣减产量。所有地块操作立即落盘消除 DB 延迟。

**Tech Stack:** Go, protoactor-go, GoFrame v2, GORM/PostgreSQL, protobuf

---

## File Structure

| 文件 | 职责 |
|------|------|
| `protocol/client/flower.proto` | 新增偷花/查看好友花园 proto |
| `src/apps/role/internal/logic/role_steal.go` (新) | 偷花 handler + 查看好友花园 handler |
| `src/apps/role/internal/logic/steal_record.go` (新) | steal_record GORM 模型 + DB 操作 |
| `src/apps/role/internal/logic/role_plot.go` | 收获逻辑修改：FOR UPDATE + yield 扣减 + steal_record 清理 + force-save |
| `gameconfig/gosrc/garden.FriendConfig.go` | 新增偷花配置字段 |
| `gameconfig/json/garden_tbfriendconfig.json` | 新增偷花配置值 |

---

### Task 1: Proto 变更

**Files:**
- Modify: `protocol/client/flower.proto`

- [ ] **Step 1: 在 flower.proto 中 PPlotInfo 加 can_steal 字段，新增 4 个消息**

在 `PPlotInfo` 末尾加 `bool can_steal = 6;`，在文件末尾（RspRemovePlant 之后）加：

```proto
// 查看好友花园 (24013)
message ReqFriendPlotInfo {
    option (msg_id) = 24013;
    int64 friend_id = 1;
}
message RspFriendPlotInfo {
    option (msg_id) = 24014;
    repeated PPlotInfo plots = 1;
    int32 steal_used  = 2;  // 今日对该好友已偷次数
    int32 steal_limit = 3;  // 每日上限
}

// 偷花 (24015)
message ReqStealFlower {
    option (msg_id) = 24015;
    int64 friend_id = 1;
    int32 plot_id   = 2;
}
message RspStealFlower {
    option (msg_id) = 24016;
    bool              success = 1;
    string            error   = 2;
    repeated PGoodInfo rewards = 3;  // 偷到的物品
}
```

注意需要 `import "bag.proto";` 以引用 PGoodInfo。

- [ ] **Step 2: 生成 proto 并验证编译**

```bash
make pb && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add protocol/ && git commit -m "feat(steal-flower): add proto definitions for friend garden view and steal"
```

---

### Task 2: GameConfig 新增偷花字段

**Files:**
- Modify: `gameconfig/json/garden_tbfriendconfig.json`
- Modify: `gameconfig/gosrc/garden.FriendConfig.go`

- [ ] **Step 1: JSON 配置加字段**

在 `garden_tbfriendconfig.json` 中追加 5 个字段：

```json
{
  "unlock_level": 6,
  "friend_max_count": 50,
  "apply_send_limit": 30,
  "apply_receive_limit": 50,
  "apply_expire_seconds": 604800,
  "delete_reapply_cd_seconds": 86400,
  "search_result_limit": 20,
  "steal_unlock_level": 6,
  "steal_per_friend_daily_limit": 10,
  "steal_reward_num": 1,
  "flower_max_be_stolen_times": 3,
  "owner_min_keep_num": 1
}
```

- [ ] **Step 2: Go struct 加字段**

在 `GardenFriendConfig` 中追加：

```go
StealUnlockLevel         int32
StealPerFriendDailyLimit int32
StealRewardNum           int32
FlowerMaxBeStolenTimes   int32
OwnerMinKeepNum          int32
```

并在 `NewGardenFriendConfig` 中追加 5 个解析块，格式与现有字段一致（参考 `delete_reapply_cd_seconds` 的模式）：

```go
{ var _ok_ bool; var __json_steal_unlock_level__ interface{}; if __json_steal_unlock_level__, _ok_ = _buf["steal_unlock_level"]; !_ok_ || __json_steal_unlock_level__ == nil { err = errors.New("steal_unlock_level error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_steal_unlock_level__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.StealUnlockLevel = __x__ }}
{ var _ok_ bool; var __json_steal_per_friend_daily_limit__ interface{}; if __json_steal_per_friend_daily_limit__, _ok_ = _buf["steal_per_friend_daily_limit"]; !_ok_ || __json_steal_per_friend_daily_limit__ == nil { err = errors.New("steal_per_friend_daily_limit error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_steal_per_friend_daily_limit__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.StealPerFriendDailyLimit = __x__ }}
{ var _ok_ bool; var __json_steal_reward_num__ interface{}; if __json_steal_reward_num__, _ok_ = _buf["steal_reward_num"]; !_ok_ || __json_steal_reward_num__ == nil { err = errors.New("steal_reward_num error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_steal_reward_num__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.StealRewardNum = __x__ }}
{ var _ok_ bool; var __json_flower_max_be_stolen_times__ interface{}; if __json_flower_max_be_stolen_times__, _ok_ = _buf["flower_max_be_stolen_times"]; !_ok_ || __json_flower_max_be_stolen_times__ == nil { err = errors.New("flower_max_be_stolen_times error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_flower_max_be_stolen_times__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.FlowerMaxBeStolenTimes = __x__ }}
{ var _ok_ bool; var __json_owner_min_keep_num__ interface{}; if __json_owner_min_keep_num__, _ok_ = _buf["owner_min_keep_num"]; !_ok_ || __json_owner_min_keep_num__ == nil { err = errors.New("owner_min_keep_num error"); return } else { var __x__ int32;  { var _ok_ bool; var _x_ float64; if _x_, _ok_ = __json_owner_min_keep_num__.(float64); !_ok_ { err = errors.New("__x__ error"); return }; __x__ = int32(_x_) }; _v.OwnerMinKeepNum = __x__ }}
```

- [ ] **Step 3: 编译验证**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add gameconfig/ && git commit -m "feat(steal-flower): add steal config fields to TbFriendConfig"
```

---

### Task 3: steal_record 模型

**Files:**
- Create: `src/apps/role/internal/logic/steal_record.go`

- [ ] **Step 1: 创建 steal_record GORM 模型**

```go
package logic

import (
	"context"
	"time"

	"gserver/core/gxypgx"
)

type StealRecord struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	OwnerID   int64     `gorm:"column:owner_id;index:idx_owner_plot"`
	PlotID    int32     `gorm:"column:plot_id;index:idx_owner_plot"`
	StealerID int64     `gorm:"column:stealer_id;index:idx_stealer_owner"`
	FlowerID  int32     `gorm:"column:flower_id"`
	StealTime time.Time `gorm:"column:steal_time"`
}

func (StealRecord) TableName() string { return "steal_record" }

func initStealSchema(ctx context.Context) {
	_ = gxypgx.DB().AutoMigrate(&StealRecord{})
}

// countPlotStolen 查地块被偷次数
func countPlotStolen(ctx context.Context, ownerID int64, plotID int32) (int64, error) {
	var count int64
	err := gxypgx.DB().WithContext(ctx).Model(&StealRecord{}).
		Where("owner_id = ? AND plot_id = ?", ownerID, plotID).
		Count(&count).Error
	return count, err
}

// countDailySteal 查偷花者今日对目标的偷取次数
func countDailySteal(ctx context.Context, stealerID, ownerID int64) (int64, error) {
	todayStart := time.Now().Truncate(24 * time.Hour)
	var count int64
	err := gxypgx.DB().WithContext(ctx).Model(&StealRecord{}).
		Where("stealer_id = ? AND owner_id = ? AND steal_time >= ?", stealerID, ownerID, todayStart).
		Count(&count).Error
	return count, err
}

// insertStealRecord 写入偷取记录（在事务内调用）
func insertStealRecord(ctx context.Context, ownerID int64, plotID int32, stealerID int64, flowerID int32) error {
	return gxypgx.DB().WithContext(ctx).Create(&StealRecord{
		OwnerID:   ownerID,
		PlotID:    plotID,
		StealerID: stealerID,
		FlowerID:  flowerID,
		StealTime: time.Now(),
	}).Error
}

// deletePlotStealRecords 清理地块偷取记录
func deletePlotStealRecords(ctx context.Context, ownerID int64, plotID int32) error {
	return gxypgx.DB().WithContext(ctx).
		Where("owner_id = ? AND plot_id = ?", ownerID, plotID).
		Delete(&StealRecord{}).Error
}
```

- [ ] **Step 2: 在 role_main.go 的 Init 或 DelayInit 中调用 initStealSchema**

找到 role_main.go 中已有的 schema 初始化位置（类似 friend 的 InitFriendSchema），添加 `initStealSchema(ctx)` 调用。

- [ ] **Step 3: 编译验证**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add src/apps/role/internal/logic/steal_record.go && git commit -m "feat(steal-flower): add steal_record model and DB helpers"
```

---

### Task 4: 地块操作立即落盘 (force-save)

**Files:**
- Modify: `src/apps/role/internal/logic/role_plot.go`

- [ ] **Step 1: 在 RolePlot 上添加 forceSave 方法**

```go
func (r *RolePlot) forceSave(ctx context.Context) error {
	r.MarkDirty()
	return r.Role.saveRoleModule(ctx, r)
}
```

注意：`saveRoleModule` 在 `role_main.go:305` 定义，带 dirty 检查和版本管理。先 `MarkDirty()` 再调用即可绕过 dirty 检查。

- [ ] **Step 2: 在 ReqPlantFlower 末尾调用 forceSave**

找到 `ReqPlantFlower` 方法末尾（`r.MarkDirty()` 附近），替换为 `r.forceSave(ctx)`。

- [ ] **Step 3: 在 ReqWaterFlower 末尾调用 forceSave**

同上。

- [ ] **Step 4: 在 ReqHarvestFlower 末尾调用 forceSave**

同上。注意这个会在 Task 7 中进一步修改（加 FOR UPDATE），此处先加 forceSave。

- [ ] **Step 5: 在 ReqRemovePlant 末尾调用 forceSave**（如有）

- [ ] **Step 6: 编译验证**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add src/apps/role/internal/logic/role_plot.go && git commit -m "feat(steal-flower): force-save plot data on all mutating operations"
```

---

### Task 5: 查看好友花园 (ReqFriendPlotInfo)

**Files:**
- Create: `src/apps/role/internal/logic/role_steal.go`

- [ ] **Step 1: 创建 role_steal.go，实现 ReqFriendPlotInfo**

```go
package logic

import (
	"context"
	"time"

	"gserver/core/gxypgx"
	"gserver/gameconfig"
	"gserver/protocol/pb"

	"gorm.io/gorm"
)

func (r *RoleMain) ReqFriendPlotInfo(ctx context.Context, req *pb.ReqFriendPlotInfo) (*pb.RspFriendPlotInfo, error) {
	friendID := req.FriendId

	// 1. 检查好友关系
	if !isFriend(ctx, r.RoleID, friendID) {
		return nil, ErrNotFriend
	}

	// 2. 读目标的 role_plot 行
	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	err := gxypgx.DB().WithContext(ctx).Table("role_plot").
		Where("role_id = ?", friendID).First(&row).Error
	if err != nil {
		return &pb.RspFriendPlotInfo{}, nil
	}

	// 3. 计算每个地块的派生状态和 can_steal
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()
	dailyCount, _ := countDailySteal(ctx, r.RoleID, friendID)
	now := time.Now()

	rsp := &pb.RspFriendPlotInfo{
		StealLimit: cfg.StealPerFriendDailyLimit,
		StealUsed:  int32(dailyCount),
	}

	for _, plot := range row.Plots {
		state := getPlotState(plot)
		info := &pb.PPlotInfo{
			PlotId:       plot.PlotID,
			FlowerId:     plot.FlowerID,
			State:        pb.PlotState(state),
			HarvestCount: plot.HarvestCount,
			StateTime:    plot.StateTime.Unix(),
		}

		// can_steal: 可收获 + 未被偷满 + 每日次数未满
		if state == int32(pb.PlotState_PLOT_HARVESTABLE) {
			stolen, _ := countPlotStolen(ctx, friendID, plot.PlotID)
			if stolen < int64(cfg.FlowerMaxBeStolenTimes) && dailyCount < int64(cfg.StealPerFriendDailyLimit) {
				info.CanSteal = true
			}
		}

		rsp.Plots = append(rsp.Plots, info)
	}

	return rsp, nil
}

// isFriend 复用 friend HTTP 检查
func isFriend(ctx context.Context, myID, targetID int64) bool {
	rel, _ := getRelation(ctx, myID, targetID)
	return rel == relationFriend
}

// getPlotState 判断地块派生状态（与 role_plot.go 中一致）
func getPlotState(plot *PlotData) int32 {
	if plot.State == int32(pb.PlotState_PLOT_GROWING) && time.Now().After(plot.StateTime) {
		return int32(pb.PlotState_PLOT_HARVESTABLE)
	}
	return plot.State
}
```

注意：`getPlotState` 可能已在 role_plot.go 中存在。如果是，删除 role_steal.go 中的重复定义，直接调用已有的。检查方式：`grep -n "func getPlotState" src/apps/role/internal/logic/role_plot.go`。

- [ ] **Step 2: 编译验证**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add src/apps/role/internal/logic/role_steal.go && git commit -m "feat(steal-flower): implement ReqFriendPlotInfo handler"
```

---

### Task 6: 偷花 (ReqStealFlower)

**Files:**
- Modify: `src/apps/role/internal/logic/role_steal.go`

- [ ] **Step 1: 在 role_steal.go 中添加 ReqStealFlower handler**

```go
import (
	"errors"
	"fmt"
)

var (
	ErrNotFriend          = errors.New("对方不是你的好友")
	ErrStealNotHarvestable = errors.New("该鲜花尚未成熟")
	ErrStealFlowerFull    = errors.New("该鲜花已被摘取完毕")
	ErrStealDailyFull     = errors.New("今日对该好友的摘取次数已用完")
	ErrStealLocked        = errors.New("该鲜花已无法摘取")
)

func (r *RoleMain) ReqStealFlower(ctx context.Context, req *pb.ReqStealFlower) (*pb.RspStealFlower, error) {
	friendID := req.FriendId
	plotID := req.PlotId
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()

	// 1. 检查好友关系
	if !isFriend(ctx, r.RoleID, friendID) {
		return &pb.RspStealFlower{Success: false, Error: ErrNotFriend.Error()}, nil
	}

	// 2. FOR UPDATE 锁目标的 role_plot 行
	tx := gxypgx.DB().WithContext(ctx).Begin()
	defer tx.Rollback()

	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	err := tx.Table("role_plot").
		Set("gorm:query_option", "FOR UPDATE").
		Where("role_id = ?", friendID).First(&row).Error
	if err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常，请稍后重试"}, nil
	}

	// 3. 检查地块可收获
	plot, ok := row.Plots[plotID]
	if !ok {
		return &pb.RspStealFlower{Success: false, Error: ErrStealLocked.Error()}, nil
	}
	state := getPlotState(plot)
	if state != int32(pb.PlotState_PLOT_HARVESTABLE) {
		return &pb.RspStealFlower{Success: false, Error: ErrStealNotHarvestable.Error()}, nil
	}

	// 4. 检查地块被偷次数
	stolenCount, err := countPlotStolen(ctx, friendID, plotID)
	if err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}
	if stolenCount >= int64(cfg.FlowerMaxBeStolenTimes) {
		return &pb.RspStealFlower{Success: false, Error: ErrStealFlowerFull.Error()}, nil
	}

	// 5. 检查每日偷取次数
	dailyCount, err := countDailySteal(ctx, r.RoleID, friendID)
	if err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}
	if dailyCount >= int64(cfg.StealPerFriendDailyLimit) {
		return &pb.RspStealFlower{Success: false, Error: ErrStealDailyFull.Error()}, nil
	}

	// 6. 写入 steal_record
	if err := tx.Create(&StealRecord{
		OwnerID:   friendID,
		PlotID:    plotID,
		StealerID: r.RoleID,
		FlowerID:  plot.FlowerID,
		StealTime: time.Now(),
	}).Error; err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}

	if err := tx.Commit().Error; err != nil {
		return &pb.RspStealFlower{Success: false, Error: "网络异常"}, nil
	}

	// 7. 给偷花者发奖励
	flowerCfg := gameconfig.GameConfig().TbFlower.Get(plot.FlowerID)
	if flowerCfg == nil {
		return &pb.RspStealFlower{Success: false, Error: "配置异常"}, nil
	}
	rewardNum := int(cfg.StealRewardNum)
	if err := r.Bag.SaveGoods(ctx, nil,
		[]*gamecfg.GardenGoodStack{bag.MakeGoodStack(int(flowerCfg.HarvestItemId), rewardNum)},
		"steal_flower"); err != nil {
		return &pb.RspStealFlower{Success: false, Error: "背包异常"}, nil
	}

	return &pb.RspStealFlower{
		Success: true,
		Rewards: []*pb.PGoodInfo{
			{PropId: flowerCfg.HarvestItemId, Num: int64(rewardNum)},
		},
	}, nil
}
```

- [ ] **Step 2: 确认 import 完整**

需要确保 `gamecfg` 和 `bag` 包的 import 路径正确：
- `gamecfg "gserver/gameconfig"` — 检查现有 role_plot.go 的 import
- `bag` package — 检查 role_bag.go 的 package 是否是 `logic`（如果是，直接用函数名）

- [ ] **Step 3: 编译验证**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add src/apps/role/internal/logic/role_steal.go && git commit -m "feat(steal-flower): implement ReqStealFlower handler"
```

---

### Task 7: 收获逻辑修改 — 偷花扣减 + steal_record 清理

**Files:**
- Modify: `src/apps/role/internal/logic/role_plot.go`

这是最复杂的改动。当前 `ReqHarvestFlower` 完全在内存操作，需要改为：
1. FOR UPDATE 锁 role_plot 行
2. 读取每个地块的 steal_record 被偷次数
3. 扣减 yield
4. 地块归空时清理 steal_record

- [ ] **Step 1: 在收获的第二轮（计算产出）中加入偷花扣减**

找到 `ReqHarvestFlower` 的第二轮循环（约 line 276），在 `finalNum` 计算之后、构建 `harvestItems` 之前，加入：

```go
// 偷花扣减
stolenCount, _ := countPlotStolen(ctx, r.RoleID, plotID)
ownerMinKeep := gameconfig.GameConfig().TbFriendConfig.Get().OwnerMinKeepNum
finalNum = maxInt32(finalNum-int32(stolenCount), ownerMinKeep)
```

需要加一个 `maxInt32` 辅助函数：
```go
func maxInt32(a, b int32) int32 {
	if a > b { return a }
	return b
}
```

- [ ] **Step 2: 地块归空时清理 steal_record**

在收获后地块归空的逻辑处（`harvest_count >= finalTimes` → 设为 EMPTY 的地方），加：

```go
if plot.HarvestCount >= finalTimes {
	plot.State = int32(pb.PlotState_PLOT_EMPTY)
	plot.FlowerID = 0
	plot.HarvestCount = 0
	plot.StateTime = time.Time{}
	// 清理偷取记录
	_ = deletePlotStealRecords(ctx, r.RoleID, plotID)
}
```

- [ ] **Step 3: 确认 forceSave 已在 Task 4 中添加**

确保 `ReqHarvestFlower` 末尾已调用 `r.forceSave(ctx)`。

- [ ] **Step 4: 编译验证**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add src/apps/role/internal/logic/role_plot.go && git commit -m "feat(steal-flower): adjust harvest yield for stolen flowers and cleanup records"
```

---

### Task 8: Schema 初始化注册

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go`

- [ ] **Step 1: 找到现有 schema 初始化位置，添加 initStealSchema**

在 role_main.go 中搜索 `AutoMigrate` 或 `InitSchema`，找到模块初始化的位置。添加 `initStealSchema(ctx)` 调用。

- [ ] **Step 2: 编译并最终验证**

```bash
go build ./...
```

- [ ] **Step 3: Final commit**

```bash
git add -A && git commit -m "feat(steal-flower): register steal_record schema migration"
```

---

## Spec Coverage Checklist

| 设计要求 | 对应 Task |
|----------|-----------|
| 查看好友花园 ReqFriendPlotInfo | Task 5 |
| 偷花 ReqStealFlower | Task 6 |
| 好友关系检查 | Task 5, 6 (isFriend) |
| 地块可收获判断 | Task 5, 6 (getPlotState) |
| 每好友每日偷取上限 | Task 6 (countDailySteal) |
| 单花被偷上限 | Task 6 (countPlotStolen) |
| 主人保底收益 | Task 7 (ownerMinKeep) |
| 偷花者获得奖励入背包 | Task 6 (SaveGoods) |
| 收获时扣减被偷产量 | Task 7 |
| 地块归空清理 steal_record | Task 7 |
| FOR UPDATE 并发控制 | Task 6 (偷花锁), Task 7 (收获锁由 forceSave 提供) |
| 地块操作立即落盘 | Task 4 |
| 配置字段 | Task 2 |
| Proto 定义 | Task 1 |
| can_steal 字段 | Task 1 (proto), Task 5 (填充) |
