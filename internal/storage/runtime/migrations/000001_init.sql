CREATE TABLE query_records (
    phone_number TEXT NOT NULL,
    source TEXT NOT NULL,
    tag TEXT NOT NULL,
    confidence INTEGER NOT NULL,
    hit_count INTEGER NOT NULL,
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (phone_number, source)
);

CREATE INDEX idx_query_records_phone_number
    ON query_records (phone_number);

CREATE TABLE runtime_metadata (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
