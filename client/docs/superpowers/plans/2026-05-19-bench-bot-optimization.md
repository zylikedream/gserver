# Bench Bot Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild `cmd/bench/` to simulate real player behaviors — breeding, planting, harvesting, main tasks, resident orders — with config-driven bot types and state tracking.

**Architecture:** Hybrid config-driven approach. YAML defines bot type ratios and per-type scripts (sequence of game actions). Go implements action handlers (login, breed, plant, harvest, claim_task, submit_order, etc.) with access to per-bot BotState that auto-tracks inventory/tasks/plots/flowers from server responses.

**Tech Stack:** Go, protobuf/protoreflect, gopkg.in/yaml.v3, existing pkg/client SDK

---

### Task 1: Redesign config.go with bot types and script steps

**Files:**
- Modify: `cmd/bench/config.go` (full rewrite)
- Test: `cmd/bench/config_test.go`

**Config struct:**

```go
type Config struct {
    Addr           string          `yaml:"addr"`
    AccountPattern string          `yaml:"account_pattern"`
    TotalBots      int             `yaml:"total_bots"`
    StartupRate    int             `yaml:"startup_rate"`     // bots/sec, 0=instant
    ReportInterval time.Duration   `yaml:"report_interval"`
    LogFile        string          `yaml:"log_file"`
    BotTypes       []BotTypeConfig `yaml:"bot_types"`
    ChatMixin      *ChatMixinConfig `yaml:"chat_mixin,omitempty"`
}

type BotTypeConfig struct {
    ID     string        `yaml:"id"`
    Weight int           `yaml:"weight"`
    Script []ScriptStep  `yaml:"script"`
}

type ScriptStep struct {
    // Each step has exactly one key (the action name) and a map of args.
    // YAML: - login: {}
    //       - breed: {flower_id: 101}
    //       - wait_range: {min: 0, max: 5}
    Do   string
    Args map[string]interface{}
}

func (s *ScriptStep) UnmarshalYAML(value *yaml.Node) error {
    // Step: map with single key → key = Do, value = Args
    var m map[string]interface{}
    if err := value.Decode(&m); err != nil {
        return err
    }
    for k, v := range m {
        s.Do = k
        if v == nil {
            s.Args = map[string]interface{}{}
        } else {
            args, ok := v.(map[string]interface{})
            if !ok {
                return fmt.Errorf("script step %q: args must be a map", k)
            }
            s.Args = args
        }
        return nil // only first key
    }
    return fmt.Errorf("empty script step")
}

type ChatMixinConfig struct {
    Chance   float64  `yaml:"chance"`
    Channel  int32    `yaml:"channel"`
    Messages []string `yaml:"messages"`
}

// DurationRange stays the same
type DurationRange struct {
    Min time.Duration
    Max time.Duration
}
```

- [ ] **Step 1: Write failing config test**

Create `cmd/bench/config_test.go`:

```go
package main

import (
    "os"
    "testing"
    "time"
)

func TestLoadConfigMinimal(t *testing.T) {
    data := []byte(`
addr: "127.0.0.1:11086"
account_pattern: "test_%d"
total_bots: 100
bot_types:
  - id: newbie
    weight: 100
    script:
      - login: {}
      - wait_range: {min: 0, max: 5}
`)
    path := writeTempConfig(t, data)
    cfg, err := LoadConfig(path)
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Addr != "127.0.0.1:11086" {
        t.Errorf("addr = %q", cfg.Addr)
    }
    if cfg.TotalBots != 100 {
        t.Errorf("total = %d", cfg.TotalBots)
    }
    if len(cfg.BotTypes) != 1 {
        t.Fatalf("bot_types = %d", len(cfg.BotTypes))
    }
    if cfg.BotTypes[0].ID != "newbie" {
        t.Errorf("id = %q", cfg.BotTypes[0].ID)
    }
    if len(cfg.BotTypes[0].Script) != 2 {
        t.Fatalf("script steps = %d", len(cfg.BotTypes[0].Script))
    }
    if cfg.BotTypes[0].Script[0].Do != "login" {
        t.Errorf("step[0].Do = %q", cfg.BotTypes[0].Script[0].Do)
    }
}

func TestLoadConfigWithChatMixin(t *testing.T) {
    data := []byte(`
addr: "127.0.0.1:11086"
account_pattern: "test_%d"
total_bots: 100
bot_types:
  - id: newbie
    weight: 100
    script:
      - login: {}
chat_mixin:
  chance: 0.1
  channel: 1
  messages: ["hello", "world"]
`)
    path := writeTempConfig(t, data)
    cfg, err := LoadConfig(path)
    if err != nil {
        t.Fatal(err)
    }
    if cfg.ChatMixin == nil {
        t.Fatal("chat_mixin is nil")
    }
    if cfg.ChatMixin.Chance != 0.1 {
        t.Errorf("chance = %f", cfg.ChatMixin.Chance)
    }
    if cfg.ChatMixin.Channel != 1 {
        t.Errorf("channel = %d", cfg.ChatMixin.Channel)
    }
    if len(cfg.ChatMixin.Messages) != 2 {
        t.Errorf("messages = %d", len(cfg.ChatMixin.Messages))
    }
}

func writeTempConfig(t *testing.T, data []byte) string {
    t.Helper()
    f, err := os.CreateTemp("", "bench-*.yaml")
    if err != nil {
        t.Fatal(err)
    }
    if _, err := f.Write(data); err != nil {
        f.Close()
        os.Remove(f.Name())
        t.Fatal(err)
    }
    f.Close()
    t.Cleanup(func() { os.Remove(f.Name()) })
    return f.Name()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/zyr/workspace/hy_client && go test ./cmd/bench/ -run TestLoadConfigMinimal -v`
Expected: FAIL — `LoadConfig` not updated, `ScriptStep` not defined

- [ ] **Step 3: Implement new Config struct**

Rewrite `cmd/bench/config.go` with the struct above and updated `LoadConfig`:

```go
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read config: %w", err)
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, fmt.Errorf("parse config: %w", err)
    }
    if cfg.Addr == "" {
        return nil, fmt.Errorf("addr is required")
    }
    if cfg.AccountPattern == "" {
        return nil, fmt.Errorf("account_pattern is required")
    }
    if cfg.TotalBots <= 0 {
        return nil, fmt.Errorf("total_bots must be > 0")
    }
    if len(cfg.BotTypes) == 0 {
        return nil, fmt.Errorf("bot_types is required")
    }
    // Validate weights sum (but don't require 100 — we normalize)
    totalWeight := 0
    for _, bt := range cfg.BotTypes {
        if bt.Weight <= 0 {
            return nil, fmt.Errorf("bot_type %q: weight must be > 0", bt.ID)
        }
        if len(bt.Script) == 0 {
            return nil, fmt.Errorf("bot_type %q: script is empty", bt.ID)
        }
        totalWeight += bt.Weight
    }
    if totalWeight <= 0 {
        return nil, fmt.Errorf("total weight must be > 0")
    }
    if cfg.ReportInterval <= 0 {
        cfg.ReportInterval = 5 * time.Second
    }
    return &cfg, nil
}

func (cfg *Config) BotTypeFor(index int) *BotTypeConfig {
    // Determine bot type by index using weighted round-robin.
    // index is the bot's sequential number (0..TotalBots-1).
    totalWeight := 0
    for _, bt := range cfg.BotTypes {
        totalWeight += bt.Weight
    }
    pos := index % totalWeight
    for _, bt := range cfg.BotTypes {
        pos -= bt.Weight
        if pos < 0 {
            return &BotTypeConfig{
                ID:     bt.ID,
                Weight: bt.Weight,
                Script: bt.Script,
            }
        }
    }
    // Fallback (shouldn't reach here)
    return &BotTypeConfig{
        ID:     cfg.BotTypes[0].ID,
        Weight: cfg.BotTypes[0].Weight,
        Script: cfg.BotTypes[0].Script,
    }
}
```

Remove old fields: `Bots`, `Scenario`, `Action` struct, `LoadConfig` old validation for scenario.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/zyr/workspace/hy_client && go test ./cmd/bench/ -run "TestLoadConfig" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/config.go cmd/bench/config_test.go
git commit -m "refactor(bench): config-driven bot types and script steps"
```

---

### Task 2: Create state.go — per-bot state tracking

**Files:**
- Create: `cmd/bench/state.go`
- Test: `cmd/bench/state_test.go`

- [ ] **Step 1: Write state_test.go**

```go
package main

import (
    "testing"
    pb "hy_client/pb"
)

func TestBotStateUpdateBag(t *testing.T) {
    s := NewBotState()
    s.UpdateBag(&pb.RspBagInfo{
        Goods: []*pb.PGoodInfo{
            {PropId: 1, Num: 500},
            {PropId: 101, Num: 1},
        },
    })
    if s.Inventory[1] != 500 {
        t.Errorf("gold = %d, want 500", s.Inventory[1])
    }
    if s.Inventory[101] != 1 {
        t.Errorf("seed = %d, want 1", s.Inventory[101])
    }
}

func TestBotStateNotifyBagUpdate(t *testing.T) {
    s := NewBotState()
    s.Inventory[1] = 500
    s.UpdateBagNotify(&pb.NotifyBagUpdate{
        Goods: []*pb.PBagGoodUpdate{
            {PropId: 1, PreNum: 500, Num: 580},
            {PropId: 2001, PreNum: 0, Num: 2},
        },
    })
    if s.Inventory[1] != 580 {
        t.Errorf("gold = %d, want 580", s.Inventory[1])
    }
    if s.Inventory[2001] != 2 {
        t.Errorf("soil = %d, want 2", s.Inventory[2001])
    }
}

func TestBotStatePlots(t *testing.T) {
    s := NewBotState()
    s.UpdatePlots([]*pb.PPlotInfo{
        {PlotId: 1, State: pb.PlotState_PLOT_EMPTY},
    })
    empty := s.FindEmptyPlots()
    if len(empty) != 1 || empty[0] != 1 {
        t.Errorf("empty plots = %v, want [1]", empty)
    }

    s.UpdatePlots([]*pb.PPlotInfo{
        {PlotId: 1, State: pb.PlotState_PLOT_PLANTED, FlowerId: 101},
    })
    empty = s.FindEmptyPlots()
    if len(empty) != 0 {
        t.Errorf("empty plots = %v, want []", empty)
    }
}

func TestBotStateFlowers(t *testing.T) {
    s := NewBotState()
    s.UpdateFlowers([]*pb.PFlowerInfo{
        {FlowerId: 101, State: pb.FlowerState_FLOWER_BREED_DONE},
    })
    if !s.IsBreedDone(101) {
        t.Error("IsBreedDone(101) = false, want true")
    }
}

func TestBotStateTasks(t *testing.T) {
    s := NewBotState()
    s.UpdateTask(&pb.PMainTaskInfo{
        TaskId: 1003, Status: pb.MainTaskStatus_MAIN_TASK_CLAIMABLE,
    })
    if s.Tasks[1003] != 1 {
        t.Error("task 1003 should be claimable (status=1)")
    }
}
```

- [ ] **Step 2: Run test — should fail (no state.go yet)**

Run: `cd /home/zyr/workspace/hy_client && go test ./cmd/bench/ -run "TestBotState" -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Implement state.go**

```go
package main

import (
    pb "hy_client/pb"
)

type BotState struct {
    Inventory map[int32]int64         // prop_id → count
    Tasks     map[int32]int32          // task_id → status (0=progress, 1=claimable, 2=finished)
    Plots     map[int32]*PlotState     // plot_id → plot state
    Flowers   map[int32]*FlowerState   // flower_id → flower state
}

type PlotState struct {
    PlotID       int32
    FlowerID     int32
    State        int32   // 0=empty, 1=planted, 2=growing, 3=harvestable
    HarvestCount int32
    StateTime    int64   // unix timestamp
}

type FlowerState struct {
    FlowerID  int32
    State     int32   // 0=unlocked, 1=breeding, 2=breed_done, 3=harvested
    StateTime int64
    Level     int32
}

func NewBotState() *BotState {
    return &BotState{
        Inventory: make(map[int32]int64),
        Tasks:     make(map[int32]int32),
        Plots:     make(map[int32]*PlotState),
        Flowers:   make(map[int32]*FlowerState),
    }
}

// === Inventory updates ===

func (s *BotState) UpdateBag(rsp *pb.RspBagInfo) {
    for _, g := range rsp.Goods {
        s.Inventory[g.PropId] = g.Num
    }
}

func (s *BotState) UpdateBagNotify(notify *pb.NotifyBagUpdate) {
    for _, g := range notify.Goods {
        if g.Num == 0 {
            delete(s.Inventory, g.PropId)
        } else {
            s.Inventory[g.PropId] = g.Num
        }
    }
}

// === Task updates ===

func (s *BotState) UpdateTask(task *pb.PMainTaskInfo) {
    s.Tasks[task.TaskId] = int32(task.Status)
}

// === Plot updates ===

func (s *BotState) UpdatePlots(plots []*pb.PPlotInfo) {
    for _, p := range plots {
        s.Plots[p.PlotId] = &PlotState{
            PlotID:       p.PlotId,
            FlowerID:     p.FlowerId,
            State:        int32(p.State),
            HarvestCount: p.HarvestCount,
            StateTime:    p.StateTime,
        }
    }
}

func (s *BotState) FindEmptyPlots() []int32 {
    var ids []int32
    for id, p := range s.Plots {
        if p.State == 0 { // PLOT_EMPTY
            ids = append(ids, id)
        }
    }
    return ids
}

func (s *BotState) FindHarvestablePlots() []int32 {
    var ids []int32
    for id, p := range s.Plots {
        if p.State == 3 { // PLOT_HARVESTABLE
            ids = append(ids, id)
        }
    }
    return ids
}

// === Flower updates ===

func (s *BotState) UpdateFlowers(flowers []*pb.PFlowerInfo) {
    for _, f := range flowers {
        s.Flowers[f.FlowerId] = &FlowerState{
            FlowerID:  f.FlowerId,
            State:     int32(f.State),
            StateTime: f.StateTime,
            Level:     f.Level,
        }
    }
}

func (s *BotState) IsBreedDone(flowerID int32) bool {
    f, ok := s.Flowers[flowerID]
    return ok && f.State >= 2 // BREED_DONE or HARVESTED
}
```

- [ ] **Step 4: Run tests — should pass**

Run: `cd /home/zyr/workspace/hy_client && go test ./cmd/bench/ -run "TestBotState" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/state.go cmd/bench/state_test.go
git commit -m "feat(bench): per-bot state tracking for inventory/tasks/plots/flowers"
```

---

### Task 3: Create actions.go — game action handlers

**Files:**
- Create: `cmd/bench/actions.go`
- Create: `cmd/bench/actions_test.go`

- [ ] **Step 1: Write failing test**

```go
package main

import (
    "testing"
)

func TestParseIntArg(t *testing.T) {
    args := map[string]interface{}{"flower_id": 101}
    v := getIntArg(args, "flower_id")
    if v != 101 {
        t.Errorf("got %d, want 101", v)
    }
}

func TestParseIntSliceArg(t *testing.T) {
    args := map[string]interface{}{"plot_ids": []interface{}{1, 2, 3}}
    v := getIntSliceArg(args, "plot_ids")
    if len(v) != 3 || v[0] != 1 || v[1] != 2 || v[2] != 3 {
        t.Errorf("got %v, want [1 2 3]", v)
    }
}

func TestParseFloatArg(t *testing.T) {
    args := map[string]interface{}{"min": 0.0, "max": 5.0}
    min := getFloatArg(args, "min")
    max := getFloatArg(args, "max")
    if min != 0 || max != 5 {
        t.Errorf("got min=%f max=%f", min, max)
    }
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /home/zyr/workspace/hy_client && go test ./cmd/bench/ -run "TestParseArg" -v`
Expected: FAIL

- [ ] **Step 3: Implement actions.go**

```go
package main

import (
    "fmt"
    "math/rand"
    "time"

    "hy_client/pb"
    "hy_client/pkg/client"
)

type BotActions struct {
    client *client.Client
    state  *BotState
    log    *BotLogger
}

func NewBotActions(cl *client.Client, state *BotState, log *BotLogger) *BotActions {
    return &BotActions{client: cl, state: state, log: log}
}

// === Arg helpers ===

func getStringArg(args map[string]interface{}, key string) string {
    if v, ok := args[key]; ok {
        if s, ok := v.(string); ok {
            return s
        }
    }
    return ""
}

func getIntArg(args map[string]interface{}, key string) int32 {
    if v, ok := args[key]; ok {
        switch n := v.(type) {
        case int:
            return int32(n)
        case int32:
            return n
        case float64:
            return int32(n)
        }
    }
    return 0
}

func getIntSliceArg(args map[string]interface{}, key string) []int32 {
    if v, ok := args[key]; ok {
        if list, ok := v.([]interface{}); ok {
            result := make([]int32, len(list))
            for i, item := range list {
                switch n := item.(type) {
                case int:
                    result[i] = int32(n)
                case float64:
                    result[i] = int32(n)
                }
            }
            return result
        }
    }
    return nil
}

func getFloatArg(args map[string]interface{}, key string) float64 {
    if v, ok := args[key]; ok {
        switch n := v.(type) {
        case float64:
            return n
        case int:
            return float64(n)
        }
    }
    return 0
}

func getDurationArg(args map[string]interface{}, key string) time.Duration {
    // Supports both number (seconds) and string (like "5s")
    if v, ok := args[key]; ok {
        switch n := v.(type) {
        case float64:
            return time.Duration(n) * time.Second
        case int:
            return time.Duration(n) * time.Second
        case string:
            d, err := time.ParseDuration(n)
            if err == nil {
                return d
            }
        }
    }
    return 0
}

// === Action handlers ===
// Each returns error only for unrecoverable failures (network, invalid params).
// Game-level failures (insufficient resources) are logged and return nil.

func (a *BotActions) Login(args map[string]interface{}) error {
    if err := a.client.Connect(); err != nil {
        return fmt.Errorf("connect: %w", err)
    }
    if _, err := a.client.Handshake(); err != nil {
        return fmt.Errorf("handshake: %w", err)
    }
    if _, err := a.client.Login(); err != nil {
        return fmt.Errorf("login: %w", err)
    }
    // Pull initial state
    a.pullInitialState()
    return nil
}

func (a *BotActions) pullInitialState() {
    // Best-effort: pull bag, flower, plot, task info in parallel-ish sequence
    if rsp, err := a.client.RequestWithResponse(&pb.ReqBagInfo{}); err == nil {
        if bag, ok := rsp.(*pb.RspBagInfo); ok {
            a.state.UpdateBag(bag)
        }
    }
    if rsp, err := a.client.RequestWithResponse(&pb.ReqFlowerInfo{}); err == nil {
        if f, ok := rsp.(*pb.RspFlowerInfo); ok {
            a.state.UpdateFlowers(f.Flowers)
        }
    }
    if rsp, err := a.client.RequestWithResponse(&pb.ReqPlotInfo{}); err == nil {
        if p, ok := rsp.(*pb.RspPlotInfo); ok {
            a.state.UpdatePlots(p.Plots)
        }
    }
    if rsp, err := a.client.RequestWithResponse(&pb.ReqMainTaskInfo{}); err == nil {
        if t, ok := rsp.(*pb.RspMainTaskInfo); ok {
            a.state.UpdateTask(t.Task)
        }
    }
}

func (a *BotActions) Breed(args map[string]interface{}) error {
    flowerID := getIntArg(args, "flower_id")
    _, err := a.client.RequestWithResponse(&pb.ReqFlowerStartBreed{FlowerId: flowerID})
    if err != nil {
        return fmt.Errorf("start_breed flower=%d: %w", flowerID, err)
    }
    return nil
}

func (a *BotActions) WaitForBreed(args map[string]interface{}) error {
    // Wait based on breed config: 10s + extra_max jitter
    extra := getIntArg(args, "extra_max")
    base := 10 * time.Second
    jitter := time.Duration(0)
    if extra > 0 {
        jitter = time.Duration(rand.Int63n(int64(extra)+1)) * time.Second
    }
    time.Sleep(base + jitter)
    return nil
}

func (a *BotActions) FinishBreed(args map[string]interface{}) error {
    flowerID := getIntArg(args, "flower_id")
    rsp, err := a.client.RequestWithResponse(&pb.ReqFlowerFinishBreed{FlowerId: flowerID})
    if err != nil {
        return fmt.Errorf("finish_breed flower=%d: %w", flowerID, err)
    }
    if f, ok := rsp.(*pb.RspFlowerFinishBreed); ok {
        a.state.UpdateFlowers([]*pb.PFlowerInfo{f.Flower})
    }
    return nil
}

func (a *BotActions) EnsureBreed(args map[string]interface{}) error {
    flowerID := getIntArg(args, "flower_id")
    if a.state.IsBreedDone(flowerID) {
        return nil // already done
    }
    if err := a.Breed(args); err != nil {
        return err
    }
    time.Sleep(10*time.Second + time.Duration(rand.Int63n(3))*time.Second)
    return a.FinishBreed(args)
}

func (a *BotActions) ClaimTask(args map[string]interface{}) error {
    taskID := getIntArg(args, "task_id")
    _, err := a.client.RequestWithResponse(&pb.ReqMainTaskClaim{})
    if err != nil {
        return fmt.Errorf("claim_task task=%d: %w", taskID, err)
    }
    return nil
}

func (a *BotActions) Plant(args map[string]interface{}) error {
    plotIDs := getIntSliceArg(args, "plot_ids")
    flowerID := getIntArg(args, "flower_id")
    _, err := a.client.RequestWithResponse(&pb.ReqPlotPlant{PlotIds: plotIDs, FlowerId: flowerID})
    if err != nil {
        return fmt.Errorf("plant plots=%v flower=%d: %w", plotIDs, flowerID, err)
    }
    return nil
}

func (a *BotActions) Water(args map[string]interface{}) error {
    plotIDs := getIntSliceArg(args, "plot_ids")
    if len(plotIDs) == 0 {
        return nil
    }
    _, err := a.client.RequestWithResponse(&pb.ReqPlotWater{PlotIds: plotIDs})
    if err != nil {
        return fmt.Errorf("water plots=%v: %w", plotIDs, err)
    }
    return nil
}

func (a *BotActions) WaitForHarvest(args map[string]interface{}) error {
    extra := getIntArg(args, "extra_max")
    base := 10 * time.Second
    jitter := time.Duration(0)
    if extra > 0 {
        jitter = time.Duration(rand.Int63n(int64(extra)+1)) * time.Second
    }
    time.Sleep(base + jitter)
    return nil
}

func (a *BotActions) Harvest(args map[string]interface{}) error {
    plotIDs := getIntSliceArg(args, "plot_ids")
    if len(plotIDs) == 0 {
        return nil
    }
    rsp, err := a.client.RequestWithResponse(&pb.ReqPlotHarvest{PlotIds: plotIDs})
    if err != nil {
        return fmt.Errorf("harvest plots=%v: %w", plotIDs, err)
    }
    if h, ok := rsp.(*pb.RspPlotHarvest); ok {
        a.state.UpdatePlots(h.Plots)
    }
    return nil
}

func (a *BotActions) PlantCycle(args map[string]interface{}) error {
    plotMax := int(getIntArg(args, "plot_max"))
    if plotMax <= 0 {
        plotMax = 1
    }

    // Harvest all harvestable plots first
    harvestable := a.state.FindHarvestablePlots()
    for i := 0; i < len(harvestable) && i < plotMax; i++ {
        a.Harvest(map[string]interface{}{"plot_ids": []interface{}{harvestable[i]}})
    }

    // Find empty plots and plant
    empty := a.state.FindEmptyPlots()
    planted := 0
    for _, pid := range empty {
        if planted >= plotMax {
            break
        }
        err := a.Plant(map[string]interface{}{
            "plot_ids":  []interface{}{pid},
            "flower_id": 101,
        })
        if err != nil {
            continue
        }
        planted++
    }

    // Water all planted plots
    for _, pid := range empty[:minInt(planted, len(empty))] {
        a.Water(map[string]interface{}{"plot_ids": []interface{}{pid}})
    }

    return nil
}

func (a *BotActions) CheckOrders(args map[string]interface{}) error {
    _, err := a.client.RequestWithResponse(&pb.ReqResidentOrderInfo{})
    return err
}

func (a *BotActions) SubmitOrders(args map[string]interface{}) error {
    rsp, err := a.client.RequestWithResponse(&pb.ReqResidentOrderInfo{})
    if err != nil {
        return fmt.Errorf("check_orders: %w", err)
    }
    orderRsp, ok := rsp.(*pb.RspResidentOrderInfo)
    if !ok {
        return nil
    }
    for _, slot := range orderRsp.Slots {
        if slot.CoolDownEnd > time.Now().Unix() {
            continue // still in cooldown
        }
        // Check if we can afford the demands
        affordable := true
        for _, demand := range slot.Demands {
            if a.state.Inventory[demand.PropId] < demand.Num {
                affordable = false
                break
            }
        }
        if !affordable {
            continue
        }
        _, err := a.client.RequestWithResponse(&pb.ReqResidentOrderSubmit{SlotId: slot.SlotId})
        if err != nil {
            a.log.Printf("submit_order slot=%d: %v", slot.SlotId, err)
        }
    }
    return nil
}

func (a *BotActions) WaitRange(args map[string]interface{}) error {
    min := getFloatArg(args, "min")
    max := getFloatArg(args, "max")
    if max <= min {
        time.Sleep(time.Duration(min) * time.Second)
        return nil
    }
    d := time.Duration((min + rand.Float64()*(max-min)) * float64(time.Second))
    time.Sleep(d)
    return nil
}

func minInt(a, b int) int {
    if a < b { return a }
    return b
}
```

Note: `SendMessage` (generic proto fallback) is intentionally excluded from first pass — the typed actions cover all needed flows. It can be added later if needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/zyr/workspace/hy_client && go test ./cmd/bench/ -run "TestParseArg" -v`
Expected: PASS

- [ ] **Step 5: Add the state update callbacks to actions**

The actions need to register for push notifications that update state. Add at the end of actions.go:

```go
// RegisterOnMessage sets up state-tracking callbacks on the client.
// Must be called after client creation, before login.
func (a *BotActions) RegisterOnMessage() {
    a.client.OnMessage(func(msg proto.Message) {
        switch m := msg.(type) {
        case *pb.NotifyBagUpdate:
            a.state.UpdateBagNotify(m)
        case *pb.NotifyMainTaskUpdate:
            a.state.UpdateTask(m.Task)
        }
    })
}
```

Need to add `"google.golang.org/protobuf/proto"` import too.

- [ ] **Step 6: Commit**

```bash
git add cmd/bench/actions.go cmd/bench/actions_test.go
git commit -m "feat(bench): game action handlers for breed/plant/harvest/task/order"
```

---

### Task 4: Create script.go — script engine

**Files:**
- Create: `cmd/bench/script.go`
- Modify: `cmd/bench/bot.go` (will use it)

- [ ] **Step 1: Implement script engine**

```go
package main

import (
    "fmt"
    "math/rand"
    "time"

    "hy_client/pkg/client"
)

type ScriptRunner struct {
    actions *BotActions
    client  *client.Client
    state   *BotState
    botID   int
    botType string
    log     *BotLogger
    mixin   *ChatMixinConfig
}

func NewScriptRunner(actions *BotActions, cl *client.Client, state *BotState, botID int, botType string, log *BotLogger, mixin *ChatMixinConfig) *ScriptRunner {
    return &ScriptRunner{
        actions: actions,
        client:  cl,
        state:   state,
        botID:   botID,
        botType: botType,
        log:     log,
        mixin:   mixin,
    }
}

func (r *ScriptRunner) RunScript(script []ScriptStep) error {
    for _, step := range script {
        if err := r.executeStep(step); err != nil {
            return err
        }
        r.maybeChat()
    }
    return nil
}

func (r *ScriptRunner) executeStep(step ScriptStep) error {
    action := r.dispatch(step.Do, step.Args)
    if action == nil {
        r.log.Printf("unknown action: %s", step.Do)
        return nil // skip unknown actions gracefully
    }
    return r.retryable(step.Do, action)
}

func (r *ScriptRunner) dispatch(do string, args map[string]interface{}) func() error {
    switch do {
    case "login":
        return func() error { return r.actions.Login(args) }
    case "wait_range":
        return func() error { return r.actions.WaitRange(args) }
    case "breed":
        return func() error { return r.actions.Breed(args) }
    case "wait_for_breed":
        return func() error { return r.actions.WaitForBreed(args) }
    case "finish_breed":
        return func() error { return r.actions.FinishBreed(args) }
    case "ensure_breed":
        return func() error { return r.actions.EnsureBreed(args) }
    case "claim_task":
        return func() error { return r.actions.ClaimTask(args) }
    case "plant":
        return func() error { return r.actions.Plant(args) }
    case "water":
        return func() error { return r.actions.Water(args) }
    case "wait_for_harvest":
        return func() error { return r.actions.WaitForHarvest(args) }
    case "harvest":
        return func() error { return r.actions.Harvest(args) }
    case "plant_cycle":
        return func() error { return r.actions.PlantCycle(args) }
    case "check_orders":
        return func() error { return r.actions.CheckOrders(args) }
    case "submit_orders":
        return func() error { return r.actions.SubmitOrders(args) }
    case "loop":
        return r.buildLoop(args)
    default:
        return nil
    }
}

func (r *ScriptRunner) buildLoop(args map[string]interface{}) func() error {
    count := 0  // 0 = infinite
    if c, ok := args["count"]; ok {
        switch n := c.(type) {
        case int:
            count = n
        case float64:
            count = int(n)
        }
    }

    // Extract sub-script from args
    // The loop step in YAML: - loop: {count: 0, script: [...]}
    // After YAML parse, "script" is []interface{} of maps
    var subScript []ScriptStep
    if rawScript, ok := args["script"]; ok {
        if steps, ok := rawScript.([]interface{}); ok {
            for _, raw := range steps {
                if stepMap, ok := raw.(map[string]interface{}); ok {
                    for k, v := range stepMap {
                        var argsMap map[string]interface{}
                        if v != nil {
                            argsMap, _ = v.(map[string]interface{})
                        }
                        if argsMap == nil {
                            argsMap = map[string]interface{}{}
                        }
                        subScript = append(subScript, ScriptStep{Do: k, Args: argsMap})
                    }
                }
            }
        }
    }

    return func() error {
        if count == 0 {
            for {
                if err := r.RunScript(subScript); err != nil {
                    return err
                }
            }
        } else {
            for i := 0; i < count; i++ {
                if err := r.RunScript(subScript); err != nil {
                    return err
                }
            }
        }
        return nil
    }
}

func (r *ScriptRunner) retryable(name string, fn func() error) error {
    var lastErr error
    maxRetries := 1
    // login gets more retries
    if name == "login" {
        maxRetries = 3
    }
    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 {
            time.Sleep(time.Second)
            r.log.Printf("retry %s attempt %d/%d", name, attempt, maxRetries)
        }
        start := time.Now()
        err := fn()
        lat := time.Since(start)
        if err != nil {
            lastErr = err
            r.log.Printf("action=%s lat=%v error=%v", name, lat, err)
            continue
        }
        r.log.Printf("action=%s lat=%v ok=true", name, lat)
        return nil
    }
    return fmt.Errorf("%s failed after %d retries: %v", name, maxRetries, lastErr)
}

func (r *ScriptRunner) maybeChat() {
    if r.mixin == nil {
        return
    }
    if rand.Float64() >= r.mixin.Chance {
        return
    }
    msg := r.mixin.Messages[rand.Intn(len(r.mixin.Messages))]
    err := r.client.Send(&pb.ReqChatSendChannel{
        ChannelType: r.mixin.Channel,
        Content:     msg,
    })
    if err != nil {
        r.log.Printf("action=chat error=%v", err)
    } else {
        r.log.Printf("action=chat channel=%d ok=true", r.mixin.Channel)
    }
}
```

Need import for `pb "hy_client/pb"`.

- [ ] **Step 2: Verify script.go compiles**

Run: `cd /home/zyr/workspace/hy_client && go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add cmd/bench/script.go
git commit -m "feat(bench): script engine with action dispatch and retry"
```

---

### Task 5: Rewrite bot.go with state tracking and script runner

**Files:**
- Modify: `cmd/bench/bot.go`
- Modify: `cmd/bench/metrics.go` (add BotLogger)

- [ ] **Step 1: Add BotLogger to metrics.go**

Add at end of `cmd/bench/metrics.go`:

```go
type BotLogger struct {
    botID   int
    botType string
}

func NewBotLogger(botID int, botType string) *BotLogger {
    return &BotLogger{botID: botID, botType: botType}
}

func (l *BotLogger) Printf(format string, args ...interface{}) {
    fmt.Printf("[bot=%d type=%s] %s\n", l.botID, l.botType, fmt.Sprintf(format, args...))
}
```

- [ ] **Step 2: Rewrite bot.go**

```go
package main

import (
    "hy_client/pkg/client"
    pb "hy_client/pb"
)

type Bot struct {
    index      int
    uid        string
    cfg        *Config
    botTypeCfg *BotTypeConfig
    metrics    *MetricsCollector
    cl         *client.Client
    state      *BotState
    log        *BotLogger
    stopCh     chan struct{}
}

func NewBot(index int, uid string, cfg *Config, botTypeCfg *BotTypeConfig, metrics *MetricsCollector) *Bot {
    return &Bot{
        index:      index,
        uid:        uid,
        cfg:        cfg,
        botTypeCfg: botTypeCfg,
        metrics:    metrics,
        stopCh:     make(chan struct{}),
    }
}

func (b *Bot) Stop() {
    close(b.stopCh)
    if b.cl != nil {
        b.cl.Close()
    }
}

func (b *Bot) Run() {
    b.metrics.total.Add(1)
    b.metrics.alive.Add(1)
    defer b.metrics.alive.Add(-1)
    defer b.metrics.RecordType(b.botTypeCfg.ID, "bot", 0, false)

    // Create client
    b.cl = client.NewClient(client.Config{
        Addr:       b.cfg.Addr,
        AccountUID: b.uid,
    })
    defer b.cl.Close()

    // Setup state tracking
    b.state = NewBotState()
    b.log = NewBotLogger(b.index, b.botTypeCfg.ID)

    // Setup action handlers
    actions := NewBotActions(b.cl, b.state, b.log)
    actions.RegisterOnMessage()

    // Run script
    runner := NewScriptRunner(actions, b.cl, b.state, b.index, b.botTypeCfg.ID, b.log, b.cfg.ChatMixin)

    // Wrap in stopCh-aware goroutine
    done := make(chan error, 1)
    go func() {
        done <- runner.RunScript(b.botTypeCfg.Script)
    }()

    select {
    case <-b.stopCh:
        return
    case err := <-done:
        if err != nil {
            b.log.Printf("bot stopped: %v", err)
        }
    }
}
```

- [ ] **Step 3: Remove old code**

Delete from bot.go:
- `Bot.executeAction()` method (old generic proto sender)
- `Bot.Run()` old script-loop method
- `yamlValuesToList`, `yamlValueToProto`, `yamlValueToString` helper functions
- `client.NewMessageByName` usage (script engine handles dispatch)

Also remove unused imports: `"hy_client/pkg/client"` from old code stays, but remove `"google.golang.org/protobuf/reflect/protoreflect"` and `"time"` if no longer used directly.

- [ ] **Step 4: Build check**

Run: `cd /home/zyr/workspace/hy_client && go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/bot.go cmd/bench/metrics.go
git commit -m "feat(bench): bot with state tracking and script engine"
```

---

### Task 6: Update manager.go with staggered startup

**Files:**
- Modify: `cmd/bench/manager.go`

- [ ] **Step 1: Rewrite manager.go**

```go
package main

import (
    "fmt"
    "math/rand"
    "os"
    "os/signal"
    "sync"
    "syscall"
    "time"
)

type BotManager struct {
    cfg     *Config
    metrics *MetricsCollector
    bots    []*Bot
}

func NewBotManager(cfg *Config) *BotManager {
    return &BotManager{
        cfg:     cfg,
        metrics: NewMetricsCollector(),
    }
}

func (m *BotManager) Run() {
    totalBots := m.cfg.TotalBots
    rate := m.cfg.StartupRate
    botTypes := m.cfg.BotTypes

    fmt.Printf("Starting %d bots against %s\n", totalBots, m.cfg.Addr)
    for _, bt := range botTypes {
        fmt.Printf("  %s: %d%%\n", bt.ID, bt.Weight)
    }

    // Start reporter
    stopReporter := make(chan struct{})
    go m.reportLoop(stopReporter)

    // Start bots with staggering
    var wg sync.WaitGroup
    startTime := time.Now()

    if rate <= 0 {
        // Instant start (original behavior)
        for i := 0; i < totalBots; i++ {
            m.startBot(i, &wg)
        }
    } else {
        // Staggered: rate bots per second
        ticker := time.NewTicker(time.Second / time.Duration(rate))
        defer ticker.Stop()

        for i := 0; i < totalBots; i++ {
            <-ticker.C
            m.startBot(i, &wg)
        }
    }

    elapsed := time.Since(startTime)
    fmt.Printf("All bots launched in %v\n", elapsed)

    // Wait for signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    fmt.Println("\nShutting down...")
    close(stopReporter)

    for _, bot := range m.bots {
        bot.Stop()
    }
    wg.Wait()
    m.metrics.PrintFinal()
}

func (m *BotManager) startBot(i int, wg *sync.WaitGroup) {
    uid := fmt.Sprintf(m.cfg.AccountPattern, i)
    botType := m.cfg.BotTypeFor(i)
    bot := NewBot(i, uid, m.cfg, botType, m.metrics)
    m.bots = append(m.bots, bot)

    // Add random jitter before starting (0-500ms)
    jitter := time.Duration(rand.Int63n(500)) * time.Millisecond
    time.Sleep(jitter)

    wg.Add(1)
    go func() {
        defer wg.Done()
        bot.Run()
    }()
}

func (m *BotManager) reportLoop(stop chan struct{}) {
    ticker := time.NewTicker(m.cfg.ReportInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            m.metrics.Report(time.Now())
        case <-stop:
            return
        }
    }
}
```

- [ ] **Step 2: Update MetricsCollector with per-type reporting**

Add to `metrics.go` a `typeMetrics` map and a `RecordType` method:

```go
// In MetricsCollector struct, add:
typeMetrics map[string]*msgMetrics  // per-bot-type metrics

// In NewMetricsCollector, add initialization:
typeMetrics: make(map[string]*msgMetrics),

// Add RecordType method:
func (m *MetricsCollector) RecordType(botType, action string, lat time.Duration, success bool) {
    m.mu.Lock()
    r, ok := m.typeMetrics[botType]
    if !ok {
        r = &msgMetrics{}
        m.typeMetrics[botType] = r
    }
    m.mu.Unlock()

    r.count.Add(1)
    r.totalLat.Add(int64(lat))
    if lat > time.Duration(r.maxLat.Load()) {
        r.maxLat.Store(int64(lat))
    }
    if !success {
        r.fail.Add(1)
    }
}

// Update Report to show per-type metrics:
// After the per-message report, add:
fmt.Println("--- By Bot Type ---")
for name, r := range m.typeMetrics {
    count := r.count.Load()
    if count == 0 { continue }
    fail := r.fail.Load()
    avgLat := time.Duration(r.totalLat.Load() / count)
    maxLat := time.Duration(r.maxLat.Load())
    pct := 100.0
    if fail > 0 {
        pct = 100.0 * float64(count-fail) / float64(count)
    }
    fmt.Printf("[%s]  avg: %v  max: %v  ok: %.1f%%\n", name, avgLat, maxLat, pct)
}
```

- [ ] **Step 3: Build check**

Run: `cd /home/zyr/workspace/hy_client && go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add cmd/bench/manager.go cmd/bench/metrics.go
git commit -m "feat(bench): staggered startup and per-bot-type metrics"
```

---

### Task 7: Update main.go and create new bench.yaml

**Files:**
- Modify: `cmd/bench/main.go`
- Modify: `cmd/bench/bench.yaml`

- [ ] **Step 1: Update main.go**

```go
package main

import (
    "flag"
    "fmt"
    "os"

    "hy_client/pkg/client"
)

func main() {
    configPath := flag.String("config", "bench.yaml", "path to config file")
    flag.Parse()

    cfg, err := LoadConfig(*configPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }

    client.RegisterMessages()

    mgr := NewBotManager(cfg)
    mgr.Run()
}
```

Main.go doesn't change much — just remove the unused `fmt` import if present. Actually `fmt` is still used.

- [ ] **Step 2: Write new bench.yaml**

```yaml
addr: "127.0.0.1:11086"
account_pattern: "loadtest_%d"
total_bots: 1000
startup_rate: 50
report_interval: 5s

bot_types:
  - id: newbie
    weight: 40
    script:
      - login: {}
      - wait_range: {min: 0, max: 5}
      - breed: {flower_id: 101}
      - wait_for_breed: {extra_max: 2}
      - finish_breed: {flower_id: 101}
      - claim_task: {task_id: 1003}
      - plant: {plot_ids: [1], flower_id: 101}
      - claim_task: {task_id: 1004}
      - water: {plot_ids: [1]}
      - claim_task: {task_id: 1005}
      - wait_for_harvest: {extra_max: 2}
      - harvest: {plot_ids: [1]}
      - claim_task: {task_id: 1006}
      - claim_task: {task_id: 1007}
      - loop:
          count: 0
          script:
            - plant_cycle: {plot_max: 3}
            - wait_range: {min: 3, max: 8}

  - id: planter
    weight: 25
    script:
      - login: {}
      - ensure_breed: {flower_id: 101}
      - loop:
          count: 0
          script:
            - plant_cycle: {plot_max: 3}
            - wait_range: {min: 3, max: 8}

  - id: order
    weight: 15
    script:
      - login: {}
      - ensure_breed: {flower_id: 101}
      - loop:
          count: 0
          script:
            - check_orders: {}
            - submit_orders: {}
            - plant_cycle: {plot_max: 2}
            - wait_range: {min: 5, max: 15}

chat_mixin:
  chance: 0.1
  channel: 1
  messages:
    - "大家好"
    - "加油种花"
    - "hello ~"
```

- [ ] **Step 3: Build and final compile check**

Run: `cd /home/zyr/workspace/hy_client && go build ./cmd/bench/`
Expected: No errors, produce `bench` binary

- [ ] **Step 4: Run full test suite**

Run: `cd /home/zyr/workspace/hy_client && go test ./cmd/bench/ -v`
Expected: All tests pass

Run: `cd /home/zyr/workspace/hy_client && go test ./...`
Expected: All tests in project pass

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/main.go cmd/bench/bench.yaml
git commit -m "feat(bench): new config format with bot types and chat mixin"
```

---

### Task 8: Remove stale code and clean up

**Files:**
- Modify: `cmd/bench/bot.go` (removed old helpers)
- Modify: `cmd/bench/config.go` (removed old Action struct)

- [ ] **Step 1: Verify no dead code remains**

Check: `grep -n "yamlValuesToList\|yamlValueToProto\|yamlValueToString\|type Action struct\|NewMessageByName" cmd/bench/`

Expected: No matches (all removed in previous tasks). If any remain, remove them.

- [ ] **Step 2: Check for unused imports across all bench files**

Run: `cd /home/zyr/workspace/hy_client && go vet ./cmd/bench/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add cmd/bench/
git commit -m "chore(bench): remove stale code from old architecture"
```

---

## Verification

1. **Build**: `make build` — both `hy` and `bench` binaries compile
2. **Tests**: `go test ./...` — all tests pass
3. **Config parse**: `./bench -config bench.yaml` — parses config successfully
4. **Against test server**: Run with a local or test gserver instance, verify:
   - Bots stagger-launch at 50/sec
   - Newbie bots complete full flow: login→breed→plant→water→harvest→claim
   - Planter bots enter planting cycle
   - Order bots check and submit orders
   - Chat mixin fires ~10% of steps
   - Metrics show per-type breakdown
   - Ctrl+C shuts down cleanly
