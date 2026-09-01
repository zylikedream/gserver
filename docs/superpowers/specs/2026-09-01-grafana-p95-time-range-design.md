# Grafana p95 Top-10 Time-Range Design

## Goal

Make the `Top 10 Slow Protocols p95` table honor Grafana's selected time range. When a user selects `3h`, the table must calculate protocol p95 over the full three-hour interval rather than only the last five minutes.

## Current behavior

The panel in `deploy/docker/grafana/dashboards/gserver-metrics.json` is an instant table query:

```promql
topk(10,
  histogram_quantile(
    0.95,
    sum by (msg_name, le) (
      rate(client_request_duration_seconds_bucket{node=~"$node",instance=~"$instance"}[5m])
    )
  )
)
```

The outer Grafana time picker does not change the inner `[5m]` window. Because the query is instant, it evaluates only at the current time. After a load test stops, the last five minutes can contain no requests and `histogram_quantile` returns `NaN`, even when the selected dashboard range includes recent load-test traffic.

## Decision

Change only the Top-10 table to use Grafana's `$__range` variable:

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

`$__range` makes the p95 window equal to the selected Grafana range. The request-count filter removes protocols with no observations in that range, preventing meaningless `NaN` rows.

The panel remains an instant table. It will show one Top-10 result for the full selected interval, not a time-series of p95 values.

## Scope

- Modify the Top-10 p95 PromQL and clarify the panel title as `Top 10 Slow Protocols p95 (Selected Range)`.
- Preserve the `node` and `instance` dashboard variables.
- Preserve the existing `Client Request p95` time-series panel and its `[5m]` rolling-window semantics.
- Do not change Go metric collection, histogram buckets, service configuration, Prometheus scrape configuration, or other panels.

## Semantics and edge cases

- A selected range containing protocol requests returns a numeric p95 based on all observations in that range.
- A selected range with no protocol requests returns an empty table rather than `NaN` rows.
- A very short selected range can still have insufficient samples for a stable percentile; that reflects the selected data window and is not hidden by the query.
- The p95 remains an estimated percentile from Prometheus histogram buckets and is measured from Role client-message receipt through handler return, not full network round-trip latency.
- Aggregation continues across matching service instances and result labels, matching the existing panel behavior.

## Verification

1. Validate the dashboard JSON remains valid after the edit.
2. Run the old and new PromQL directly against local Prometheus with a range containing the completed load test. The new query must return numeric protocol values when the old `[5m]` query returns `NaN` after the test has stopped.
3. Query a range with no traffic and confirm the new table returns no protocol rows.
4. Open or reload the GServer Metrics dashboard, choose `3h`, and confirm the table shows numeric rows for the load-test period.
5. Confirm the Client Request p95 trend panel still uses a rolling `[5m]` window.
