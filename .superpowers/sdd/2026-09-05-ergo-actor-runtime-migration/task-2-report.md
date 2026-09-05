# Task 2 Report — Runtime-Neutral `gxyactor` Seam

## Status

Implemented and committed as `4730eb5` (`refactor: add runtime-neutral actor seam`).

## Changed files

- `core/gxyactor/runtime.go`
  - Added normalized `PID` with `Runtime`, `Node`, `ID`, and `Creation` identity dimensions.
  - Added runtime-neutral `Request`, `ActorContext`, `Runtime`, lifecycle markers, and runtime installation seam.
  - Added equality and legacy-adapter normalization helpers.
- `core/gxyactor/runtime_test.go`
  - Added focused contract tests for all PID identity dimensions, opaque Send/Call messages, runtime helper dispatch, and ActorProducer without a Protoactor requirement.
- `core/gxyactor/actor.go`
  - Removed Protoactor types from `IActor`, `ActorProducer`, `ActorBase`, context storage, Receive, and decorator APIs.
  - Kept lifecycle ordering, handler registration, timer dispatch, panic/error stop behavior, tracing, metrics, logging, and termination cleanup through neutral messages/context.
- `core/gxyactor/helper.go`
  - Changed public Send/Call/Respond/Stop/Activate/local lookup helpers to normalized PID and opaque values.
  - Added runtime dispatch while preserving legacy application fallback and helper aliases.
- `core/gxyactor/system.go`
  - Kept the existing Protoactor implementation private to the legacy adapter and added normalized PID conversion.
  - Added a private context/actor bridge so runtime-specific process contexts do not cross the public seam.
- `core/gxyactor/actor_mgr.go`
  - Stores normalized PIDs and accepts legacy values only at the private compatibility boundary.
- `core/gxyactor/activator_manager.go`
  - Updated activation bookkeeping and pool/termination paths for normalized PID values; Protoactor responses are converted only at the private adapter boundary.
- `core/gxyactor/actor_test.go`
  - Ported focused manager/equality/decorator/router assertions to normalized PID semantics.
- `core/gxyactor/actor_activation_ownership_test.go`
  - Ported ownership activation test support to normalized PID values.
- `core/gxyactor/actor_bench_test.go`
  - Ported benchmark support assertions to normalized PID values.

`core/gxyactor/types.go` and `core/gxyactor/actor_timer.go` required no source changes: their public types already use the neutral ActorService/timer/PID seam after the changes above.

## TDD evidence

### Failing contract-test command

Command run before production seam changes:

```text
go test ./core/gxyactor -run 'Test(PID|Runtime|ActorBase|ActorTimer)' -count=1
```

Expected failure: the new contract test could not compile because the checkout does not contain the generated `gserver/gameconfig/gosrc` package; setup failed with:

```text
src/util/common.go:6:2: package gserver/gameconfig/gosrc is not in std (/usr/local/go/src/gserver/gameconfig/gosrc)
FAIL gserver/core/gxyactor [setup failed]
```

The failure occurred before production changes and was an environment/repository generated-source prerequisite, not a contract assertion failure.

### Passing focused test command

The same focused tests were run after implementation, with a temporary, removed generated-source stub containing only the existing `GardenProbEntry` fields needed by `src/util/common.go`:

```text
go test ./core/gxyactor -run 'Test(PID|Runtime|ActorBase|ActorTimer|ActorMgr|ActivatorRouter)' -count=1
```

Output summary:

```text
ok   gserver/core/gxyactor  0.173s
```

Compile-only affected-package check also passed with the same temporary generated-source stub:

```text
go test ./core/gxyactor -run '^$' -count=1
ok   gserver/core/gxyactor  0.175s [no tests to run]
```

No formatter, linter, or project-wide suite was run.

## Public interface decisions

- `PID` is a value with exactly the required normalized identity dimensions. Equality compares all four dimensions; zero identities are treated as non-identities and do not compare equal.
- `Runtime` is the smallest public operation interface needed by current helpers and the future Ergo adapter: actor-kind registration, activation, local lookup, Send, Call, Respond, and Stop.
- Business messages are `any`; Send and Call no longer require `proto.Message`.
- `Request` exposes only a normalized sender identity, and `ActorContext` supplies message, header, self, sender, watch/unwatch, child listing, and stop operations. No process, envelope, supervisor, or runtime-specific type appears in those interfaces.
- Lifecycle is represented by neutral started/stopping/stopped/auto-response markers and started/stopped payloads. ActorBase continues Init → handler registration → DelayInit → mailbox handling → Terminate ordering.
- Respond accepts the neutral request handle and an optional error, allowing adapters to preserve typed response failure semantics.
- Existing Protoactor process/context/envelope handling remains only in the private legacy bridge in `system.go`; it is not part of the business-facing API.

## Concerns

1. The repository checkout lacks the generated `gameconfig/gosrc` package, so focused verification required a temporary local stub that was removed after each command. The committed tree contains no generated stub.
2. The existing legacy bootstrap still depends on Protoactor internally until the later Ergo adapter task. The dependency is confined to private runtime/activator implementation paths; the public `gxyactor` contract does not expose those symbols.
3. Business Actor packages and their runtime-specific test fakes still require the later migration tasks to consume the neutral context/PID signatures end to end. Task 2 intentionally does not migrate those business Actors.
