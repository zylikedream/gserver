# Actor Location Ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a consistency-first Actor Activation Directory that self-heals stale owners, self-fences nodes after lease loss, and rejects stale Role writes inside PostgreSQL transactions.

**Architecture:** Redis stores an O(1) per-activation directory entry plus one lease per node process. Every owner hit goes through the owner Activator, which distinguishes a live local actor from a stale directory entry. Redis coordinates ownership; PostgreSQL serializes Role writer epochs at the persistence seam.

**Tech Stack:** Go 1.25.1, protoactor-go, go-redis/v9, Redis Lua, Go protobuf, GORM PostgreSQL, go-sqlmock, miniredis.

**Spec:** `docs/superpowers/specs/2026-09-03-actor-location-ownership-design.md`

## Global Constraints

- Role ownership is consistency-first: infrastructure uncertainty must not create a second writer.
- Claim precedes `SpawnNamed`; a Claim loser never creates the actor.
- Redis errors are infrastructure failures, never locate misses.
- Owner hits go through the owner Activator; callers never infer activation liveness from a Redis key.
- Online actors are not migrated during scale-out; offline players are redistributed on later activation.
- Owner keys have no fixed TTL while owned; node lease validity determines takeover.
- Node lease deadline loss triggers process self-fencing exactly once.
- PostgreSQL rejects stale epochs inside the same transaction as business writes.
- Shard ownership, online actor migration, mailbox/state migration, local locate cache, node Set scan and per-actor heartbeat are permanent non-goals, not deferred work.
- Use `cockroachdb/errors`, structured `gxylog`, dependency injection instead of monkey patching, and `testing.B.Loop()` in Go benchmarks.

---

### Task 1: Add typed owner-resolution protocol

**Files:**
- Modify: `protocol/server/gactor.proto`
- Regenerate: `protocol/pb/gactor.pb.go`
- Modify: `core/gxyactor/actor_test.go`

**Interfaces:**
- `ActorActive.allow_spawn` distinguishes a directory-miss candidate from an owner-resolution request.
- `ActorLocateRetry` is a typed response; error text is never control flow.

- [ ] **Step 1: Write the failing test**

```go
func TestActorLocateRetryIsProtoMessage(t *testing.T) {
    var msg proto.Message = &pb.ActorLocateRetry{}
    if msg == nil {
        t.Fatal("ActorLocateRetry is nil")
    }
}
```

- [ ] **Step 2: Run RED**

```bash
go test -run '^TestActorLocateRetryIsProtoMessage$' -count=1 ./core/gxyactor
```

Expected: compile failure because `pb.ActorLocateRetry` does not exist.

- [ ] **Step 3: Extend and regenerate the protocol**

```proto
message ActorActive {
    string kind = 1;
    string id = 2;
    bool allow_spawn = 3;
}

message ActorLocateRetry {
}
```

Run `make pb`.

- [ ] **Step 4: Run GREEN**

```bash
go test -run '^(TestActorLocateRetryIsProtoMessage|TestHashableActorActive_Hash)$' -count=1 ./core/gxyactor
```

---

### Task 2: Self-heal stale directory owners

**Files:**
- Modify: `core/gxyactor/actor_locator.go`
- Modify: `core/gxyactor/activator_manager.go`
- Modify: `core/gxyactor/actor_activation_ownership_test.go`
- Modify: `core/gxyactor/actor_locator_test.go`
- Modify: `core/gxyactor/actor_test.go`

**Interfaces:**
- `actorLocator.release(ctx, kind, id, owner) (bool, error)` reports whether the exact owner was deleted.
- `activatorManager.requestActor(ctx, node, kind, id, allowSpawn) (PID, retry bool, err error)` sends `ActorActive`.
- `getActor` retries `ActorLocateRetry` at most `actorLocateMaxAttempts`.

- [ ] **Step 1: Write failing stale-owner tests**

```go
func TestActorLocatorReleaseReportsMatch(t *testing.T) {}
func TestActivationExistingLocalActorReturnsPID(t *testing.T) {}
func TestActivationMissingLocalActorReleasesAndRetries(t *testing.T) {}
func TestGetActorRetryRelocatesOfflineActor(t *testing.T) {}
func TestGetActorWithoutSpawnReturnsNotFoundAfterStaleCleanup(t *testing.T) {}
```

The relocation test seeds a valid node-a owner, injects an owner request that conditionally releases and returns retry, then asserts the next attempt selects from the current node list. The `spawn=false` test asserts no candidate spawn occurs after stale cleanup.

- [ ] **Step 2: Run RED**

```bash
go test -run '^(TestActorLocatorReleaseReportsMatch|TestActivation|TestGetActorRetry|TestGetActorWithoutSpawn)' -count=1 ./core/gxyactor
```

- [ ] **Step 3: Implement conditional Release result**

```go
func (l *actorLocator) release(
    ctx context.Context,
    kind, id string,
    owner ActorOwner,
) (bool, error) {
    result, err := l.redis.Eval(
        ctx,
        actorLocatorReleaseScript,
        []string{actorLocatorOwnerKey(kind, id)},
        encodeActorOwner(owner, l.leaseToken),
    ).Int64()
    if err != nil {
        return false, errors.Wrap(err, "release actor owner")
    }
    return result == 1, nil
}
```

Migrate every caller without a compatibility wrapper.

- [ ] **Step 4: Implement owner Activator resolution**

For each `hashableActorActive`:

1. Claim validates the directory owner.
2. `owned_by_other` returns `ActorLocateRetry`.
3. `already_owned` with a local PID returns that PID.
4. `already_owned` without a local PID conditionally releases and returns retry.
5. `acquired` with `allow_spawn=false` conditionally releases and returns retry.
6. Only `acquired` with `allow_spawn=true` calls `SpawnNamed`.

Touch failure and actor termination remove local maps and conditionally Release. Redis Release errors are logged but do not preserve dead local state.

- [ ] **Step 5: Implement bounded relocation**

`getActor` loops at most `actorLocateMaxAttempts`. On directory hit it resolves the owner address and calls `requestActor(..., false)`. On miss with `spawn=true` it selects a current healthy candidate and calls `requestActor(..., true)`. Typed retry restarts Locate; retry exhaustion returns `errActorLocateRetryExhausted`. A missing active-owner address returns unavailable and never steals a valid lease.

- [ ] **Step 6: Run GREEN**

```bash
go test -race -run '^(TestActorLocator|TestActivation|TestGetActor)' -count=1 ./core/gxyactor
```

---

### Task 3: Self-fence after node lease loss

**Files:**
- Modify: `core/gxyactor/actor_locator.go`
- Modify: `core/gxyactor/activator_manager.go`
- Modify: `core/gxyactor/actor_locator_test.go`

**Interfaces:**
- `actorLocator.leaseValid(now time.Time) bool` checks the conservative local deadline.
- `startLeaseHeartbeat(ctx, onLost func(error)) func()` invokes `onLost` exactly once.
- Production `onLost` calls `gxylog.Fatal`; tests inject a non-terminating callback.

- [ ] **Step 1: Write failing watchdog tests**

```go
func TestActorLocatorTokenMismatchFencesImmediately(t *testing.T) {}
func TestActorLocatorRenewalErrorsFenceAtDeadlineOnce(t *testing.T) {}
func TestActorLocatorGracefulStopDoesNotFence(t *testing.T) {}
```

Use short test-only lease and heartbeat durations. Close the Redis client after acquisition and assert one callback after the last confirmed deadline.

- [ ] **Step 2: Run RED**

```bash
go test -run '^TestActorLocator.*Fence|^TestActorLocatorGracefulStop' -count=1 ./core/gxyactor
```

- [ ] **Step 3: Implement deadline tracking**

```go
type actorLocator struct {
    redis             redis.UniversalClient
    nodeID            string
    leaseToken        string
    leaseTTL          time.Duration
    heartbeatInterval time.Duration
    leaseDeadline     atomic.Int64
    fenced            atomic.Bool
}
```

Capture `started := time.Now()` before every acquire or renew. On success store `started.Add(leaseTTL).UnixNano()`, which is conservative relative to Redis command completion. Token mismatch fences immediately. Redis errors log and retry only before the last confirmed deadline. `sync.Once` guarantees one fatal callback.

- [ ] **Step 4: Wire production self-fencing**

```go
g.stopLease = g.locator.startLeaseHeartbeat(ctx, func(err error) {
    gxylog.Fatal(g.ctx, "actor node lease lost",
        gxylog.Str("node", g.nodeInstanceName),
        gxylog.Err(err))
})
```

Graceful module stop cancels and waits for the watchdog before releasing the lease. Claim rejects a locally fenced locator.

- [ ] **Step 5: Run GREEN**

```bash
go test -race -run '^TestActorLocator' -count=1 ./core/gxyactor
```

---

### Task 4: Establish the PostgreSQL Role fence

**Files:**
- Create: `src/apps/role/internal/logic/role_actor_fence.go`
- Create: `src/apps/role/internal/logic/role_actor_fence_test.go`
- Modify: `src/apps/role/internal/logic/role_schema.go`
- Modify: `src/apps/role/internal/logic/role_main.go`

**Interfaces:**
- `advanceRoleActorFence(ctx, db, roleID, owner) error` establishes ownership before Role state loads.
- `lockRoleActorFence(ctx, tx, roleID, owner) error` validates ownership inside saves.
- `errRoleActorOwnershipLost` classifies stale owners.

- [ ] **Step 1: Write failing SQL tests**

```go
func TestAdvanceRoleActorFenceInsertsFirstOwner(t *testing.T) {}
func TestAdvanceRoleActorFenceAdvancesGreaterEpoch(t *testing.T) {}
func TestAdvanceRoleActorFenceAllowsSameOwnerIdempotently(t *testing.T) {}
func TestAdvanceRoleActorFenceRejectsOlderEpoch(t *testing.T) {}
func TestLockRoleActorFenceRejectsMissingOwner(t *testing.T) {}
```

Use `newGormDBForRole` and go-sqlmock. A zero-row advance or lock returns `errRoleActorOwnershipLost`.

- [ ] **Step 2: Run RED**

```bash
go test -run '^(TestAdvanceRoleActorFence|TestLockRoleActorFence)' -count=1 ./src/apps/role/internal/logic
```

- [ ] **Step 3: Add the model and migration**

```go
type RoleActorFence struct {
    RoleID   int64     `gorm:"column:role_id;primaryKey"`
    NodeID   string    `gorm:"column:node_id;not null"`
    Epoch    uint64    `gorm:"column:epoch;not null"`
    UpdateAt time.Time `gorm:"column:update_at;not null"`
}

func (RoleActorFence) TableName() string { return "role_actor_fence" }
```

Add `&RoleActorFence{}` to `InitRoleSchema`. Implement the exact `INSERT ... ON CONFLICT ... WHERE` and `SELECT ... FOR UPDATE` statements from the spec. Reject empty node, epoch zero, SQL errors and zero affected rows.

- [ ] **Step 4: Establish the fence before loading**

At the start of `RoleMain.DelayInit`, reject a missing `actorOwner`, then call `advanceRoleActorFence` before `initRole`.

- [ ] **Step 5: Run GREEN**

```bash
go test -run '^(TestAdvanceRoleActorFence|TestLockRoleActorFence|TestRoleMain)' -count=1 ./src/apps/role/internal/logic
```

---

### Task 5: Fence every Role save transaction

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go`
- Modify: `src/apps/role/internal/logic/role_main_core_test.go`
- Modify: `src/apps/role/internal/logic/role_save_test.go`
- Remove: `src/apps/role/internal/logic/role_owner_fencing_test.go`

**Interfaces:**
- Full Role saves and single-module saves call `lockRoleActorFence` before business writes in the same transaction.
- Ownership loss returns an error, preserves dirty state, and reaches the existing timer-save actor stop path.

- [ ] **Step 1: Write failing transaction-order tests**

```go
func TestSaveLocksRoleFenceBeforeModuleWrites(t *testing.T) {}
func TestSaveRejectsStaleFenceWithoutBusinessWrites(t *testing.T) {}
func TestSaveRoleModuleLocksFenceInSameTransaction(t *testing.T) {}
func TestSaveRoleModuleKeepsDirtyStateOnFenceLoss(t *testing.T) {}
```

Success expects `BEGIN -> SELECT ... FOR UPDATE -> module write -> COMMIT`. Stale ownership expects `BEGIN -> empty fence query -> ROLLBACK` with no business write.

- [ ] **Step 2: Run RED**

```bash
go test -run '^TestSave.*Fence|^TestSaveRoleModule.*Fence' -count=1 ./src/apps/role/internal/logic
```

- [ ] **Step 3: Fence full and single-module saves**

Make `lockRoleActorFence` the first operation inside the existing full-save transaction. Wrap `defaultSaveRoleModule` in one transaction and lock before `saveRoleModuleState`.

Remove `checkRoleSaveOwner` and `roleActorOwnerLookup`; a Redis pre-check has cross-store TOCTOU and no longer authorizes persistence.

Replace version-zero `db.Save(modState)` with `db.Create(modState)` so a stale insert path cannot fall back to UPDATE. Clear dirty state only after commit; retain the existing version rollback bookkeeping.

- [ ] **Step 4: Run GREEN**

```bash
go test -race -run '^(TestSave|TestDirtyRoleModules|TestRoleMain)' -count=1 ./src/apps/role/internal/logic
go test ./src/apps/role/internal/logic ./src/lib/rolelib
```

---

### Task 6: Align architecture documentation and benchmarks

**Files:**
- Modify: `docs/architecture/adr-0006-actor-location-ownership.md`
- Modify: `docs/superpowers/specs/2026-09-03-actor-location-ownership-design.md`
- Modify: `docs/architecture/actor-location.md`
- Modify: `docs/architecture/actor-init-race.md`
- Modify: `docs/architecture/actor-system.md`
- Modify: `docs/architecture/overview.md`
- Modify: `docs/architecture/actor-location-research.md`
- Modify: `core/gxyactor/actor_bench_test.go`
- Modify: `core/gxyactor/actor_locate_bench_test.go`

**Interfaces:**
- Documentation uses Directory Entry, Node Lease, Local Activation and Fencing Epoch consistently.
- Benchmarks use `testing.B.Loop()` and seed valid node leases for directory hits.

- [ ] **Step 1: Remove contradictory claims**

Remove statements that owner keys prove actor liveness, fixed owner TTL makes ownership safe, Redis hits may directly construct PIDs, Role Redis pre-check is true fencing, or shard/online migration is deferred work.

- [ ] **Step 2: Record exact failure semantics**

Document the CP decision, stale-owner self-healing, offline-only redistribution, fatal self-fence, PostgreSQL fencing and permanent rejection of shard ownership and online actor migration.

- [ ] **Step 3: Compile benchmarks and focused tests**

```bash
go test -run '^$' -bench '^$' ./core/gxyactor
go test -run '^(TestGetActorLocateKey|TestRegisterActorLocate|TestGetActorLocateNodeName)' -count=1 ./core/gxyactor
```

---

### Task 7: End-to-end verification and commits

**Files:**
- Verify and commit all changed files.

- [ ] **Step 1: Run ownership race tests**

```bash
go test -race -run '^(TestActorLocator|TestActivation|TestGetActor)' -count=1 ./core/gxyactor
```

- [ ] **Step 2: Run PostgreSQL fencing tests**

```bash
go test -race -run '^(TestAdvanceRoleActorFence|TestLockRoleActorFence|TestSave)' -count=1 ./src/apps/role/internal/logic
```

- [ ] **Step 3: Run package and project verification**

```bash
go test ./core/gxyactor ./src/apps/role/internal/logic ./src/lib/rolelib
go test ./...
go build ./...
```

- [ ] **Step 4: Check generated code and formatting**

```bash
gofmt -w core/gxyactor/*.go src/apps/role/internal/logic/*.go src/lib/rolelib/*.go
git diff --check
git status --short --branch
```

- [ ] **Step 5: Commit coherent changes**

Commit the revised ADR/spec/plan before implementation commits. Keep protocol, directory, lease watchdog, PostgreSQL fence and final documentation in reviewable commits. Do not commit pressure-run artifacts unrelated to the final implementation.
