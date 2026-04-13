# External Integrations

**Analysis Date:** 2026-04-13

## APIs & External Services

**Message Queue:**
- Apache Pulsar - High-performance distributed messaging
  - SDK: github.com/apache/pulsar-client-go v0.11.0
  - Config: Configurable via `mq.type = "pulsar"` in config files

- Redis - Message queuing and pub/sub
  - SDK: github.com/redis/go-redis/v9 v9.14.0
  - Config: `mq.type = "redis"`, Redis credentials in configuration

**Service Discovery:**
- Consul - Service registry and discovery
  - SDK: github.com/hashicorp/consul/api v1.32.4
  - Config: `registery.type = "consul"`, address in config

- Etcd - Distributed key-value store for service discovery
  - SDK: go.etcd.io/etcd/client/v3 v3.6.5
  - Config: Optional, can be enabled in configuration files

## Data Storage

**Databases:**
- MongoDB - Primary data persistence
  - Connection: `mongodb://root:test@localhost:27017/admin`
  - Database: "galaxy"
  - Client: go.mongodb.org/mongo-driver v1.17.4
  - Features: Replica set support, connection pooling

**File Storage:**
- Local filesystem - File storage
  - No external cloud storage detected

**Caching:**
- Redis - In-memory caching
  - Connection: localhost:6379
  - Client: github.com/redis/go-redis/v9 v9.14.0
  - DB: 10 (separate database)

## Authentication & Identity

**Auth Provider:**
- Custom authentication implementation
  - Implementation: Custom auth system in role service
  - No external OAuth providers detected

## Monitoring & Observability

**Error Tracking:**
- Zap logging - Structured logging
  - SDK: go.uber.org/zap v1.27.0
  - Config: File-based logging with rotation

**Logs:**
- File-based logging with rotation
  - Path: ./log directory
  - Format: Date-based files ({Y-m-d}.log)
  - Framework: GoFrame logging + Zap

## CI/CD & Deployment

**Hosting:**
- Self-hosted containerized deployment
  - Platform: Container-based (Docker)

**CI Pipeline:**
- No external CI/CD tools detected
  - Custom build scripts

## Environment Configuration

**Required env vars:**
- Configuration managed through TOML files
- Database credentials in configuration files
- No external secrets management system detected

**Secrets location:**
- Configuration files (TOML)
- Hardcoded in some configuration examples

## Webhooks & Callbacks

**Incoming:**
- WebSocket endpoints - Game client connections
- HTTP endpoints - Health checks and management APIs

**Outgoing:**
- Event bus - Internal event system
- Message queue - Cross-service communication
- No external webhooks detected

## Protocol Buffers

**Message Serialization:**
- Protocol Buffers v3 - Message serialization
  - SDK: google.golang.org/protobuf v1.36.9
  - SDK: github.com/gogo/protobuf v1.3.2
  - Files: protocol/pb/*.pb.go
  - Usage: Inter-service communication and client-server messaging

---

*Integration audit: 2026-04-13*
```