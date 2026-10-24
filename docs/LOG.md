# Build log

One entry per phase: what landed, what was measured, what was deferred and
why. Newest first.

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
- Module `github.com/MerhiOPS/sentrix`, `CLAUDE.md` conventions, LF-enforcing
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
