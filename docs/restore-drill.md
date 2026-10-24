# Restore drill

A backup is only as good as the last restore that was checked. This is the
procedure, how it is verified, and what it measured.

## Procedure

```sh
# while the server runs — VACUUM INTO reads one consistent snapshot
sentrix-server backup  --data /var/lib/sentrix --out /var/backups/sentrix-$(date +%F).db
cp /var/lib/sentrix/server-key.pem /var/lib/sentrix/server-cert.pem /var/backups/

# into an empty directory; it refuses to overwrite a database
sentrix-server restore --from /var/backups/sentrix-2026-10-24.db --data /var/lib/sentrix-restored
cp /var/backups/server-*.pem /var/lib/sentrix-restored/
```

`restore` copies the file, opens it the way the server does (every pragma
verified, pending migrations applied) and runs `PRAGMA integrity_check`;
any failure removes the restored file. Both commands are written to the
audit log.

## How it is verified

| check | test |
|---|---|
| a known dataset comes back exactly: every table, the search index's shadow tables included, has the same row count and the same SHA-256 over its rows in key order | `TestRestoreReproducesDatasetExactly` |
| the backup is one snapshot across tables: a writer commits marker pairs (a sample and a log line with the same number, in one batch) while the backup runs; every restored marker has its pair | `TestBackupUnderConcurrentWritesIsCrossTableConsistent` (last run: backup taken with 57 of 78 marker batches written, all pairs intact) |
| restore never overwrites | both tests, and the CLI (`already holds a database`) |
| timed, larger dataset | `SENTRIX_DRILL=1 go test ./internal/infra/sqlite -run TestRestoreDrillTimed -v` |

## Measured (i5-13400F, NVMe)

`SENTRIX_DRILL=1 SENTRIX_DRILL_HOSTS=50 SENTRIX_DRILL_BATCHES=720`: 50 hosts ×
2 h of 10 s batches, each batch 2 core samples, 36 detail values and 5 log
lines.

| | |
|---|---|
| rows | 72 000 core samples, 1 296 000 detail values, 180 000 log lines (+ rollups, FTS index) |
| database | 73.2 MiB |
| backup file | 68.5 MiB (free pages are not copied) |
| backup time | 0.22 s |
| restore (copy + open + verify + migrations + `integrity_check`) | 1.02 s |
| restored digest | identical in every table |

Both scale roughly with file size; a database in the tens of GB is a
minutes-scale backup, and the snapshot is a read transaction, so ingest
continues meanwhile (the WAL grows until it ends).

## What a restore loses

Everything acknowledged after the backup was taken. Agents delete a batch
from their WAL once it is acknowledged, so they cannot resend it; only
batches still unacknowledged while the server was down are replayed. Hosts
enrolled after the backup must re-enroll. Data erased after the backup
**comes back** with the restore: re-run the erasure (the audit log lists
erasures by subject hash).
