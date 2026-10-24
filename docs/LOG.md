# Build log

One entry per phase: what landed, what was measured, what was deferred and
why. Newest first.

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
