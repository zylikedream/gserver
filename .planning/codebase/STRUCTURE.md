# Codebase Structure

**Analysis Date:** 2026-04-13

## Directory Layout

```
/home/zyr/workspace/gserver/
├── node/                    # Node entry point
│   └── main.go             # Server main entry
├── core/                   # Framework layer
│   ├── gxyactor/          # Actor system implementation
│   │   ├── actor_mgr.go    # Actor management
│   │   ├── logger.go       # Actor logging
│   │   └── types.go       # Actor service types
│   ├── gxyapp.go/         # Application framework
│   │   └── app.go         # Base app interface
│   ├── gxynet/            # Networking layer
│   │   ├── endpoint/      # Network endpoints
│   │   ├── packet/        # Packet handling
│   │   ├── codec/         # Message codecs
│   │   └── message/       # Message processing
│   ├── gxymodule/         # Module system
│   │   └── module.go      # Module lifecycle
│   ├── gxymongo/          # MongoDB integration
│   ├── gxymq/             # Message queue
│   ├── gxylocator/        # Service location
│   └── gxyhttp/           # HTTP utilities
├── apps/                  # Application layer
│   ├── gateway/           # Gateway service
│   │   ├── gate_app.go    # Gateway app entry
│   │   └── internal/logic/# Gateway logic
│   │       └── session_mgr.go
│   ├── role/              # Role service
│   │   ├── role_app.go    # Role app entry
│   │   ├── role_service.go# Role service implementation
│   │   └── internal/logic/# Role business logic
│   │       ├── role_module.go
│   │       ├── role_basic.go
│   │       ├── role_bag.go
│   │       └── event/
│   └── world/             # World service
│       ├── world_app.go   # World app entry
│       └── server/        # World server implementations
│           └── activity_server.go
├── protocol/              # Protocol definitions
│   └── pb/                # Generated protobuf
│       ├── *.pb.go        # Protocol implementations
│       └── *.pb.binary.go# Binary protocol implementations
├── util/                  # Utilities
│   ├── ets/               # ETS (Erlang Term Storage) like utilities
│   ├── uid/               # Unique ID generation
│   └── msg_handler/       # Message handling utilities
├── lib/                   # Library helpers
│   └── grain/             # Grain helpers for actor system
└── gameconfig/            # Game configuration
    ├── gameconfig.go      # Config generator
    └── src/               # Generated game data
        ├── activity.*     # Activity related tables
        ├── item.*         # Item related tables
        ├── sign.*         # Sign system tables
        ├── global.*       # Global configuration
        └── controller.*   # Time controller tables
```

## Directory Purposes

**`core/` - Framework Layer:**
- Purpose: Reusable components and infrastructure
- Contains: Actor system, networking, storage, messaging
- Key files: `gxyactor/`, `gxynet/`, `gxymodule/`
- Generated: No

**`apps/` - Application Layer:**
- Purpose: Business logic and service implementations
- Contains: Gateway, Role, World services
- Key files: `*/role_app.go`, `*/gate_app.go`, `*/world_app.go`
- Generated: No

**`protocol/pb/` - Protocol Layer:**
- Purpose: Message definitions and serialization
- Contains: Protobuf generated files
- Key files: All `.pb.go` and `.pb.binary.go` files
- Generated: Yes (from .proto files)

**`util/` - Utilities:**
- Purpose: Helper functions and utilities
- Contains: ETS, UID, message handlers
- Key files: `ets/`, `uid/`, `msg_handler/`
- Generated: No

**`lib/` - Library Helpers:**
- Purpose: Grain abstractions and helpers
- Contains: Actor-related helpers
- Key files: `grain/`
- Generated: No

**`gameconfig/` - Configuration:**
- Purpose: Game data and configuration tables
- Contains: Generated game tables
- Key files: `src/` directory with all game data
- Generated: Yes (from config source files)

## Key File Locations

**Entry Points:**
- `/home/zyr/workspace/gserver/node/main.go`: Server main entry
- `/home/zyr/workspace/gserver/apps/gateway/gate_app.go`: Gateway app entry
- `/home/zyr/workspace/gserver/apps/role/role_app.go`: Role app entry
- `/home/zyr/workspace/gserver/apps/world/world_app.go`: World app entry

**Configuration:**
- `/home/zyr/workspace/gserver/gameconfig/gameconfig.go`: Config generator
- `/home/zyr/workspace/gserver/gameconfig/src/`: All generated game data

**Core Logic:**
- `/home/zyr/workspace/gserver/core/gxymodule/module.go`: Module lifecycle
- `/home/zyr/workspace/gserver/core/gxyactor/types.go`: Actor types
- `/home/zyr/workspace/gserver/core/gxynet/endpoint/endpoint.go`: Network interface
- `/home/zyr/workspace/gserver/core/gxyapp.go/app.go`: Application framework

**Testing:**
- Files ending with `_test.go` in respective directories

## Naming Conventions

**Files:**
- `*_app.go`: Application entry points
- `*_service.go`: Service implementations
- `*_mgr.go`: Manager classes
- `*_module.go`: Module implementations
- `internal/`: Private implementation details
- `logic/`: Business logic implementations
- `event/`: Event handling code

**Directories:**
- `core/`: Framework components
- `apps/`: Service applications
- `protocol/`: Message definitions
- `util/`: Utility functions
- `lib/`: Library helpers
- `gameconfig/`: Game configuration

## Where to Add New Code

**New Service:**
- Primary code: `apps/[service_name]/`
- App entry: `apps/[service_name]/[service_name]_app.go`
- Logic: `apps/[service_name]/internal/logic/`
- Tests: `apps/[service_name]/*_test.go`

**New Network Handler:**
- Implementation: `core/gxynet/[component]/`
- Endpoint: `core/gxynet/endpoint/`
- Codec: `core/gxynet/codec/`

**New Game Feature:**
- Tables: `gameconfig/src/[feature_name].*`
- Logic: `apps/role/internal/logic/[feature_name].go`
- Events: `apps/role/internal/event/`

**New Utility:**
- Location: `util/[utility_name]/`
- Interface: Follow existing patterns in util/

## Special Directories

**`protocol/pb/`:**
- Purpose: Contains all generated protobuf files
- Generated: Yes
- Committed: Yes (generated files are committed)

**`gameconfig/src/`:**
- Purpose: Contains all generated game data tables
- Generated: Yes
- Committed: Yes (generated files are committed)

**`apps/*/internal/`:**
- Purpose: Private implementation details
- Generated: No
- Committed: Yes

---

*Structure analysis: 2026-04-13*