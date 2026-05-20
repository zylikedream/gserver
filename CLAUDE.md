# OpenWolf

@.wolf/OPENWOLF.md

This project uses OpenWolf for context management. Read and follow .wolf/OPENWOLF.md every session. Check .wolf/cerebrum.md before generating code. Check .wolf/anatomy.md before reading files.


# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Build & Test

```bash
go build ./...       # or: make build
make pb              # generate protobuf from protocol/ submodule
go test ./...
go run node/main.go --config config/game.toml
```

Project uses Go 1.25.1. No linter/formatter configured in Makefile.

## Tool Preferences

- **Go 代码引用,查询,读取**：尽量用 LSP tool（goToDefinition, findReferences, hover），不用 grep/find
- **Web 搜索/浏览网页**：用 `playwright-cli` skill，不用 WebSearch/WebFetch

## Architecture Overview

GServer is a distributed game server on the **Actor model** ([protoactor-go](https://github.com/asynkron/protoactor-go)) + **GoFrame v2** utility framework.

### Module/App System

`core/gxymodule/` — lifecycle: `Init → Start → StartAfter → StopBefore → Stop`. Modules form parent-child trees. Apps (top-level modules) registered via `RegisterApp()` and loaded from TOML config.

### Actor System (`core/gxyactor/`)

Activator-based grain management. Two-layer location: **Redis** caches which node hosts each actor (key: `gserver:locate:node:actor:{kind}:{id}`, value: `nodeInstanceName`, TTL=12h, no renewal), **Consul** resolves node address. See `docs/system/actor-location.md`.

### Persistence (`core/gxypgx/`)

PostgreSQL via **GORM** with `gorm:"column:name"` tags. AutoMigrate for schema. Role data uses per-module tables with dirty-tracking and periodic flush (600s). See `src/apps/role/internal/logic/role_main.go`.

### Apps

- **gateway** (`src/apps/gateway/`): TCP (gnet v2), LTPV packet codec, session actors
- **role** (`src/apps/role/`): Player grains with sub-modules (Basic, Bag, Extra, Public, Flower, Plot). PostgreSQL + JSONB columns. In-process event bus.
- **world** (`src/apps/world/`): Singleton server actors.
