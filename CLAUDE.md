# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
# Build (default target from hack/hack.mk)
make build

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

### Core Layers

**Module System** (`core/gxymodule/`): Hierarchical module lifecycle. Every component implements `IModule` (init → start → startAfter → stopBefore → stop). Modules form parent-child trees via `ModuleBase.AddModule()`.

**App System** (`core/gxyapp.go/`): Apps are top-level modules registered globally with `RegisterApp()`. Each app declares dependencies via `Deps()`. The node loads apps based on the `[node] apps` list in the TOML config.

**Actor System** (`core/gxyactor/`): Wraps protoactor-go with grain management (distributed virtual actors), actor managers (named actor collections), and system-level actors.

**Service Layer** (`core/gxyservice/`): Services are actors that register for service discovery (Consul or etcd). Grain services expose `Weight()` for load balancing.

### Apps (Microservices)

- **gateway** (`apps/gateway/`): TCP network entry point. Manages client sessions as actors. Routes protobuf messages to backend services.
- **role** (`apps/role/`): Player data service. Each player is a grain (virtual actor) with sub-modules (Basic, Bag, Sign, Activity, Account). Uses MongoDB for persistence with optimistic locking (`hashstructure` v2 for change detection, `bson:"inline"` for state embedding).
- **world** (`apps/world/`): Singleton server actors (e.g., ActivityServer). Spawns named actors on startup.

### Startup Flow

`node/main.go` → creates `Node` from config → adds to root `ModuleBase` → `StartModule()` recursively inits and starts the module tree.

### Key Patterns

- **Message dispatch** (`util/msg_handler.go`): Reflection-based handler registration. Methods with signature `Fn(ctx, *ReqType) (*RspType, error)` are auto-registered and dispatched by protobuf message type name.
- **Grain pattern** (`lib/grain.go`, `core/gxyactor/grain_manager.go`): Virtual actors keyed by string ID. `GetRoleGrain(roleID)` retrieves or spawns a player actor.
- **Persistence**: Role state uses `IPersistState` interface with `bson:"inline"` for embedding and `hash:"-"` tags to exclude version fields from change-detection hashes.
- **Service discovery**: Consul (default) or etcd via `core/gxyregistery/`. Configured in `[registery]` TOML section.
- **Message queue**: Redis or Pulsar via `core/gxymq/`. Config type set in `[mq]` TOML section.

### Protobuf

Protocol definitions in `protocol/pb/` use [gogo/protobuf](https://github.com/gogo/protobuf). Message types double as actor messages (e.g., `pb.ActorStop`, `pb.ActorActive`).

### Configuration

TOML files in `config/`. Node startup reads `game.toml` which declares which apps to load and service endpoints. Sensitive config (passwords) is gitignored.

### Project-Scoped Libraries

- `core/gxynet/` — TCP networking (gnet v2), packet codec, endpoints, connections
- `core/gxymongo/` — MongoDB wrapper (已移除)
- `core/gxyredis/` — Redis wrapper
- `core/gxyhttp/` — HTTP utilities
- `core/gxylocator/` — Service/script locator
- `core/gxylog/` — Logging (zap)
- `core/gxytimer/` — Cron scheduling (robfig/cron)
- `core/gxymq/` — Message queue abstraction (Redis/Pulsar)
- `util/` — Reflection helpers, UID generation, time utilities
- `gameconfig/` — Auto-generated config structs from game design tables
