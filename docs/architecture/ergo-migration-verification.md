# Ergo Runtime Migration Verification

Date: 2026-09-06
Branch: `feat/ergo-actor-runtime`

## Automated verification

| Check | Result |
|---|---|
| `go build ./...` | Pass |
| `make lint` | Pass; root and `client` both report `0 issues` |
| `make test` | Pass; 36 packages passed, 26 packages have no tests |
| `go list -deps ./...` | No Protoactor module present |
| `git diff --check` | Pass |
| Chat focused tests | Pass |
| Two-node Ergo channel stage (`-count=3`) | Pass: activation, register/unregister, Send, History Call, terminate, reactivate |
| Ergo adapter focused tests | Pass |
| Ownership/fencing focused tests | Pass: Claim-before-Spawn, duplicate activation, init failure cleanup, stale owner cleanup, lease takeover, Redis fail-closed |

## Runtime smoke

Command:

```text
go run node/main.go --config config/all.toml
```

Observed:

- Ergo Framework 3.3.0 node started with canonical node incarnation and listener `127.0.0.1:25101`.
- Redis, PostgreSQL, Consul registration, metrics, tracing, Chat, Friend, and Guild services initialized.
- Log emitted `node start success`.
- SIGTERM-style supervised stop completed service shutdown, Redis subscriber closure, service deregistration, and Ergo graceful shutdown.

The smoke used a temporary ignored worktree configuration derived from the development config, with the Ergo `[actor]` cookie/network/shutdown settings. It did not exercise a real Gateway login, a second complete server node, or a full cross-node business traffic replay.

## Failure and safety evidence

- Redis Claim failures do not spawn local Actors.
- `allowSpawn=false` remains locate-only and releases an already-claimed transient owner without spawning.
- Init/confirmation failure stops the process and conditionally releases the matching owner.
- Old termination cleanup cannot delete a newer owner after lease takeover.
- PostgreSQL `role_actor_fence` remains the durable write fence; Ergo PID/discovery is not treated as ownership.
- Stale runtime PIDs are not translated across the clean cutover; callers must reactivate or reconnect.

## Known limits

No pre-migration benchmark artifact was available in this worktree. Therefore local/remote Send, remote Call, activation latency, mailbox depth, and Actor memory are functionally covered but not numerically compared against a baseline. A real two-node full-stack smoke and Gateway login replay remain operational follow-ups, not claims of this verification run.
