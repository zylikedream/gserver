# Load Test Bot — Design Spec

## Overview

Add a `cmd/bench/` program that uses the existing `hy_client/pkg/client/` SDK to run
N concurrent bot connections against gserver for load testing. Bots follow a
configurable script loop, and the program reports latency/success metrics.

## Configuration (YAML)

```yaml
addr: "127.0.0.1:9000"
bots: 1000

# %d → bot index 0..N-1
account_pattern: "loadtest_%d"

scenario:
  - msg: ReqBasicInfo
    delay: 5s

  - msg: ReqChatSendChannel
    fields:
      channel_type: 1
      content: "hello"
    delay: 10s

  - msg: ReqFlowerWater
    fields:
      plot_ids: [1, 2, 3]
    delay: 3-8s        # random jitter range

report_interval: 5s
```

- `msg` uses proto message names (ReqXXX), not CLI command names.
- `fields` is optional; omitted → empty message.
- `delay` can be a fixed duration (`5s`) or a range (`3-8s` → uniform random).
- `report_interval`: how often to print aggregated metrics.

## Architecture

### Files

```
cmd/bench/
├── main.go        # flag parsing, start Manager
├── config.go      # YAML loading, validation
├── bot.go         # single bot: connect → login → script loop
├── manager.go     # BotManager: spawn/stop N bots, signal handling
└── metrics.go     # per-msg-type metrics collection + reporter
```

### Component Diagram

```
Config → BotManager
            │
            ├── Bot[0] ─── client.Client ─── script loop
            ├── Bot[1] ─── client.Client ─── script loop
            ├── Bot[2] ─── client.Client ─── script loop
            │   ...
            └── Bot[N] ─── client.Client ─── script loop
                            │
                     MetricsCollector (shared)
                            │
                     Reporter (every report_interval)
```

## Bot Lifecycle

1. **Connect** — `client.NewClient(Config{Addr, AccountUID})` → `c.Connect()`
2. **Login** — `c.Handshake()` → `c.Login()` (both blocking with 10s timeout)
3. **Script loop** — iterate scenario actions in order, then repeat from start:

   ```
   for each action in scenario:
       build proto message from msg+fields
       start timer
       err = c.Request(msg)
       record metric: msg_type, latency, success/fail
       if err != nil: log warning, continue
       sleep(delay)
   ```

4. **Exit** — `c.Close()` on finish or interrupt

### Error Handling

| Failure | Action |
|---------|--------|
| Connect fail | retry 3× with 1s backoff, then mark bot dead |
| Login timeout | mark dead |
| Request timeout (10s) | record failure, continue next action |
| Request other error | record failure, continue next action |
| Mid-session disconnect | mark dead, record duration |

## Metrics

Collected per `(msg_name, success)` pair:

```
metrics := struct {
    count      int64
    totalLat   time.Duration
    maxLat     time.Duration
    failCount  int64
}
```

Reporter output (every `report_interval`, plus final summary):

```
[Bots]  alive: 998/1000  dead: 2
[ReqFriendList]      587  avg: 12ms  max: 45ms  ok: 100.0%
[ReqChatSendChannel] 412  avg: 8ms   max: 120ms ok: 99.5%
[ReqFlowerWater]     210  avg: 15ms  max: 200ms ok: 98.1%
```

## Reuse from Existing Code

- `pkg/client/` — `Client`, `RegisterMessages()`, `NewMessageByID()`, `Connect()`, `Handshake()`, `Login()`, `Request()` — all reused as-is.
- `cmd/hy/autocmd.go` — `parseFieldValue()` is moved to `pkg/client/` so both `cmd/hy/` and `cmd/bench/` can use it.
- `cmd/hy/autocmd.go` — `expandCommaSeparated()` is moved alongside.

## What Does NOT Change

- REPL (`cmd/hy/`) — untouched.
- CLI commands (`cmd/hy/commands.go`, `cmd/hy/autocmd.go`) — untouched.
- Client SDK (`pkg/client/`) — only addition: export `parseFieldValue` and `expandCommaSeparated`.

## Dependencies

Add: `gopkg.in/yaml.v3` (YAML config parsing).

## Out of Scope (v1)

- Distributed mode (multiple machines).
- Real-time metrics dashboard.
- Dynamic scenario injection at runtime.
- Per-bot unique scenarios (all bots share same scenario).
