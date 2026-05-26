# hy_client

Console debugging client and benchmark tool for gserver.

## Architecture

| Directory | Purpose |
|-----------|---------|
| `cmd/hy/` | Interactive REPL client for manual debugging |
| `cmd/bench/` | Configurable benchmark bot framework |
| `pkg/client/` | Core SDK — codec, connection, message registry |
| `pb/` | Generated protobuf Go code |
| `proto/client/` | Proto source files (gserver protocol submodule) |

## Build & Run

```bash
git submodule update --init   # clone proto/client submodule
make proto                    # regenerate protobuf (if protos changed)
make build                    # build both binaries
make test                     # run all tests
```

### hy — Interactive REPL

```bash
./bin/hy                                # connect with defaults
./bin/hy --addr=127.0.0.1:9000 --account=account001
```

All `Req*` protocols auto-register as CLI commands at startup. Commands follow a `domain.action` naming convention (e.g. `bag.info`, `friend.search_player`, `mail.list`). Type `help` to see all available commands.

### bench — Benchmark Bot

```bash
./bin/bench -config cmd/bench/bench.yaml
```

Bench runs configurable bot types against the game server. Bot behaviors are defined entirely in YAML — no Go code changes needed to add or modify bot scripts.

#### Configuration

```yaml
addr: "127.0.0.1:11086"
account_pattern: "loadtest_%d"
total_bots: 1000
startup_rate: 50           # bots/sec, 0 = instant
report_interval: 5s

bot_types:
  - id: newbie
    weight: 40              # weight determines bot type ratio
    script:
      - login: {}
      - ensure_breed: {flower_id: 101}
      - loop:
          count: 0
          script:
            - plant_cycle: {plot_max: 3}
            - wait_range: {min: 3, max: 8}

chat_mixin:
  chance: 0.1
  channel: 1                # 1 = world, 4 = guild
  messages: ["大家好", "hello ~"]
```

#### Available Actions

| Action | Description |
|--------|-------------|
| `login` | Connect, handshake, login, pull initial state |
| `breed` | Start breeding a flower |
| `wait_for_breed` | Sleep for breed duration + jitter |
| `finish_breed` | Complete breeding |
| `ensure_breed` | Skip if already bred, otherwise breed + wait + finish |
| `plant` | Plant flower in specified plots |
| `water` | Water specified plots |
| `wait_for_harvest` | Sleep for growth duration + jitter |
| `harvest` | Harvest specified plots |
| `plant_cycle` | Harvest → plant → water in sequence |
| `claim_task` | Claim main task reward |
| `check_orders` | Check resident order status |
| `submit_orders` | Submit affordable resident orders |
| `wait_range` | Sleep for random duration between min and max |
| `gm` | Execute GM command |
| `loop` | Repeat sub-script (count=0 = infinite) |

## Protocol

Transport format (LTIV):

```
[Size:2B LE][Type:1B][ID:2B LE][Payload:protobuf]
```

Messages are identified by numeric `msg_id` defined in proto options. All `Req*`, `Rsp*`, and `Notify*` messages with `option (msg_id)` are auto-registered at startup.
