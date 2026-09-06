# Task 5 Report — Activator and Ownership Fencing

## Status

Integrated activation spawning and confirmation behind a private runtime-neutral spawner seam. Existing Protoactor spawning remains the default legacy implementation until the final runtime cutover; the seam allows the Ergo adapter to provide the same lifecycle operations without changing ownership rules.

## Changes

- Added `actorActivationSpawner` with spawn, init-confirmation, and stop operations.
- Added a serialized `requestLocal` activation path used by focused ownership tests and runtime-neutral activation callers.
- Preserved Claim-before-Spawn, `allowSpawn=false` locate-only behavior, duplicate local activation fail-closed behavior, conditional owner release on spawn/init failure, and owner/epoch stale cleanup.
- Production `HandleMessage` now uses the spawner seam for spawn, confirmation, and stop rather than directly coupling the activation decision to Protoactor Touch.
- Added failure-injection coverage for concurrent activation, duplicate local activation, init confirmation failure, stale termination cleanup, lease takeover, Redis failure, and locate-only requests.
- Preserved PostgreSQL role fencing; no replacement with runtime PID, Registrar, or Grid checks.

## Verification

```text
go test ./core/gxyactor -run 'TestActivation' -count=1
ok   gserver/core/gxyactor 0.269s

go test ./core/gxyactor -run '(Activation|Locator|Fence|Ownership)' -count=1
ok   gserver/core/gxyactor 0.681s

go test ./src/apps/role/internal/logic -run '(Activation|Locator|Fence|Ownership)' -count=1
ok   gserver/src/apps/role/internal/logic 0.189s
```

The changed Go files were formatted with `gofmt`. No project-wide suite or linter was run.

## Boundaries

- The default production spawner remains the private Protoactor bridge. Task 8 performs the clean dependency cutover after all business consumers migrate.
- Redis lease, Claim/Release, epoch, and PostgreSQL `role_actor_fence` remain authoritative. Ergo PID identity is not ownership metadata.
- Pending activation confirmation remains mailbox-serialized in the existing activator; the new spawner seam supplies the runtime-specific confirmation operation.
