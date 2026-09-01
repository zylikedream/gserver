---
name: gserver-pressure-testing
description: Use whenever the user plans, runs, analyzes, or documents a GServer pressure/load test, benchmark, bench bot run, login-capacity test, or saturation test. Enforce a repeatable preflight, fixed parameters, evidence collection, cleanup, and a per-run record under docs/pressure/ with parameters, results, and conclusions.
compatibility: Requires the GServer repository, client/cmd/bench, Prometheus and optionally Grafana/Loki; use the repository's configured Go, Docker, and shell tooling.
---

# GServer Pressure Testing

Treat every pressure test as a reproducible experiment, not an ad-hoc command. Create the run record before starting traffic, capture exact inputs and evidence during the run, then finish the same record with measured results and a conclusion.

## Non-negotiable record rule

- Every run, including a smoke run, gets one file: `docs/pressure/runs/YYYY-MM-DD-<run-name>.md`.
- Create the file before launching `bench`; never postpone the record until after the run.
- Record facts, queries, time ranges, and commands. Write `未测` or `未知` instead of filling gaps with estimates.
- Keep one document per run. Do not overwrite an earlier run or silently fold multiple parameter sets into one result.
- Do not put passwords, cookies, access tokens, database credentials, or raw authenticated URLs in the record.

Read `<repo-root>/docs/pressure/README.md` and `<repo-root>/docs/pressure/template.md` in the repository as the canonical record format. Resolve `<repo-root>` before using either file; do not rely on a fixed checkout path.

## Fixed workflow

### 1. Define the experiment

Before changing configuration, write the run goal and pass/fail criteria:

- target: login correctness, admission-control behavior, capacity, steady-state stability, or regression comparison;
- scope: local or cloud, services under test, database/cache state, and whether data may be reset;
- load shape: bot count, startup rate, warm-up, steady duration, stop method, and bot mix;
- account pool: unique `account_pattern` for this run and whether accounts/roles are pre-created;
- server parameters: especially `[login_limit]` values (`enabled`, `rate`, `burst`, `max_inflight`, `queue_size`, `wait_timeout`);
- observability window: exact start/end timestamps and timezone.

Do not call a run a capacity result when it only validates a low-limit rejection path. State the experiment type explicitly.

### 2. Preflight

1. Confirm the repository branch and working-tree state. Do not hide unrelated user changes.
2. Build the server and bench binary with the repository's normal commands. If source or `.proto` files changed, follow the repository generation/build rules first.
3. Start or verify PostgreSQL, Redis, Consul, account, gate, and logical services required by the scenario.
4. For local pressure tests, use a dedicated `build/env/pressure_<operator>.env.toml` copied from the operator's development environment. Set `[log].level = "info"` so Debug traffic logs do not flood the test evidence while Info/Warn/Error remain available for diagnosis. Generate server configs through `./build/script/svr_init.sh pressure_<operator>`; do not hand-edit generated `config/*.toml` as the permanent configuration path.
5. Verify endpoints and metrics before traffic:
   - bench gate address, normally `127.0.0.1:11086` in local runs;
   - account prelogin URL, when configured;
   - Prometheus `up{job="game-services"}` for the services being measured;
   - no pre-existing restart, bind, database, or Consul errors in the selected baseline window.
6. Decide whether to reset data. Use a unique account prefix regardless; local pressure data may be discarded when the experiment permits it.

If a preflight check fails, stop and record the failure as a blocked run. Do not begin traffic and later reinterpret it as a valid result.

### 3. Freeze and record load parameters

The bench client reads YAML. Its important fields are:

```yaml
addr: "127.0.0.1:11086"
account_server: "http://127.0.0.1:18080"
platform: "guest"
account_pattern: "pressure_<date>_%d"
total_bots: 300
startup_rate: 30
report_interval: 5s
silent: true
```

Record the complete bench YAML or an immutable path plus checksum. Record the complete relevant server TOML section. The client has no duration field: `client/cmd/bench` runs until `SIGINT`/`SIGTERM`; use an external timeout or a supervised terminal and record the exact stop mechanism. Scripts with `loop.count: 0` are intentionally unbounded and must be stopped by the run operator.

Recommended progression for a capacity experiment, unless the goal requires another shape:

| Tier | Bots | Startup rate | Steady duration | Purpose |
|---|---:|---:|---:|---|
| Smoke | 100 | 20/s | 5m | connectivity and data-path sanity |
| Medium | 300 | 30/s | 10m | first stable operating point |
| Target | 600 | 50/s | 15m | planned target load |
| Saturation | 1000 | 100/s | 10m | locate overload boundary |

Use the smallest tier that answers the question. A low-limit branch-validation run can intentionally use, for example, `rate=10`, `burst=20`, `max_inflight=5`, `queue_size=10`, `wait_timeout=1s`; label it as branch validation, not capacity.

### 4. Execute

Create the record, then launch the exact recorded configuration. Example:

```bash
# Run until SIGINT after the specified duration; preserve the bench output.
timeout -s INT -k 30s 10m ./bin/bench -config client/cmd/bench/pressure.yaml \
  2>&1 | tee /tmp/gserver-pressure-<run-name>.log
```

Use a different account prefix per run. Record the actual launch and stop timestamps, signal, exit status, bots launched, and any interruption. If the command, binary, config, or server parameters change, start a new run record or add a clearly separated phase with its own parameter/result pair.

Stop early and mark the run invalid or partial if a service restarts, database connections exhaust, CPU stays above 90%, memory grows without recovery, login queues never drain, or errors become unbounded. Preserve the evidence explaining the stop.

### 5. Collect evidence

Capture raw values before interpreting them. For each query, record datasource, exact query, selected range, evaluation time, and returned value or empty/error state.

Prometheus checks:

```promql
up{job="game-services"}

max_over_time(sum(online_players{node="gate"})[15m:])

sum by (result) (
  increase(login_limit_total[$__range])
)

sum by (msg_name, result) (
  increase(client_requests_total{result!="ok"}[$__range])
)

sum by (msg_name) (
  increase(client_requests_total{result="error"}[$__range])
)
```

Use `increase(counter[$__range])` for selected-range totals and `rate(counter[5m])` for rates. For login admission control, record `login_inflight`, `login_queue_length`, `login_limit_total{result=...}`, `login_wait_duration_seconds`, and `session_disconnect_total{reason=...}` when present. The expected login-limit result labels are `ok`, `rate_limited`, `queue_full`, `queue_timeout`, and `error`.

For logs, use a concrete Loki matcher, for example `{job="gserver"} |= \`handle client protocol failed\``. `{}` alone is invalid in this Loki setup. Record service, level, message, protocol, role/account context, and trace/context IDs when available. A valid empty result is evidence of no matching lines in that window, not proof that logging is broken.

If the Grafana UI is used, follow the `grafana-investigation` skill when available. Never store credentials or session material in the run document.

### 6. Conclude and clean up

Finish the same record with:

- measured load and achieved bot/online counts;
- latency or throughput values with units and percentile definition;
- login limiter results and non-OK/error totals;
- service health, resource observations, and relevant logs;
- deviations, missing measurements, and whether the run is valid, partial, or invalid;
- a conclusion tied directly to the stated pass/fail criteria;
- follow-up actions with an owner or next experiment, if needed.

Then stop bench and temporary services as appropriate, remove temporary override files and credentials, and state what data was reset or retained. Do not clean up evidence needed to reproduce the conclusion.

## Output contract

When asked to run or analyze pressure testing, return:

1. the run document path;
2. exact parameters and time window;
3. evidence-backed results;
4. conclusion (`通过`, `不通过`, `部分完成`, or `无效`) and why;
5. cleanup status and unresolved risks.

A pressure test is not complete until the repository record contains parameters, results, and conclusion—even when the run failed before traffic.
