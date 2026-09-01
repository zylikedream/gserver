# Grafana p95 Time-Range Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `Top 10 Slow Protocols p95` Grafana table calculate over the dashboard's selected time range and omit protocols with no observations.

**Architecture:** Keep the existing instant table and Prometheus histogram calculation. Replace the fixed `[5m]` range selector with Grafana's `$__range` variable, then filter the percentile vector against the histogram count rate for the same range. Leave the separate p95 time-series panel's rolling `[5m]` behavior unchanged.

**Tech Stack:** Grafana dashboard JSON, Prometheus PromQL, `client_request_duration_seconds` classic histogram, local Prometheus API on `http://127.0.0.1:9092`.

## Global Constraints

- Modify only `deploy/docker/grafana/dashboards/gserver-metrics.json` for implementation.
- Preserve the existing `$node` and `$instance` dashboard variables.
- Preserve `Client Request p95` as a rolling `[5m]` time-series panel.
- Do not change Go metric collection, histogram buckets, service configuration, or Prometheus scrape configuration.
- Keep the Top-10 panel as an instant table returning one aggregate result for the selected range.
- A no-traffic range must return no table rows rather than `NaN` values.

---

### Task 1: Update the Top-10 p95 PromQL

**Files:**
- Modify: `deploy/docker/grafana/dashboards/gserver-metrics.json:410-423`
- Reference: `docs/superpowers/specs/2026-09-01-grafana-p95-time-range-design.md`

**Interfaces:**
- Consumes: Grafana variables `$__range`, `$node`, and `$instance`; Prometheus histogram metrics `client_request_duration_seconds_bucket` and `client_request_duration_seconds_count`.
- Produces: An instant table query with at most ten `msg_name` rows and numeric p95 values for protocols observed in the selected range.

- [ ] **Step 1: Replace the fixed five-minute query window**

Replace the panel expression:

```promql
topk(10, histogram_quantile(0.95, sum by (msg_name, le) (rate(client_request_duration_seconds_bucket{node=~"$node",instance=~"$instance"}[5m]))))
```

with:

```promql
topk(10,
  (
    histogram_quantile(
      0.95,
      sum by (msg_name, le) (
        rate(client_request_duration_seconds_bucket{node=~"$node",instance=~"$instance"}[$__range])
      )
    )
    and on (msg_name)
    (
      sum by (msg_name) (
        rate(client_request_duration_seconds_count{node=~"$node",instance=~"$instance"}[$__range])
      ) > 0
    )
  )
)
```

Keep the target fields `format: "table"`, `instant: true`, `range: false`, and `refId: "A"` unchanged.

- [ ] **Step 2: Clarify the table title**

Change the panel title from:

```text
Top 10 Slow Protocols p95
```

to:

```text
Top 10 Slow Protocols p95 (Selected Range)
```

- [ ] **Step 3: Confirm adjacent p95 semantics remain unchanged**

Verify the `Client Request p95` panel still contains:

```promql
rate(client_request_duration_seconds_bucket{node=~"$node",instance=~"$instance"}[5m])
```

Do not alter that panel.

- [ ] **Step 4: Validate JSON syntax**

Run:

```bash
python3 -m json.tool deploy/docker/grafana/dashboards/gserver-metrics.json >/dev/null
```

Expected: exit code `0` and no output.

- [ ] **Step 5: Validate the selected-range query against local Prometheus**

Use the equivalent concrete three-hour query against the local Prometheus API:

```promql
topk(10,
  (
    histogram_quantile(
      0.95,
      sum by (msg_name, le) (
        rate(client_request_duration_seconds_bucket[3h])
      )
    )
    and on (msg_name)
    (
      sum by (msg_name) (
        rate(client_request_duration_seconds_count[3h])
      ) > 0
    )
  )
)
```

Run it through:

```bash
curl -sG \
  --data-urlencode 'query=topk(10, (histogram_quantile(0.95, sum by (msg_name, le) (rate(client_request_duration_seconds_bucket[3h]))) and on (msg_name) (sum by (msg_name) (rate(client_request_duration_seconds_count[3h])) > 0)))' \
  http://127.0.0.1:9092/api/v1/query
```

Expected: a successful Prometheus response with numeric protocol values when the selected three-hour window includes the load test; no `NaN` values caused solely by zero recent five-minute traffic.

- [ ] **Step 6: Validate the no-traffic behavior**

Run the same query with a range that contains no traffic, for example `[5m]` after the load test has been idle:

```bash
curl -sG \
  --data-urlencode 'query=topk(10, (histogram_quantile(0.95, sum by (msg_name, le) (rate(client_request_duration_seconds_bucket[5m]))) and on (msg_name) (sum by (msg_name) (rate(client_request_duration_seconds_count[5m])) > 0)))' \
  http://127.0.0.1:9092/api/v1/query
```

Expected: an empty result vector, not rows with `NaN` values.

- [ ] **Step 7: Commit the Dashboard change**

```bash
git add deploy/docker/grafana/dashboards/gserver-metrics.json
git commit -m "fix(grafana): honor selected range in protocol p95 table"
```

## Verification checklist

- JSON parser passes.
- The Top-10 panel uses `$__range` for both histogram buckets and histogram counts.
- The panel remains an instant table.
- The 3h equivalent query returns numeric p95 rows from the load-test period.
- A no-traffic range returns no rows.
- The separate p95 trend panel remains on `[5m]`.
