# 鲜花系统 MVP 设计

## 概述

鲜花系统（`RoleFlower`）是鲜花领域的统一模块，培育只是其上的第一个功能。后续鲜花升级、种花等功能都在此模块上扩展。模块名、表名、数据结构均以"花"为核心，不以"培育"命名。

## 决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 模块命名 | `RoleFlower`（非 `RoleBreed`） | 培育是花的子功能，后续升级/种花都挂在花上 |
| 数据存储 | 单表 `role_flower`，花数据存 jsonb map | 和 `RoleBag` 同模式，单行持久化 |
| 完成检查 | 被动检查（`now >= StateTime`） | 不设定时器，客户端对比时间判断 |
| GM 命令 | 复用现有 proto GM 模块 | 不新建 HTTP 端口 |
| `BREED_DONE` | 仅响应生成，不持久化 | 服务器不主动切换状态 |
| `StateTime` | 存培育完成时间（非开始时间） | 客户端直接 `state_time - now` 做倒计时 |

## 数据模型

### 表 `role_flower`

```go
// FlowerData 单朵花的运行时数据，序列化为 jsonb
type FlowerData struct {
    FlowerID  int32     `json:"flower_id"`
    Status    int32     `json:"status"`       // FlowerStatus 枚举值
    StateTime time.Time `json:"state_time"`   // BREEDING 时为培育完成时间
}

type FlowerMap map[int32]*FlowerData

// Value/Scan 实现 database/sql 驱动接口，JSON 序列化
func (m FlowerMap) Value() (driver.Value, error) { ... }
func (m *FlowerMap) Scan(value interface{}) error { ... }

type RoleFlowerState struct {
    RolePersistState
    Flowers FlowerMap `gorm:"column:flowers;type:jsonb"`
}

func (RoleFlowerState) TableName() string { return "role_flower" }
```

### 状态枚举（proto 定义）

```protobuf
enum FlowerStatus {
    FLOWER_UNLOCKED   = 0;  // 已解锁，可培育
    FLOWER_BREEDING   = 1;  // 培育中
    FLOWER_BREED_DONE = 2;  // 培育完成，待收获（仅响应，不持久化）
    FLOWER_HARVESTED  = 3;  // 已收获，可种植
}
```

持久化状态：`UNLOCKED` → `BREEDING` → `HARVESTED`。`BREED_DONE` 仅在 `GetBreedInfo` 响应时由服务器生成。

### 模块结构

```go
type RoleFlower struct {
    RoleModule
    RoleFlowerState
}
```

注册到 `roleModules.Flower *RoleFlower`，自动反射注册。

## Proto 接口

ID 段 `23001~23099`，文件 `protocol/client/breed.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| `ReqBreedInfo` | 23001 | C→S | 查询培育状态 |
| `RspBreedInfo` | 23002 | S→C | 返回所有已解锁花的状态 |
| `ReqStartBreed` | 23003 | C→S | 开始培育 |
| `RspStartBreed` | 23004 | S→C | 结果 |
| `ReqFinishBreed` | 23005 | C→S | 收获培育成果 |
| `RspFinishBreed` | 23006 | S→C | 结果 |

### 消息定义

```protobuf
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
    int64 state_time = 3;   // Unix 时间戳
}

// 查询
message ReqBreedInfo { option (msg_id) = 23001; }
message RspBreedInfo {
    option (msg_id) = 23002;
    repeated PFlowerState flowers = 1;
}

// 开始培育
message ReqStartBreed {
    option (msg_id) = 23003;
    int32 flower_id = 1;
}
message RspStartBreed { option (msg_id) = 23004; }

// 收获
message ReqFinishBreed {
    option (msg_id) = 23005;
    int32 flower_id = 1;
}
message RspFinishBreed { option (msg_id) = 23006; }
```

## 核心逻辑

### GetBreedInfo

1. 遍历 `Flowers` map 构造 `[]PFlowerState`
2. 对 `Status == BREEDING` 的花，如果 `now >= StateTime`，响应中改为 `BREED_DONE`
3. 返回给客户端

### StartBreed(flower_id)

1. 校验 `Flowers[flower_id]` 存在（已解锁）
2. 校验 map 中没有花的 `Status == pb.FlowerStatus_BREEDING`
3. `TbFlower.Get(flower_id)` 读配置，`Bag.CheckGoods(breed_cost)` 检查材料
4. `Bag.SaveGoods` 扣除 `breed_cost` 材料
5. 设 `Status = pb.FlowerStatus_BREEDING, StateTime = now + BreedTime`
6. `MarkDirty`，返回成功

### FinishBreed(flower_id)

1. 校验 `Flowers[flower_id]` 存在且 `Status == pb.FlowerStatus_BREEDING`
2. 校验 `now >= StateTime`（未完成返回错误）
3. `Bag.SaveGoods` 添加种子 `MakeGoodStack(flower_id, 1)`
4. 设 `Status = pb.FlowerStatus_HARVESTED`
5. `MarkDirty`，返回成功

## GM 命令

在 `role_gm.go` 中添加，复用现有 go/doc 注释路由。

```go
// UnlockFlower 解锁花的培育权限
// 用法: unlock_flower [花ID]
// 示例: unlock_flower 101
func (r *RoleGM) UnlockFlower(flowerID int) error {
    // 通过 r.Role.Flower 访问鲜花模块，插入 FlowerData
}

// FinishBreedGM 立即完成当前培育
// 用法: finish_breed
// 示例: finish_breed
func (r *RoleGM) FinishBreedGM() error {
    // 找 BREEDING 的花，StateTime 设为过去时间
}
```

> GM 的 `FinishBreedGM` 直接将 BREEDING 花的 `StateTime` 设为 `time.Now().Add(-time.Second)`，玩家正常走 `FinishBreed` 流程领取即可。

## 配置表依赖

已就绪，无需新建：

- `TbFlower`：`Get(id)` 返回 `Id, Name, Quality, BreedTime, BreedCost`
- `TbItem`：材料物品和花种子物品的属性

## 文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `protocol/client/breed.proto` | 新建 | Proto 定义 |
| `src/apps/role/internal/logic/role_flower.go` | 新建 | 鲜花模块主逻辑 |
| `src/apps/role/internal/logic/role_flower_test.go` | 新建 | 单元测试 |
| `src/apps/role/internal/logic/role_gm.go` | 修改 | 添加 unlock_flower / finish_breed 命令 |
| `src/apps/role/internal/logic/role_main.go` | 修改 | roleModules 加 Flower 字段 |
| `src/apps/role/internal/logic/role_schema.go` | 修改 | AutoMigrate 加 RoleFlowerState |
| `docs/system/flower.md` | 新建 | 系统文档 |

## 错误码

| 变量 | 说明 |
|------|------|
| `ErrFlowerLocked` | 花未解锁 |
| `ErrFlowerBreedBusy` | 已有花在培育中 |
| `ErrFlowerNotBreeding` | 该花未在培育中 |
| `ErrFlowerNotDone` | 培育尚未完成 |

## MVP 不做的事

- 多队列培育
- 元宝加速/跳过
- 配方自然获取（商店/等级/成就）
- 培育动画效果
- 材料获取途径跳转
- 培育失败/变异
