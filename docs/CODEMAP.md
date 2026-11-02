# Code map

Files are kept under 300 lines (`CONTRIBUTING.md`), so a feature spans
several small files. This map is how to find your way: every package, what
it owns, and the files to open first for each flow. `go run ./scripts/ci
codemap` fails when a package is missing from it.

File names follow one rule: `<topic>_<role>.go` (`monitor_check.go`,
`monitor_read.go`, `alert_dto.go`). Files of one topic sort together in a
directory listing.

## Layers

```
domain  ←  app  ←  infra        (imports point inward only; scripts/ci layering)
wire    ←  agent, infra/ingesthttp
cmd     wires everything; web embeds the built dashboard
```

## Follow a flow

**An agent sample, from host to chart**
1. `internal/infra/collector/` reads the host → `internal/agent/batcher.go`
   seals a batch → `wal.go` keeps it on disk → `shipper.go` sends it
   (`outcome.go`: what each response status means).
2. `internal/infra/ingesthttp/handler.go` → `decode.go` → `mapping.go`
   (wire → domain).
3. `internal/app/ingest/service.go` (validate, fold counters, redact logs)
   → `internal/infra/sqlite/ingest.go` (`CommitBatch`: one transaction)
   → `ingest_rows.go`, `ingest_detail.go`, `ingest_logs.go`,
   `rollup_write.go`.
4. After commit: `internal/infra/livehub/hub.go` → WebSocket
   `internal/infra/httpapi/stream.go` → `web/src/hooks/useLiveStream.ts`.
5. History: `internal/app/query/metrics.go` → `sqlite/rollup_read.go` →
   `httpapi/metrics_handler.go` → `web/src/components/CorePanel.tsx`.

**An uptime check**
1. `internal/app/uptime/scheduler.go` decides it is due →
   `checker.go` runs it through `internal/infra/probe/` (`http.go`,
   `tcp.go`, `ping.go`; every dial through `internal/infra/netguard/`).
2. `internal/domain/monitor_state.go` (`StepMonitor`) turns the result
   into the next state, the time it accounts for and any incident.
3. `internal/infra/sqlite/monitor_check.go` (`RecordCheck`: one
   transaction) → notifications via `internal/app/alerting/dispatch.go`
   and `internal/infra/notify/monitor.go`.
4. Reads: `internal/app/uptime/views.go` (uptime, bars) →
   `httpapi/monitor_handlers.go` → `web/src/components/UptimePage.tsx`,
   `MonitorDetailPage.tsx`.

**An alert**
`internal/app/alerting/evaluator.go` (each minute) → `state.go` (the state
machine) → `sqlite/alert_state.go` (event then state, one transaction) →
`dispatch.go` → `internal/infra/notify/notifier.go`.

**A login**
`httpapi/auth_handlers.go` → `internal/app/identity/login.go` (with
`login_guard.go` throttling) → `sqlite/sessions.go`; every request after
it: `httpapi/session_mw.go`.

## Packages

| package | owns |
|---|---|
| `cmd/sentrix-agent/` | the agent binary: flags, run loop, Windows service |
| `cmd/sentrix-server/` | the server binary: `serve` (`serve.go`, `wire.go`, `background.go`, `alerts.go`, `uptime.go`), admin commands (`admin.go`, `user.go`, `plan.go`, `erase.go`, `backup.go`), demo mode (`demo*.go`) |
| `internal/domain/` | entities and rules, no I/O: hosts and samples (`sample.go`, `series.go`, `catalog.go`), logs and redaction, alerts and channels, monitors (`monitor.go`, `monitor_state.go`, `uptime.go`), tenants, users, plans, retention, `scope.go` (tenant isolation) |
| `internal/wire/` | the agent↔server protocol (`batch.go`, `enroll.go`, `token.go`, `limits.go`) |
| `internal/agent/` | the agent runtime: batch, disk WAL, ship, enrollment, credential protection |
| `internal/app/ingest/` | turning a batch into stored data exactly once; agent authentication (`auth.go`) |
| `internal/app/query/` | the read side: metrics, detail series, logs and log search |
| `internal/app/identity/` | tenants, enrollment, hosts, users, sessions, login throttling, plans |
| `internal/app/alerting/` | alert rules, evaluation, the notification dispatcher (shared with uptime) |
| `internal/app/uptime/` | monitors: admin and views, the scheduler, the checker, reference targets |
| `internal/app/redaction/` | a tenant's own redaction rules |
| `internal/app/retention/` | the janitor: hosts (`janitor.go`) and monitors (`monitors.go`) |
| `internal/app/export/` | the tenant data export, including uptime (`monitors.go`) |
| `internal/app/compliance/` | erasure with its post-check, and the audit log |
| `internal/buildinfo/` | the binary's version and commit |
| `internal/infra/sqlite/` | the product's storage. By topic: `ingest*.go`, `rollup_*.go`, `*_read.go`, `alert_*.go`, `monitor*.go`, `hosts.go`/`enroll.go`/`credentials.go`, `users.go`/`sessions.go`/`tenants.go`, `plans.go`, `retention.go`, `erase.go`, `backup.go`, `migrate.go` + `migrations/` (immutable once merged), `open.go`/`dsn.go` (connections), `stmtcache.go` |
| `internal/infra/memstore/` | the in-memory reference store: same topics as `sqlite/` |
| `internal/infra/storetest/` | the storage contract: `suite.go` lists every case; `*_cases.go` behaviour, `*_isolation.go` cross-tenant cases, `isolation.go` the reflected check that every scoped method has one |
| `internal/infra/httpapi/` | the dashboard API: `api.go` (routes), `*_handlers.go`, `*_dto.go` (JSON shapes, validated against `docs/openapi.yaml` in tests), `session_mw.go`, `origin.go`, `stream*.go` (WebSocket), `static.go` |
| `internal/infra/ingesthttp/` | the agent endpoint: decode, map, rate limit, respond |
| `internal/infra/livehub/` | in-process pub/sub for live charts and log tail |
| `internal/infra/collector/` | host metrics, one file per subsystem |
| `internal/infra/logtail/` | log sources: files (rotation-safe), journald, Windows Event Log |
| `internal/infra/redact/` | built-in redaction rules and the redactor |
| `internal/infra/netguard/` | the SSRF boundary: which addresses may be dialled |
| `internal/infra/notify/` | webhook and Slack delivery (alerts and monitors) |
| `internal/infra/probe/` | uptime checks: HTTP(S), TCP, ping |
| `internal/infra/password/` | Argon2id |
| `internal/infra/tlscert/` | the server's TLS key and the agents' pin |
| `internal/infra/diskguard/` | the disk watermark that sheds logs |
| `internal/e2e/` | whole-system tests and benchmarks: agent to SQLite, outages, uptime, ingest ceiling |
| `scripts/ci/` | the repository checks (`go run ./scripts/ci all`) |
| `web/` | `embed.go` embeds the built dashboard |

## The dashboard (`web/src/`)

| directory | holds |
|---|---|
| `api/` | the API client; `types.gen.ts` is generated from `docs/openapi.yaml` |
| `hooks/` | data fetching and live streams (`useHosts.ts`, `useMonitors.ts`, `useLiveStream.ts`, …), routing (`useRoute.ts`) |
| `lib/` | pure logic, unit-tested: formatting, status, uptime arithmetic for display |
| `components/` | one component per file, named after what it renders (`UptimePage.tsx`, `MonitorRow.tsx`, …) |
| `test/` | the fetch stub and fixtures for component tests |
| `../e2e/` | browser tests against the demo server (Playwright) |
