-- 0002: dashboard users and browser sessions.
--
-- Email is unique across tenants: the login form does not know the tenant,
-- the email finds it. Sessions store only the SHA-256 of their token, and do
-- not copy the role: it is read from the user on every lookup, so demoting or
-- disabling a user takes effect on their next request, not at session end.

CREATE TABLE users (
    key           INTEGER PRIMARY KEY,
    tenant_key    INTEGER NOT NULL REFERENCES tenants (key) ON DELETE CASCADE,
    id            BLOB    NOT NULL UNIQUE CHECK (length(id) = 16),
    email         TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL CHECK (role IN ('admin', 'viewer')),
    created_at    INTEGER NOT NULL,
    disabled_at   INTEGER,
    UNIQUE (tenant_key, key)
) STRICT;

CREATE INDEX users_tenant ON users (tenant_key);

CREATE TABLE sessions (
    token_hash BLOB    PRIMARY KEY CHECK (length(token_hash) = 32),
    tenant_key INTEGER NOT NULL,
    user_key   INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    FOREIGN KEY (tenant_key, user_key) REFERENCES users (tenant_key, key) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE INDEX sessions_user ON sessions (user_key);
