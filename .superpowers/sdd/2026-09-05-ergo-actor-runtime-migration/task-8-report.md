# Task 8 Report: Ergo Runtime Cutover

## Completed

- Removed the Protoactor-only `core/gxyactor/system.go` and `core/gxyactor/logger.go` adapters.
- Removed the Protoactor dependency and checksum entries from `go.mod` and `go.sum`.
- Kept the public `gxyactor` seam runtime-neutral; helper dispatch now requires the installed Ergo runtime and has no legacy application fallback.
- Cut `gxynode` over to `core/gxyactor/ergoapp`, which starts Ergo with explicit node identity, listener host/port, cookie, network options, and shutdown timeout.
- Updated Activator control processes and business actors to Ergo named processes and GServer protobuf envelopes; remote named Send/Call use canonical node and logical process IDs.
- Added explicit `[actor]` defaults (`cookie`, `network`, `shutdown_timeout`) to all TOML templates and development environment files.
- Updated tracked operational and architecture documentation to describe Ergo and GServer ownership boundaries.
- Removed runtime-specific test coverage that depended on deleted adapters while retaining neutral PID, lifecycle, and dispatch coverage.

## Verification

```text
$ go test ./core/gxyactor/internal/ergo ./core/gxyactor
aok: both packages

$ go test ./...
go test: 36 packages ok, 26 no tests

$ git diff --check
aok: no whitespace errors

$ grep legacy runtime references in tracked source/config/docs
aok: no Protoactor references outside historical migration evidence
```

## Concerns and operational notes

- The clean cutover intentionally does not translate old runtime PIDs. Gateways holding stale identities must reactivate or reconnect.
- Actor ownership remains GServer-owned: Redis lease/Claim/Release/epoch and PostgreSQL `role_actor_fence` remain authoritative; Ergo discovery and process registration do not grant ownership.
- `actor.cookie` is now required by the Ergo bootstrap. Deployments must provide the same cookie to nodes that should join one cluster; templates default to `gserver-ergo` for development.
- Named Ergo processes are used for activator control and business actors so remote delivery can address a canonical `nodeInstanceName` plus logical process ID.
