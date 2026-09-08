# Task 6 Report — Ergo-backed Chat Channel

## Status

Implemented the Chat Channel Actor path on the runtime-neutral `gxyactor` seam and verified it across two live Ergo nodes. Channel production and focused tests no longer import or name Protoactor runtime types.

## Changes

- `src/apps/chat/channel_actor.go`
  - Replaced the member's Protoactor PID with `gxyactor.PID`.
  - Kept register/unregister/send/history state transitions mailbox-owned.
  - Added the neutral `ReqChatChannelHistory` call handler so Ergo `Call` returns the existing `RspChatChannelHistory` contract rather than treating a request as a one-way message.
- `src/apps/chat/channel_actor_test.go`
  - Updated white-box assertions and lifecycle comments to use only neutral PID/context types.
  - Corrected the test fixture to inject its selected channel strategy before exercising message handlers.
- `core/gxyactor/internal/ergo/message.go`
  - Added descriptor-based protobuf fallback registration/decoding using the global generated protobuf registry. Remote channel protobuf values therefore use `GServerEnvelope` without requiring business code to import the private adapter or maintain a second registration list.
- `core/gxyactor/runtime.go`, `core/gxyactor/actor.go`, `core/gxyactor/helper.go`, `core/gxyactor/internal/ergo/actor.go`
  - Added runtime binding to callback contexts and helper dispatch. A callback on node A now responds/sends through node A's adapter even when a focused two-node test has another adapter installed globally.
- `core/gxyactor/stage_ergo_test.go`
  - Added a real two-node Ergo stage test. It activates a channel through an ownership-gated activation callback, sends remote register/unregister/send protobuf values, calls history remotely, stops the process, confirms local publication disappears, reactivates, and verifies fresh state through another remote Send/Call cycle.

## Verification

Required focused chat command:

```text
go test ./src/apps/chat -run 'Test(Channel|Chat)' -count=1
ok   gserver/src/apps/chat 0.188s
```

Required two-node stage command (also run three times for stability):

```text
go test ./core/gxyactor -run '^TestErgoChannelTwoNodeActivationSendCallTerminateReactivate$' -count=3
ok   gserver/core/gxyactor 0.445s
```

The Ergo adapter package was also rerun after the callback and envelope changes:

```text
go test ./core/gxyactor/internal/ergo -count=1
ok   gserver/core/gxyactor/internal/ergo 1.431s
```

No formatter, linter, or project-wide suite was run.

## Concerns / boundaries

- The stage test uses the existing Task 5 activation callback boundary and an in-test owner gate; it does not replace Claim/Release/fencing or alter the production activator. The adapter continues to delegate activation ownership to that boundary.
- Ergo's runtime creation identifies the node incarnation, not each actor activation; reactivation is proven by termination cleanup and fresh mailbox state, not by requiring a different normalized PID creation string.
- The global protobuf registry fallback is intentionally limited to generated protobuf descriptors; explicitly registered stable names continue to take precedence, and unknown/malformed envelope errors remain typed.
- Gateway/Guild/Role migration and Protoactor dependency removal are intentionally out of scope.
