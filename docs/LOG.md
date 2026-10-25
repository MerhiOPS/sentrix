# Build log

One entry per phase: what landed, what was measured, what was deferred and
why. Newest first.

---

## Phase 8 — Showcase polish · 2026-10-24

**Landed**
- **Demo mode** (`serve --demo`): seeds a tenant, an admin with a random
  password (printed at start, kept in `demo.json` 0600) and an alert rule,
  and runs four simulated hosts. Each is the **real agent** — batcher, disk
  WAL, enrollment, pinned TLS — with simulated collectors, enrolled through
  the server's own ingest listener (`agent.EnrollAs`, so the demo never sends
  the machine's real hostname). Their logs carry an email, an address and a
  bearer token, so the server's redaction is visible; web-01 crosses the
  alert threshold every 10 minutes, so the alert fires and resolves.
  The demo's dashboard may be plain HTTP on any address (a container
  publishes it); nothing else relaxes that check, and `--demo --insecure`
  is refused.
- **Docker image** (`Dockerfile`: Node build → Go build → distroless static,
  nonroot, `/data` volume; default command is the demo).
- **README** (quick start, install, architecture diagram, measured numbers
  with where each comes from, screenshots) and `docs/STORAGE-ADAPTERS.md`
  (the ports, the contract and its reflected cross-tenant suite, the
  `CommitBatch` atomicity caveat and how a ClickHouse adapter would rebuild
  it, erasure and retention there, and when it is worth it).
- **Clean-machine CI** — a fresh GitHub runner is the clean VM:
  `scripts/smoke-docker.sh` builds the image, runs it, logs in and times
  live data; `scripts/smoke-deb.sh` installs the real `.deb` packages, runs
  both services under systemd, enrolls the packaged agent, checks it
  reports, and restarts both.
- Screenshots retaken from the demo; the earlier ones showed this
  workstation's hostname.

**Acceptance**
- on a clean VM, `docker run` → login → live charts in under 60 s, timed —
  CI (`docker` job): **10.5 s** from `docker run` to a logged-in session with
  live data on all four hosts ✅ (the image is built in CI, not pulled from a
  registry: publishing an image is the owner's call)
- from-source build time recorded — web 3.3 s + Go 9.6 s with a cold build
  cache (module cache warm) ≈ 13 s on 16 threads ✅
- security pass — tracked files scanned for usernames, server addresses,
  keys and emails (only test fixtures and documentation ranges);
  `govulncheck`: no vulnerable code reached (one advisory in
  `golang.org/x/crypto/openpgp`, a package Sentrix never imports, no fix
  available); `npm audit`: 0 vulnerabilities ✅
- Phase 6's open item, partly closed: the `.deb` packages install, start
  under systemd, enroll and report on a clean runner, and come back after a
  service restart (`packages` job ✅). A machine **reboot** is still untested
  (a CI runner cannot reboot).

**Caught before shipping**
- `--demo` would have blocked the server's start: the demo enrolls through
  the listener, so it now runs after it.
- `agent.Enroll` sends the machine's hostname: the demo would have shown the
  real hostname four times.
- The packages started nothing on first install because `enroll` and
  `tenant create` run before the first start, when systemd has not yet
  created the state directory — the pre-install scripts create it.
- On this workstation the dashboard showed no logs: `/home` is 96 % full and
  the disk guard (logs shed below 20 % free) was doing its job. The demo ran
  on another filesystem for the screenshots.
- Demo log lines produced impossible addresses (`203.0.113.361`) that the
  redactor correctly left alone — the generator now keeps octets valid.

**Still open (end of v1)**
- `.deb` survival across a real reboot; the Windows service run on a real
  Windows host.
- Erasure: verifying FTS `optimize` (Phase 7).
- Rare-term log search latency (Phase 4).
- The Phase 2–4 screenshots with the hostname remain in git history; removing
  them needs a history rewrite and force-push — the owner's decision.

---

## Phase 7 — Commercial-lite + compliance · 2026-10-24

**Landed**
- **Plans** per tenant: `max_hosts`, `log_bytes_per_day`,
  `metrics_retention_days`, `logs_retention_days` (migration 0006,
  `sentrix-server tenant plan`). Over `max_hosts` (enabled hosts) a new
  enrollment is refused with the token left unused; existing hosts keep
  reporting; disabling a host frees a slot. The janitor applies each
  tenant's retention (`metrics_retention_days` caps every metric kind).
- **Metering from the write path**, inside `CommitBatch` after the replay
  gate: core samples stored, log bytes stored, and distinct hosts per day
  (the monthly peak). Aggregates are per tenant, so erasing a host keeps the
  billed totals but not the host. Nothing scans storage.
- **Settings page** and `GET /api/v1/usage` (every role): hosts and today's
  logs against their limits, warning from 80 %, plus the month's totals.
- **Export**: `GET /api/v1/export?table=metrics|logs|alert_events&from&to
  &format=csv|ndjson[&host]` (admin), streamed in bounded pages through the
  same scoped readers as the dashboard. Table, format, range and host are
  all validated before the first byte (OLD B12); a mid-stream failure ends
  NDJSON with an error record (CSV just ends — documented).
- **Erasure**: `sentrix-server erase host|user|tenant … --yes`: plant a
  canary in the subject's own data (log line → search index, process
  snapshot; a session; the tenant row and every host), delete with cascades,
  FTS `optimize`, `incremental_vacuum`, `wal_checkpoint(TRUNCATE)` (refuses
  if a reader blocks it), then **byte-scan the database and WAL** for the
  canary, the subject's id, a host's credential hash, a user's email and
  password hash. Every run is audited (`audit_log`, subject as SHA-256).
  The series cache is reset after an erasure (a deleted host's key can be
  reused without AUTOINCREMENT).
- **Backup / restore**: `sentrix-server backup` (`VACUUM INTO` on its own
  connection — the read pool's `query_only` refuses VACUUM even with INTO;
  one read transaction, so ingest continues) and `restore` (into an empty
  directory only; open + pragma verification + migrations +
  `integrity_check`; a failure removes the file).
- `docs/data-handling.md`, `docs/restore-drill.md`.

**Acceptance**
- restore reproduces a known dataset exactly — `TestRestoreReproducesDatasetExactly`
  (row count + SHA-256 per table in key order, FTS shadow tables included) ✅
- restored tables agree on the marker pairs — `TestBackupUnderConcurrentWritesIsCrossTableConsistent`
  (backup taken while a writer committed sample+log marker pairs; last run 57 of 78 batches in the
  snapshot, every pair intact) ✅
- erasure post-check proven to fail when a row is left behind and when `secure_delete` is off —
  `TestPostCheckFailsWhenARowIsLeftBehind`, `TestPostCheckFailsWithSecureDeleteOff` ✅; a clean erasure of
  a host, user and tenant passes and touches no other host or tenant — `TestEraseHost…`, `TestEraseUserAndTenant` ✅
- over `max_hosts`, a new enrollment is refused and existing hosts keep reporting —
  `plans/max-hosts-new-only` on SQLite and memstore ✅

**Measured** — timed restore drill (`docs/restore-drill.md`): 50 hosts × 2 h
of realistic batches = 72 000 core samples, 1.3 M detail values, 180 000 log
lines; database 73.2 MiB, backup 68.5 MiB in **0.22 s**, restore with full
verification in **1.02 s**, identical digest in every table.

**Mutation testing** — 23 mutations (host limit and its enabled-only count,
metering amounts, first-host-of-the-day, the month window, tenant retention
cap, export validation, host check, the NDJSON error record, export role,
every erasure purge step, the WAL scan, the chunk overlap, the residue
decision, the failed-audit outcome, the planted log canary, restore's
overwrite refusal and integrity check). 21 killed after five tests were
added for survivors (reversed range, a row only in the WAL, the log canary
being searchable, a corrupt backup). **Two remain, and are reported, not
hidden:**
- Removing the FTS `optimize` from erasure is **not detected**. The index
  stores 3-character terms with prefix compression, so the byte-scan cannot
  see a deleted line's residue there, and FTS5's own views hide deleted
  documents. `optimize` is required by §4.3 and kept; verifying it needs a
  segment-level check that does not exist yet. Open.
- Removing restore's "already holds a database" check survives because the
  copy itself opens with `O_EXCL` — an equivalent mutant (two guards).

**Deviations**
- Metering counts core samples (not detail values): exact and cheap; detail
  volume follows from host count.
- Usage is visible to every role; plan changes are an operator CLI, not an
  API (there is no operator role yet).

---

## Phase 6 — Hardening + packaging · 2026-10-24

**Landed**
- **Agent disk WAL** replacing the in-memory buffer: one file per batch,
  written atomically (temp + fsync + rename), loaded in seq order at start
  (torn files and temp leftovers discarded and counted). Bounded at 100 MB
  (drop oldest, counted) and 71 h (the server refuses samples past 72 h).
  A failed write is a counted drop and returns an error, so log positions
  are not advanced for lines that were not kept. Positions now advance once
  the batch is in the WAL (it is resent until acked or dropped and counted).
  FIFO replay with jittered exponential backoff and a 100 ms pace (10
  batches/s) while a backlog drains.
- WAL depth and drops already rode in `agent_stats`; now shown on the host
  card (only when abnormal: drops, refused samples, backlog > 1, skew > 30 s).
- **Per-host ingest rate cap**: token bucket 20 req/s, burst 40 → `429` +
  `Retry-After`, nothing stored; the agent keeps the batch.
- **Retention janitor** (see Deviations): per `(tenant, host)` bounded
  deletes (5 000 rows, 10 ms pause between batches so ingest interleaves),
  every kind with its own horizon (raw 7 d, details 7 d, processes 3 d,
  1m rollups 30 d, 1h rollups 400 d, logs 14 d — FTS rows go with the log
  rows via the trigger), then `incremental_vacuum` and a bounded FTS merge
  (never a full `optimize`, which would hold the writer). `ttl_overdue` is
  measured after every run — including for hosts whose pruning failed.
- **Probes**: `/healthz` (alive, no dependency checks), `/readyz` (a read on
  the reader pool and on the writer within 2 s, else 503), `/api/v1/system`
  (admin: fleet counts, worst host lag, agent WAL depth/drops; DB and `-wal`
  size, disk headroom, `ttl_overdue`, janitor last run, WS subscribers and
  drops, writer waits).
- **Windows**: credential DPAPI-encrypted under the service's own account on
  its first start (encrypting at enroll time would bind it to the admin who
  ran enroll, and the service could never read it); state directory ACL
  replaced by a protected DACL for SYSTEM, Administrators and the virtual
  account. `sentrix-agent install|uninstall` registers the service as
  `NT SERVICE\sentrix-agent` (not LocalSystem) with restart-on-failure; the
  binary answers the Service Control Manager and logs to `agent.log`
  (rotated at 10 MiB).
- **Packaging**: nfpm `.deb`/`.rpm` for agent and server, amd64 and arm64;
  hardened systemd units (`ProtectSystem=strict`, no capabilities, state
  directories 0700); the agent runs as `sentrix-agent` in `adm` and
  `systemd-journal` where those groups exist; RHEL's root-only
  `/var/log/messages` documented (`CAP_DAC_READ_SEARCH`), not granted.
  Windows zip with install steps. `scripts/package.sh` builds all of it with
  SHA256SUMS. `version` reports the commit (from Go's embedded VCS info).
- Alerts left undelivered by a previous run are marked failed at startup
  (deferred from Phase 5).
- `docs/runbook.md`: disk full, DB/`-wal` growth, a tenant flooding logs, an
  agent stuck replaying, revoked/disabled credential and pin mismatch,
  restore (stop-and-copy until Phase 7's backup).

**Acceptance**
- kill the server 2 min under load → agent replays, zero loss, no
  duplicates — `scripts/outage.sh` against the real binaries: `kill -9`,
  120 s down, 240 s span, **0 missing 5 s slots, 0 re-applied samples,
  0 agent drops** ✅; in-process version `TestServerOutageLosesNothing` ✅
- kill the agent mid-batch → no duplicates — `TestAgentKilledThenRestartedNoDuplicates`
  (WAL full of unsent batches + one batch stored whose ack was lost; restarted
  agent: rows = samples kept, conflict counter 0, so the replay gate — not the
  primary key — deduplicated) ✅
- WAL past cap drops oldest and reports the count — `TestWALPastCapDropsOldestAndCounts` ✅
- shed test keeps metrics flowing — `TestOverQuotaShedsLogsKeepsMetrics`, `TestLowDiskShedsLogsKeepsMetrics` (Phase 4) ✅
- changed server key makes a pinned agent fail, not warn — `TestPinnedClientAcceptsOnlyThePinnedKey`,
  `TestEnrollRefusesWrongPinBeforeSendingSecret` (the pinned client refuses the handshake) ✅
- `--insecure` on a public address refuses to start — `TestCheckListen` ✅
- `.deb` installs and survives reboot on a clean VM — **not done**: no VM or
  container runtime on this machine. Packages built and inspected
  (`dpkg-deb --info/-c`: binary, unit, conffile, maintainer scripts), units
  pass `systemd-analyze verify`. Open.
- Windows service — DPAPI round trip and the protected ACL tested on the
  Windows CI runner; the service install/run itself has **not** been run on
  a real Windows host. Open.
- ingest ceiling benchmarked — below ✅

**Measured** — `BenchmarkIngestCeiling` (`go test ./internal/e2e -run '^$'
-bench IngestCeiling -benchtime 20000x`): the full server path (HTTP
handler, gzip + JSON decode, credential cache, validation, counter folding,
rollups, detail series, one SQLite transaction per batch), 8 concurrent
clients, 1 000 hosts, each batch = 2 core samples + 36 detail values
(8 cores, 2 mounts, 2 disks, 2 NICs), i5-13400F:

| | batches/s | hosts at 5 s (one batch / 10 s) |
|---|---|---|
| before | ~580 | ~5 800 |
| with prepared statements | **840–940** | **~8 400–9 400** |

The profile showed SQLite's SQL parser at ~30 % of CPU: every row's INSERT
was parsed again. Ingest statements are now prepared once on the writer
connection and reused through `Tx.StmtContext`. The first version
**deadlocked** — preparing on the pool inside a transaction waits for the
only writer connection, which the transaction holds — so the cache never
prepares inside a transaction: a miss runs unprepared and is prepared once
the writer is free. Encoding runs in the timed loop, so the figures are
conservative. Binaries: agent 7.3 MB, server 13.4 MB (stripped, with the
dashboard).

**Mutation testing** — 26 mutations of the Phase 6 guards (WAL cap, age,
reload, discard/drop counting, failed-write accounting, positions on failed
write, replay pace, rate cap and its refill, per-host buckets, Retry-After,
janitor batch loop, overdue, compaction, every host predicate in the prune
SQL, the LIMIT, memstore rollup source and target ownership, the undelivered
sweep's predicate, readiness, the system endpoint's role, build info), each
compile-checked first: all killed. One survived on the first pass — pruning
details with the host predicate removed, because the sibling host in the
test had no details; it does now.

**Caught before shipping**
- The janitor skipped measuring `ttl_overdue` for a host whose pruning
  failed — exactly the host it exists to report (test written first).
- The prepared-statement cache deadlock above.
- The runbook first claimed agents replay data acknowledged after a backup;
  they do not (acked batches leave the WAL). Corrected: a restore loses what
  was acknowledged since the copy.

**Deviations**
- The retention janitor was in PLAN §4 but in no phase, while Phase 6's
  system report needs `ttl_overdue`. Built here with global defaults;
  Phase 7's plans make the horizons per tenant.
- `/api/v1/system` shows server-wide figures (DB size, disk) to every
  tenant's admins. Fine for a self-hosted single company; a hosted
  multi-tenant deployment needs an operator role (Phase 7).

---

## Phase 5 — Alerts · 2026-10-24

**Landed**
- Rules: metric + aggregation (avg/max/min; rates have one value per
  bucket) + comparator + threshold + duration (whole minutes, 1m–24h) +
  hosts (empty = every host) + cooldown + channels, and `no_data` rules
  from `hosts.last_seen`. Every rule states its own `on_no_data`
  (fire / resolve / hold).
- Evaluation off the ingest path: every 10 s the evaluator checks for a
  newly closed minute and, 30 s after it closes, evaluates that 1m rollup
  bucket — one `TenantBuckets` query per tenant for all hosts (OLD C-M9),
  using the same bucket value the chart shows (`query.BucketValue`). A
  pure state machine per (rule, host): ok → pending → firing → resolved, on
  an injectable clock. Disabled rules and disabled hosts are never
  evaluated (OLD C-M2); a rule matching no enabled host is shown **inert**
  (OLD C-M12).
- Event row written before the state row, in one transaction (R1).
  Cooldown suppresses repeat **firing** notifications only, never a resolve.
  Silences hold back notifications, never transitions.
- Delivery off the evaluation path: a bounded queue (full → event marked
  failed at once), 3 attempts per channel with backoff, per-channel failures
  recorded on the event, any failure marks it failed (OLD C-M6).
- Notifiers: webhook (documented JSON body) and Slack (control sequences
  escaped). **SSRF guard** in the dialer's `Control` hook, on the address
  actually connected to (no separate resolve, so DNS rebinding cannot pass):
  loopback, RFC 1918, ULA, link-local incl. 169.254.169.254, CGNAT,
  0.0.0.0/8, multicast, IPv4-mapped IPv6, NAT64/6to4/Teredo (which embed
  IPv4), reserved ranges. `Proxy: nil`, no redirects, 10 s timeout, body
  caps; transport errors are stripped of the URL (a Slack URL is a
  credential and errors are shown on the event).
- Channel URLs masked for **every** role (OLD B2); a masked URL sent back on
  update keeps the stored secret; a masked URL is refused on create.
- Host disable / enable (API + UI): the agent's cached credential is dropped
  at once, not left to the 30 s TTL.
- Storage: migration 0005; every reference (rule → host, rule → channel,
  state, event, silence) is a composite `(tenant_key, key)` foreign key, so
  a cross-tenant link cannot be stored even by a buggy adapter. memstore
  implements everything; 6 contract cases and 17 cross-tenant cases.
- Dashboard: Alerts page (active alerts, rules with inert / no-channel
  badges, recent transitions with delivery outcome, channels, silences,
  admin rule form with on_no_data visible); host page Disable/Enable.

**Acceptance**
- fire/resolve cycle on a fake clock — `TestFireAndResolveCycle`, `TestEvaluatorFiresNotifiesAndResolves` ✅
- a single spike does not fire a 5 m rule — `TestSingleSpikeDoesNotFireFiveMinuteRule` ✅
- `on_no_data` resolve vs hold (and fire) — `TestOnNoDataResolveVsHold` ✅
- webhook to 127.0.0.1 and to a name resolving to it refused — `TestRefusesLoopbackByAddressAndByName`;
  manual run: both refused at dial time, the event shows `failed` with the reason ✅
- admin and viewer both see masked secrets — `TestChannelSecretsMaskedForEveryRole`, manual run (screenshots) ✅
- disabled host never fires — `TestDisabledHostNeverFires`; manual run: agent refused on its next batch (403), rules shown inert ✅
- tenant A cannot create a rule on tenant B's host — `TestRuleOnAnotherTenantsHostRefused`, `RuleRepo.CreateRule` /
  `UpdateRule` cross-tenant cases ✅

**Observed in the manual run** (real agent, 5 s interval): a `max cpu > 0
for 1m` rule created at 16:23:07 fired at 16:24:35 for the 16:23 bucket —
the documented ~1.5 min floor. All API responses validated against
`docs/openapi.yaml` in tests.

**Mutation testing** — 38 mutations of the guards (deny list entries,
`Proxy`, redirects, URL stripping, cooldown/silence rules, duration
boundary, disabled host/rule filters, retries, queue-full marking, masking,
every tenant predicate in the new SQL, memstore ownership checks, cache
invalidation, role checks), each compile-checked first: all killed. Two
survived on the first pass and the tests were strengthened: the repeated-
tick guard (re-evaluation is idempotent, so the test now counts reads), and
the `ListRules` cross-tenant case (it checked host links, which the
mutated query still filtered — it now checks rule ids).

**Caught before shipping**
- The demo host's real hostname contains the workstation's user name. The
  Phase 5 screenshots use a renamed host; the **Phase 2–4 screenshots
  already committed show it** and are to be retaken (Phase 8).

**Deferred**
- A notification still queued at shutdown keeps an empty outcome (shown as
  "pending" forever); a startup sweep marking them is Phase 6.
- Batches arriving more than 30 s after their minute closes are stored but
  not seen by that minute's evaluation.
- Rule editing in the UI is create/delete only (the API has PUT).

---

## Phase 4 — Logs · 2026-10-24

**Landed**
- Agent sources: files (glob allowlist, identity by inode / Windows file
  index, retained handle drained across rotation, `FILE_SHARE_DELETE`,
  truncation, multi-line join, first run tails from the end), journald
  (`journalctl -o json`, persisted cursor, byte-array messages), Windows
  Event Log (`wevtutil`, per-channel EventRecordID).
- Positions are committed only when the batch holding the lines **settles**
  (acked, or refused as permanently invalid) and persisted atomically —
  at-least-once, never lost (R1). Logs split into batch-sized chunks; the
  marks ride in the last chunk.
- Server redaction before storage and before the index: bearer/basic tokens,
  Authorization headers, URL credentials, key=value secrets, emails, IPv4,
  IPv6 (regex candidates confirmed by `net.ParseIP`, so times and MACs are
  untouched — OLD C-M10) — on message, source **and every label** (OLD A-C2).
  Tenant RE2 rules (mask/drop, capped), cache invalidated on change.
- Quota: `BuildFunc` now receives `TxState` — counters **and** the tenant's
  remaining log budget, both read in the batch transaction; usage written in
  the same commit, including for fully dropped batches (OLD C-M1, C-M11).
  Over quota or below the 20 % disk watermark: logs shed with a reason,
  metrics stored. No redactor configured: logs shed, never stored raw, and
  the batch never fails.
- Search: FTS5 trigram, case-insensitive substring terms ≥ 3 chars, AND / OR
  / phrases, every term double-quoted (FTS syntax in input is literal text),
  newest-first keyset paging, validated opaque cursors (OLD B9). Live tail on
  the stream WebSocket with server-side filters (OLD B10).
- Dashboard: Logs page with host, level, text and range filters, live mode,
  load-older paging, chart click → ±5 min around that moment; admin
  redaction-rules panel.

**Acceptance**
- rename-rotation loses zero lines and re-ships none — `TestRenameRotationLosesNothingShipsNothingTwice`
  (rotated name inside and outside the globs) ✅; copytruncate best effort, documented in code
- secret in a label and in `source` redacted on the **stored** row — `TestSecretsRedactedOnTheStoredRow`,
  `TestEveryFieldIsRedacted`; manual run: 0 raw secrets in the logs table ✅
- FTS operator syntax searched literally — `logs/search-semantics`, `TestFTSSyntaxIsSearchedLiterally` ✅
- over-quota tenant still ingests metrics — `TestOverQuotaShedsLogsKeepsMetrics`, `TestOverQuotaTenantStillIngestsMetrics` ✅
- tenant A cannot find tenant B's lines — `LogReader.SearchLogs` cross-tenant case, `TestTenantCannotSearchAnotherTenantsLogs` ✅
- search latency over 1M lines measured (below) ✅

**Measured** — 1 000 000 lines, 10 hosts, one tenant, stored through `CommitBatch`

| | result |
|---|---|
| ingest | 18 350 lines/s (2 000 lines per batch) |
| size | 278 B/line including the trigram index (265 MiB) |
| newest page, no text | 0.25 ms |
| common term | 58 ms · one host 70 ms |
| two terms AND | 75 ms · phrase 92 ms |
| rare term (10 hits) | 110 ms · OR 125 ms |

`go test ./internal/infra/sqlite -run '^$' -bench LogSearch -benchtime 20x`.
The rare term being among the slowest is a plan effect (the logs table is
walked newest-first and each row checked against the match set). An
FTS-driven join was tried to fix it and was **catastrophically slower** — the
first search had not finished after 50 minutes — so it was reverted, and
every benchmark search now has a 10 s budget so a bad plan fails fast.
Improving rare-term latency is open (e.g. choosing the plan by the match
count).

**Caught before shipping**
- A rotation scheme whose rotated name also matches the globs would have
  re-read the whole rotated file — files are now matched by identity.
- A partial line inside a committed mark would have been skipped after a
  restart — the mark excludes held partial lines (a test added after a
  mutation survived).
- Redactor missing would have failed batches with 403 (agent stops all
  shipping) — now logs are shed and metrics flow.
- A stat error other than "not exists" was silently skipped forever — now
  surfaced.

**Deferred**
- Windows Event Log is compiled, unit-tested on fixtures and vetted for
  Windows, but has not run on a Windows host yet (OLD C-M13 still unproven).

---

## Phase 3 — Depth · 2026-10-24

**Landed**
- Detail metrics from a closed catalog (per-core CPU, per-mount usage,
  per-device disk bytes, per-NIC network bytes): unknown metrics, wrong kinds
  and wrong labels are refused at ingest. Values stored at a per-metric
  fixed-point scale (×100 percent, ×1 bytes).
- Detail counters get the same reset folding as core counters, keyed
  `d:<metric>:<labels-hash>` in `host_counters`, in the same transaction.
- Series resolution with a per-tenant ceiling (`tenants.series_limit`,
  default 10 000): new series past the ceiling are refused and counted;
  existing series are never blocked and the batch never fails (OLD A-C1).
  The series cache is filled **only after commit** (R1).
- Process snapshots every 30 s: union of top 20 by CPU and top 20 by RSS,
  names only; CPU measured over the interval, not since process start.
- Core rollups (1m, 1h) maintained inside the batch transaction from rows
  actually inserted: gauges sum/min/max/n, counters first/last with
  first_ts/last_ts.
- Query: automatic step (~300 points), source chosen by step — raw, 1m or
  1h — and reported in every frame; rates divide by the covered span
  (OLD B5); NaN → null gaps. Detail-series frames with the same rules.
- API: `/hosts/{id}/series`, `/series/{sid}/metrics`, `/processes`, all in
  `openapi.yaml` and spec-validated in tests; a series id only resolves with
  its own host and tenant.
- Dashboard: host page with CPU / Memory / Disk / Network / Processes tabs,
  range picker 1h–30d, per-series small multiples (natural sort), process
  table; clicking a chart opens `#/logs?host&at` (page arrives in Phase 4).

**Acceptance**
- counter reset gives no negative or spiked rate — `TestCounterResetNeverGivesNegativeRate`,
  `TestDetailCountersFoldPerSeriesAcrossReboot` ✅
- rate over a bucket with an interior gap divides by covered span — `TestRateDividesByCoveredSpan`,
  `TestRateFromRollupsUsesEdgesAcrossBuckets`, `TestDetailFrameGaugeMeanAndCounterRate` ✅
- 30-day chart reads from the 1h rollup — `TestSourceFollowsWindow` (asserts the 1h read),
  `TestThirtyDayChartReadsHourlyRollup` (API reports `source: 1h`), manual run ✅
- `{"core":"3"}` and `{ "core" : "3" }` are one series — `TestLabelSpellingIsOneSeries` ✅
- series ceiling does not block existing series or fail the batch — `depth/series-ceiling` ✅
- plus: rollups equal an aggregation of raw, with a conflicting duplicate in
  the input — `depth/rollups-equal-raw`, run against SQLite (incremental
  upserts) **and** memstore (aggregates raw at read time).

**Bugs caught by the tests before they shipped**
- Rollup-path rates used a stale "last" value (84.6 B/s instead of 100): the
  bucket's first edge moved `lastTS` without its values. Fixed by recording
  both edges in one call.
- Mutation-proven guards: rollups counting conflicts, the first/last CASE
  inverted, the series cache filled before commit (produced exactly the
  predicted stale-key FOREIGN KEY failure), the ceiling ignored.

**Observed in the manual run** (real collectors, 2 s interval, ~4 min)
- Every 1m bucket's `n` equals its raw row count; a 20 s two-core burn shows
  as a 72 % peak; a 600 MiB fsync'd write as 4.8 MiB/s on nvme0n1;
  16 per-core series, 3 mounts, 2 devices, 2 NICs; 30 processes per snapshot;
  zero rejected, conflicting or dropped.

**Deviations**
- Omitting `step` now means automatic (~300 points), not "natural
  resolution". OLD B8's concern (unbounded results without `step`) holds by
  construction; an explicit too-fine step is still refused.
- Series labels are stored as a JSON object (deterministic key order);
  the canonical string is used only for hashing.

**Deferred**
- Detail-series retention (short raw retention) — with the retention
  janitor and plans in Phase 7.

---

## Phase 2 — Read path + dashboard shell · 2026-10-24

**Landed**
- Users and sessions: email unique across tenants (it finds the tenant at
  login); Argon2id (46 MiB, t=1, p=1, semaphore of 2; tampered parameters in a
  stored hash are refused, not allocated); session tokens stored as SHA-256,
  minted fresh on every login, idle 12 h / absolute 7 d, `LastSeen` written at
  most once a minute; the role is read live from the user on every request.
- Login: unknown emails still cost one full verification against a dummy hash
  (timing does not reveal which emails exist); all failures are one generic
  401; throttling per account+client with a **bounded limiter that fails
  closed** when full (failing open would let a junk-key flood unthrottle a
  real account).
- Read side: `LatestReader` (fleet grid never scans history), metrics use case
  with columnar frames, NaN → JSON `null` gaps (OLD B13), and a point cap that
  refuses with or without `step` (OLD B1/B8).
- Live path: ingest publishes **after commit** only (never for replays) into
  `livehub` — per-host bounded ring, newest-first backfill (OLD B6), tenant
  filtering inside the hub, slow subscribers dropped instead of blocking ingest.
- HTTP API: session cookie (HttpOnly, Secure, SameSite=Strict), `Origin`
  required on state-changing requests and the WebSocket upgrade, 404 (not 403)
  for another tenant's resources with a body identical to an unknown id,
  security headers + CSP, admin-only enrollment-token endpoint.
- `docs/openapi.yaml` is the source: TS types generated (CI drift check), and
  **every Go response in the API tests is validated against it** — including a
  check that every returned status is documented (kin-openapi accepts
  undocumented statuses; the test does not).
- Dashboard: React 19 + TanStack Query + uPlot; login, fleet grid with live
  CPU/memory charts, live values, status badges ("No data from host", never
  "down"), admin "Add a host" token flow. One WebSocket for all hosts;
  reconnect resumes from the oldest host's latest point, overlap deduped.
- Server: two listeners — ingest (TLS) and dashboard (`127.0.0.1:8080`,
  plain HTTP refused off loopback without `--api-tls`); `user create` CLI
  reads the password without echo, never from a flag.

**Acceptance**
- fleet grid updates live — manual run in Chromium (screenshots), `TestStreamDeliversOwnTenantAfterCommit` ✅
- viewer cannot call admin endpoints — `TestViewerCannotCallAdminEndpoints`, manual curl ✅
- tenant B gets 404 for tenant A's host and no WS frames — `TestOtherTenantsHostIs404Everywhere`,
  `TestStreamGivesNoFramesForAnotherTenantsHost`, `TestTenantIsolationInHub` ✅
- point-cap rejection with and without `step` — `TestMetricsPointCapAndNullGaps`, `TestSeriesPointCapWithAndWithoutStep` ✅
- reconnect backfill produces no duplicate points — `liveFeed.test.ts`, `ringbuffer.test.ts`,
  `TestStreamBackfillSinceOnReconnect`, `TestBackfillSinceIsBoundedAndNewerOnly` ✅

**Observed in the manual run**
- WebSocket held open 45 s with frames flowing: the ingest server's 30 s
  `ReadTimeout` does not survive the upgrade.
- Server restarted under a live agent: the agent retried the same `batch_seq`
  with jittered backoff (732 ms, 6.3 s); afterwards **340 samples over 678 s at
  2 s with zero gaps**, zero conflicts, zero drops.
- A stale Phase 1 server still bound to the ingest port: the agent's pinned
  enrollment refused it — "server key does not match the pinned key".

**Deviations**
- TypeScript 7 → **5.9.3**: openapi-typescript needs TypeScript's JS compiler
  API, which the native TS 7 compiler does not provide.
- `web/dist/` commits only `.gitkeep`, not a stub `index.html`: a tracked stub
  is overwritten by every build and invites committing build output. A server
  built without the UI logs a warning and serves the API only.

**Deferred**
- Disabling users and hosts (ports, CLI, UI) — the read paths already honour
  `disabled_at`; nothing writes it yet. Phase 5 (disabled hosts are part of
  alerting) or Phase 7 (erasure), whichever needs it first.
- Host detail page and rollups — Phase 3, as planned.

---

## Phase 1 — Walking skeleton · 2026-10-24

**Landed**
- `wire`: ingest protocol (`/ingest/v1/enroll`, `/ingest/v1/batch`), token
  format `stx1.<secret>[.<spki-pin>]`, limits; JSON field names pinned by test.
- `app/identity`: tenants, single-use enrollment tokens (1 h), credentials as
  SHA-256 of 32 random bytes. `app/ingest`: authenticator with bounded TTL
  cache, batch use case (validate → clamp to `[now−72h, now+60s]` → snap to the
  interval grid → in-batch dedupe → counter reset folding).
- `IngestStore.CommitBatch` takes a **build function executed inside the
  write transaction**: replay gate, counter state, rows, `latest_core`, host
  liveness/health and the `batch_seq` record commit together or not at all —
  R1 holds by construction, with no read-then-write race.
- Seq retention is derived from the ingest window (2 × MaxAge) and passed in
  `Prepared`, so the two values cannot drift apart (they are one config).
- SQLite adapter + memstore reference adapter; contract suite (15 cases) runs
  against both; reflection-driven cross-tenant check; statement audit through a
  recording driver. A deliberately tenant-blind adapter fails the suite.
- `ingesthttp`: response contract (400/413/422 drop · 401/403 stop · 5xx
  retry), compressed **and** decompressed caps (gzip bomb → 413), server
  timeouts, `--insecure` refused off loopback, 5xx bodies carry no internals.
- `tlscert`: self-signed P-256 key made once (0600, never overwritten), SPKI
  pin, pinned client (TLS 1.3, pin checked on every handshake).
- Agent: collectors (one file per subsystem; whole-disk IO only, virtual NICs
  excluded), epoch-floored persisted `batch_seq`, bounded buffer (drop-oldest,
  counted), shipper applying the contract, jittered backoff honouring
  `Retry-After`, seal + bounded drain on shutdown. CLIs for both binaries.

**Measured**

| what | result |
|---|---|
| core sample storage, REAL gauges + UUID keys | 142.2 B/sample |
| INTEGER×100 gauges + UUID keys | 100.5 B/sample |
| **INTEGER×100 gauges + integer keys (chosen)** | **67.6 B/sample (−53%)** |
| adapter write rate, one tx per 2-sample batch | ~42,000 batches/s (16-core dev box) |
| agent RSS over 60 s at 5 s interval | 13.3 MB (peak 13.7 MB) — budget 30 MB |
| agent CPU over 60 s | 0.13 % of one core — budget 1 % |
| binaries (static, stripped) | agent 7.0 MB, server 11.4 MB |

Benchmark: `go test ./internal/infra/sqlite -run '^$' -bench Encoding -benchtime 1x`.

**Acceptance**
- agent on this machine → server → SQLite: 13 samples, exact 5 s grid,
  monotonic counters, clock skew 2 ms (manual run with the real binaries) ✅
- replay stores nothing — `ingest/replay-applies-nothing`, `TestReplayIsAckedNotReapplied` ✅
- fresh agent state dir does not reuse seqs — `TestFreshStateDirDoesNotRepeatSeq` ✅
- forged `host_id`/`tenant_id` in the body ignored — `TestEnrollThenBatchStoresUnderCredentialHost` ✅
- token reuse rejected — `enroll/single-use`, `TestTokenReuseRejected`, manual CLI ✅
- `ts = now + 1y` rejected and counted — `TestIngestClampsSnapsAndDedupes`, `ingest/health-persisted` ✅
- counter reset never goes backwards — `TestIngestCounterResetNeverGoesBackwards` ✅
- gzip bomb → 413 — `TestGzipBombRejected` ✅
- wrong pin: enrollment fails before any request reaches the server — `TestEnrollRefusesWrongPinBeforeSendingSecret` ✅
- Linux + Windows: both build CGO-free; collectors and all packages are tested
  on the Windows CI runner. A manual Windows agent run is still to do.

Every guard above was mutation-tested (break it → its test fails).

**Defence in depth, observed:** with the tenant filter removed from the host
lookup (mutation), the composite `(tenant_key, host_key)` foreign key still
refused tenant B's write into tenant A's host. With the tenant filter removed
from the counter-state read, the cross-tenant test cannot notice (the host key
is already scoped) — the statement audit does. That is why both exist.

**Deviations from the plan**
- Credentials are looked up by hash, **not** compared in constant time: the
  lookup key is SHA-256 of a 32-byte random secret, so timing reveals at most a
  hash prefix. Documented in `app/ingest/auth.go`.
- Migrations live in `internal/infra/sqlite/migrations/`, not `/migrations`:
  `go:embed` only reaches the package's own tree, and the adapter importing a
  root package would break the layering rule.
- The agent may import `infra/tlscert` (pinning) — added to the layering rule.
- Telemetry rows use adapter-internal integer keys; public IDs stay UUIDs.

**Deferred**
- Host disable/revocation CLI (the cache's `Invalidate` exists for it) — with
  user/admin tooling in Phase 2.
- Credential at rest on Windows (DPAPI + ACL) — Phase 6, as planned.

---

## Phase 0 — Skeleton · 2026-10-24

**Landed**
- Module `github.com/MerhiOPS/sentrix`, `CONTRIBUTING.md` conventions, LF-enforcing
  `.gitattributes`, `.gitignore` covering secrets and data files.
- `scripts/ci` with four checks — `lines` (300/file), `layering` (DDD import
  direction incl. `wire`/`agent` rules), `rawsql` (SQL only in
  `internal/infra/sqlite`), `platform` (`x/sys/windows` only behind a windows
  constraint). Each check was mutation-tested: breaking the check makes its
  test fail (R2).
- Domain: typed IDs (canonical UUID v4, strict parse), `Scope`/`OpsScope`
  (zero values refuse to yield a tenant), `Host` + liveness (`ok`/`stale`/
  `no_data`/`disabled`, never "down"), `CoreSample`/`DetailSample` validation
  (non-finite and out-of-range rejected — OLD A-M4), `Labels` canonical form
  (quoted, so `=`/`,` inside values cannot collide), `LogLine`, `Level`
  normalisation (unknown, never guessed), sentinel errors.
- SQLite foundation: writer pool (1 conn, `BEGIN IMMEDIATE`) + read pool
  (`query_only`), pragmas in the DSN so every pooled connection gets them,
  boot-time verification that refuses to start if one did not apply.
- Migration runner: per-file checksum, edited migration refused, failed
  migration leaves no partial state, CRLF not treated as an edit.
- Web skeleton: React 19, Vite 8, TypeScript 7, Vitest; committed
  `web/dist/index.html` stub so `go:embed` works on a fresh clone (Phase 2).
- CI: Ubuntu + Windows test matrix, race detector on Linux, CGO-free
  cross-compile, golangci-lint v2.15.0 (config verified), web
  typecheck/test/build.

**Assumption tests — results on modernc.org/sqlite v1.61.0 (SQLite 3.54.1)**

| assumption | result |
|---|---|
| FTS5 `trigram` tokenizer available; case-insensitive substring match | ✅ |
| `foreign_keys` off by default and per-connection; on via DSN | ✅ |
| `query_only` read pool rejects writes | ✅ |
| `_txlock=immediate` takes the write lock at BEGIN (deferred does not — control) | ✅ |
| `auto_vacuum=INCREMENTAL` applied on a new file | ✅ |
| `secure_delete=1` removes deleted content from db + WAL (control: survives with 0) | ✅ |
| `VACUUM INTO` is consistent across tables under a concurrent writer | ✅ |
| multi-statement `Exec` (migrations) | ✅ |

The plan stands; no changes needed before Phase 1. The online-backup API was
not tested: the plan uses `VACUUM INTO`, which is now proven.

**Decisions taken while the owner was away** (PLAN §7, reversible)
- TLS default: self-signed + SPKI pin carried in the enrollment token.
- Repo stays private for now.
- v2 scope cuts accepted as written in PLAN §1.

**Deferred**
- Port interfaces: added per phase as their first consumer appears (PLAN §4).
- `migrations/` embed package: created in Phase 1 with `0001` — `go:embed`
  fails on an empty pattern.
