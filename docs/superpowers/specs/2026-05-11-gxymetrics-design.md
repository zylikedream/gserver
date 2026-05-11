# gxymetrics Design

## Goal

Add Prometheus metrics to gserver with a thin wrapper package `core/gxymetrics/`, exposing a `/metrics` HTTP endpoint and instrumenting key paths.

## SDK

`prometheus/client_golang` — thin wrapper, no abstraction layer. Use `prometheus.DefaultRegisterer`.

## Package Structure

```
core/gxymetrics/
  metrics.go      — Init(), config
  http.go         — register /metrics endpoint via gxyhttp
  collectors.go   — all pre-defined metric variables
```

## V1 Metrics

### Runtime (zero code, default collectors)

| Metric | Type | Source |
|--------|------|--------|
| `go_goroutines` | Gauge | prometheus default |
| `go_gc_duration_seconds` | Summary | prometheus default |
| `go_memstats_alloc_bytes` | Gauge | prometheus default |

### Custom (instrumented)

| Metric | Type | Location | Labels |
|--------|------|----------|--------|
| `tcp_connections` | Gauge | `gxynet` OnOpen/OnClose | `role` (server/connector) |
| `actor_active_count` | Gauge | `gxyactor` activator_manager | `kind` |
| `actor_messages_total` | Counter | `gxyactor` doReceive default | `kind` |
| `actor_message_duration_seconds` | Histogram | `gxyactor` doReceive default | `kind` |
| `db_query_duration_seconds` | Histogram | `gxypgx` queries | — |
| `redis_request_duration_seconds` | Histogram | `gxyredis` ops | `cmd` |

## Instrumentation Points

1. **gxynet/tcpserver.go** OnOpen: Inc Gauge, OnClose: Dec Gauge (role=server)
2. **gxynet/tcpconnector.go** same pattern (role=connector)
3. **gxyactor/activator_manager.go** spawnActor: Inc Gauge, on actor stop: Dec Gauge
4. **gxyactor/actor.go** doReceive default branch: Inc Counter + time Histogram
5. **gxypgx/pgx.go** wrap GORM callbacks or manual timing around queries
6. **gxyredis/redis.go** wrap Redis client calls with timing

## /metrics Endpoint

Register `promhttp.Handler()` on the GoFrame HTTP server via `gxyhttp`. Path: `/metrics`.

## Decisions

- **SDK**: prometheus/client_golang, no OTEL
- **API style**: thin wrapper, direct prometheus types
- **Scope**: framework + core-layer instrumentation only; business metrics deferred
- **Config**: read from `[metrics]` section in TOML (path, enabled)
