# Architecture

**Analysis Date:** 2026-04-13

## Pattern Overview

**Overall:** Distributed Actor Model with Module-based Service Architecture

**Key Characteristics:**
- Microservices using protoactor-go actor system
- Module-based lifecycle management
- Hierarchical dependency structure
- Event-driven communication
- Separate core framework layer

## Layers

**Framework Layer (`core/`):**
- Purpose: Provides reusable components and abstractions
- Location: `/home/zyr/workspace/gserver/core/`
- Contains: Actor system, networking, storage, messaging, utilities
- Depends on: Go standard library, protoactor-go, GoFrame
- Used by: Application layer

**Application Layer (`apps/`):**
- Purpose: Business logic and service implementations
- Location: `/home/zyr/workspace/gserver/apps/`
- Contains: Gateway, Role, World applications
- Depends on: Framework layer, protocol definitions
- Used by: Client connections, inter-service communication

**Protocol Layer (`protocol/pb/`):**
- Purpose: Message definitions and serialization
- Location: `/home/zyr/workspace/gserver/protocol/pb/`
- Contains: Protobuf definitions for all services
- Depends on: Protocol Buffers
- Used by: All services for communication

**Utilities Layer (`util/`, `lib/`):**
- Purpose: Helper functions and grain abstractions
- Location: `/home/zyr/workspace/gserver/util/`, `/home/zyr/workspace/gserver/lib/`
- Contains: ETS, UID generation, message handlers, grain helpers
- Depends on: Framework components
- Used by: Application and framework layers

**Configuration Layer (`gameconfig/`):**
- Purpose: Game data and configuration tables
- Location: `/home/zyr/workspace/gserver/gameconfig/`
- Contains: Generated game tables, constants
- Depends on: Go code generation
- Used by: Role service for game data

## Data Flow

**Client Request Flow:**

1. Client connects to Gateway via TCP/WebSocket
2. Gateway creates session actor
3. Gateway routes messages to appropriate service
4. Service processes request using business logic
5. Response sent back through Gateway

**Inter-Service Communication:**

1. Service A sends message to Service B
2. Message routed through actor system
3. Service B grain processes message
4. Response sent back asynchronously

**State Management:**
- Actor state maintained in memory
- Persistent state via MongoDB (gxymongo)
- Event sourcing for state changes
- Database index for fast lookups

## Key Abstractions

**Actor Interface (`gxyactor`):**
- Purpose: Abstract actor behavior and lifecycle
- Examples: `/home/zyr/workspace/gserver/core/gxyactor/types.go`, `/home/zyr/workspace/gserver/core/gxyactor/actor_mgr.go`
- Pattern: Service grain with PID-based addressing

**Module Interface (`gxymodule`):**
- Purpose: Abstract service lifecycle and dependencies
- Examples: `/home/zyr/workspace/gserver/core/gxymodule/module.go`
- Pattern: Hierarchical module with init/start/stop phases

**Network Endpoint (`gxynet/endpoint`):**
- Purpose: Abstract network connection handling
- Examples: `/home/zyr/workspace/gserver/core/gxynet/endpoint/endpoint.go`
- Pattern: Interface with SendData/SendMsg methods

**App Interface (`gxyapp.go`):**
- Purpose: Application entry point with dependency management
- Examples: `/home/zyr/workspace/gserver/core/gxyapp.go/app.go`
- Pattern: Register/Retrieve apps with dependency graph

## Entry Points

**Main Node:**
- Location: `/home/zyr/workspace/gserver/node/main.go`
- Triggers: Game server startup
- Responsibilities: Initialize root module, parse command line, handle shutdown

**Gateway App:**
- Location: `/home/zyr/workspace/gserver/apps/gateway/gate_app.go`
- Triggers: Client connections
- Responsibilities: Network setup, session management, message routing

**Role App:**
- Location: `/home/zyr/workspace/gserver/apps/role/role_app.go`
- Triggers: Role-related requests
- Responsibilities: Role management, data persistence, business logic

**World App:**
- Location: `/home/zyr/workspace/gserver/apps/world/world_app.go`
- Triggers: World simulation events
- Responsibilities: Game world state, activity servers

## Error Handling

**Strategy:** Hierarchical error propagation with logging

**Patterns:**
- Module-level error handling in OnModStop/OnModStart
- Actor-specific error handling in message processing
- Network errors handled at endpoint level
- Database errors handled with retry logic

## Cross-Cutting Concerns

**Logging:** GoFrame logging framework with structured logging
**Validation:** Protocol buffer validation + business logic validation
**Authentication:** Account-based authentication at gateway layer

---

*Architecture analysis: 2026-04-13*