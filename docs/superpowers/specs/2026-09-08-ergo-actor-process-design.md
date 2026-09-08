# Ergo-Native Actor Process Design

## Goal

Remove the Protoactor-shaped `ActorBase.Receive` lifecycle bridge and the embedded business `ActorBase`. Map Ergo callbacks directly into one adapter-owned GServer process wrapper while keeping timer, handler dispatch, tracing, metrics, logging, and stop-reason behavior centralized.

## Decision

`internal/ergo.ergoActor` owns a `gxyactor.ActorProcess`. `ActorProcess` owns the business `IActor` and all cross-cutting process state. Business Actor structs contain only business state and receive a callback-scoped `gxyactor.ActorContext`.

```text
Ergo act.Actor callback
        ↓
internal/ergo.ergoActor
        ↓
gxyactor.ActorProcess
        ↓
business IActor callback
```

No callback is converted into a synthetic lifecycle message.

## Interfaces

`IActor` becomes the business behavior contract:

```go
type IActor interface {
    Init(ActorContext, []any) error
    DelayInit(ActorContext) error
    HandleMessage(ActorContext, any) error
    Terminate(ActorContext, error)
}
```

`ActorContext` embeds `context.Context` so it remains valid for database, logging, tracing, and reflected handler calls. It exposes only callback-valid Actor capabilities:

```go
type ActorContext interface {
    context.Context
    Sender() PID
    Self() PID
    Stop(error)
    Watch(PID)
    Unwatch(PID)
    Children() []PID
    Timer() *ActorTimer
    Span() trace.Span
    SetLogValue(string, any)
    AddMsgHandler(any, ...string) []*gxyutil.MethodMeta
    AutoHandleMsg(any) (any, error)
    Respond(any, ...error) error
}
```

The concrete context remains private to `internal/ergo`. Runtime-specific PID, process, request reference, and tracing transport state do not cross the seam.

## ActorProcess responsibilities

`ActorProcess` is created by the Ergo adapter after calling the registered producer. It owns:

- the business `IActor`;
- actor kind and persistent logging context;
- `ActorTimer`;
- reflected message-handler registry;
- active metrics state;
- the application stop reason.

Its adapter-facing methods are:

```go
func NewActorProcess(kind string, actor IActor) *ActorProcess
func (*ActorProcess) Init(ActorContext, []any) error
func (*ActorProcess) HandleMessage(ActorContext, any) error
func (*ActorProcess) HandleCall(ActorContext, any) (any, error)
func (*ActorProcess) Terminate(ActorContext, error)
```

Ergo guarantees one `ProcessInit`, sequential message/call callbacks, and one `ProcessTerminate`; `ActorProcess` therefore contains no mutex and no `sync.Once`.

## Lifecycle mapping

```text
ergoActor.Init(args...)
  → ActorProcess.Init(ctx, args)
  → business.Init
  → register business handlers
  → business.DelayInit

ergoActor.HandleMessage(message)
  → decode transport envelope/monitor event
  → ActorProcess.HandleMessage(ctx, message)
  → timer/control handling or business.HandleMessage

ergoActor.HandleCall(request)
  → decode
  → ActorProcess.HandleCall(ctx, request)
  → reflected handler dispatch
  → encode returned response

ergoActor.Terminate(reason)
  → ActorProcess.Terminate(ctx, effectiveReason)
  → stop timer
  → business.Terminate
  → decrement active metric
```

An initialization error is returned to Ergo synchronously and prevents process publication. A message/call error is returned to Ergo and terminates that process. Panics are recovered at the `ActorProcess` seam, logged once with Actor identity, and returned as errors. Business `ctx.Stop(reason)` stores the application reason before requesting a normal Ergo stop; termination prefers that application reason.

## Message model

Delete Protoactor lifecycle artifacts:

- `ActorBase.Receive`;
- `LifecycleError` and side-channel error fields;
- `ActorInitMsg`;
- `LifecycleMessage` and its constants;
- `ActorStartedMessage`;
- `ActorStoppedMessage`;
- `ContextDecorator` and decorated contexts.

`ActorTerminatedMessage` remains: it is a runtime-neutral watched-process notification, not a lifecycle callback. Ergo `gen.MessageDownPID` is translated to it before business dispatch.

## Business migration

Remove `*gxyactor.ActorBase` from Channel, Session, Guild, Role, and Activator structs. Constructors no longer create base contexts. Replace promoted calls with callback context operations:

```text
Timer()          → ctx.Timer()
Stop(err)        → ctx.Stop(err)
Self()           → ctx.Self()
Span()           → ctx.Span()
SetLogValue(...) → ctx.SetLogValue(...)
AddMsgHandler    → ctx.AddMsgHandler
AutoHandleMsg    → ctx.AutoHandleMsg
Actx.Watch       → ctx.Watch
Actx.Unwatch     → ctx.Unwatch
Respond(...Actx) → ctx.Respond
```

Methods reached by reflection may accept `gxyactor.ActorContext`; it embeds `context.Context`, satisfying the existing handler discovery rule.

## Testing

- ActorProcess lifecycle tests call explicit `Init`, `HandleMessage`, `HandleCall`, and `Terminate` methods; no synthetic messages.
- Verify init and delay-init ordering, init failure, panic/error propagation, timer mailbox activation, handler dispatch, stop reason, metrics cleanup, and one real Ergo path.
- Migrate Chat, Session, Guild, Role, Activator, and adapter tests to construct business Actors without `ActorBase`.
- Run `go test -race ./core/gxyactor/...`, `go test ./...`, `go build ./...`, and `make lint`.

## Non-goals

- No compatibility aliases or deprecated lifecycle messages.
- No changes to Redis ownership, activation Claim/Release, PostgreSQL fencing, protobuf business protocols, or network envelope format.
- No alternate runtime implementation.
