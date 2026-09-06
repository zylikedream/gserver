# Task 7 Report — Gateway Session, Guild, Role, and Role Libraries

## Status

Migrated the remaining business-facing actor paths to the runtime-neutral `gxyactor` seam. No Protoactor imports or concrete runtime symbols remain in `src/apps/gateway`, `src/apps/guild`, `src/apps/role`, or `src/lib`.

## Changes

- Gateway Session now forwards client requests with neutral `gxyactor.Send`, preserving the existing fire-and-forget behavior while allowing the current Ergo callback runtime to supply the Session sender identity. Login admission/rejection, endpoint PID storage, role termination cleanup, and session close behavior remain unchanged.
- Shared actor lookup helpers now dispatch `GetLocalActor` and `GetLocalActorAll` through the installed runtime. Role service weight uses runtime-neutral local enumeration rather than the legacy actor application count.
- Role notification routing now obtains the local canonical node identity through a neutral `gxyactor.NodeInstanceName` capability and uses lookup-only `ActivateActor(..., false)` to resolve the target owner node without spawning. Local notification delivery and enumeration therefore work with the Ergo adapter.
- Ergo adapter exposes only its canonical node-instance name as a string capability for the neutral helper; no Ergo type crosses into business code.
- Removed stale runtime-specific terminology from Gateway comments/tests and updated role-library PID fixtures to Ergo-neutral identity values.

## Verification

Required package command:

```text
go test ./src/apps/gateway ./src/apps/guild ./src/apps/role ./src/lib/... -count=1
?    gserver/src/apps/gateway [no test files]
?    gserver/src/apps/guild [no test files]
ok   gserver/src/apps/role 0.521s
?    gserver/src/lib [no test files]
ok   gserver/src/lib/gatetoken 0.476s
?    gserver/src/lib/guildlib [no test files]
ok   gserver/src/lib/rolelib 0.512s
```

Focused business tests:

```text
go test ./src/apps/gateway/internal/logic ./src/apps/guild/logic ./src/apps/role/internal/logic ./src/lib/rolelib -count=1
ok   gserver/src/apps/gateway/internal/logic 0.845s
ok   gserver/src/apps/guild/logic 0.698s
ok   gserver/src/apps/role/internal/logic 0.897s
ok   gserver/src/lib/rolelib 0.680s
```

Runtime-neutral helper and adapter tests:

```text
go test ./core/gxyactor -run 'Test(RuntimeNeutral|RuntimeDispatchesLocal|Uninitialized)' -count=1
ok   gserver/core/gxyactor 0.180s

go test ./core/gxyactor/internal/ergo -count=1
ok   gserver/core/gxyactor/internal/ergo 1.382s
```

Prohibited-symbol grep (imports, Protoactor concrete types, Ergo concrete types) returned no matches:

```text
grep pattern: github\\.com/asynkron/protoactor-go | actor\\.(PID|Context|Props|...) | remote\\.(Remote|Configure) | gen\\.(Node|Process|PID|Supervisor) | act\\.Actor
paths: src/apps/gateway; src/apps/guild; src/apps/role; src/lib
result: No matches found
```

`git diff --check` passed with no whitespace errors.

## Concerns / boundaries

- The legacy `CallSync` helper remains private to the legacy activation/runtime internals; no Task 7 business caller uses it. Session request delivery uses neutral `Send` and intentionally continues to ignore transport errors as before.
- Ownership, Claim/Release, epoch checks, PostgreSQL role fencing, mailbox serialization, timers, dirty tracking, and periodic save logic were not weakened or moved. Existing focused tests cover login rejection, termination cleanup, duplicate-login behavior, and fence-sensitive saves.
- Protoactor module removal and production bootstrap/configuration cleanup remain Task 8 work.

## Committed-tree rerun

After commit `dd85127`, the required package command was rerun:

```text
go test ./src/apps/gateway ./src/apps/guild ./src/apps/role ./src/lib/... -count=1
?    gserver/src/apps/gateway [no test files]
?    gserver/src/apps/guild [no test files]
ok   gserver/src/apps/role 0.603s
?    gserver/src/lib [no test files]
ok   gserver/src/lib/gatetoken 0.579s
?    gserver/src/lib/guildlib [no test files]
ok   gserver/src/lib/rolelib 0.594s
```

The prohibited-symbol grep was rerun against the committed production trees and returned `No matches found`; the worktree was clean after verification.
