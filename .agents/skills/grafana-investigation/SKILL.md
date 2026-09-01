---
name: grafana-investigation
description: Use when investigating Grafana dashboards, Loki logs, Prometheus metrics, Explore queries, datasource health, or time-range-dependent observability results, especially when the user asks to log in to Grafana and inspect a service error.
---

# Grafana Investigation

Use the real Grafana UI through `playwright-cli`. Preserve the selected time range and report the query, result state, and evidence instead of paraphrasing an unverified dashboard.

## Safety

- Prefer an existing authenticated session. If login is required, use credentials supplied by the user only for the current session; never write passwords into this skill, repository files, screenshots, reports, shell scripts, or chat summaries.
- Do not reset Grafana passwords, delete dashboards, change datasources, or mutate alerts unless the user explicitly requests that exact action.
- Treat a failed login, datasource error, LogQL parse error, and valid empty result as different outcomes.
- Close the browser session when the inspection is finished unless the user asks to keep it available.

## Workflow

1. Open the requested Grafana URL with `playwright-cli open`.
2. Run `playwright-cli snapshot`; use only current refs. Re-snapshot after navigation, datasource changes, or rerenders.
3. If the login page appears, fill the username and password supplied in the current conversation, submit, and verify that the URL leaves `/login`.
4. Go to **Explore**. Select the requested datasource by its visible name (`Loki` for logs, `Prometheus` for metrics).
5. For Loki, switch to **Code** mode and use a selector with a non-empty matcher. Examples:
   ```logql
   {job="gserver"} |= `error`
   {job="gserver"} |= `handle client protocol failed`
   ```
   Never run `{}` alone; Loki rejects selectors whose matchers can match an empty value.
6. Set the time range requested by the user before running the query. Record the exact range, query, line limit, and whether the result is `No logs found`, a parse/datasource error, or returned log lines.
7. For Prometheus, record the exact PromQL and distinguish a rate from a selected-range total. Use `increase(counter[$__range])` for event counts and `rate(counter[5m])` for a time-series rate.
8. Inspect returned lines for service, level, message, protocol, role/account ID, error cause, and trace/context IDs. Expand a line only when its fields are needed.
9. Report findings with concrete evidence and mark any inference. Do not call an expected business rejection a system failure without the log/error reason.

## Useful Local Queries

For a GServer Loki datasource:

```logql
{job="gserver"} |= `error`
{job="gserver"} |= `handle client protocol failed`
{job="gserver"} |= `metrics server error`
```

For a GServer Prometheus datasource:

```promql
up{job="game-services"}

sum by (msg_name, result) (
  increase(client_requests_total{result!="ok"}[$__range])
)

sum by (msg_name) (
  increase(client_requests_total{result="error"}[$__range])
)
```

## Common Failure Modes

| Observation | Meaning | Next action |
|---|---|---|
| `Invalid username or password` | Current Grafana database password differs from compose defaults | Ask for the current password or have the user authenticate; do not reset it silently |
| `401 Unauthorized` from Grafana API | Anonymous access is disabled | Use the authenticated UI session |
| Loki parse error about empty-compatible matcher | Selector was `{}` or used an empty-compatible regex | Add a concrete label matcher such as `{job="gserver"}` |
| Valid query with `No logs found` | No matching logs in the selected interval | Widen the time range or query a narrower/known message |
| Metrics exist but logs do not | Metrics and logs use separate emission paths | Trace the code path and check whether the error is intentionally converted/swallowed |

## Updating This Skill

Add a workflow step only after it has been exercised successfully in Grafana. Preserve exact working query syntax, observed UI labels, and failure distinctions. Never add credentials, session cookies, environment-specific secrets, or unverified assumptions. Keep this file a reusable procedure; put project-specific datasource URLs and credentials in project configuration, not here.
