CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER NOT NULL PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS query_records (
    phone_number TEXT NOT NULL,
    source TEXT NOT NULL,
    tag TEXT NOT NULL,
    confidence INTEGER NOT NULL,
    hit_count INTEGER NOT NULL,
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (phone_number, source)
);

CREATE INDEX IF NOT EXISTS idx_query_records_phone_number
    ON query_records (phone_number);

CREATE TABLE IF NOT EXISTS runtime_metadata (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO schema_migrations (version, applied_at)
VALUES (1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
