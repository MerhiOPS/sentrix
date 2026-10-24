# Build log

One entry per phase: what landed, what was measured, what was deferred and
why. Newest first.

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
