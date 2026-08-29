CREATE TABLE query_records (
    phone_number TEXT NOT NULL,
    source TEXT NOT NULL,
    tag TEXT NOT NULL,
    confidence BIGINT NOT NULL,
    hit_count BIGINT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (phone_number, source)
);

CREATE INDEX idx_query_records_phone_number
    ON query_records (phone_number);

CREATE TABLE runtime_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
