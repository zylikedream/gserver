# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
# Build (uses GoFrame build tool)
make build

# Generate protobuf from protocol/ submodule
make pb

# Run tests
go test ./...

# Run a single test
go test ./apps/role/internal/logic/ -run TestRoleModule

# Run the server
go run node/main.go --config config/game.toml
```

No linter or formatter is configured in the Makefile. The project uses Go 1.25.1.

## Architecture

GServer is a distributed game server built on the **Actor model** using [protoactor-go](https://github.com/asynkron/protoactor-go) with [GoFrame (gf) v2](https://github.com/gogf/gf) as the utility framework.

### Module System (`core/gxymodule/`)

Every component implements `IModule` with lifecycle: `Init → Start → StartAfter → StopBefore → Stop`. Modules form parent-child trees via `ModuleBase.AddModule()`. `StartModule()` recursively inits/starts the tree; `StopModule()` tears down in reverse order.

### App System (`core/gxyapp.go/`)

Apps are top-level modules registered globally with `RegisterApp(name, app)`. Each app declares dependencies via `Deps()`. The node loads apps based on the `[node] apps` list in TOML config. Some apps (actor, http, service) are preloaded automatically.

**Registered apps:** role, redis, mq, gate.

### Actor System (`core/gxyactor/`)

Wraps protoactor-go with grain management and actor managers:

- **ActorBase** — base actor with message handler, timer, send/call/respond methods. The `Receive` loop handles lifecycle events (`Started` → `Init` + `DelayInit`, `Stopped` → `Terminate`), timer dispatch, and message routing.
- **GrainBase** — extends ActorBase for virtual actors. Extracts `grainID` from `ActorContext.InitArgs`.
- **GrainManager** — creates consistent-hash pools of `grainActivator` actors per grain kind. Activators handle spawning, Redis-based PID registration (SETNX with TTL renewal), and deregistration on termination.
- **ActorMgr** — thread-safe named actor collection (`sync.Map` wrapper).

**Grain location flow:** `GetGrain(kind, id)` → lookup PID in Redis (`gserver:locate:node:{kind}:{id}`) → if not found, select node via consistent hash → send `ActorActive` to remote activator → activator spawns grain and registers in Redis.

### Service Layer (`core/gxyservice/`)

Services are actors that register for service discovery (Consul or etcd via `core/gxyregistery/`). Three selector strategies: random, round-robin, consistent hash (virtual nodes with AVL tree ring). Grain services expose `Weight()` for load balancing.

### Apps (Microservices)

- **gateway** (`apps/gateway/`): TCP network entry point using gnet v2. Manages client sessions as actors. LTPV packet codec (Length+Type+Path+Value). Sessions handle handshake, then route protobuf messages to role grains.
- **role** (`apps/role/`): Player data service. Each player is a grain with sub-modules (Basic, Bag, Sign, Activity, Account, Public, Extra). Uses **PostgreSQL** for persistence with JSONB columns. In-process event bus (`internal/event/eventbus.go`) for cross-module events.
- **world** (`apps/world/`): Singleton server actors (e.g., ActivityServer). Spawns named actors on startup. Uses sync.Map (ETS wrapper) for shared state.

### Startup Flow

`node/main.go` → reads config → creates `Node` → adds to root `ModuleBase` → `StartModule()` recursively initializes the module tree. Shutdown on signal triggers `StopModule()` in reverse order.

### Key Patterns

- **Message dispatch** (`util/msg_handler.go`): Reflection-based handler registration. Scans exported methods with signature `Fn(ctx, *ReqType) (*RspType, error)` or `Fn(ctx, *ReqType) error`, maps them by `ArgType.Name()`. Actors call `AddHandler(self)` in `DelayInit` to auto-register.
- **Grain pattern** (`lib/grain.go`): `GetRoleGrain(roleID)` wraps `GetGrain("role", id)`. The role app registers its grain producer at startup.
- **Persistence** (`core/gxypgx/`): PostgreSQL via pgx/v5. `UpsertOne` uses `INSERT ... ON CONFLICT DO UPDATE`. Struct fields use `db:"column_name"` tags; embedded state uses `db:"inline"`. Map and slice fields auto-map to JSONB columns.
- **Locator** (`core/gxylocator/`): Redis-based actor PID lookup with TTL-based registration and Lua scripts for atomic unregister.
- **Service discovery**: Consul (default) or etcd via `core/gxyregistery/`. Configured in `[registery]` TOML section.
- **Message queue**: Redis pub/sub or Pulsar via `core/gxymq/`. Priority-based processing (Critical/High/Normal).

### Protobuf

Protocol definitions in `protocol/` (git submodule). Proto files use standard proto3 with `google.protobuf.Any` for wrapping client messages. Generate with `make pb`.

### Configuration

TOML files in `config/`. `game.toml` declares apps to load, PostgreSQL/Redis connections, service discovery, and logging. `gate.toml` adds TCP server and packet codec config. Sensitive config (passwords) is gitignored.

### Project-Scoped Libraries

- `core/gxynet/` — TCP networking (gnet v2), LTPV packet codec, endpoints
- `core/gxypgx/` — PostgreSQL wrapper (pgx/v5 pool, reflection-based CRUD)
- `core/gxyredis/` — Redis wrapper (also a registered app)
- `core/gxyhttp/` — HTTP utilities
- `core/gxylocator/` — Redis-based actor location service with Lua scripts
- `core/gxylog/` — Logging (zap)
- `core/gxytimer/` — Cron scheduling (robfig/cron)
- `core/gxymq/` — Message queue abstraction (Redis/Pulsar) with priority workers
- `core/gxyregistery/` — Service registry (Consul/etcd) with selector strategies
- `util/` — Reflection-based message dispatch, UID generation, time utilities
- `lib/` — Application-level helpers (e.g., `GetRoleGrain`)
- `gameconfig/` — Git submodule: auto-generated config structs from game design tables

## graphify

This project has a graphify knowledge graph at graphify-out/.

Rules:
- Before answering architecture or codebase questions, read graphify-out/GRAPH_REPORT.md for god nodes and community structure
- If graphify-out/wiki/index.md exists, navigate it instead of reading raw files
- After modifying code files in this session, run `graphify update .` to keep the graph current (AST-only, no API cost)
