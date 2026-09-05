# Task 4 Report — Actor Lifecycle, Supervision, Observability, Timers

## Status

Implemented and committed with the Task 4 changes. The public `core/gxyactor` seam remains runtime-neutral; Ergo-specific process and tracing translation remains private under `core/gxyactor/internal/ergo`.

## Changes

- `ActorBase` now completes `Init`, reflection-handler registration, and `DelayInit` as one lifecycle gate before normal mailbox messages are handled. Pre-ready business messages fail rather than reaching business state.
- Initialization and receive failures are retained as lifecycle errors so the Ergo `ProcessInit` callback returns `ActorInitFailed`; failed processes are removed from the adapter PID map before returning.
- Handler errors and panics request stop and are returned by the Ergo adapter, preserving one-for-one stop semantics. Reflection response errors are sent as response errors and do not become successful values.
- Termination is guarded by `sync.Once`: timer sources stop before business `Terminate`; the original stop reason is retained; active metrics decrement only once.
- Timer callbacks only enqueue `ActorTimerMsg`; cron state is updated from `ActorTimer.Active` while handling the mailbox message, never from the timer goroutine.
- Ergo callback contexts carry the current Ergo trace as an OpenTelemetry remote span context and retain the private process callback for traced local/remote Send and Call. Response handles accept the neutral actor context without exposing Ergo types.
- Legacy logger terminology was changed from `Proto.Actor` to the runtime-neutral `gserver-actor-runtime` label. Final actor error logs include actor kind, node, actor ID, and the existing structured error field.

## TDD / regression coverage

Added `core/gxyactor/lifecycle_test.go` covering:

- Init → DelayInit → business message → Terminate ordering.
- Init failure preventing business message delivery and requesting stop.
- Handler panic requesting stop.
- Cron state mutation only during mailbox-side timer activation.

Updated the narrow Ergo adapter test contract so a handler response error is followed by an unavailable/stopped PID, matching ADR 0008 one-for-one stop behavior. Added adapter coverage that failed initialization does not publish a local PID.

## Verification

Commands run:

```text
go test ./core/gxyactor -run 'Test(Lifecycle|Actor|Timer|Trace)' -count=1
ok   gserver/core/gxyactor 0.607s

go test ./core/gxyactor/internal/ergo -run '^TestAdapterLocalSendCallTimeoutAndStop$' -count=1
ok   gserver/core/gxyactor/internal/ergo 1.229s

go test ./core/gxyactor/internal/ergo -run '^TestAdapterInitFailureDoesNotPublishPID$' -count=1
ok   gserver/core/gxyactor/internal/ergo 0.229s

go test ./core/gxyactor/internal/ergo -run 'Test(Adapter|Start|Spawn)' -count=1
ok   gserver/core/gxyactor/internal/ergo 1.352s

go test ./core/gxyactor/internal/ergo -count=1
ok   gserver/core/gxyactor/internal/ergo 1.354s

go test ./core/gxyactor -count=1
ok   gserver/core/gxyactor 0.655s
```

## Concerns / boundaries

- Activation publication, pending waiter completion, Claim/Release, and Actor Directory ownership remain outside this task. The adapter only removes its private process/PID bookkeeping on failed initialization or termination; Task 5 owns the conditional ownership release boundary.
- Ergo's native tracing carries identity through process Send/Call. The adapter reconstructs an OpenTelemetry remote span context for GServer handlers; no Ergo type crosses the public seam.
- No formatter, linter, or project-wide suite was run.

## Committed-tree rerun

After commit `d9a2de8`, the required focused command and the minimal touched-adapter command were rerun:

```text
go test ./core/gxyactor -run 'Test(Lifecycle|Actor|Timer|Trace)' -count=1
ok   gserver/core/gxyactor 0.609s

go test ./core/gxyactor/internal/ergo -run 'Test(Adapter|Start|Spawn)' -count=1
ok   gserver/core/gxyactor/internal/ergo 1.353s
```
