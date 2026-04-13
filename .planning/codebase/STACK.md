# Technology Stack

**Analysis Date:** 2026-04-13

## Languages

**Primary:**
- Go 1.25.1 - Primary language for all server components

## Runtime

**Environment:**
- Go 1.25.1 runtime
- Native binary compilation

**Package Manager:**
- Go modules (go.mod)
- Lockfile: go.sum (present)

## Frameworks

**Core:**
- GoFrame v2.9.4 - Main application framework used for HTTP, configuration, and utilities
- gogf/gf v2.9.4 - Core framework providing HTTP server, configuration management, and utilities

**Message Queue:**
- Apache Pulsar Client v0.11.0 - Pulsar message queue integration
- Redis v9.14.0 - Redis client for caching and messaging

**Database:**
- MongoDB v1.17.4 - MongoDB driver for data persistence
- Redis v9.14.0 - Redis client for caching and messaging

**Networking:**
- gnet v2.9.3 - High-performance networking library
- gorilla/websocket v1.5.3 - WebSocket support

**Actor Framework:**
- Protoactor-go v0.0.0-20250909165758-e952b3c0850e - Actor model implementation for distributed systems
- asynkron/protoactor-go v0.0.0-20250909165758-e952b3c0850e - Actor system

## Key Dependencies

**Critical:**
- protoactor-go v0.0.0-20250909165758-e952b3c0850e - Core actor system
- go.mongodb.org/mongo-driver v1.17.4 - MongoDB driver for data persistence
- github.com/redis/go-redis/v9 v9.14.0 - Redis client
- github.com/apache/pulsar-client-go v0.11.0 - Pulsar message queue client

**Infrastructure:**
- github.com/hashicorp/consul/api v1.32.4 - Service discovery
- go.etcd.io/etcd/client/v3 v3.6.5 - Configuration and discovery
- github.com/robfig/cron/v3 v3.0.1 - Job scheduling

**Configuration:**
- viper v1.21.0 - Configuration management
- mapstructure/v2 v2.4.0 - Configuration mapping

## Configuration

**Environment:**
- TOML configuration files
- Environment variables for sensitive data

**Build:**
- Go build system
- Protobuf generation for protocol definitions

## Platform Requirements

**Development:**
- Go 1.25.1 runtime
- Protobuf compiler (for protocol buffer generation)

**Production:**
- Go runtime environment
- MongoDB database
- Redis server
- Pulsar message queue (optional)
- Service registry (Consul or Etcd)

---

*Stack analysis: 2026-04-13*
```