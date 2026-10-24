# Runbook

What to look at, and what to do, when something is wrong. Every check below
uses something the server exposes today: `GET /healthz`, `GET /readyz`,
`GET /api/v1/system` (admin session), the dashboard, and the logs
(`journalctl -u sentrix-server`, `journalctl -u sentrix-agent`, or
`C:\ProgramData\sentrix-agent\agent.log` on Windows).

Paths assume the packages: server data in `/var/lib/sentrix`, agent state in
`/var/lib/sentrix-agent`.

---

## Disk full (or filling) on the server

**Symptoms**
- `/api/v1/system` → `server.disk_free_ratio` below `0.20`.
- Server log: `disk guard: log ingest shedding`.
- Log searches stop showing new lines; metrics keep arriving (by design —
  below 20 % free the server sheds **logs**, never metrics).

**Do**
1. Find what grew: `du -sh /var/lib/sentrix/*`. Normally it is
   `sentrix.db`; see the next section if it is `sentrix.db-wal`.
2. Check retention is keeping up: `server.ttl_overdue_s` should be near `0`
   and `server.janitor_last_run` within the last 10 minutes. If the janitor
   is failing, its error is in the server log (`retention run`).
3. Free space outside Sentrix, or grow the volume. Freed database pages are
   returned to the filesystem by the janitor (`PRAGMA incremental_vacuum`)
   after each run that deleted rows.
4. If one tenant is the cause, see "A tenant flooding logs".

Do **not** delete `sentrix.db-wal` or `sentrix.db-shm` by hand: they are part
of the database while the server runs.

## Database or `-wal` file growth

**Symptoms** — `server.db_wal_bytes` keeps growing (hundreds of MB) instead
of shrinking back after load.

**Why it happens** — SQLite can only checkpoint the WAL back into the
database when no reader is still using older pages. A long-lived read (a huge
export, a stuck request) holds it open.

**Do**
1. `server.writer_waits` / `writer_wait_ms` rising fast means writes are
   queueing behind something; `/readyz` returns 503 when the writer has not
   answered within 2 s.
2. Restart the server (`systemctl restart sentrix-server`). Agents buffer in
   their WAL meanwhile and replay; nothing is lost. The checkpoint runs on
   the next start.
3. If `sentrix.db` itself is large, compare with the expected size: about
   68 B per raw core sample and 278 B per log line (measured, see
   `docs/LOG.md`). Retention defaults: raw 7 d, details 7 d, processes 3 d,
   1-minute rollups 30 d, hourly rollups 400 d, logs 14 d.

## A tenant flooding logs

**Symptoms** — one tenant's hosts send far more log lines than usual; disk
use climbs; other tenants may be shed once the disk watermark is crossed.

**What already protects you**
- Each tenant has a daily log budget (`log_bytes_per_day`, default 1 GiB).
  Past it, that tenant's **logs** are shed with reason `quota` and its
  metrics keep flowing. The budget is charged inside the batch transaction,
  so replays and retries cannot overspend it.
- Each host is rate-limited to 20 requests/s (burst 40); beyond that it gets
  `429` with `Retry-After` and resends later.

**Do**
1. Find the source in the dashboard: Logs page, filter by host.
2. Drop the noisy lines with a tenant redaction rule (action `drop`):
   admins can add one from the Logs page and it applies to the next batch.
   Changing a tenant's daily budget has no CLI yet (it arrives with plans in
   Phase 7); the default is 1 GiB/day.
3. On the host, narrow what the agent tails (`SENTRIX_AGENT_ARGS` in
   `/etc/default/sentrix-agent`) and restart the agent.

## An agent stuck replaying

**Symptoms** — the host card shows "N batches waiting on the agent
(replaying)"; `/api/v1/system` → `fleet.agent_wal_depth_max` high; the host's
data is minutes or hours behind.

**How replay works** — the agent ships its disk WAL oldest first (FIFO on
purpose: counters are folded in batch order) at up to 10 batches/s, so live
data arrives late rather than out of order. A 1-hour outage at the default
10 s batches is 360 batches: about 40 s to drain.

**Do**
1. Agent log: `server unavailable; will retry` means it still cannot reach
   the server (network, TLS, server down). `rate limited` (status 429) means
   the per-host cap is being hit — expected briefly during replay.
2. `server refused the credential` → see the next section.
3. `server refused batch permanently; dropped` — a batch the server will
   never accept (malformed, over limits). It is dropped and counted
   ("batches dropped by the agent" on the host card) so it cannot block the
   queue.
4. The WAL is capped at 100 MB; past that the oldest batches are dropped and
   counted. Batches older than 71 h are dropped locally because the server
   refuses samples older than 72 h.

## Revoked or disabled credential

**Symptoms** — agent log: `server refused the credential; buffering until it
is accepted again` with status 401 (unknown credential) or 403
(`host is disabled`). The host shows **disabled** in the dashboard.

**Do**
- *Disabled on purpose*: nothing. The agent keeps buffering (up to its WAL
  cap) and re-checks every 5 minutes; re-enabling the host (host page →
  Enable host) resumes it without a restart, and the backlog replays.
- *401 — the server does not know the credential* (database restored from
  before the enrollment, host deleted): re-enroll with a new token.
  Linux: stop the agent, remove `/var/lib/sentrix-agent/identity.json`, run
  `sudo -u sentrix-agent sentrix-agent enroll …`, start it. Windows: same with
  `C:\ProgramData\sentrix-agent\identity.json`, then restart the service.
  The new enrollment is a new host; the old one keeps its history.
- *TLS: `server key does not match the pinned key`*: the server's key changed
  (data directory lost or replaced). The agent refuses to talk to it — that
  is the pin working. Restore the old `server-key.pem`/`server-cert.pem` into
  the server's data directory, or re-enroll every agent.

## Restore

The supported backup and its timed restore drill arrive in Phase 7
(`VACUUM INTO`, `docs/restore-drill.md`). Until then, a consistent copy is
taken with the server **stopped**:

```sh
systemctl stop sentrix-server
cp -a /var/lib/sentrix /var/backups/sentrix-$(date +%F)   # db, -wal, keys
systemctl start sentrix-server
```

To restore: stop the server, replace `/var/lib/sentrix` with the copy
(ownership `sentrix:sentrix`, mode 0700), start it. Keep `server-key.pem`:
agents are pinned to it.

What a restore loses: everything the server **acknowledged** between the
copy and the restore. Agents delete a batch from their WAL once it is
acknowledged, so they cannot resend it. Only batches still unacknowledged
(for example while the server was down for the restore) are replayed. Hosts
enrolled after the copy are unknown to the restored database and must be
re-enrolled (see above: 401).
