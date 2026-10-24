# Data handling

What Sentrix stores, for how long, how it is erased, and what a backup
keeps. This describes the software; it is not a legal Data Processing
Agreement — there is no customer to sign one.

## What is stored

| data | where it comes from | contains personal data? |
|---|---|---|
| host metrics (CPU, memory, disk, load, network, per-core/mount/device/NIC) | the agent | no, unless a hostname, mount or NIC name is personal |
| process snapshots (top processes: name, CPU, memory) | the agent | process names can be |
| logs (message, source, labels) | only the sources an operator lists (`--log-file`, `--journald`, `--eventlog`) | often — usernames, emails, IPs |
| users (email, Argon2id password hash, role) and sessions (SHA-256 of the token) | the operator / login | yes (email) |
| alert rules, channels (webhook/Slack URLs — credentials), events | admins | no |
| metering (per tenant per day: samples, log bytes, distinct hosts) | the write path | no (aggregates) |
| audit log (operator actions; subjects stored as SHA-256 of their id) | operator commands | no |

**Before storage**, every log line's message, source and every label is
redacted: bearer and basic tokens, `Authorization` headers, URL credentials,
`key=value` secrets, emails, IPv4 and IPv6 addresses, plus each tenant's own
RE2 rules (mask or drop). Redacted values are never stored or indexed.

## Retention

The retention janitor runs every 10 minutes and deletes in bounded batches
per (tenant, host). Defaults, capped per tenant by its plan
(`metrics_retention_days` caps every metric kind, `logs_retention_days`
sets logs; `sentrix-server tenant plan`):

| kind | default |
|---|---|
| raw core samples | 7 days |
| per-core / mount / device / NIC detail | 7 days |
| process snapshots | 3 days |
| 1-minute rollups | 30 days |
| hourly rollups | 400 days |
| logs (and their search index entries) | 14 days |

`ttl_overdue` in `GET /api/v1/system` is the age of the oldest row past its
retention: it should stay near zero, and is what shows a stuck janitor.

## Erasure

```sh
sentrix-server erase host   --data DIR --tenant T --host H --yes
sentrix-server erase user   --data DIR --tenant T --user U --yes
sentrix-server erase tenant --data DIR --tenant T --yes
```

Run with the server stopped (the WAL can only be truncated with no reader).
Each run:

1. plants a random canary in the subject's own data (a log line, so the
   search index too, and a process snapshot for a host; a session for a user;
   the tenant row and every host for a tenant), and collects the identifiers
   that must vanish (the canary, the subject's id, a host's credential hash,
   a user's email and password hash);
2. deletes the subject — every table holding its data references it with
   `ON DELETE CASCADE` (a used enrollment token only loses its link to the
   host), and the log delete trigger removes the search index rows;
3. merges the search index (`optimize`), returns free pages
   (`incremental_vacuum`), and folds the WAL into the database and truncates
   it (`wal_checkpoint(TRUNCATE)`); `secure_delete` is on for every write,
   so freed content is zeroed;
4. **byte-scans the database file and its WAL** for every identifier; any
   hit fails the erasure;
5. writes an audit entry (action, SHA-256 of the subject id, outcome).

Tests show the check working, not just passing: it fails when a copy of the
canary is left in another row (`TestPostCheckFailsWhenARowIsLeftBehind`) and
when `secure_delete` is off (`TestPostCheckFailsWithSecureDeleteOff`).

**Limits of the check.** It finds byte strings. The search index stores
3-character terms with prefix compression, so residue of a deleted line
inside the index would not show up as the canary; the `optimize` step is what
removes it. Usage aggregates (per tenant per day) are kept after a host is
erased: they hold no data about the host.

## Backups

`sentrix-server backup` (see `docs/restore-drill.md`). A backup is a full
copy: **data erased after a backup still exists in that backup until the
backup is deleted.** Keep backups only as long as needed, and after
restoring an old backup, re-run any erasures made since (the audit log has
them).

## Export

Admins can export their tenant's own metrics (raw, within retention), logs
and alert events: `GET /api/v1/export?table=…&from=…&to=…&format=csv|ndjson`.
