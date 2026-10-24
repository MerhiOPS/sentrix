# Sentrix — Build Plan

**Status:** draft v2 — reviewed · 2026-10-24 · Owner: Ahmad Merhi

Sentrix is a clean rebuild of `serverwatch-OLD` as a **showcase**: one
dashboard for CPU, memory, disk, network, processes and logs across a fleet of
Linux and Windows servers. It keeps every measured lesson from OLD and drops
the parts that only matter at SaaS scale.

The headline property: **storage is a port, not a product.** v1 ships with
SQLite so the whole system runs as one binary with zero dependencies. Every
store sits behind an interface with a shared contract-test suite, run against
**two** implementations (SQLite and an in-memory reference), so a ClickHouse or
Postgres adapter is an addition, not a rewrite.

OLD's measurements (bytes/value, query latencies) were taken on ClickHouse and
Postgres. They justify *shapes*; they are **not** SQLite numbers and are not
quoted as such. Sentrix re-measures on SQLite.

---

## 1. Goals and non-goals

**Goals**
- `sentrix-agent`: single static binary (`CGO_ENABLED=0`), Linux + Windows, no
  runtime deps. Budget: < 30 MB RSS, < 1% of one core at a 5 s interval —
  measured; if it misses, say so rather than changing the goal.
- `sentrix-server`: single binary — API, ingest, alerting and the embedded
  dashboard (`go:embed`). `./sentrix-server` + one data directory (SQLite file
  plus its `-wal`/`-shm` siblings, TLS key, state) = running system. Local disk
  only: WAL mode needs shared memory and does not work on network filesystems.
- Multi-tenant from day one: every tenant-owned row carries `tenant_id`; every
  tenant-scoped port method takes a `Scope` that can only be built from a
  non-zero `TenantID`. Cross-tenant work (janitor, metering, backup) uses a
  separate, greppable `OpsScope`. §4.1 says how this is enforced and tested,
  given SQLite has no RLS.
- Metrics and logs in one place, correlated by host and time.
- Swappable storage proven by tests: one contract suite, run against every
  adapter, and shown to **fail** against a deliberately broken adapter (R2).
- Honest docs: every number in the README is measured, with the script that
  measured it.

**Non-goals (v1)** — deferred explicitly, not forgotten
- ClickHouse / Postgres adapters (the port and contract tests are the
  deliverable; the adapters are v2).
- Billing and payments. Plans/limits exist; nobody is charged.
- GPU metrics, distributed tracing, cloud auto-discovery.
- Horizontal scaling. One server process; SQLite WAL mode.
- Windows MSI. Windows ships as a zip + `sentrix-agent install` service command.
- Email notifier, alert grouping, delivery dead-letter queue (v2; webhook and
  Slack only in v1 — see Phase 5).
- Credential rotation and expired-credential recovery (OLD deferred this as a
  design decision; v1 credentials are long-lived and revoked by disabling the
  host).
- Agent live/replay dual lane (OLD C-M8). Replay is FIFO; see Phase 6.
- Process command lines (they carry secrets). Process **names** only.

---

## 2. Architecture

```
┌──────────────┐  HTTPS, outbound-only     ┌──────────────────────────────┐
│ sentrix-     │  POST /ingest/v1/batch    │ sentrix-server               │
│ agent        │ ────────────────────────► │  ingest ─► ports ─► SQLite   │
│ (per host)   │  JSON+gzip, batched       │  query  ─► REST + WebSocket  │
└──────────────┘  per-host bearer cred     │  alerting (ticker)           │
                  + SPKI pin (in token)    │  embedded React dashboard    │
                                           └──────────────────────────────┘
```

**Push, not scrape** — agents sit behind NAT; outbound-only means no inbound
ports on monitored machines. Cost: "agent down" and "network down" are
indistinguishable, so the UI says *"no data from host"*, never "host is down".

**HTTPS + JSON instead of gRPC (changed from OLD).** gRPC needs a protoc
toolchain and a second wire format. JSON with gzip over keep-alive HTTPS is
small enough at 10 s batches and debuggable with `curl`. HTTP/2 is not
required (note: a custom `tls.Config` on `http.Transport` disables HTTP/2
unless `ForceAttemptHTTP2` is set — irrelevant if we stay on HTTP/1.1). The
ingest schema is versioned (`/ingest/v1`) and defined once in `internal/wire/`;
it is agent↔server only. The browser API is a separate contract in
`docs/openapi.yaml` (Phase 2).

**Ingest wire contract (fixed in Phase 1, relied on by Phase 6):**
- `POST /ingest/v1/enroll` `{token, host_info}` → `{credential}`.
- `POST /ingest/v1/batch` `{batch_seq, samples, details, logs, agent_stats}`
  → `200 {batch_seq, server_ts_ms, replayed, logs_shed, logs_shed_reason}`.
- **Agent response handling — every status has exactly one meaning:**
  `200` = stored (or replay acked) → delete from buffer; `logs_shed > 0` means
  metrics were stored and the logs were refused (quota / disk) → do **not**
  resend them, count them. `400`/`413`/`422` = permanently invalid → drop and
  count, never retry (OLD C-Minor: any other reading replays the WAL head
  forever). `401`/`403` = credential revoked → stop shipping, keep buffering,
  log loudly. `429` + `Retry-After` and `5xx`/network = nothing stored → back
  off with jitter and resend the same batch. Metrics are **never** shed
  (OLD: a metric shed would silently drop replayed history).
- Body limits: compressed body capped by `http.MaxBytesReader`, **decompressed**
  size capped separately (gzip bomb), samples/lines per batch capped (OLD A-M5).
  Non-finite values rejected per sample (OLD A-M4).

**TLS and pinning (changed from OLD).** OLD's TOFU pin protected connections
*after* enrollment but not the token on first contact. Sentrix:
- Default: the server generates a self-signed key on first boot and keeps it.
  An enrollment token is `<secret>.<spki-sha256>`; the agent verifies the pin
  **before** sending the secret, and hard-fails on any later change.
- Operator-supplied CA cert (`--tls-cert/--tls-key`): agent uses system roots,
  token carries no pin. Pinning a CA-issued leaf would brick every agent at
  renewal unless the key is reused.
- `--insecure` (plain HTTP) refuses to bind a non-loopback address.

---

## 3. Repository layout and conventions

```
sentrix/
├── cmd/{sentrix-agent,sentrix-server}/   wire-up only, < 100 lines main()
├── internal/
│   ├── domain/      entities + value objects. Pure Go, stdlib only.
│   ├── app/         use cases + PORT interfaces (defined by the consumer)
│   │   ├── ingest/  query/  alerting/  identity/  ops/
│   ├── infra/       the ONLY place drivers appear
│   │   ├── sqlite/      store adapters (modernc.org/sqlite, pure Go, no cgo)
│   │   ├── memstore/    in-memory reference adapter (tests + contract proof)
│   │   ├── storetest/   contract suite every adapter must pass
│   │   ├── httpapi/     REST + WS + embedded web
│   │   ├── ingesthttp/  agent endpoint
│   │   ├── livehub/     in-process pub/sub for WS (never touches the DB)
│   │   ├── collector/   one file per subsystem, build-tagged per OS
│   │   ├── logtail/     file / journald / Windows Event Log
│   │   ├── redact/      log redaction
│   │   └── notify/      webhook / Slack (+ SSRF guard)
│   ├── agent/       agent runtime: loop, seq store, WAL, shipper, credential
│   └── wire/        ingest wire types (shared by agent and server)
├── web/             React 19 + Vite + TS, uPlot, TanStack Query
├── migrations/      embedded SQL, numbered, immutable once merged
├── packaging/       nfpm (.deb/.rpm), systemd units, Windows service
├── scripts/         ci/ (line limit, layering, raw-SQL checks), bench/, drill/
└── docs/
```

**Libraries (v1):** `modernc.org/sqlite`, `shirou/gopsutil/v4`,
`golang.org/x/crypto/argon2`, `golang.org/x/sys/windows` (`svc`, `svc/mgr`,
DPAPI), `github.com/coder/websocket` (OLD's choice; Origin check on by
default), stdlib `net/http` `ServeMux` patterns (no router), nfpm. Web:
React 19, Vite, TanStack Query, uPlot, `openapi-typescript`. journald and
Windows Event Log are read by exec'ing `journalctl -o json` and `wevtutil`, as
OLD did — both libsystemd and EvtSubscribe bindings would need cgo or a large
syscall layer.

Carried over from OLD's `CLAUDE.md`, enforced by `go run ./scripts/ci all`:
1. **300 lines per file** (`.go .ts .tsx .sql`; generated code and `_test.go`
   exempt).
2. **SRP** — one collector per subsystem, one reason to change per file.
3. **DDD layering** — `domain` imports stdlib only; `app` imports `domain` and
   ports; `infra` implements ports; `wire` imports stdlib only and is imported
   only by `agent` and `infra/ingesthttp`; `agent` may import `infra/collector`,
   `infra/logtail`, `wire`, never `infra/sqlite` or `httpapi`. CI fails on an
   import pointing outward.
4. **No raw SQL outside `infra/sqlite`.**
5. **R1 — durable state advances only after the write it describes is stored.**
6. **R2 — a check must fail when the thing it checks is broken.** New tests are
   shown failing before the fix. Tests that need something absent fail, they
   do not skip (OLD: an "integration" run passed in 0.125 s with every test
   skipped).
7. Platform code behind build tags, never `runtime.GOOS`; Linux builds must
   not import `golang.org/x/sys/windows`.

`go:embed` fails the build when the pattern matches nothing, so a stub
`web/dist/index.html` is committed and overwritten by the web build; a plain
`go build ./...` on a fresh clone must work.

---

## 4. Storage (SQLite v1)

**Connection setup** (per connection, via DSN `_pragma`, because most pragmas
are per-connection and silently default otherwise):
- `journal_mode=WAL`, `busy_timeout`, **`foreign_keys=ON`** (off by default in
  SQLite — without it every FK in the schema is decoration), `synchronous=NORMAL`.
- Two `*sql.DB`s: a writer with `MaxOpenConns(1)` using `BEGIN IMMEDIATE`
  (`_txlock=immediate`) so a deferred read→write upgrade cannot hit
  `SQLITE_BUSY` mid-transaction; and a read pool with **`query_only=ON`** so a
  write through the read path fails instead of contending.
- `auto_vacuum=INCREMENTAL` is set when the file is created, **before** the
  first table — it cannot be changed afterwards without a full `VACUUM`.
  `journal_mode` cannot change inside a transaction, so pragmas live in open
  code, not in migrations.
- `secure_delete=ON` on the writer (cost measured in Phase 7; see §4.3).
- All writes — ingest, janitor, erasure, admin — go through the single writer.
  Long read transactions block WAL checkpoints and grow the `-wal` file without
  bound; export and other long reads page by keyset in short transactions, and
  `-wal` size is reported in `/api/v1/system`.

**Shapes**
- **Wide row for core metrics** (12 values/row) in a `WITHOUT ROWID` table keyed
  `(tenant_id, host_id, ts)`. The PK makes a duplicate sample structurally
  impossible; a conflict is counted, not silently ignored. OLD's 1.20 vs 5.45
  B/value was ClickHouse with codecs; SQLite has no compression, so the reason
  that carries over is that a narrow row repeats the key per value.
  **Unverified, measure in Phase 1:** storing gauges as scaled integers
  (e.g. percent × 100) — SQLite records store small integers in 1–4 bytes but
  every `REAL` in 8 — is the SQLite analogue of OLD's Float32+2dp saving.
- Narrow `series` + `samples_detail` for variable cardinality (per-core,
  per-mount, per-NIC) at short retention. Labels canonicalised (sorted keys,
  string values) then hashed. A per-tenant series ceiling refuses **new** series
  only — resolving existing series must never be blocked, and hitting the
  ceiling never fails the batch (OLD A-C1).
- `latest_core` upserted on ingest → fleet grid never scans history. (OLD's
  975 ms → 3.7 ms was Postgres; the SQLite figure gets measured.)
- **Rollups** 1m and 1h are maintained **inside the adapter's batch write**, in
  the same transaction as the raw rows. They are an adapter concern, not an app
  concern, because a ClickHouse adapter would use materialized views instead;
  the port only reads them, and the contract suite asserts rollup reads equal
  an aggregation of raw. Gauges: sum, count, min, max. Counters: first/last
  value **and** `first_ts`/`last_ts` per bucket — rates divide by the covered
  span, never the bucket width (OLD B5) — never `avg`.
- **Counters** are stored reset-adjusted: the app folds a reboot/wraparound into
  a per-host offset in `host_counters`, computed on a draft and committed in the
  batch transaction (R1; OLD's five R1 defects were all in this family).
- **Logs**: `logs` rowid table (FTS5 external content needs an integer rowid) +
  FTS5 external-content index on `message`. See §4.2 for search semantics.
- **Process snapshots**: top-N JSON blob per 30 s sample, never per-PID series.
- **Timestamps**: metric timestamps clamped to `[now − max_wal_age, now + 60 s]`
  and snapped to the sampling grid; log timestamps clamped but **not** snapped.
  Rejections are counted per host.

**Ingest is one unit of work.** A batch's raw rows, rollups, `latest_core`,
counter state, usage counters and its `batch_seq` record are written by a
single port method (`IngestStore.CommitBatch`) in one SQLite transaction. R1
then holds by construction for everything OLD got wrong across two stores. A
future adapter that cannot do this must document which half can land without
the other.

**Idempotent ingest.** `(host_id, batch_seq)` unique in `ingest_batches`;
replayed batch = `200 replayed=true`, nothing re-applied. Two conditions make
this correct, both learned in OLD:
- The agent's `batch_seq` must never repeat — it is persisted and **floored by
  epoch nanoseconds**, so a lost or restored state dir cannot restart at 1 and
  have every new batch acked as a "replay" and discarded.
- `ingest_batches` rows are pruned only when older than `max_wal_age` plus a
  margin; the timestamp clamp rejects anything older, so a pruned seq cannot be
  re-accepted with data. The two constants are one config value.

**Retention**: per-tenant days; the janitor deletes in bounded batches per
`(tenant, host)` through the single writer (interleaved with ingest, so it
cannot stall it), deletes the matching FTS rows, runs `PRAGMA
incremental_vacuum` and periodic FTS `optimize`. Monitored, not trusted:
`ttl_overdue` = age of the oldest row beyond its tenant's retention.

### 4.1 Tenant isolation without RLS

OLD's conclusion, measured three times: the boundary is the repository layer,
and anything in the database is a backstop. SQLite has no backstop, so the
layer is tested harder:
1. `Scope` has unexported fields and a constructor that rejects a zero
   `TenantID`. Handlers get it from the session (REST) or credential (ingest),
   never from a parameter, header or body.
2. Every tenant-table statement in `infra/sqlite` binds `tenant_id`, including
   `UPDATE`/`DELETE` by id (`WHERE id = ? AND tenant_id = ?`).
3. Child tables reference `(tenant_id, host_id)` → `hosts(tenant_id, id)` with
   composite FKs, so a row cannot point at another tenant's host.
4. **Cross-tenant contract test:** two tenants with identical fixtures; every
   scoped port method is called under tenant B with tenant A's ids and must
   return nothing / `ErrNotFound` / zero rows affected. The method list is
   derived by reflection from the port interfaces, so a new method without a
   case fails the suite.
5. **Statement audit:** the contract suite runs the SQLite adapter behind a
   recording driver wrapper; any statement touching a tenant table without a
   `tenant_id` predicate fails the test. Shown failing on a planted query (R2).
6. HTTP: another tenant's host id returns `404`, not `403`; WS subscriptions
   are filtered by tenant **in the hub**.

### 4.2 Log search semantics

`unicode61` (the plan v1 choice) is token matching: it cannot do substring
search, so v1's "substring + token AND/OR with unicode61" was self-
contradictory. v2 defines `LogStore.Query` as:
- terms are matched as **case-insensitive substrings**; whitespace = AND,
  explicit `OR`; quoted phrase = one substring term;
- each term must be ≥ 3 characters (`400` otherwise — never a silent full scan);
- no stemming, no ranking; results newest-first, keyset by `(ts, id)`.

SQLite: FTS5 `trigram` tokenizer (`case_sensitive 0`, SQLite ≥ 3.34), joined
to `logs` with an explicit `tenant_id` filter. User input is **never** passed
raw to `MATCH`: each term is emitted as a double-quoted FTS5 string with
embedded quotes doubled, so FTS5 operators/column filters in user input are
literal text and syntax errors cannot surface as `500`. The memstore and a
future ClickHouse adapter (`ngrambf_v1` / `ILIKE`) implement the same
semantics; the contract suite pins them. Trigram index size is a known cost —
measured and published, not guessed.

### 4.3 What a SQLite `DELETE` actually removes

OLD's worst R2 failure was erasure that reported success while rows sat on
disk. SQLite has the same class: deleted content survives in free pages, in the
`-wal` file until checkpoint, and in FTS5 segments until merge. Erasure is
therefore: delete rows (incl. FTS rows) with `secure_delete=ON` → FTS
`optimize` → `wal_checkpoint(TRUNCATE)` → post-check. The post-check seeds a
canary string in the erased subject's data beforehand and **byte-scans the
database and WAL files** for it; it is shown failing with `secure_delete=OFF`.

**Ports** (in `app/`, added in the phase that first consumes them — not all up
front): `IngestStore`, `MetricReader` (raw + rollups), `LogStore`, `HostRepo`,
`TenantRepo`, `UserRepo`, `SessionRepo`, `EnrollmentRepo`, `AlertRepo`,
`UsageRepo`, `AuditRepo`.

---

## 5. Phases

Each phase ends with: CI green, acceptance criteria checked, a short entry in
`docs/LOG.md` (what landed, what was deferred and why), and a git tag
`phase-N`. Every acceptance item marked *(test)* names a test that was shown
failing before the code that makes it pass (R2).

### Phase 0 — Skeleton (S)
- Go module `github.com/MerhiOPS/sentrix`, layout above, `CLAUDE.md`.
- `scripts/ci`: line limit, layering, raw-SQL checks — each with a test that
  proves it fails on a violating fixture (R2).
- Domain types: `TenantID`, `HostID`, `Scope`, `Host`, `CoreSample`,
  `DetailSample`, `Series`, `LogLine`, `Level`, sentinel errors.
- Migration runner: `schema_migrations` with a checksum per applied file;
  editing an applied migration fails startup (OLD: a migrator that replayed
  every file was a production defect).
- **SQLite assumption tests** (`infra/sqlite/assumptions_test.go`): every
  engine behaviour this plan relies on, asserted against the pinned
  `modernc.org/sqlite`: FTS5 + `trigram` available; `foreign_keys` is
  per-connection and off by default; `query_only` rejects writes; `BEGIN
  IMMEDIATE` via DSN; `secure_delete` zeroes content; `VACUUM INTO` produces a
  consistent snapshot under a concurrent writer. If one fails, this plan
  changes before Phase 1.
- GitHub Actions: gofmt, vet, build, test `-race` on **ubuntu and windows**
  runners (Windows code is tested, not just cross-compiled), golangci-lint
  (config validated by `golangci-lint config verify`), ci checks, web
  typecheck/test/build.
- **Accept:** CI green on both OSes; each repo check demonstrably fails on a
  bad fixture; assumption tests pass.

### Phase 1 — Walking skeleton (L)
- Agent: CPU, memory, disk, network, load, uptime via gopsutil; 5 s loop;
  batches every 10 s; gzip JSON POST; counters sent raw and monotonic (rates
  are server-side; the agent is untrusted). Persisted, epoch-floored
  `batch_seq`. In-memory bounded buffer (disk WAL is Phase 6). Ack carries
  `server_ts_ms`; agent logs its own clock skew.
- Enrollment: `sentrix-server token create --tenant <t>` (CLI against the data
  dir; there is no auth UI until Phase 2) prints a single-use token (1 h TTL,
  carries the SPKI pin) → agent exchanges it for a per-host credential.
  Credentials are 32 random bytes, stored as SHA-256 (high-entropy secrets do
  not need Argon2, and a slow hash per batch is a DoS), compared in constant
  time, cached with invalidation on host disable. Server derives `tenant_id`
  and `host_id` **from the credential, never the payload**.
- `sentrix-server tenant create` CLI. Credential at rest: `0600` on Linux.
- Ingest endpoint with the full response contract (§2), body/decompression
  caps, timestamp clamp/snap, in-batch dedupe, counter reset folding,
  `IngestStore.CommitBatch`. Server timeouts set (`ReadHeaderTimeout`,
  `ReadTimeout`, `IdleTimeout` — the ingest port is internet-facing; OLD B15).
- SQLite adapter + memstore for `IngestStore`, `HostRepo`, `EnrollmentRepo`;
  migration 0001. `storetest` contract suite with the cross-tenant test (§4.1);
  both adapters pass; a deliberately tenant-blind wrapper fails it.
- `scripts/bench/ingest`: sustained batches/s on the dev box and bytes/sample
  for REAL vs scaled-integer encoding. Decide the encoding here, before data
  exists.
- **Accept:** agent on this machine → server → rows in SQLite; replayed batch
  stores nothing new (test); fresh agent state dir does not cause new batches
  to be discarded as replays (test); forged `host_id` in payload is ignored
  (test); token reuse rejected (test); `ts = now + 1 y` rejected and counted
  (test); counter reset produces no negative rate (test); gzip bomb rejected
  with `413` (test); Linux + Windows agents build and run; agent RSS/CPU
  measured and written down.

### Phase 2 — Read path + dashboard shell (M)
- Auth: `sentrix-server user create --admin` bootstrap CLI; users, Argon2id
  (pinned params ~46 MiB, t=1, p=1, semaphore — comment says why not RFC 9106's
  64 MiB), sessions as random tokens stored hashed, HttpOnly Secure
  SameSite=Strict cookies, rotated on login, idle + absolute expiry. Login
  throttled per account+IP with a **bounded** limiter (OLD B4). Roles: admin,
  viewer. `Origin` checked on WS upgrade and state-changing requests.
- REST: `/api/v1/hosts`, `/hosts/{id}`, `/hosts/{id}/metrics?from&to&step`
  (columnar `{ts:[], values:[]}`, server-side point cap — reject, don't
  silently downsample; applies when `step` is omitted too, OLD B8).
- One multiplexed WebSocket `/api/v1/stream` fed by `livehub` on the ingest
  path — **live path never queries the DB**. Per-client bounded send buffer;
  a slow client is dropped and counted. Reconnect backfill from a bounded
  in-memory ring, walked newest-first, never copied whole (OLD B6).
- Web: login, Fleet grid (status: ok / stale 3× interval / no data 10×; UI text
  "no data from host"), live CPU/mem chart (uPlot) with client ring buffer
  (dedupe by timestamp, OLD B7).
- `docs/openapi.yaml` is the source of truth. Web types generated by
  `openapi-typescript` (CI fails on drift); Go handler responses validated
  against the spec in `httpapi` tests, so the Go side cannot drift silently.
- **Accept:** fleet grid updates live; viewer cannot call admin endpoints
  (test); tenant B gets `404` for tenant A's host and no WS frames for it
  (test); point-cap rejection with and without `step` (test); reconnect
  backfill produces no duplicate points (test).

### Phase 3 — Depth (M)
- Detail metrics: per-core CPU, per-mount disk + IO, per-NIC net; series
  resolution cached `(tenant, host, metric, labels_hash)`; ceiling per §4.
- Top-N processes by CPU and RSS every 30 s, **name only**, as snapshot JSON.
- Rollups 1m/1h in the batch transaction (§4); counter state persisted (R1).
- Query layer picks raw / 1m / 1h by window (~300 points target). Missing
  buckets are `null`, never `0` (OLD B13); charts draw gaps.
- Web: Host detail page — CPU / Memory / Disk / Network / Processes tabs;
  chart click carries a timestamp to the (Phase 4) logs route.
- **Accept:** a counter reset does not produce a negative or spiked rate
  (test); a rate over a bucket with an interior gap divides by covered span
  (test); 30-day chart reads from 1h rollup (test asserts source);
  `{"core":"3"}` and `{"core": "3"}` resolve to one series (test); series
  ceiling does not block existing series or fail the batch (test).

### Phase 4 — Logs (L)
- Agent tails files: glob allowlist, file identity (inode / Windows file ID) +
  offset persisted, retained handle across rotation so the old file is drained
  before the new one (OLD C-C2), `FILE_SHARE_DELETE` on Windows so the retained
  handle does not break rename, truncation detected (size < offset). Multi-line
  join with a length cap. journald via `journalctl` with persisted cursor.
  Windows Event Log (Application/System) via `wevtutil` polling with a
  persisted record id; Security off by default — needs `Event Log Readers`.
  Read positions advance only after the server acks the batch containing
  those lines (R1; at-least-once — a crash re-sends, never loses).
- **Redaction at ingest** (server side, before storage and before FTS):
  bearer tokens, `Authorization` headers, passwords in URLs/conn strings,
  emails, IPv4/IPv6 (candidate regex + `net.ParseIP`, so timestamps are not
  mangled — OLD C-M10), per-tenant custom rules (RE2, count- and length-capped,
  precompiled) — applied to `message`, `source` **and every label**; a drop
  rule matching any field drops the line.
- Log quota in **bytes/day per tenant** (default in `tenants`; Phase 7 turns it
  into a plan field), checked before and recorded in the batch transaction
  (OLD C-M1 double count). Over quota → `200 logs_shed>0`, metrics stored.
  Redacted/dropped/shed line counts stored as metrics, including for a batch
  where every line was dropped (OLD C-M11).
- Server disk watermark (moved from Phase 6 — it is the same shed path): free
  space of the data dir below 20% → shed logs, keep metrics.
- Search: `/api/v1/logs?hosts&level&q&from&to&cursor` with §4.2 semantics,
  keyset-paginated, cursor validated (OLD B9); live tail over the existing WS
  with the same filters (OLD B10).
- Web: Logs page with filters, live tail, jump-to-time from any chart.
- **Accept:** rename-rotation test loses zero lines and re-ships none (test;
  copytruncate is documented best-effort — lines written between copy and
  truncate are unrecoverable by any tailer); a secret in a label and in
  `source` is redacted, asserted on the **stored** row (test); FTS operator
  syntax in `q` is searched literally (test); over-quota tenant still ingests
  metrics (test); tenant A cannot find tenant B's lines (test); search latency
  over 1M lines measured and published.

### Phase 5 — Alerts (M)
- Rules: `metric + aggregation + comparator + threshold + duration + scope`,
  evaluated against 1m rollups on a ticker off the ingest path, one query per
  rule across hosts (OLD C-M9). UI states the 1 min latency floor. State per
  `(rule, host)`: ok → pending → firing → resolved; cooldown suppresses
  repeat **firing** notifications only, never a resolve. Per-rule
  `on_no_data: fire | resolve | hold`, shown on the rule.
- `no_data` as a first-class rule type, evaluated from `hosts.last_seen`.
- Disabled hosts and disabled rules are never evaluated (OLD C-M2, C-M12). A
  rule matching no hosts shows as inert.
- Notifiers: webhook and Slack. **SSRF guard**: the dialer's `Control` hook
  validates the address actually being connected to (defeats DNS rebinding
  without a separate resolve step); deny loopback, link-local (incl.
  169.254.169.254), RFC1918, CGNAT `100.64/10`, `0.0.0.0/8`, IPv6 ULA/link-
  local, IPv4-mapped IPv6, multicast; `Proxy: nil` (an env proxy would bypass
  it); no redirects; time and response-size caps.
- Event row written **before** state update in one transaction (R1). Delivery
  outcome recorded per channel; any channel failing marks the event failed
  (OLD C-M6). Retry: 3 attempts with backoff, then the failure is visible on
  the event — no dead-letter queue in v1.
- Silences / maintenance windows suppress notifications, not transitions.
  Injectable clock.
- Secrets in channel config are never returned by the API to **any** role
  (OLD B2); a masked value sent back on update keeps the stored secret.
- **Accept:** fire/resolve cycle on a fake clock (test); single spike does not
  fire a 5 m rule (test); `on_no_data` resolve vs hold (test); webhook to
  127.0.0.1 and to a name resolving to 127.0.0.1 refused (test); admin and
  viewer both see masked secrets (test); disabled host never fires (test);
  tenant A cannot create a rule on tenant B's host (test).

### Phase 6 — Hardening + packaging (L)
- Agent disk WAL: one file per batch, bounded (100 MB, drop-oldest, counted),
  write failure counted as a drop and surfaced (OLD C-M7). Batches older than
  `max_wal_age` are dropped locally (the server would reject them). Flushed on
  reconnect with exponential backoff **and jitter**; replay paced and
  byte-capped per request. **FIFO by design**: counter folding depends on
  batch order, so during a long replay live data arrives late rather than out
  of order (OLD C-M8 — a live lane is v2). Log read positions now advance once
  the batch is in the WAL, not on ack.
- WAL depth / drops shipped in `agent_stats` and shown per host.
- Per-host ingest request-rate cap → `429` + `Retry-After` (nothing stored,
  agent keeps the batch).
- Credential at rest: DPAPI (user scope, under the service account) plus a file
  ACL limited to that account and Administrators on Windows. Service runs as a
  virtual account (`NT SERVICE\sentrix-agent`), not LocalSystem.
- Self-monitoring: `/healthz` (process alive, no dependency checks), `/readyz`
  (DB answers a read and the writer queue is draining), `/api/v1/system`
  (ingest lag, agent WAL depth, DB and `-wal` size, disk headroom,
  `ttl_overdue`, WS drops).
- Packaging: nfpm `.deb`/`.rpm` with systemd units for agent and server; agent
  runs as a dedicated user in `adm`/`systemd-journal` (Debian/Ubuntu). RHEL's
  root-only `/var/log/messages` needs `CAP_DAC_READ_SEARCH` — documented, not
  granted by default. Windows zip + `sentrix-agent install|uninstall`.
  `--version` reports the commit.
- `docs/runbook.md`: disk full, DB/`-wal` growth, a tenant flooding logs, an
  agent stuck replaying, revoked credential, restore.
- **Accept:** kill the server 2 min under load → agent replays, zero metric
  loss, no duplicates, row count equals samples sent (scripted test); kill the
  agent mid-batch → no duplicates (test); WAL past cap drops oldest and reports
  the count (test); shed test keeps metrics flowing (test); changed server key
  makes a pinned agent fail, not warn (test); `--insecure` on a public address
  refuses to start (test); `.deb` installs and survives reboot on a clean VM;
  ingest ceiling (hosts at 5 s) benchmarked and published.

### Phase 7 — Commercial-lite + compliance (M)
- Plans: `max_hosts`, `log_bytes_per_day`, `metrics_retention_days`,
  `logs_retention_days`. Over `max_hosts` → refuse **new** enrollments only.
  Usage vs limits visible in Settings before the limit is hit.
- Metering from the write path (peak concurrent hosts/month, samples, log
  bytes), inside `CommitBatch` so a replay cannot double-count — never by
  scanning storage.
- Tenant export: `GET /api/v1/export?table&from&to&format=csv|ndjson`
  streamed in keyset pages; `table`/`format` validated before any header is
  written (OLD B12); a mid-stream failure ends NDJSON with an error record
  (status is already sent).
- GDPR erasure: host / user / tenant, as `sentrix-server erase …`, per §4.3,
  with the canary post-check that fails if anything remains (R2); audit entry
  per run.
- Backup + **restore drill**: `VACUUM INTO` (one read transaction → a
  cross-table point-in-time snapshot, unlike OLD's ClickHouse `BACKUP`, and
  free pages are not copied). The SQLite online-backup API is an alternative
  only if Phase 0 confirms modernc exposes it. Restore into a clean dir → row
  counts + checksums compared; repeated with a concurrent writer and paired
  markers to prove cross-table consistency; timed; result in
  `docs/restore-drill.md`. Note: backups retain erased data until they expire.
- `docs/data-handling.md`: what is stored, retention, erasure procedure,
  backup expiry. Not a legal DPA — there is no customer to sign one.
- **Accept:** restore reproduces a known dataset exactly (test); restored
  tables agree on the marker pairs (test); erasure post-check proven to fail
  when a row is left behind and when `secure_delete` is off (test); over
  `max_hosts`, a new enrollment is refused and existing hosts keep reporting
  (test).

### Phase 8 — Showcase polish (S)
- README: screenshot/GIF, quickstart (`docker run` of the published image, or
  two release binaries), architecture diagram, measured numbers with scripts.
- `docs/STORAGE-ADAPTERS.md`: how to add ClickHouse — the ports, the contract
  suite, the `CommitBatch` atomicity caveat, what OLD measured and why it would
  be chosen at scale.
- Demo mode: `sentrix-server --demo` seeds a tenant and runs simulated agents
  **through the real ingest endpoint** on loopback, so the demo exercises the
  product rather than bypassing it. Plain HTTP is allowed only because it is
  loopback.
- Security pass: no secrets/IPs in repo, `govulncheck`, `npm audit`.
- **Accept:** on a clean VM, `docker run` of the image → login → live charts in
  under 60 s, timed and written down; from-source build time also recorded.

---

## 6. Risks

| risk | mitigation |
|---|---|
| SQLite write throughput at high ingest | single writer, one transaction per batch; bench in Phase 1 (decides encoding) and Phase 6 (publishes the host ceiling) |
| FTS5 trigram index size on large logs | external content avoids storing text twice but the index is still large; retention janitor + `optimize`; size per line measured and in README |
| `-wal` file growth from long readers | keyset paging in short transactions; `-wal` size in `/api/v1/system` |
| Tenant leak with no database backstop | §4.1: scoped handles, composite FKs, reflected cross-tenant contract test, statement audit |
| modernc.org/sqlite behaviour differs from C SQLite docs | Phase 0 assumption tests on the pinned version; re-run on every bump |
| Windows code paths | Windows CI runner runs the tests; Event Log still best-effort (OLD C-M13 burst loss unproven) |
| Scope creep from "SaaS" ideas | billing, email, grouping, MSI, rotation explicitly v2 |

## 7. Open questions
1. Demo hosting: publish a live demo instance or screenshots only?
2. License: proprietary, or source-available (BSL) for the showcase? (A public
   repo also makes Windows CI minutes free.)
3. TLS default: self-signed + pin-in-token (this plan) vs. requiring a
   CA-issued cert. Affects quickstart friction and the browser warning.

---

## 8. Review notes (v2)

Changes:
- Status bumped; intro states OLD's numbers are ClickHouse/Postgres, not SQLite — v1 quoted them as if they transferred.
- Contract suite runs against SQLite **and** an in-memory reference adapter, plus a broken adapter that must fail — one adapter proves nothing about swappability (R2).
- FTS5 `unicode61` replaced by `trigram` with defined substring semantics (≥ 3 chars) — unicode61 cannot do substring search, so v1's semantics were contradictory.
- FTS5 `MATCH` input quoted per term — raw user input is query-syntax injection and turns typos into 500s.
- Added §4.3: SQLite `DELETE` leaves data in free pages, `-wal` and FTS segments; erasure uses `secure_delete`, `optimize`, checkpoint and a byte-scan canary post-check — OLD's C-C1 in SQLite form.
- Connection pragmas made explicit: `foreign_keys=ON` (off by default), `BEGIN IMMEDIATE`, `query_only` read pool, `auto_vacuum` set before first table (v1's `incremental_vacuum` was a no-op without it).
- Added `-wal` growth risk from long readers; export pages in short transactions.
- Ingest became one `CommitBatch` unit of work — per-table ports with separate transactions would break R1 that SQLite can give for free.
- Rollup maintenance moved from app layer to adapter — a ClickHouse adapter would use MVs and double-maintain otherwise; added `first_ts`/`last_ts` (OLD B5).
- `batch_seq` persisted and epoch-floored on the agent; `ingest_batches` pruning tied to the clamp window — without both, the replay gate silently discards new data or re-accepts old.
- Defined the full agent response-code contract; v1's "429 with shed=logs" conflated "stored, logs refused" with "nothing stored, retry". Metrics are never shed.
- Added decompressed-size cap (gzip bomb), per-batch caps, non-finite rejection (OLD A-M4/A-M5), server timeouts (B15).
- SPKI pin moved into the enrollment token — TOFU does not protect the token on first contact; CA-cert mode skips pinning so renewals do not brick agents.
- Credentials hashed with SHA-256, not Argon2 — per-batch slow hashing is a DoS; random 32-byte secrets do not need it.
- Phase 1 enrollment uses a CLI — v1 had an admin create tokens before Phase 2 built auth. Added tenant/user bootstrap CLIs (missing entirely).
- Ports defined per phase as consumers appear, not all in Phase 0 — matches the "defined by the consumer" rule.
- Phase 0 gained SQLite assumption tests and a Windows CI test runner (v1 only cross-compiled Windows code).
- Migration runner checksums applied files (OLD migrate.sh defect).
- Layering rules extended to `wire` and `agent`; committed stub for `go:embed` so fresh clones build.
- Tenant isolation §4.1: composite FKs, reflected cross-tenant test, statement audit, 404 not 403, hub-side WS filtering.
- OpenAPI: v1 said it was generated from `internal/wire` (the agent protocol); now `openapi.yaml` is source, TS generated, Go responses validated against it.
- Added OLD read-path fixes: bounded limiter (B4), point cap without `step` (B8), bounded WS backfill (B6), null not zero gaps (B13), cursor validation (B9), export validation (B12), secrets masked for all roles (B2).
- Logs: retained handle + `FILE_SHARE_DELETE` (C-C2), truncation detection, positions advance after ack/WAL (R1), redaction of `source`/labels asserted on stored rows, IPv6 via `ParseIP` (C-M10), metering of fully-dropped batches (C-M11), quota recorded once (C-M1); copytruncate stated as best-effort. Log timestamps not snapped.
- Disk watermark moved to Phase 4 (same shed path as quota).
- Alerts: SSRF guard via dialer `Control` hook, wider deny list, `Proxy: nil`; cooldown on firing only; disabled hosts/rules; one query per rule. Email, grouping and dead-letter deferred to v2 (scope).
- Agent WAL: FIFO replay stated as a decision (C-M8), WAL write failures counted (C-M7), stale batches dropped locally.
- Windows: virtual service account, DPAPI user scope + ACL; Linux log-read groups and RHEL capability stated honestly.
- Backup via `VACUUM INTO` (certain to exist, cross-table consistent); backup API marked unverified for modernc.
- `dpa.md` replaced by `data-handling.md` — a DPA is meaningless without customers.
- Process command lines excluded — they carry secrets.
- Phase 8 acceptance made measurable (clean VM, `docker run`, timed); demo runs through real ingest.
- Every acceptance item marked as a test; added missing ones (cross-tenant, token reuse, seq reset, gzip bomb).

Considered and left unchanged:
- SQLite as v1 store, Go/React stack, module path — fixed decisions.
- JSON over HTTPS instead of gRPC — sound for 10 s batches; keeps one HTTP stack.
- Phase order — dependencies hold once the disk watermark moved to Phase 4 and enrollment uses a CLI.
- Argon2id parameters (~46 MiB, t=1, p=1) — OLD's reasoning holds for a small box.
- `.rpm` kept — nfpm produces it at near-zero cost.
- Windows Event Log kept in v1 but best-effort; OLD C-M13 remains unproven.
- Not verified here (no network/module access during review): modernc's bundled SQLite version, trigram availability, online-backup API. Phase 0 assumption tests settle them.
