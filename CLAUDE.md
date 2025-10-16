# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based distributed game server using GoFrame v2 framework and ProtoActor actor system. It implements a microservices architecture with separate gateway and game services, using Protocol Buffers for communication.

## Key Technologies

- **Language**: Go 1.25.1
- **Framework**: GoFrame v2.9.3
- **Actor System**: ProtoActor (asynkron/protoactor-go)
- **Database**: MongoDB with official Go driver
- **Cache**: Redis
- **Service Discovery**: etcd via GoFrame contrib
- **Protocol**: Protocol Buffers
- **Network**: gnet framework
- **Deployment**: Docker + Kubernetes

## Essential Commands

### Build and Development
```bash
# Build the project
make build

# Generate protobuf files from .proto definitions
make pb

# Generate DAO/Entity files from database
make dao

# Generate service layer code
make service

# Generate API controllers
make ctrl

# Update GoFrame and CLI tools
make up
```

### Running Services
```bash
# Run gateway server
go run node/gateway/main.go

# Run game server
go run node/game/main.go

# Or use VSCode launch configurations:
# - "Run game" (node/game/main.go)
# - "Run gate" (node/gateway/main.go)
```

### Docker and Deployment
```bash
# Build Docker image (tagged with git commit)
make image

# Deploy to Kubernetes
make deploy ENV=develop

# Deploy with specific tag
make deploy TAG=v1.0.0
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Test specific package
go test ./core/gxynet/
```

## Architecture Overview

### Directory Structure
- `core/`: Core libraries and frameworks
  - `gxyactor/`: Actor system implementation with grain management
  - `gxymongo/`, `gxyredis/`: Database client wrappers
  - `gxynet/`: Network utilities using gnet
  - `gxyregistery/`: Service registry using etcd
- `service/`: Microservices
  - `gateway/`: Entry point for client connections
  - `role/`: Character/role management service
  - `activity/`: Game activity service
- `protocol/`: Protocol buffer definitions and generated code
- `node/`: Node configurations and main entry points
- `gameconfig/`: Game configuration system with JSON data files

### Service Architecture
1. **Gateway Service**: Handles client connections, authentication, and message routing
2. **Role Service**: Manages player characters, inventory, and progression
3. **Activity Service**: Handles game-specific activities and events

### Actor System
The project uses ProtoActor with a grain-based architecture:
- Grains are distributed actors representing game entities
- Grain manager handles actor lifecycle and distribution
- Messages are defined as Protocol Buffer messages
- Services communicate via actor messages

### Configuration System
- Database config: `node/config/db.toml`
- Service configs: `node/{service}/config/{service}.toml`
- Game configs: JSON files in `gameconfig/data/` with Go bindings in `gameconfig/src/`

## Development Workflow

1. **Protocol-First**: Define protobuf messages before implementation
2. **Code Generation**: Use `make pb`, `make dao`, `make service` to generate code
3. **Actor Implementation**: Implement game logic as ProtoActor grains
4. **Configuration**: Add service configs to appropriate TOML files
5. **Game Configs**: Update JSON configs and regenerate Go bindings

### Git Submodules
The project uses submodules for external dependencies:
```bash
# Initialize submodules
git submodule update --init --recursive
```

## Important Patterns

### Adding New Protocol Messages
1. Add `.proto` file to `protocol/` directory
2. Run `make pb` to generate Go code
3. Import generated code from `protocol/pb/`

### Adding New Services
1. Create service directory under `service/`
2. Implement main.go following existing patterns
3. Add configuration files in `node/{service}/config/`
4. Update deployment manifests if needed

### Database Operations
- Use generated DAO/Entity files from `make dao`
- MongoDB operations through `gxymongo` wrapper
- Redis operations through `gxyredis` wrapper

### Actor Implementation
- Extend grain system in `core/gxyactor/`
- Implement message handlers following ProtoActor patterns
- Use grain manager for actor lifecycle management

## Dependencies
- External services: MongoDB, Redis, etcd
- Git submodules for protocol definitions and game configs
- GoFrame framework for web/microservices functionality
- ProtoActor for distributed actor system