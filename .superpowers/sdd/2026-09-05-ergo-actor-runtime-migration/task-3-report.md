# Task 3 Report — Ergo Runtime Adapter and Wire Envelope

## Status

Implemented the private adapter under `core/gxyactor/internal/ergo/`. The adapter satisfies the Task 2 runtime-neutral interface without exposing Ergo types through `core/gxyactor`. No business actors, ownership/lease/fencing code, lifecycle observability, or Protoactor implementation was migrated or removed.

## Ergo dependency selection

Pinned `ergo.services/ergo v1.999.330` in `go.mod` and `go.sum`. The module declares `go 1.21`, so it is compatible with this repository's Go 1.25.1 module. I read the downloaded source before implementation: this release provides the exact `ergo.StartNode`, `gen.Node`, `gen.Process`, `gen.PID`, `gen.Network`, and `act.Actor` APIs named by the brief. The older `github.com/halturin/ergo v1.2.6` release was also inspected, but it uses a different module/API surface and does not provide the `ergo.services/ergo/gen` + `act` API required by the Task 3 contract.

## Implementation

- `runtime.go`: pinned-node startup options (name, instance name, cookie, acceptor host/port, network options, shutdown timeout), runtime-neutral `gxyactor.Runtime` implementation, spawn, local lookup, Send/Call/Respond/Stop, timeout enforcement, and stable error mapping. Activation is delegated through an injected callback; no ownership semantics are implemented here.
- `actor.go`: private Ergo `act.Actor` bridge, neutral lifecycle/context/request conversion, handler dispatch, response-error routing, watch/unwatch, and termination cleanup.
- `pid.go`: normalized `Runtime/Node/ID/Creation` conversion, Ergo creation preservation, canonical local node instance normalization, runtime/stale-incarnation rejection, and private raw PID mapping.
- `message.go`: explicit `GServerEnvelope{Type, Data}`, stable constructor registry, protobuf marshal/unmarshal, and typed unknown/missing/malformed wire errors. Only protobuf business values are enveloped for remote targets; local values remain direct. Lifecycle and activation controls are never encoded as business envelopes; the adapter registers the envelope and neutral control marker types with Ergo's registry but never routes those controls remotely.
- `runtime_test.go`: deterministic envelope and PID validation tests plus real local Ergo-node Send/Call/timeout/response-error/Stop coverage.

## Verification

Focused command required by the brief:

```text
go test ./core/gxyactor/internal/ergo -count=1
ok   gserver/core/gxyactor/internal/ergo 1.255s
```

The changed Go files were formatted with:

```text
gofmt -w core/gxyactor/internal/ergo/*.go
```

The focused tests were rerun against the committed tree after the final review-round fixes. No formatter output was produced. No linter or project-wide suite was run.

## Concerns / follow-up boundaries

1. Ergo's node-level `CallWithTimeout` accepts integer seconds. The adapter executes it asynchronously and applies the caller's exact `context`/duration deadline locally, so sub-second caller timeouts do not block; the underlying Ergo request may finish later and its result is discarded.
2. Task 4 owns full activation/lifecycle observability and ownership integration. `Spawn` records the runtime PID after Ergo accepts the process; delayed business `DelayInit` confirmation and Claim/Release remain outside this adapter as required.
3. Normalized logical PID map misses now fail closed with `ErrUnknownPID`; an explicitly injected runtime-local resolver may provide the raw PID only when it returns a nonzero PID with matching creation. No numeric suffix is fabricated.
## Review round 1 fixes

The review regression tests were first run before production fixes and failed as expected: compilation reported the missing `Adapter.resolvePID` field and `ErrDuplicateRegistration`; after those symbols were added, the deterministic no-route assertion failed with `no-route error = no route`.

After the fixes, the committed-tree focused command passed:

```text
gofmt -w core/gxyactor/internal/ergo/*.go
go test ./core/gxyactor/internal/ergo -count=1
ok   gserver/core/gxyactor/internal/ergo 1.255s
```

The changes remain limited to the adapter, adapter tests, and this report. No Task 4 lifecycle/ownership work was added.
