# 鲜花系统 MVP 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现鲜花系统 MVP——RoleFlower 模块，包含培育（解锁→培育→收获）完整流程、proto 接口、GM 命令、单元测试。

**Architecture:** RoleFlower 作为 RoleModule 注册到 roleModules，单表 `role_flower` 用 jsonb map 存花数据（同 RoleBag 模式）。被动检查培育完成，BREED_DONE 仅在响应时生成。

**Tech Stack:** Go 1.25, protobuf, GORM (PostgreSQL jsonb), gomonkey (测试 mock)

---

### Task 1: 新建 breed.proto 并生成 Go 代码

**Files:**
- Create: `protocol/client/breed.proto`

- [ ] **Step 1: 创建 proto 文件**

```protobuf
// ID: 23001~23099
syntax = "proto3";
option go_package="./pb;pb";
package galaxy.protocol;

import "msg_options.proto";

enum FlowerStatus {
    FLOWER_UNLOCKED   = 0;
    FLOWER_BREEDING   = 1;
    FLOWER_BREED_DONE = 2;
    FLOWER_HARVESTED  = 3;
}

message PFlowerState {
    int32 flower_id = 1;
    FlowerStatus status = 2;
    int64 state_time = 3;
}

message ReqBreedInfo {
    option (msg_id) = 23001;
}

message RspBreedInfo {
    option (msg_id) = 23002;
    repeated PFlowerState flowers = 1;
}

message ReqStartBreed {
    option (msg_id) = 23003;
    int32 flower_id = 1;
}

message RspStartBreed {
    option (msg_id) = 23004;
}

message ReqFinishBreed {
    option (msg_id) = 23005;
    int32 flower_id = 1;
}

message RspFinishBreed {
    option (msg_id) = 23006;
}
```

- [ ] **Step 2: 生成 protobuf Go 代码**

Run: `make pb`
Expected: 无报错，生成 `protocol/pb/breed.pb.go`

- [ ] **Step 3: 验证生成文件存在**

Run: `ls protocol/pb/breed.pb.go`
Expected: 文件存在

- [ ] **Step 4: 提交**

```bash
git add protocol/client/breed.proto protocol/pb/breed.pb.go
git commit -m "feat(flower): 添加鲜花系统 proto 定义 (23001-23006)"
```

---

### Task 2: 新建 role_flower.go — 数据模型和模块骨架

**Files:**
- Create: `src/apps/role/internal/logic/role_flower.go`

- [ ] **Step 1: 创建文件，包含 FlowerMap Value/Scan、RoleFlowerState、RoleFlower 骨架**

```go
package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"time"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/pkg/errors"
)

var (
	ErrFlowerLocked     = errors.New("flower not unlocked")
	ErrFlowerBreedBusy  = errors.New("another flower is breeding")
	ErrFlowerNotBreeding = errors.New("flower is not breeding")
	ErrFlowerNotDone    = errors.New("breed not finished yet")
)

// ========== 数据模型 ==========

type FlowerData struct {
	FlowerID  int32     `json:"flower_id"`
	Status    int32     `json:"status"`
	StateTime time.Time `json:"state_time"`
}

type FlowerMap map[int32]*FlowerData

func (m FlowerMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *FlowerMap) Scan(value interface{}) error {
	if value == nil {
		*m = make(FlowerMap)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("invalid type for FlowerMap")
	}
	var flowerMap map[int32]*FlowerData
	if err := json.Unmarshal(bytes, &flowerMap); err != nil {
		return err
	}
	*m = FlowerMap(flowerMap)
	return nil
}

type RoleFlowerState struct {
	RolePersistState
	Flowers FlowerMap `gorm:"column:flowers;type:jsonb"`
}

func (RoleFlowerState) TableName() string { return "role_flower" }

// ========== 模块 ==========

type RoleFlower struct {
	RoleModule
	RoleFlowerState
}

var _ IRoleModule = (*RoleFlower)(nil)

func (r *RoleFlower) PersistState() IPersistState {
	return &r.RoleFlowerState
}

func (r *RoleFlower) OnModInit(ctx context.Context) error {
	if r.Flowers == nil {
		r.Flowers = make(FlowerMap)
	}
	return nil
}

// ========== 公开方法 ==========

func (r *RoleFlower) UnlockFlower(flowerID int32) {
	r.Flowers[flowerID] = &FlowerData{
		FlowerID: flowerID,
		Status:   int32(pb.FlowerStatus_UNLOCKED),
	}
	r.MarkDirty()
}

func (r *RoleFlower) FindBreeding() *FlowerData {
	for _, f := range r.Flowers {
		if f.Status == int32(pb.FlowerStatus_BREEDING) {
			return f
		}
	}
	return nil
}

// ========== Proto Handler ==========

func (r *RoleFlower) ReqBreedInfo(ctx context.Context, req *pb.ReqBreedInfo) (*pb.RspBreedInfo, error) {
	now := time.Now()
	rsp := &pb.RspBreedInfo{Flowers: []*pb.PFlowerState{}}
	for _, f := range r.Flowers {
		status := f.Status
		// 被动检查：BREEDING 且已到完成时间 → 响应中返回 BREED_DONE
		if status == int32(pb.FlowerStatus_BREEDING) && now.After(f.StateTime) {
			status = int32(pb.FlowerStatus_BREED_DONE)
		}
		rsp.Flowers = append(rsp.Flowers, &pb.PFlowerState{
			FlowerId:  f.FlowerID,
			Status:    pb.FlowerStatus(status),
			StateTime: f.StateTime.Unix(),
		})
	}
	return rsp, nil
}

func (r *RoleFlower) ReqStartBreed(ctx context.Context, req *pb.ReqStartBreed) (*pb.RspStartBreed, error) {
	flowerID := req.FlowerId

	// 校验已解锁
	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}

	// 校验没有正在培育的花
	if r.FindBreeding() != nil {
		return nil, ErrFlowerBreedBusy
	}

	// 读配置
	cfg := gameconfig.GameConfig().TbFlower.Get(flowerID)
	if cfg == nil {
		return nil, errors.Errorf("flower config not found: %d", flowerID)
	}

	// 校验并扣除材料
	if !r.Role.Bag.CheckGoods(cfg.BreedCost) {
		return nil, bag.ErrGoodNotEnough
	}
	if err := r.Role.Bag.SaveGoods(ctx, cfg.BreedCost, nil, "breed"); err != nil {
		return nil, err
	}

	// 更新状态
	flower.Status = int32(pb.FlowerStatus_BREEDING)
	flower.StateTime = time.Now().Add(time.Duration(cfg.BreedTime) * time.Second)
	r.MarkDirty()

	return &pb.RspStartBreed{}, nil
}

func (r *RoleFlower) ReqFinishBreed(ctx context.Context, req *pb.ReqFinishBreed) (*pb.RspFinishBreed, error) {
	flowerID := req.FlowerId

	// 校验花存在且正在培育
	flower, ok := r.Flowers[flowerID]
	if !ok {
		return nil, ErrFlowerLocked
	}
	if flower.Status != int32(pb.FlowerStatus_BREEDING) {
		return nil, ErrFlowerNotBreeding
	}

	// 校验培育完成
	if time.Now().Before(flower.StateTime) {
		return nil, ErrFlowerNotDone
	}

	// 发放种子进背包
	if err := r.Role.Bag.SaveGoods(ctx, nil, []*gamecfg.GardenGoodStack{
		MakeGoodStack(int(flowerID), 1),
	}, "breed"); err != nil {
		return nil, err
	}

	// 更新状态
	flower.Status = int32(pb.FlowerStatus_HARVESTED)
	r.MarkDirty()

	return &pb.RspFinishBreed{}, nil
}
```

- [ ] **Step 2: 验证编译**

Run: `go build ./src/apps/role/...`
Expected: 编译失败（因为 roleModules 还没加 Flower 字段）——这是预期的，Task 3 会修复。

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_flower.go
git commit -m "feat(flower): 添加 RoleFlower 模块骨架和培育逻辑"
```

---

### Task 3: 注册 RoleFlower 到 roleModules

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go` (roleModules struct)
- Modify: `src/apps/role/internal/logic/role_schema.go` (AutoMigrate)

- [ ] **Step 1: 在 roleModules 中添加 Flower 字段**

在 `src/apps/role/internal/logic/role_main.go` 的 `roleModules` 结构体中，在 `GM *RoleGM` 前添加：

```go
type roleModules struct {
	Bag    *RoleBag
	Basic  *RoleBasic
	Public *RolePublic
	Extra  *RoleExtra
	Flower *RoleFlower
	GM     *RoleGM
}
```

- [ ] **Step 2: 在 AutoMigrate 中注册 RoleFlowerState**

在 `src/apps/role/internal/logic/role_schema.go` 的 `AutoMigrate` 调用中添加 `&RoleFlowerState{}`：

```go
if err := db.AutoMigrate(
	&RoleAccount{},
	&RoleBasicState{},
	&RoleBagState{},
	&RoleExtraPersistState{},
	&RolePublicState{},
	&RoleFlowerState{},
); err != nil {
```

- [ ] **Step 3: 验证编译通过**

Run: `go build ./src/apps/role/...`
Expected: 编译成功，无报错

- [ ] **Step 4: 提交**

```bash
git add src/apps/role/internal/logic/role_main.go src/apps/role/internal/logic/role_schema.go
git commit -m "feat(flower): 注册 RoleFlower 到 roleModules 和 schema"
```

---

### Task 4: 添加 GM 命令

**Files:**
- Modify: `src/apps/role/internal/logic/role_gm.go`

- [ ] **Step 1: 在 role_gm.go 末尾添加两个 GM 命令**

在 `role_gm.go` 的 `RemoveGoods` 方法后添加：

```go
// UnlockFlower 解锁花的培育权限
// 用法: unlock_flower [花ID]
// 示例: unlock_flower 101
func (r *RoleGM) UnlockFlower(flowerID int) error {
	r.Role.Flower.UnlockFlower(int32(flowerID))
	return nil
}

// FinishBreedGM 立即完成当前培育（将完成时间设为过去）
// 用法: finish_breed
// 示例: finish_breed
func (r *RoleGM) FinishBreedGM() error {
	breeding := r.Role.Flower.FindBreeding()
	if breeding == nil {
		return fmt.Errorf("no flower is breeding")
	}
	breeding.StateTime = time.Now().Add(-time.Second)
	r.Role.Flower.MarkDirty()
	return nil
}
```

注意：`role_gm.go` 当前 import 中没有 `"time"` 和 `"github.com/pkg/errors"`。需要添加 `"time"` 到 import，`errors.New` 改用 `fmt.Errorf`（已有 `fmt` import）。

- [ ] **Step 2: 验证编译通过**

Run: `go build ./src/apps/role/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add src/apps/role/internal/logic/role_gm.go
git commit -m "feat(flower): 添加 unlock_flower / finish_breed GM 命令"
```

---

### Task 5: 单元测试

**Files:**
- Create: `src/apps/role/internal/logic/role_flower_test.go`

- [ ] **Step 1: 创建测试文件**

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
	"gserver/src/apps/role/internal/logic/bag"
	proto "google.golang.org/protobuf/proto"

	"github.com/agiledragon/gomonkey/v2"
)

// ========== test setup ==========

var flowerTestCfgInited bool

func initFlowerTestConfig(t *testing.T) {
	t.Helper()
	if flowerTestCfgInited {
		return
	}
	gc := gameconfig.NewGameConfig()
	// 物品表：培育材料 + 花种子
	items := []map[string]interface{}{
		{"id": float64(1001), "name": "soil", "desc": "", "major_type": float64(2),
			"sub_type": float64(20), "quality": float64(1), "price": float64(10),
			"max_stack": float64(99), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(2001), "name": "fertilizer", "desc": "", "major_type": float64(2),
			"sub_type": float64(20), "quality": float64(1), "price": float64(20),
			"max_stack": float64(99), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": true},
		{"id": float64(101), "name": "rose_seed", "desc": "", "major_type": float64(2),
			"sub_type": float64(10), "quality": float64(1), "price": float64(0),
			"max_stack": float64(99), "icon": "", "use_type": float64(1),
			"use_param": "", "can_sell": false},
	}
	tbItem, err := gamecfg.NewGardenTbItem(items)
	if err != nil {
		t.Fatal(err)
	}
	// 鲜花表
	flowers := []map[string]interface{}{
		{"id": float64(101), "name": "rose", "quality": float64(1),
			"breed_time": float64(10),
			"breed_cost": []interface{}{
				map[string]interface{}{"id": float64(1001), "num": float64(2)},
				map[string]interface{}{"id": float64(2001), "num": float64(1)},
			}},
		{"id": float64(102), "name": "sunflower", "quality": float64(1),
			"breed_time": float64(20),
			"breed_cost": []interface{}{
				map[string]interface{}{"id": float64(1001), "num": float64(1)},
			}},
	}
	tbFlower, err := gamecfg.NewGardenTbFlower(flowers)
	if err != nil {
		t.Fatal(err)
	}
	gc.Tables = &gamecfg.Tables{TbItem: tbItem, TbFlower: tbFlower}
	flowerTestCfgInited = true
}

func setupTestFlower(t *testing.T) *RoleFlower {
	t.Helper()
	initFlowerTestConfig(t)
	patch := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, _ proto.Message) {},
	)
	t.Cleanup(patch.Reset)
	flower := &RoleFlower{
		RoleModule:     RoleModule{Role: &RoleMain{}},
		RoleFlowerState: RoleFlowerState{Flowers: make(FlowerMap)},
	}
	// 给背包模块初始化（StartBreed/FinishBreed 需要访问 Bag）
	flower.Role.(*RoleMain).Bag = &RoleBag{
		RoleModule:   RoleModule{Role: flower.Role.(*RoleMain)},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	return flower
}

func setupTestFlowerWithMaterials(t *testing.T) *RoleFlower {
	t.Helper()
	f := setupTestFlower(t)
	bag := f.Role.(*RoleMain).Bag
	bag.Goods[1001] = bag.BagGood{GoodID: 1001, Num: 100}
	bag.Goods[2001] = bag.BagGood{GoodID: 2001, Num: 100}
	return f
}

// ========== UnlockFlower ==========

func TestFlowerUnlock(t *testing.T) {
	f := setupTestFlower(t)
	f.UnlockFlower(101)

	if _, ok := f.Flowers[101]; !ok {
		t.Fatal("expected flower 101 in map")
	}
	if f.Flowers[101].Status != int32(pb.FlowerStatus_UNLOCKED) {
		t.Fatalf("expected UNLOCKED, got %d", f.Flowers[101].Status)
	}
	if !f.IsDirty() {
		t.Fatal("expected dirty")
	}
}

// ========== FindBreeding ==========

func TestFindBreeding_None(t *testing.T) {
	f := setupTestFlower(t)
	if f.FindBreeding() != nil {
		t.Fatal("expected nil when no flower breeding")
	}
}

func TestFindBreeding_OneBreeding(t *testing.T) {
	f := setupTestFlower(t)
	f.UnlockFlower(101)
	f.Flowers[101].Status = int32(pb.FlowerStatus_BREEDING)
	if found := f.FindBreeding(); found == nil || found.FlowerID != 101 {
		t.Fatal("expected to find flower 101 breeding")
	}
}

// ========== ReqStartBreed ==========

func TestStartBreed_Success(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.UnlockFlower(101)
	f.ClearDirty()

	_, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 101})
	if err != nil {
		t.Fatal(err)
	}
	if f.Flowers[101].Status != int32(pb.FlowerStatus_BREEDING) {
		t.Fatalf("expected BREEDING, got %d", f.Flowers[101].Status)
	}
	if !f.IsDirty() {
		t.Fatal("expected dirty")
	}
	// 材料应被扣除
	bagModule := f.Role.(*RoleMain).Bag
	if bagModule.Goods[1001].Num != 98 {
		t.Fatalf("expected soil num 98, got %d", bagModule.Goods[1001].Num)
	}
}

func TestStartBreed_NotUnlocked(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	_, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 101})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

func TestStartBreed_AlreadyBreeding(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.UnlockFlower(101)
	f.UnlockFlower(102)
	f.Flowers[101].Status = int32(pb.FlowerStatus_BREEDING)

	_, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 102})
	if !errors.Is(err, ErrFlowerBreedBusy) {
		t.Fatalf("expected ErrFlowerBreedBusy, got %v", err)
	}
}

func TestStartBreed_MaterialNotEnough(t *testing.T) {
	f := setupTestFlower(t) // 没有材料
	f.UnlockFlower(101)

	_, err := f.ReqStartBreed(context.Background(), &pb.ReqStartBreed{FlowerId: 101})
	// 材料不足，Bag.CheckGoods 返回 false，应返回 ErrGoodNotEnough
	if err == nil {
		t.Fatal("expected error for insufficient materials")
	}
}

// ========== ReqFinishBreed ==========

func TestFinishBreed_Success(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.UnlockFlower(101)
	f.Flowers[101].Status = int32(pb.FlowerStatus_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(-time.Second) // 已完成

	_, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 101})
	if err != nil {
		t.Fatal(err)
	}
	if f.Flowers[101].Status != int32(pb.FlowerStatus_HARVESTED) {
		t.Fatalf("expected HARVESTED, got %d", f.Flowers[101].Status)
	}
	// 种子应进背包
	bagModule := f.Role.(*RoleMain).Bag
	if bagModule.Goods[101].Num != 1 {
		t.Fatalf("expected seed num 1, got %d", bagModule.Goods[101].Num)
	}
}

func TestFinishBreed_NotBreeding(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.UnlockFlower(101)

	_, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 101})
	if !errors.Is(err, ErrFlowerNotBreeding) {
		t.Fatalf("expected ErrFlowerNotBreeding, got %v", err)
	}
}

func TestFinishBreed_NotDone(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	f.UnlockFlower(101)
	f.Flowers[101].Status = int32(pb.FlowerStatus_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(time.Hour) // 还没完成

	_, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 101})
	if !errors.Is(err, ErrFlowerNotDone) {
		t.Fatalf("expected ErrFlowerNotDone, got %v", err)
	}
}

func TestFinishBreed_NotUnlocked(t *testing.T) {
	f := setupTestFlowerWithMaterials(t)
	_, err := f.ReqFinishBreed(context.Background(), &pb.ReqFinishBreed{FlowerId: 999})
	if !errors.Is(err, ErrFlowerLocked) {
		t.Fatalf("expected ErrFlowerLocked, got %v", err)
	}
}

// ========== ReqBreedInfo ==========

func TestBreedInfo_BreedDone(t *testing.T) {
	f := setupTestFlower(t)
	f.UnlockFlower(101)
	f.Flowers[101].Status = int32(pb.FlowerStatus_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(-time.Second)

	rsp, err := f.ReqBreedInfo(context.Background(), &pb.ReqBreedInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Flowers) != 1 {
		t.Fatalf("expected 1 flower, got %d", len(rsp.Flowers))
	}
	if rsp.Flowers[0].Status != pb.FlowerStatus_BREED_DONE {
		t.Fatalf("expected BREED_DONE, got %v", rsp.Flowers[0].Status)
	}
}

func TestBreedInfo_StillBreeding(t *testing.T) {
	f := setupTestFlower(t)
	f.UnlockFlower(101)
	f.Flowers[101].Status = int32(pb.FlowerStatus_BREEDING)
	f.Flowers[101].StateTime = time.Now().Add(time.Hour)

	rsp, err := f.ReqBreedInfo(context.Background(), &pb.ReqBreedInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Flowers[0].Status != pb.FlowerStatus_BREEDING {
		t.Fatalf("expected BREEDING, got %v", rsp.Flowers[0].Status)
	}
}

func TestBreedInfo_Empty(t *testing.T) {
	f := setupTestFlower(t)
	rsp, err := f.ReqBreedInfo(context.Background(), &pb.ReqBreedInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Flowers) != 0 {
		t.Fatalf("expected 0 flowers, got %d", len(rsp.Flowers))
	}
}

// ========== FlowerMap Value/Scan ==========

func TestFlowerMap_ScanNil(t *testing.T) {
	var m FlowerMap
	if err := m.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatal("expected empty map")
	}
}

func TestFlowerMap_ValueAndScan(t *testing.T) {
	original := FlowerMap{
		101: {FlowerID: 101, Status: 1},
	}
	val, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}
	var scanned FlowerMap
	if err := scanned.Scan(val); err != nil {
		t.Fatal(err)
	}
	if scanned[101].Status != 1 {
		t.Fatalf("expected status 1, got %d", scanned[101].Status)
	}
}
```

注意：测试代码中 `bag.BagGood` 需要确认实际类型。查看 `role_bag_test.go` 中的用法是 `bag.BagGood{GoodID: 1001, Num: 100}`，此处保持一致。

- [ ] **Step 2: 运行测试**

Run: `go test ./src/apps/role/internal/logic/ -v -run TestFlower -gcflags=all=-l -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 运行已有 bag 测试确认无回归**

Run: `go test ./src/apps/role/internal/logic/ -v -run TestBag -gcflags=all=-l -count=1`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add src/apps/role/internal/logic/role_flower_test.go
git commit -m "test(flower): 添加 RoleFlower 单元测试"
```

---

### Task 6: 编写系统文档

**Files:**
- Create: `docs/system/flower.md`

- [ ] **Step 1: 创建文档**

```markdown
# 鲜花系统

## 概述

鲜花系统（`RoleFlower`）管理玩家的鲜花数据，培育是首个子功能。模块以"花"为核心实体设计，后续升级、种花等功能在 `FlowerData` 上扩展字段。

## 数据结构

### FlowerData（jsonb map，key = flowerID）

| 字段 | 类型 | 说明 |
|------|------|------|
| flower_id | int32 | 花ID |
| status | int32 | FlowerStatus 枚举 |
| state_time | time | BREEDING 时为完成时间 |

### 状态流转

```
(不存在) → GM解锁 → UNLOCKED → StartBreed → BREEDING → FinishBreed → HARVESTED
```

`BREED_DONE` 不持久化，仅在 `ReqBreedInfo` 响应时由服务器计算生成。

## 配置表

- `TbFlower`：花配置（breed_time, breed_cost）
- `TbItem`：材料和种子物品属性

## Proto 接口

| 消息 | ID | 说明 |
|------|----|------|
| ReqBreedInfo / RspBreedInfo | 23001-23002 | 查询培育状态 |
| ReqStartBreed / RspStartBreed | 23003-23004 | 开始培育 |
| ReqFinishBreed / RspFinishBreed | 23005-23006 | 收获成果 |

## GM 命令

| 命令 | 说明 |
|------|------|
| unlock_flower [花ID] | 解锁花 |
| finish_breed | 立即完成当前培育 |

## 错误码

| 变量 | 说明 |
|------|------|
| ErrFlowerLocked | 花未解锁 |
| ErrFlowerBreedBusy | 已有花在培育中 |
| ErrFlowerNotBreeding | 花未在培育中 |
| ErrFlowerNotDone | 培育尚未完成 |

## 代码位置

| 文件 | 说明 |
|------|------|
| src/apps/role/internal/logic/role_flower.go | 模块主逻辑 |
| protocol/client/breed.proto | Proto 定义 |
```

- [ ] **Step 2: 提交**

```bash
git add docs/system/flower.md
git commit -m "docs: 添加鲜花系统文档"
```
