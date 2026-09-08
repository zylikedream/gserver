# Ergo-Native Actor Process Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Protoactor-shaped embedded `ActorBase` with an Ergo-native adapter-owned `ActorProcess` and callback-scoped `ActorContext`.

**Architecture:** Ergo callbacks map one-to-one to `ActorProcess.Init`, `HandleMessage`, `HandleCall`, and `Terminate`. Business Actors contain only business state and use `ActorContext` for timer, stop, watch, handler dispatch, response, tracing, and identity operations.

**Tech Stack:** Go 1.24, Ergo Framework 3.3, GoFrame, OpenTelemetry, Prometheus, protobuf

**Spec:** `docs/superpowers/specs/2026-09-08-ergo-actor-process-design.md`

## Global Constraints

- Clean cutover only: no compatibility aliases, deprecated lifecycle messages, or dual paths.
- Preserve Redis Claim/Release, node lease, epoch, PostgreSQL fencing, and protobuf envelope semantics.
- Actor-owned state is accessed only on Ergo callback execution; do not add mutexes or goroutines to Actor callbacks.
- Use `cockroachdb/errors`; errors originate with a stack and are logged only at the final handling point.
- Format changed Go files with `gofmt`.

---

### Task 1: Introduce ActorProcess lifecycle seam

**Files:**
- Modify: `core/gxyactor/actor.go`
- Modify: `core/gxyactor/runtime.go`
- Modify: `core/gxyactor/lifecycle_test.go`
- Modify: `core/gxyactor/runtime_test.go`

**Interfaces:**
- Produces: `ActorProcess`, the callback-scoped `ActorContext`, and direct lifecycle methods from the approved spec.
- Preserves: `ActorProducer func() IActor`, `ActorTimer`, runtime `Send/Call/Respond/Stop` operations.

- [ ] **Step 1: Rewrite lifecycle tests against the desired direct interface**

Replace `probe.Receive(ActorStartedMessage/ActorStoppedMessage)` with:

```go
process := NewActorProcess("lifecycle-test", probe)
ctx := newLifecycleContext(...)
require.NoError(t, process.Init(ctx, nil))
require.NoError(t, process.HandleMessage(ctx, "business"))
process.Terminate(ctx, reason)
```

Assert `Init → DelayInit → message → Terminate`, init failure prevents message dispatch, panic/error returns through the method result, timer events require initialized timer state, and an application stop reason reaches `Terminate`.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./core/gxyactor -run 'TestActorProcess|TestLifecycle' -count=1`
Expected: build failure because `NewActorProcess` and direct lifecycle methods do not exist.

- [ ] **Step 3: Add the direct ActorProcess implementation**

Implement the exact interfaces from the spec. Move timer, handler registry, trace/metrics/logging, panic recovery, and stop-reason state out of `ActorBase` into `ActorProcess`. Do not delete the old bridge until callers are migrated.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `go test ./core/gxyactor -run 'TestActorProcess|TestLifecycle' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit the new seam**

```bash
git add core/gxyactor/actor.go core/gxyactor/runtime.go core/gxyactor/lifecycle_test.go core/gxyactor/runtime_test.go
git commit -m "refactor: add explicit actor process lifecycle"
```

### Task 2: Map Ergo callbacks directly

**Files:**
- Modify: `core/gxyactor/internal/ergo/actor.go`
- Modify: `core/gxyactor/internal/ergo/runtime.go`
- Modify: `core/gxyactor/internal/ergo/runtime_test.go`
- Modify: `core/gxyactor/stage_ergo_test.go`

**Interfaces:**
- Consumes: `NewActorProcess`, `ActorProcess.Init/HandleMessage/HandleCall/Terminate`.
- Produces: one direct adapter path with no `Receive` or `LifecycleError` assertions.

- [ ] **Step 1: Add a failing real-Ergo lifecycle test**

Spawn a probe without `ActorBase`; assert init args arrive through `ActorProcess.Init`, a sent message reaches `HandleMessage`, a call reaches handler dispatch, and stop invokes business termination with the requested reason.

- [ ] **Step 2: Run the adapter test and verify RED**

Run: `go test ./core/gxyactor/internal/ergo -run 'TestAdapterDirectActorLifecycle' -count=1`
Expected: build/test failure because the adapter still requires the embedded base path.

- [ ] **Step 3: Replace adapter dispatch**

Make `ergoActor` own `*gxyactor.ActorProcess`; call direct lifecycle methods; remove `sync.Once`, business `Receive` assertions, `LifecycleError` reads, and mutable `message` context state. Implement `context.Context` plus Actor capabilities on the private Ergo callback context. Translate `gen.MessageDownPID` to `ActorTerminatedMessage`.

- [ ] **Step 4: Remove lifecycle types from network registration**

Delete registration of `ActorInitMsg`, `ActorStartedMessage`, `ActorStoppedMessage`, and `LifecycleMessage`; retain wire envelope, PID response, and watched-process notification types that remain reachable.

- [ ] **Step 5: Run adapter and stage tests**

Run: `go test ./core/gxyactor/internal/ergo ./core/gxyactor -run 'TestAdapter|TestErgo|TestTrace' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit adapter migration**

```bash
git add core/gxyactor/internal/ergo core/gxyactor/stage_ergo_test.go
git commit -m "refactor: map Ergo callbacks directly"
```

### Task 3: Remove business ActorBase embedding

**Files:**
- Modify: `core/gxyactor/activator_manager.go`
- Modify: `src/apps/chat/channel_actor.go`
- Modify: `src/apps/gateway/internal/logic/session.go`
- Modify: `src/apps/guild/logic/guild_actor.go`
- Modify: `src/apps/role/internal/logic/role_main.go`
- Modify: affected tests under those packages

**Interfaces:**
- Consumes: callback-scoped `ActorContext` from Task 1.
- Produces: business Actors containing no `ActorBase`, `Actx`, or promoted runtime methods.

- [ ] **Step 1: Add compile-time contract coverage**

Assert each producer can return its business Actor directly as `gxyactor.IActor` without constructing or assigning an `ActorBase`.

- [ ] **Step 2: Run affected package tests and verify RED**

Run: `go test ./core/gxyactor ./src/apps/chat ./src/apps/gateway/internal/logic ./src/apps/guild/logic ./src/apps/role/internal/logic -count=1`
Expected: build failures at embedded-base and promoted-method call sites.

- [ ] **Step 3: Migrate constructors and callbacks**

Remove `*ActorBase` fields and `NewActorBase` calls. Change lifecycle callback parameters to `gxyactor.ActorContext`. Replace timer, stop, identity, span, log value, watch, response, handler registration, and automatic dispatch calls with the corresponding context methods.

- [ ] **Step 4: Migrate tests to the direct lifecycle seam**

Use `ActorProcess` plus package fakes for lifecycle tests. Delete tests that only pin synthetic lifecycle messages or direct `Actx` mutation; retain observable stop, cleanup, and response contracts.

- [ ] **Step 5: Run affected tests**

Run: `go test ./core/gxyactor ./src/apps/chat ./src/apps/gateway/internal/logic ./src/apps/guild/logic ./src/apps/role/internal/logic -count=1`
Expected: PASS.

- [ ] **Step 6: Commit business migration**

```bash
git add core/gxyactor/activator_manager.go src/apps/chat src/apps/gateway/internal/logic src/apps/guild/logic src/apps/role/internal/logic
git commit -m "refactor: remove embedded actor base"
```

### Task 4: Delete legacy lifecycle artifacts and verify

**Files:**
- Modify: `core/gxyactor/actor.go`
- Modify: `core/gxyactor/runtime.go`
- Modify: `core/gxyactor/helper.go` if dead helpers remain
- Modify: `docs/architecture/adr-0008-ergo-actor-runtime.md`

**Interfaces:**
- Removes: `ActorBase`, `Receive`, `LifecycleError`, `ActorInitMsg`, `ActorStartedMessage`, `ActorStoppedMessage`, `LifecycleMessage`, `ContextDecorator`.
- Preserves: `ActorTerminatedMessage` as the neutral monitor notification.

- [ ] **Step 1: Delete obsolete production and test code**

Remove all legacy types, fallback branches, constructors, fields, comments, and tests. Search production and tests for every removed symbol; no alias or deprecated wrapper remains.

- [ ] **Step 2: Update ADR 0008**

Record the final direct lifecycle mapping, adapter-owned process wrapper, callback-scoped context, Actor confinement rule, and removal of the Protoactor bridge.

- [ ] **Step 3: Run focused race verification**

Run: `go test -race ./core/gxyactor/... -count=1`
Expected: PASS with no race report.

- [ ] **Step 4: Run full verification**

Run:

```bash
go test ./...
go build ./...
make lint
git diff --check
```

Expected: 36 packages pass, build succeeds, root/client lint report `0 issues`, and diff check is clean.

- [ ] **Step 5: Commit cleanup and documentation**

```bash
git add core/gxyactor docs/architecture/adr-0008-ergo-actor-runtime.md
git commit -m "refactor: remove Protoactor lifecycle bridge"
```
