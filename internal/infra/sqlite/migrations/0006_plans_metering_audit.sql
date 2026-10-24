-- 0006: per-tenant plans, write-path metering, the operator audit log.
--
-- Metering is written inside CommitBatch, after the replay gate, so a
-- resent batch can never be counted twice, and nothing is computed by
-- scanning storage. usage_daily holds aggregates only and is keyed by
-- tenant, so erasing a host removes its data but not the tenant's billed
-- totals; usage_host_days (which hosts reported on a day, for the peak) is
-- per host and is erased with it.

ALTER TABLE tenants ADD COLUMN max_hosts INTEGER NOT NULL DEFAULT 100;
ALTER TABLE tenants ADD COLUMN metrics_retention_days INTEGER NOT NULL DEFAULT 400;
ALTER TABLE tenants ADD COLUMN logs_retention_days INTEGER NOT NULL DEFAULT 14;

CREATE TABLE usage_daily (
    tenant_key INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    day        INTEGER NOT NULL, -- days since the Unix epoch, UTC
    samples    INTEGER NOT NULL DEFAULT 0, -- core + detail values stored
    log_bytes  INTEGER NOT NULL DEFAULT 0, -- log bytes stored
    hosts      INTEGER NOT NULL DEFAULT 0, -- distinct hosts that reported
    PRIMARY KEY (tenant_key, day)
) WITHOUT ROWID, STRICT;

CREATE TABLE usage_host_days (
    tenant_key INTEGER NOT NULL,
    day        INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    PRIMARY KEY (tenant_key, day, host_key),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

-- Operator actions (erasure, backup, restore, plan changes). Not tenant
-- data: subjects are stored as SHA-256 of their id, so an erased subject's
-- id does not survive in its own audit entry.
CREATE TABLE audit_log (
    key          INTEGER PRIMARY KEY,
    at           INTEGER NOT NULL,
    action       TEXT    NOT NULL,
    subject_hash TEXT    NOT NULL,
    outcome      TEXT    NOT NULL,
    detail       TEXT    NOT NULL
) STRICT;
