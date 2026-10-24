# Storage adapters

Sentrix stores everything through ports — Go interfaces in `internal/app`
— and ships two implementations: SQLite (`internal/infra/sqlite`, the
product) and an in-memory reference (`internal/infra/memstore`, which exists
so the contract describes the ports, not one adapter). This is how a third
one, ClickHouse for example, would be added, and when it would be worth it.

## The ports

`storetest.Stores` (`internal/infra/storetest/stores.go`) lists every port a
complete adapter implements:

| area | ports |
|---|---|
| identity | `identity.TenantRepo`, `EnrollmentRepo`, `UserRepo`, `SessionRepo`, `HostRepo`, `PlanRepo` |
| ingest | `ingest.Store` (`CommitBatch`), `ingest.CredentialStore` |
| read side | `query.HostReader`, `CoreReader`, `LatestReader`, `RollupReader`, `DetailReader`, `ProcessReader`, `LogReader` |
| redaction | `redaction.RuleRepo` |
| alerting | `alerting.RuleRepo`, `ChannelRepo`, `SilenceRepo`, `StateRepo`, `BucketReader`, `TenantLister`, `Sweeper` |
| retention | `retention.Store` |

Operator-only work (erasure, backup) is on `compliance.ErasureStore` and the
SQLite `Backup`/`Restore`; those are inherently storage-specific.

## The contract

```go
func TestContract(t *testing.T) {
    storetest.Run(t, func(t *testing.T) storetest.Stores { return newClickHouseStore(t) })
}
```

`storetest.Run` runs every behavioural case (38 today: replay, conflicts,
counter state, rollups equal an aggregation of raw, series ceiling, log
search semantics and paging, quota read inside the transaction, alert state,
retention, metering, …) and the **cross-tenant suite**: two tenants with
identical data, every scoped method called as tenant B against A's ids. The
list of methods is **reflected** from the port interfaces, so adding a
method without a cross-tenant case fails the suite. `CrossTenantProblems`
returns problems instead of failing, which is how the suite is shown to
catch a deliberately tenant-blind adapter.

Adapter-specific tests stay in the adapter: the SQLite package also runs the
whole contract through a recording driver and fails on any statement that
touches a tenant table without binding `tenant_key`.

## The `CommitBatch` caveat

The ingest port is one call:

```go
CommitBatch(ctx, scope, host, seq, build BuildFunc) (CommitResult, error)
```

and its contract is **atomicity**: the replay gate (`(host, seq)` seen?),
the counter state the build reads and writes, the rows, the rollups, the log
budget read and the metering all commit together or not at all. SQLite gives
that with one transaction. ClickHouse has no multi-statement transactions, so
an adapter has to rebuild the guarantee rather than inherit it:

- **Replay gate and counter state** need a linearizable store: keep them in
  a small OLTP database (Postgres, or this SQLite) and write metric rows to
  ClickHouse **after** the gate admits the batch.
- **Idempotent rows**: a crash between the gate and the insert must not lose
  or double data. Insert with `insert_deduplication_token = "<host>:<seq>"`
  and key tables on `(tenant, host, ts)` in a `ReplacingMergeTree`, so a
  resend after a crash is absorbed; mark the gate only after the insert
  succeeds.
- **Rollups**: are an adapter concern on purpose — a ClickHouse adapter uses
  materialized views (`AggregatingMergeTree`), not the in-transaction writes
  SQLite does. The contract still checks rollup reads equal an aggregation
  of raw.
- **Metering and the log budget** must stay exact under replay: they belong
  with the gate in the OLTP store.
- **Log search**: the semantics (case-insensitive substring, AND/OR,
  phrases, literal syntax) are pinned by the contract; in ClickHouse that is
  `ILIKE` with an `ngrambf_v1` skip index.

## Erasure and retention

The SQLite erasure post-check byte-scans the database and WAL files. A
ClickHouse adapter needs its own proof: `ALTER TABLE … DELETE` (a mutation)
rewrites parts asynchronously, so erasure must wait for the mutation and
check `system.parts` and the remaining parts, not just that a `SELECT` comes
back empty. Retention maps to `TTL` clauses per table, with `ttl_overdue`
still measured, not assumed.

## When it is worth it

SQLite with one writer measured **840–940 batches/s** through the full
server path (`BenchmarkIngestCeiling`), about 8 400–9 400 hosts at a 5 s
interval with per-core/disk/NIC detail, on one desktop CPU. The single
writer is the ceiling. Beyond that — or when one table holds billions of
rows and analytical queries over months matter — a columnar store wins.

The predecessor, `serverwatch-OLD`, ran on ClickHouse with codecs and measured 1.20
bytes per value for a wide row against 5.45 for a narrow one. Those are
ClickHouse numbers and are not quoted for SQLite; on SQLite, Sentrix stores
a core sample (12 values) in 67.6 B.
