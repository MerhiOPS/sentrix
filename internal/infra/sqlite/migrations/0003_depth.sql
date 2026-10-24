-- 0003: detail series, process snapshots, core rollups.
--
-- Detail series are narrow (one row per value) because their cardinality
-- varies per host (cores, mounts, devices, NICs). Values are INTEGER at the
-- series' fixed-point scale (×100 for percentages, ×1 for bytes).
--
-- Rollups are maintained inside the batch transaction from the rows that
-- were actually inserted (a conflicting duplicate never counts twice).
-- Gauges keep sum/min/max/n; counters keep first/last with the shared
-- first_ts/last_ts, so a rate divides by the span its samples cover, never
-- by the bucket width (OLD B5).

ALTER TABLE tenants ADD COLUMN series_limit INTEGER NOT NULL DEFAULT 10000;

CREATE TABLE series (
    key         INTEGER PRIMARY KEY,
    tenant_key  INTEGER NOT NULL,
    host_key    INTEGER NOT NULL,
    metric      TEXT    NOT NULL,
    labels_hash TEXT    NOT NULL,
    labels      TEXT    NOT NULL, -- JSON object, for display
    kind        TEXT    NOT NULL CHECK (kind IN ('gauge', 'counter')),
    scale       INTEGER NOT NULL CHECK (scale > 0),
    created_at  INTEGER NOT NULL,
    UNIQUE (host_key, metric, labels_hash),
    UNIQUE (tenant_key, key),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) STRICT;

CREATE INDEX series_tenant ON series (tenant_key);

CREATE TABLE samples_detail (
    tenant_key INTEGER NOT NULL,
    series_key INTEGER NOT NULL,
    ts         INTEGER NOT NULL,
    value      INTEGER NOT NULL,
    PRIMARY KEY (tenant_key, series_key, ts),
    FOREIGN KEY (tenant_key, series_key) REFERENCES series (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE process_snapshots (
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    ts         INTEGER NOT NULL,
    procs      TEXT    NOT NULL, -- JSON array of {name, cpu, rss}
    PRIMARY KEY (tenant_key, host_key, ts),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE rollup_core_1m (
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    bucket     INTEGER NOT NULL, -- unix ms of the bucket start
    n          INTEGER NOT NULL,
    cpu_sum INTEGER NOT NULL, cpu_min INTEGER NOT NULL, cpu_max INTEGER NOT NULL,
    mem_sum INTEGER NOT NULL, mem_min INTEGER NOT NULL, mem_max INTEGER NOT NULL,
    disk_sum INTEGER NOT NULL, disk_min INTEGER NOT NULL, disk_max INTEGER NOT NULL,
    load1_sum INTEGER NOT NULL, load1_min INTEGER NOT NULL, load1_max INTEGER NOT NULL,
    load5_sum INTEGER NOT NULL, load5_min INTEGER NOT NULL, load5_max INTEGER NOT NULL,
    load15_sum INTEGER NOT NULL, load15_min INTEGER NOT NULL, load15_max INTEGER NOT NULL,
    mem_used_sum INTEGER NOT NULL, mem_used_min INTEGER NOT NULL, mem_used_max INTEGER NOT NULL,
    net_rx_first INTEGER NOT NULL, net_rx_last INTEGER NOT NULL,
    net_tx_first INTEGER NOT NULL, net_tx_last INTEGER NOT NULL,
    disk_r_first INTEGER NOT NULL, disk_r_last INTEGER NOT NULL,
    disk_w_first INTEGER NOT NULL, disk_w_last INTEGER NOT NULL,
    first_ts INTEGER NOT NULL,
    last_ts  INTEGER NOT NULL,
    PRIMARY KEY (tenant_key, host_key, bucket),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE TABLE rollup_core_1h (
    tenant_key INTEGER NOT NULL,
    host_key   INTEGER NOT NULL,
    bucket     INTEGER NOT NULL,
    n          INTEGER NOT NULL,
    cpu_sum INTEGER NOT NULL, cpu_min INTEGER NOT NULL, cpu_max INTEGER NOT NULL,
    mem_sum INTEGER NOT NULL, mem_min INTEGER NOT NULL, mem_max INTEGER NOT NULL,
    disk_sum INTEGER NOT NULL, disk_min INTEGER NOT NULL, disk_max INTEGER NOT NULL,
    load1_sum INTEGER NOT NULL, load1_min INTEGER NOT NULL, load1_max INTEGER NOT NULL,
    load5_sum INTEGER NOT NULL, load5_min INTEGER NOT NULL, load5_max INTEGER NOT NULL,
    load15_sum INTEGER NOT NULL, load15_min INTEGER NOT NULL, load15_max INTEGER NOT NULL,
    mem_used_sum INTEGER NOT NULL, mem_used_min INTEGER NOT NULL, mem_used_max INTEGER NOT NULL,
    net_rx_first INTEGER NOT NULL, net_rx_last INTEGER NOT NULL,
    net_tx_first INTEGER NOT NULL, net_tx_last INTEGER NOT NULL,
    disk_r_first INTEGER NOT NULL, disk_r_last INTEGER NOT NULL,
    disk_w_first INTEGER NOT NULL, disk_w_last INTEGER NOT NULL,
    first_ts INTEGER NOT NULL,
    last_ts  INTEGER NOT NULL,
    PRIMARY KEY (tenant_key, host_key, bucket),
    FOREIGN KEY (tenant_key, host_key) REFERENCES hosts (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;
