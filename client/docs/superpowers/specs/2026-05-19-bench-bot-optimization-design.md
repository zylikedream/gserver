# Bench Bot Optimization Design

> Date: 2026-05-19
> Purpose: Rebuild load test bot to simulate real player behaviors
> Scope: 3 bot types (newbie/planter/order), config-driven scripts, state tracking, enhanced metrics

## Problem

Current bench bot executes a flat YAML scenario of protobuf messages with no game logic awareness. It cannot:
- Track inventory/task/plot state
- Make decisions based on game state
- Execute game-specific flows (breeding, planting, harvesting, tasks, orders)
- Stagger startup or add random jitter
- Log structured per-action data

## Architecture

### Config-Driven Hybrid

Bot behavior is defined in YAML as a sequence of high-level game "actions." Each action maps to a Go handler function that has access to bot state and the client connection.

```
bench.yaml → BotManager → staggered start → Bot.Run()
                                               │
                                               ├── ScriptRunner (executes YAML steps)
                                               ├── BotState (auto-updated from responses)
                                               └── MetricsCollector (per-action stats)
```

### File Layout

```
cmd/bench/
├── main.go          # Entry: parse flags, load config, start manager
├── config.go        # Config struct, ScriptStep, BotTypeConfig
├── manager.go       # Staggered startup, signal handling, reporter
├── bot.go           # Bot struct with state + script runner
├── state.go         # BotState: auto-track inventory/tasks/plots/flowers
├── actions.go       # Action handlers (login, breed, plant, harvest, etc.)
├── script.go        # Script engine (step dispatch, loop, retry)
└── metrics.go       # Enhanced metrics + per-action logging
```

## YAML Config Format

```yaml
addr: "127.0.0.1:11086"
account_pattern: "loadtest_%d"
total_bots: 1000
startup_rate: 50
report_interval: 5s
log_file: "bench.log"

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
```

## Script Action Reference

| Action | Description | Key Params |
|--------|-------------|-----------|
| `login` | Connect + Handshake + Login + pull initial state | — |
| `wait_range` | Random wait | `min`, `max` (seconds) |
| `breed` | Start breeding | `flower_id` |
| `wait_for_breed` | Wait breed completion | `extra_max` (jitter seconds) |
| `finish_breed` | Claim breed result | `flower_id` |
| `claim_task` | Claim main task reward | `task_id` |
| `plant` | Plant in plots | `plot_ids`, `flower_id` |
| `water` | Water plots | `plot_ids` |
| `wait_for_harvest` | Wait harvest ready | `extra_max` |
| `harvest` | Harvest plots | `plot_ids` |
| `plant_cycle` | Full cycle: find empty → plant → water → wait → harvest | `plot_max` |
| `ensure_breed` | Breed if not already done | `flower_id` |
| `check_orders` | Query resident order slots | — |
| `submit_orders` | Submittable orders | — |
| `loop` | Loop sub-script | `count` (0=infinite), `script` |
| `send_message` | Generic proto send (fallback) | `msg`, `fields` |

## State Tracking

BotState auto-updates from server response messages:

```go
type BotState struct {
    Inventory map[int32]int64         // prop_id → count
    Tasks     map[int32]MainTaskState  // task_id → {progress, status}
    Plots     map[int32]PlotInfo       // plot_id → {flower_id, state, harvest_count, state_time}
    Flowers   map[int32]FlowerInfo     // flower_id → {state, state_time, level}
}
```

Updated via `client.OnMessage` callback by intercepting:
- `RspBagInfo`, `NotifyBagUpdate` → inventory
- `RspMainTaskInfo`, `NotifyMainTaskUpdate` → tasks
- `RspPlotInfo`, `RspPlotPlant`, `RspPlotWater`, `RspPlotHarvest` → plots
- `RspFlowerInfo`, `RspFlowerStartBreed`, `RspFlowerFinishBreed` → flowers

## Error Handling

| Error Type | Handling |
|------------|----------|
| Network timeout | Retry up to 3 times, 1s interval |
| Invalid params | No retry, log error |
| Insufficient resources | Skip action, log warning |
| State mismatch | Re-pull state, retry once |
| Auth failure (login) | Retry 3 times, then mark bot dead |

## Startup Staggering

`startup_rate: 50` → manager launches bots at 50/sec using a ticker.
Total time = `total_bots / startup_rate` seconds.
If `startup_rate: 0`, all bots launch instantly (current behavior).

## Logging & Metrics

**Terminal output** (existing style, every 5s):
```
[Bots]  alive: 423/1000  dead: 12
[newbie]   avg: 1.2ms  max: 45ms  ok: 99.8%
[planter]  avg: 0.8ms  max: 32ms  ok: 100%
[order]    avg: 1.5ms  max: 28ms  ok: 99.5%
```

**Structured log file** (when `log_file` is set):
```
2026/05/19 14:00:01 [bot=42 type=newbie] action=breed player_id=1042 step=start lat=15ms ok=true
2026/05/19 14:00:11 [bot=42 type=newbie] action=breed player_id=1042 step=finish lat=12ms ok=true
2026/05/19 14:00:21 [bot=42 type=newbie] action=claim_task player_id=1042 task_id=1003 lat=8ms ok=true
```

## Chat Mixin

Between script steps, each bot has a `chance` probability of sending a random message to the specified channel. Implemented as a lightweight check in the main script loop — no separate goroutine needed.

## Migration from Current Config

The old `scenario` field is replaced by `bot_types`. The old `bots` field is replaced by `total_bots`. The new config is not backward-compatible with the old format — update bench.yaml.

## Verification

1. `make build` — compiles without errors
2. `make test` — all tests pass
3. Run with new bench.yaml against test server — bots login and execute game flows
4. Check structured logs for all action types
5. Verify metrics show per-bot-type breakdown
