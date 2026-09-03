# Actor Location Ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace unsafe actor locate `SET`/`DEL` with direct lookup, node leases, atomic Claim/Release, and fencing epochs.

**Architecture:** Keep direct per-actor Redis lookup in the first implementation. Put Redis state transitions behind a private `actorLocator` implementation and expose only owner-aware helpers to actor activation and role persistence. A node lease uses the existing unique `nodeInstanceName` as the per-process lease token; every takeover increments a Redis fencing epoch.

**Tech Stack:** Go 1.25.1, Redis go-redis/v9, Redis Lua scripts, protoactor-go, GoFrame, existing dependency-injection test patterns.

**Spec:** `docs/superpowers/specs/2026-09-03-actor-location-ownership-design.md`

## Global Constraints

- No node Set scan in the locate hot path.
- Claim, takeover, and Release comparisons execute atomically in Redis Lua.
- A Redis command error is infrastructure failure, never a cache miss.
- Redis unavailability fails closed; no local actor creation fallback.
- Old owner operations with a lower fencing epoch must not persist state.
- Use existing `gxyredis.Redis()` injection seam; do not create a second Redis client.
- Follow `docs/development/error-handling.md` and `docs/development/logging.md` for wrapped errors and structured logs.
- Run `gofmt` on changed Go files and targeted tests before broader verification.

---

### Task 1: Define the ownership module seam

**Files:**
- Create: `core/gxyactor/actor_locator.go`
- Test: `core/gxyactor/actor_locator_test.go`

**Interfaces:**
- Produces `ActorOwner{NodeID string, Epoch uint64}`.
- Produces private `actorLocator` methods for `Locate`, `Claim`, `Release`, `AcquireNodeLease`, `RenewNodeLease`.
- Uses an injectable `redis.UniversalClient` field so tests can run against a temporary Redis without replacing global application state.

- [ ] **Step 1: Write the failing unit/integration tests**

Add tests that exercise the desired Redis behavior through the locator seam:

```go
func TestActorLocatorClaimConcurrentHasSingleWinner(t *testing.T) {}
func TestActorLocatorClaimTakesOverExpiredOwnerWithNextEpoch(t *testing.T) {}
func TestActorLocatorReleaseDoesNotDeleteNewOwner(t *testing.T) {}
func TestActorLocatorRedisErrorIsNotMiss(t *testing.T) {}
```

Use a temporary Redis server or the repository's existing Redis test setup. The concurrent test starts two goroutines with different lease tokens and asserts exactly one `acquired` result and one owner record. The takeover test seeds an owner whose node lease is absent, then asserts the new epoch is greater. The release test seeds owner A, replaces it with owner B, calls Release for A, and asserts B remains. The error test closes the Redis client before Locate and asserts a non-nil infrastructure error.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
RUN_REDIS_TESTS=1 go test -run '^TestActorLocator' -count=1 ./core/gxyactor
```

Expected: FAIL because `actorLocator` and its operations do not exist.

- [ ] **Step 3: Add the minimum ownership types and Redis script constants**

Define:

```go
type ActorOwner struct {
    NodeID string
    Epoch  uint64
}

type actorLocator struct {
    redis      redis.UniversalClient
    nodeID     string
    leaseToken string
}
```

Use explicit key builders for actor owner, node lease, and epoch counter. Encode owner values as a stable, parseable string containing node ID and epoch. Return typed operation results so callers can distinguish acquired, already-owned, owned-by-other, and invalid-lease outcomes.

- [ ] **Step 4: Run the tests and verify GREEN**

Run the same targeted command. Confirm all four tests pass and Redis errors are returned rather than converted to misses.

- [ ] **Step 5: Commit**

```bash
git add core/gxyactor/actor_locator.go core/gxyactor/actor_locator_test.go
git commit -m "feat: add actor ownership locator seam"
```

---

### Task 2: Wire atomic Claim, Release, and node lease

**Files:**
- Modify: `core/gxyactor/actor_locator.go`
- Modify: `core/gxyactor/activator_manager.go`
- Test: `core/gxyactor/actor_locator_test.go`

**Interfaces:**
- `actorLocator.Claim(ctx, kind, id string) (ActorOwner, claimResult, error)` validates the caller lease and atomically claims or returns the current owner.
- `actorLocator.Release(ctx, kind, id string, owner ActorOwner) error` compare-and-deletes only the matching owner.
- `actorLocator.AcquireNodeLease(ctx) error` creates the node lease with the process token.
- `actorLocator.RenewNodeLease(ctx) error` refreshes TTL only when the stored token matches.
- `activatorManager` owns one locator and starts/stops the node lease heartbeat with the module lifecycle.

- [ ] **Step 1: Add failing edge-case tests**

Add tests for:

```go
func TestActorLocatorClaimActiveOwnerReturnsExistingOwner(t *testing.T) {}
func TestActorLocatorClaimRejectsInvalidLease(t *testing.T) {}
func TestActorLocatorRenewDoesNotRefreshDifferentToken(t *testing.T) {}
func TestActorLocatorReleaseMatchesNodeAndEpoch(t *testing.T) {}
```

- [ ] **Step 2: Run RED**

```bash
RUN_REDIS_TESTS=1 go test -run '^TestActorLocator' -count=1 ./core/gxyactor
```

Expected: the new cases fail against the incomplete implementation.

- [ ] **Step 3: Implement scripts and lifecycle**

Implement Claim as one Lua operation:

1. compare the candidate lease key with the candidate token;
2. read the actor owner;
3. if the existing owner lease is present, return that owner;
4. otherwise increment the fencing counter and write the new owner;
5. return the operation result and owner fields.

Implement Release as compare-and-delete on node ID and epoch. Implement lease acquisition with `SET NX EX` and renewal with a token comparison script. Use the existing unique `nodeInstanceName` as the lease token so a restart cannot renew the previous process lease. Start a bounded heartbeat goroutine in `OnModStart`; cancel and wait for it in `OnModStop`.

Move stale actor cleanup to conditional Release. Keep `trackActor` only as a cleanup inventory; it must never perform raw `DEL` on an actor owner key.

- [ ] **Step 4: Run GREEN and lifecycle tests**

```bash
RUN_REDIS_TESTS=1 go test -run '^(TestActorLocator|TestActivatorManager)' -count=1 ./core/gxyactor
```

- [ ] **Step 5: Commit**

```bash
git add core/gxyactor/actor_locator.go core/gxyactor/activator_manager.go core/gxyactor/actor_locator_test.go
git commit -m "feat: add atomic actor ownership claim"
```

---

### Task 3: Gate actor activation on Claim

**Files:**
- Modify: `core/gxyactor/activator_manager.go`
- Modify: `core/gxyactor/helper.go`
- Test: `core/gxyactor/actor_test.go`
- Test: `core/gxyactor/actor_locator_test.go`

**Interfaces:**
- Actor activation uses `actorLocator.Claim` before `SpawnNamed`.
- A losing activator returns the existing owner's PID when its node address is available, or a typed ownership error that causes the caller to re-locate.
- `GetActorOwner(ctx, kind, id)` exposes owner metadata to persistence and notification adapters without exposing Redis commands.

- [ ] **Step 1: Write failing activation tests**

Add tests that prove:

```go
func TestActorActivatorDoesNotSpawnAfterClaimLost(t *testing.T) {}
func TestActorActivatorClaimWinnerRegistersEpoch(t *testing.T) {}
func TestGetActorOwnerReturnsNodeAndEpoch(t *testing.T) {}
```

The losing test injects a locator returning `owned_by_other` and asserts the spawn function is not called. The winner test asserts the actor registration stores the claimed owner in an `actorActivator.owners` map keyed by PID rather than overwriting Redis with a raw SET.

- [ ] **Step 2: Run RED**

```bash
go test -run '^(TestActorActivator|TestGetActorOwner)' -count=1 ./core/gxyactor
```

Expected: FAIL because activation still calls `SpawnNamed` before ownership Claim and owner metadata is unavailable.

Change `actorActivator.HandleMessage` so Claim occurs before `SpawnNamed`. On `owned_by_other`, resolve the existing node through the current service lookup and return its PID without creating a local actor. On Claim success, pass the returned owner into registration and Touch failure cleanup; store the owner in `actorActivator.owners[pid]`. Pass the owner as a second actor initialization argument with `SpawnNamed(props, msg.Id, msg.Id, owner)` so RoleMain can retain the exact epoch. Replace `registerActorLocate` raw `SET` with Claim-backed registration. Replace unregister raw `DEL` with conditional Release.

Keep `getActor` direct lookup behavior, but route owner metadata through the locator. A Redis error must return an error; only Redis Nil/missing owner enters the spawn path.

- [ ] **Step 4: Run GREEN**

```bash
go test -run '^(TestActorActivator|TestGetActorOwner|TestActorLocate)' -count=1 ./core/gxyactor
```

- [ ] **Step 5: Commit**

```bash
git add core/gxyactor/activator_manager.go core/gxyactor/helper.go core/gxyactor/actor_test.go core/gxyactor/actor_locator_test.go
git commit -m "feat: gate actor activation on ownership claim"
```

---

### Task 4: Carry fencing epoch into Role persistence

**Files:**
- Modify: `src/apps/role/internal/logic/role_main.go`
- Modify: `src/lib/rolelib/rolelib.go`
- Modify: `core/gxyactor/helper.go`
- Test: `src/apps/role/internal/logic/role_main_test.go`
- Test: `src/apps/role/internal/logic/role_save_test.go`

**Interfaces:**
- `RoleMain` stores the `ActorOwner` passed as the second actor initialization argument.
- `checkRoleSaveOwner(ctx)` validates exact node ID and epoch against the current locator owner.
- Role notification lookup continues to return a node ID through the locator adapter; it does not read Redis directly.

- [ ] **Step 1: Write failing save-fencing tests**

Add cases:

```go
func TestCheckRoleSaveOwnerRejectsOlderEpoch(t *testing.T) {}
func TestCheckRoleSaveOwnerAcceptsSameNodeAndEpoch(t *testing.T) {}
func TestCheckRoleSaveOwnerRejectsDifferentNode(t *testing.T) {}
```

Inject the locator/owner lookup through existing package-level dependency variables. Seed a RoleMain with epoch 41; return current epoch 42 and assert the save is skipped. Return the same node and epoch and assert the save path proceeds.

- [ ] **Step 2: Run RED**

```bash
go test -run '^(TestCheckRoleSaveOwner|TestSaveRole)' ./src/apps/role/internal/logic
```

Expected: FAIL because RoleMain stores no epoch and compares only the node string.

Pass the claimed owner as the second actor initialization argument: `SpawnNamed(props, msg.Id, msg.Id, owner)`. In `RoleMain.Init`, require and parse `args[1].(gxyactor.ActorOwner)` for production actor activation and retain it on the RoleMain. Replace direct `rolelib.GetRoleLocateKey(...).GET` with `gxyactor.GetActorOwner`. Reject missing owner, Redis errors, different node, and epoch mismatch; only an exact current node+epoch is accepted for persistence. Keep dirty state intact when persistence is rejected.

Change `rolelib.roleLocateNode` to call the locator adapter instead of reading Redis directly, preserving its existing notification behavior and tests.

- [ ] **Step 4: Run GREEN**

```bash
go test -run '^(TestCheckRoleSaveOwner|TestSaveRole|TestPublishRoleNotify)' ./src/apps/role/internal/logic
```

- [ ] **Step 5: Commit**

```bash
git add src/apps/role/internal/logic/role_main.go src/lib/rolelib/rolelib.go core/gxyactor/helper.go src/apps/role/internal/logic/*_test.go
git commit -m "feat: fence role persistence by actor epoch"
```

---

### Task 5: Update architecture documentation and compatibility tests

**Files:**
- Modify: `docs/architecture/actor-location.md`
- Modify: `docs/architecture/actor-location-research.md`
- Modify: `docs/architecture/actor-init-race.md`
- Test: `core/gxyactor/actor_bench_test.go`
- Test: `core/gxyactor/actor_test.go`

**Interfaces:**
- Documentation reflects direct lookup, node lease, atomic Claim/Release, and fencing.
- Existing direct lookup benchmarks continue to measure the hot path; node Set scan remains comparison-only.

- [ ] **Step 1: Add failing compatibility assertions**

Update existing Redis integration tests so they expect an encoded owner containing node ID and epoch, and add a test that a second registration cannot overwrite a live owner. Run the focused tests and confirm they fail against old test expectations.

- [ ] **Step 2: Implement documentation and test updates**

Document exact key formats, lifecycle order, error semantics, stale cleanup, and network-partition limits. Remove statements claiming raw `SET`/`DEL` or a 12-hour key alone ensures safety. Keep the measured Redis layout comparison linked as evidence.

- [ ] **Step 3: Run focused compatibility tests**

```bash
go test -run '^(TestGetActorLocateKey|TestRegisterActorLocate|TestGetActorLocateNodeName)' -count=1 ./core/gxyactor
```

- [ ] **Step 4: Commit**

```bash
git add docs/architecture/actor-location.md docs/architecture/actor-location-research.md docs/architecture/actor-init-race.md core/gxyactor/actor_bench_test.go core/gxyactor/actor_test.go
git commit -m "docs: record actor ownership fencing model"
```

---

### Task 6: Run end-to-end verification

**Files:**
- Verify all changed files; no new implementation files.

- [ ] **Step 1: Run ownership integration tests**

```bash
RUN_REDIS_TESTS=1 go test -race -run '^(TestActorLocator|TestActorActivator|TestGetActorOwner)' -count=1 ./core/gxyactor
```

Expected: PASS, including concurrent Claim, expired-lease takeover, stale Release protection, and invalid lease rejection.

- [ ] **Step 2: Run Role persistence and notification tests**

```bash
go test -race -run '^(TestCheckRoleSaveOwner|TestSaveRole|TestPublishRoleNotify)' -count=1 ./src/apps/role/internal/logic ./src/lib/rolelib
```

Expected: PASS with old epochs rejected and same owner epochs accepted.

- [ ] **Step 3: Run package regression tests**

```bash
go test ./core/gxyactor ./src/apps/role/internal/logic ./src/lib/rolelib
```

- [ ] **Step 4: Build all packages**

```bash
go build ./...
```

- [ ] **Step 5: Check formatting and final diff**

```bash
gofmt -l core/gxyactor src/apps/role/internal/logic src/lib/rolelib
git diff --check
```

Expected: no formatting output and no whitespace errors. Preserve unrelated untracked benchmark/research files unless they are explicitly part of the commits above.
