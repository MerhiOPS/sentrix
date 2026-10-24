-- 0001: tenants, hosts, enrollment, ingest.
--
-- Keys: every public identifier is a UUID (`id`, 16-byte BLOB). Telemetry
-- rows are keyed by small adapter-internal integers (`tenant_key`,
-- `host_key`) instead: measured in Phase 1, this halves bytes per core
-- sample (142 → 68 B) because SQLite stores the key in every row.
--
-- Isolation (PLAN §4.1): every tenant-owned table carries tenant_key, and
-- child rows reference hosts(tenant_key, key) with a composite FK, so a row
-- cannot point at another tenant's host.
--
-- Gauges are stored as INTEGER × 100 (two decimals): 1–4 bytes per value
-- instead of 8 for REAL. Load averages likewise.

CREATE TABLE tenants (
    key        INTEGER PRIMARY KEY,
    id         BLOB    NOT NULL UNIQUE CHECK (length(id) = 16),
    name       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE hosts (
    key             INTEGER PRIMARY KEY,
    tenant_key      INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    id              BLOB    NOT NULL UNIQUE CHECK (length(id) = 16),
    name            TEXT    NOT NULL,
    os              TEXT    NOT NULL,
    platform        TEXT    NOT NULL,
    agent_version   TEXT    NOT NULL,
    interval_ms     INTEGER NOT NULL,
    created_at      INTEGER NOT NULL,
    last_seen       INTEGER,
    disabled_at     INTEGER,
    credential_hash BLOB    NOT NULL UNIQUE CHECK (length(credential_hash) = 32),
    buffer_depth    INTEGER NOT NULL DEFAULT 0,
    dropped_total   INTEGER NOT NULL DEFAULT 0,
    clock_skew_ms   INTEGER NOT NULL DEFAULT 0,
    rejected_total  INTEGER NOT NULL DEFAULT 0,
    conflict_total  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (tenant_key, key)
) STRICT;

CREATE TABLE enrollment_tokens (
    secret_hash BLOB    PRIMARY KEY CHECK (length(secret_hash) = 32),
    tenant_key  INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    used_at     INTEGER,
    host_key    INTEGER REFERENCES hosts (key) ON DELETE SET NULL
) STRICT;

CREATE INDEX enrollment_tokens_tenant ON enrollment_tokens (tenant_key);

-- One row per stored batch: the replay gate. Pruned only past twice the
-- ingest window, so a pruned seq cannot come back with acceptable data.
CREATE TABLE ingest_batches (
    tenant_key  INTEGER NOT NULL,
    host_key    INTEGER NOT NULL,
    seq         INTEGER NOT NULL,
    received_at INTEGER NOT NULL,
    PRIMARY KEY (host_key, seq),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE INDEX ingest_batches_age ON ingest_batches (host_key, received_at);

-- Counter reset folding state (R1: committed in the batch transaction).
CREATE TABLE host_counters (
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    counter    TEXT    NOT NULL,
    last_raw   INTEGER NOT NULL,
    offset     INTEGER NOT NULL,
    PRIMARY KEY (host_key, counter),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE metrics_core (
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    ts         INTEGER NOT NULL, -- unix ms, snapped to the host's interval
    cpu        INTEGER NOT NULL, -- percent × 100
    mem        INTEGER NOT NULL,
    disk       INTEGER NOT NULL,
    load1      INTEGER NOT NULL, -- load × 100
    load5      INTEGER NOT NULL,
    load15     INTEGER NOT NULL,
    mem_used   INTEGER NOT NULL, -- bytes
    uptime     INTEGER NOT NULL, -- seconds
    net_rx     INTEGER NOT NULL, -- counters: reset-adjusted, monotonic
    net_tx     INTEGER NOT NULL,
    disk_r     INTEGER NOT NULL,
    disk_w     INTEGER NOT NULL,
    PRIMARY KEY (tenant_key, host_key, ts),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

-- The fleet grid reads this, never metrics_core history.
CREATE TABLE latest_core (
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL PRIMARY KEY,
    ts         INTEGER NOT NULL,
    cpu        INTEGER NOT NULL,
    mem        INTEGER NOT NULL,
    disk       INTEGER NOT NULL,
    load1      INTEGER NOT NULL,
    load5      INTEGER NOT NULL,
    load15     INTEGER NOT NULL,
    mem_used   INTEGER NOT NULL,
    uptime     INTEGER NOT NULL,
    net_rx     INTEGER NOT NULL,
    net_tx     INTEGER NOT NULL,
    disk_r     INTEGER NOT NULL,
    disk_w     INTEGER NOT NULL,
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) STRICT;

CREATE INDEX latest_core_tenant ON latest_core (tenant_key);
