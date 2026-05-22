# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## OpenWolf

@.wolf/OPENWOLF.md

This project uses OpenWolf for context management. Read and follow .wolf/OPENWOLF.md every session.

## Build & Test

```bash
go build ./...                 # compile all packages
make test                      # run all tests (-gcflags=-l for gomonkey)
make lint                      # run golangci-lint
go test -gcflags=-l ./pkg/...  # run tests for a specific package
go run node/main.go --config config/<name>.toml  # run server with config

# Protobuf generation (always run after editing .proto files)
make pb
```

## Key Conventions

- **Plan first**: for architecture changes or new features, present a plan before writing code
- **Feature branch + PR**: develop on feature branches, merge via PR
- **gofmt**: format all Go code with `gofmt -w` before committing
- **Commit style**: concise, focus on why not what (Chinese or English OK)

## Architecture

Distributed game server on the **Actor model** (protoactor-go) + GoFrame v2.

- `core/` — shared framework (gxyactor, gxynet, gxyredis, gxypgx, etc.)
- `src/apps/` — deployable microservices (gateway, role, chat, friend, guild)
- `node/main.go` — entry point (starts a Node with apps from TOML config)
- `protocol/` — protobuf definitions (client/ + server/) + generated code (pb/)

## Gotchas

- **`go test -gcflags=-l`** is required — gomonkey patches need inlining disabled
- **`make pb`** strips `omitempty` from generated JSON tags via sed
- **Dev infra**: redis/consul via docker, grafana/prometheus/tempo via docker-compose
- **Submodules**: `protocol/client` and `gameconfig` — init after clone
- **Run config**: `config/*.toml` selects which apps to start (`gate.toml`, `all.toml`, etc.)

## development tips
- **每次开发功能需要先拉取新分支来开发, 如果开发之前有未提交的更改，提醒我先提交**
- **如果是处于开发阶段，就不用考虑数据和数据库的兼容问题。如果是生产环境，就需要注意数据和数据库的兼容问题。**
- **现在我们处于开发阶段**

以第一性原理！从原始需求和问题本质出发，不从惯例或模板出发。 
1.不要假设我清楚自己想要什么。动机或目标不清晰时，停下来讨论。 
2.目标清晰但路径不是最短的，直接告诉我并好的办法。 
3.遇到问题追根因，不打补丁。每个决策都要能答"为什么”。 
4.输出说重点，砍掉一切不改变决策的信息

