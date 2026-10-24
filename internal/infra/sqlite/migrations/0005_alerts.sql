-- 0005: alert rules, their state and events, channels, silences.
--
-- Every table carries tenant_key, and every reference to a host, rule or
-- channel is a composite (tenant_key, key) foreign key: a rule can only name
-- hosts and channels of its own tenant, so "tenant A's rule on tenant B's
-- host" cannot be stored even by a buggy adapter (PLAN §4.1).
--
-- alert_events is append-only history; an event is inserted before its
-- state row is updated, in the same transaction (R1).

CREATE TABLE channels (
    key        INTEGER PRIMARY KEY,
    tenant_key INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('webhook', 'slack')),
    url        TEXT    NOT NULL, -- a secret: never returned by the API
    created_at INTEGER NOT NULL,
    UNIQUE (tenant_key, key)
) STRICT;

CREATE TABLE alert_rules (
    key         INTEGER PRIMARY KEY,
    tenant_key  INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('threshold', 'no_data')),
    metric      TEXT    NOT NULL,
    aggregation TEXT    NOT NULL,
    comparator  TEXT    NOT NULL,
    threshold   REAL    NOT NULL,
    duration_ms INTEGER NOT NULL,
    on_no_data  TEXT    NOT NULL,
    cooldown_ms INTEGER NOT NULL,
    enabled     INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at  INTEGER NOT NULL,
    UNIQUE (tenant_key, key)
) STRICT;

CREATE TABLE alert_rule_hosts (
    tenant_key INTEGER NOT NULL,
    rule_key   INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    PRIMARY KEY (rule_key, host_key),
    FOREIGN KEY (tenant_key, rule_key) REFERENCES alert_rules (tenant_key, key) ON DELETE CASCADE,
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE alert_rule_channels (
    tenant_key  INTEGER NOT NULL,
    rule_key    INTEGER NOT NULL,
    channel_key INTEGER NOT NULL,
    PRIMARY KEY (rule_key, channel_key),
    FOREIGN KEY (tenant_key, rule_key) REFERENCES alert_rules (tenant_key, key) ON DELETE CASCADE,
    FOREIGN KEY (tenant_key, channel_key) REFERENCES channels (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE alert_states (
    tenant_key    INTEGER NOT NULL,
    rule_key      INTEGER NOT NULL,
    host_key      INTEGER NOT NULL,
    state         TEXT    NOT NULL CHECK (state IN ('ok', 'pending', 'firing')),
    since         INTEGER NOT NULL,
    last_notified INTEGER,
    PRIMARY KEY (rule_key, host_key),
    FOREIGN KEY (tenant_key, rule_key) REFERENCES alert_rules (tenant_key, key) ON DELETE CASCADE,
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE INDEX alert_states_tenant ON alert_states (tenant_key);

CREATE TABLE alert_events (
    key        INTEGER PRIMARY KEY,
    tenant_key INTEGER NOT NULL,
    rule_key   INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    from_state TEXT    NOT NULL,
    to_state   TEXT    NOT NULL,
    at         INTEGER NOT NULL,
    value      REAL,
    notify     INTEGER NOT NULL CHECK (notify IN (0, 1)),
    outcome    TEXT    NOT NULL DEFAULT '',
    delivery   TEXT    NOT NULL DEFAULT '',
    FOREIGN KEY (tenant_key, rule_key) REFERENCES alert_rules (tenant_key, key) ON DELETE CASCADE,
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) STRICT;

CREATE INDEX alert_events_tenant ON alert_events (tenant_key, key);

CREATE TABLE silences (
    key        INTEGER PRIMARY KEY,
    tenant_key INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    rule_key   INTEGER, -- NULL: every rule
    host_key   INTEGER, -- NULL: every host
    starts     INTEGER NOT NULL,
    ends       INTEGER NOT NULL,
    reason     TEXT    NOT NULL,
    FOREIGN KEY (tenant_key, rule_key) REFERENCES alert_rules (tenant_key, key) ON DELETE CASCADE,
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) STRICT;

CREATE INDEX silences_tenant ON silences (tenant_key);
