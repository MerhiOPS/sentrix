-- 0004: logs, search index, log metering, redaction rules.
--
-- logs is a rowid table: FTS5 external content needs an integer rowid. The
-- index holds trigrams of the message only (PLAN §4.2: case-insensitive
-- substring search, terms >= 3 characters) and is kept in step with the
-- table by triggers, so an insert or delete can never leave it stale — the
-- erasure path (Phase 7) depends on the delete trigger.

ALTER TABLE tenants ADD COLUMN log_bytes_per_day INTEGER NOT NULL DEFAULT 1073741824;

CREATE TABLE logs (
    id         INTEGER PRIMARY KEY,
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    ts         INTEGER NOT NULL,
    source     TEXT    NOT NULL,
    level      TEXT    NOT NULL,
    message    TEXT    NOT NULL,
    labels     TEXT    NOT NULL, -- JSON object
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) STRICT;

CREATE INDEX logs_tenant_ts ON logs (tenant_key, ts, id);
CREATE INDEX logs_host_ts ON logs (tenant_key, host_key, ts, id);

CREATE VIRTUAL TABLE logs_fts USING fts5(
    message,
    content = 'logs',
    content_rowid = 'id',
    tokenize = 'trigram case_sensitive 0'
);

CREATE TRIGGER logs_fts_insert AFTER INSERT ON logs BEGIN
    INSERT INTO logs_fts (rowid, message) VALUES (new.id, new.message);
END;

CREATE TRIGGER logs_fts_delete AFTER DELETE ON logs BEGIN
    INSERT INTO logs_fts (logs_fts, rowid, message) VALUES ('delete', old.id, old.message);
END;

-- Per host per UTC day, written in the batch transaction. The tenant's quota
-- is the sum of bytes over its hosts for the day.
CREATE TABLE log_usage (
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    day        INTEGER NOT NULL, -- days since the Unix epoch, UTC
    bytes      INTEGER NOT NULL DEFAULT 0,
    stored     INTEGER NOT NULL DEFAULT 0,
    received   INTEGER NOT NULL DEFAULT 0,
    redacted   INTEGER NOT NULL DEFAULT 0,
    dropped    INTEGER NOT NULL DEFAULT 0,
    shed       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_key, host_key, day),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE redaction_rules (
    id          INTEGER PRIMARY KEY,
    tenant_key  INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    pattern     TEXT    NOT NULL,
    replacement TEXT    NOT NULL,
    action      TEXT    NOT NULL CHECK (action IN ('mask', 'drop')),
    created_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX redaction_rules_tenant ON redaction_rules (tenant_key);
