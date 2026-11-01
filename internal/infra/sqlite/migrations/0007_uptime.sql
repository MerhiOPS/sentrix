-- 0007: uptime monitoring — monitors and their state, check results, the
-- hourly uptime rollup, incidents and events; plan limits and metering for
-- them; silences that can name a monitor.
--
-- As in 0005, every table carries tenant_key and every reference is a
-- composite (tenant_key, key) foreign key, so a monitor can only notify
-- through its own tenant's channels (PLAN §4.1).
--
-- A monitor's current state lives on its row: it is read with the
-- definition by the scheduler and written with every result. revision is
-- bumped on every write; a check stores its result only if the revision it
-- started from is still current.

ALTER TABLE tenants ADD COLUMN max_monitors INTEGER NOT NULL DEFAULT 50;
ALTER TABLE tenants ADD COLUMN min_check_interval_ms INTEGER NOT NULL DEFAULT 60000;
ALTER TABLE usage_daily ADD COLUMN checks INTEGER NOT NULL DEFAULT 0;

CREATE TABLE monitors (
    key             INTEGER PRIMARY KEY,
    tenant_key      INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    name            TEXT    NOT NULL,
    kind            TEXT    NOT NULL CHECK (kind IN ('http', 'tcp', 'ping')),
    target          TEXT    NOT NULL,
    interval_ms     INTEGER NOT NULL,
    timeout_ms      INTEGER NOT NULL,
    confirm_retries INTEGER NOT NULL,
    method          TEXT    NOT NULL, -- http only; '' otherwise
    accept_status   TEXT    NOT NULL, -- "200-399,401"; '' = the default
    keyword         TEXT    NOT NULL,
    keyword_mode    TEXT    NOT NULL CHECK (keyword_mode IN ('', 'present', 'absent')),
    keyword_case    INTEGER NOT NULL CHECK (keyword_case IN (0, 1)),
    cert_warn_days  INTEGER NOT NULL,
    paused          INTEGER NOT NULL CHECK (paused IN (0, 1)),
    created_at      INTEGER NOT NULL,
    status          TEXT    NOT NULL CHECK (status IN ('pending', 'up', 'down', 'paused')),
    since           INTEGER NOT NULL,
    last_check      INTEGER,
    failures        INTEGER NOT NULL DEFAULT 0,
    first_failure   INTEGER,
    cert            TEXT    NOT NULL DEFAULT '' CHECK (cert IN ('', 'ok', 'expiring')),
    cert_expiry     INTEGER,
    revision        INTEGER NOT NULL DEFAULT 0,
    UNIQUE (tenant_key, key)
) STRICT;

CREATE INDEX monitors_tenant ON monitors (tenant_key);

CREATE TABLE monitor_channels (
    tenant_key  INTEGER NOT NULL,
    monitor_key INTEGER NOT NULL,
    channel_key INTEGER NOT NULL,
    PRIMARY KEY (monitor_key, channel_key),
    FOREIGN KEY (tenant_key, monitor_key) REFERENCES monitors (tenant_key, key) ON DELETE CASCADE,
    FOREIGN KEY (tenant_key, channel_key) REFERENCES channels (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

-- One row per check (~30 B). Never the response body: an error class only.
CREATE TABLE monitor_results (
    tenant_key  INTEGER NOT NULL,
    monitor_key INTEGER NOT NULL,
    at          INTEGER NOT NULL,
    outcome     TEXT    NOT NULL CHECK (outcome IN ('up', 'down', 'unknown')),
    class       TEXT    NOT NULL,
    latency_ms  INTEGER,
    ttfb_ms     INTEGER,
    status      INTEGER,
    loss        INTEGER,
    cert_expiry INTEGER,
    PRIMARY KEY (tenant_key, monitor_key, at),
    FOREIGN KEY (tenant_key, monitor_key) REFERENCES monitors (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

-- Monitored time per hour, attributed to up or down (domain.StepMonitor),
-- and check counts and latency. Uptime and the daily bars are read from it.
CREATE TABLE monitor_hourly (
    tenant_key     INTEGER NOT NULL,
    monitor_key    INTEGER NOT NULL,
    hour           INTEGER NOT NULL, -- unix ms, truncated to the UTC hour
    checks         INTEGER NOT NULL DEFAULT 0,
    failures       INTEGER NOT NULL DEFAULT 0,
    up_ms          INTEGER NOT NULL DEFAULT 0,
    down_ms        INTEGER NOT NULL DEFAULT 0,
    latency_sum_ms INTEGER NOT NULL DEFAULT 0,
    latency_n      INTEGER NOT NULL DEFAULT 0,
    latency_max_ms INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_key, monitor_key, hour),
    FOREIGN KEY (tenant_key, monitor_key) REFERENCES monitors (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE monitor_incidents (
    key         INTEGER PRIMARY KEY,
    tenant_key  INTEGER NOT NULL,
    monitor_key INTEGER NOT NULL,
    started     INTEGER NOT NULL,
    ended       INTEGER, -- NULL while open
    class       TEXT    NOT NULL,
    failures    INTEGER NOT NULL,
    FOREIGN KEY (tenant_key, monitor_key) REFERENCES monitors (tenant_key, key) ON DELETE CASCADE
) STRICT;

CREATE INDEX monitor_incidents_monitor ON monitor_incidents (tenant_key, monitor_key, started);
-- At most one open incident per monitor, whatever an adapter does.
CREATE UNIQUE INDEX monitor_incidents_open ON monitor_incidents (monitor_key) WHERE ended IS NULL;

CREATE TABLE monitor_events (
    key         INTEGER PRIMARY KEY,
    tenant_key  INTEGER NOT NULL,
    monitor_key INTEGER NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('down', 'up', 'cert_expiring', 'cert_renewed')),
    at          INTEGER NOT NULL,
    class       TEXT    NOT NULL,
    down_for_ms INTEGER,
    cert_expiry INTEGER,
    notify      INTEGER NOT NULL CHECK (notify IN (0, 1)),
    outcome     TEXT    NOT NULL DEFAULT '',
    delivery    TEXT    NOT NULL DEFAULT '',
    FOREIGN KEY (tenant_key, monitor_key) REFERENCES monitors (tenant_key, key) ON DELETE CASCADE
) STRICT;

CREATE INDEX monitor_events_tenant ON monitor_events (tenant_key, key);

-- A silence may name a monitor. ALTER TABLE cannot add a composite foreign
-- key, so tenant ownership of monitor_key is enforced by the adapter, which
-- resolves the monitor inside the tenant before storing it (as every port
-- does); the column reference still cascades a deleted monitor.
ALTER TABLE silences ADD COLUMN monitor_key INTEGER REFERENCES monitors (key) ON DELETE CASCADE;
