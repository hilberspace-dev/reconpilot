CREATE TABLE IF NOT EXISTS source_batch (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_type    TEXT NOT NULL CHECK (source_type IN ('psp','bank','marketplace')),
    file_ref       TEXT NOT NULL,
    ingested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    status         TEXT NOT NULL DEFAULT 'ingesting',
    row_count      INT  NOT NULL DEFAULT 0,
    rejected_count INT  NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS transaction (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    batch_id         BIGINT NOT NULL REFERENCES source_batch(id),
    source_type      TEXT NOT NULL,
    external_ref     TEXT NOT NULL,
    counterparty_ref TEXT NOT NULL,           -- REQUIRED for group candidate narrowing
    amount_kurus     BIGINT NOT NULL,         -- integer minor units, never float
    currency         TEXT NOT NULL DEFAULT 'TRY',
    occurred_at      TIMESTAMPTZ NOT NULL,
    direction        TEXT NOT NULL CHECK (direction IN ('credit','debit')),
    dedup_key        TEXT NOT NULL UNIQUE,    -- invariant 4 at schema level
    status           TEXT NOT NULL DEFAULT 'unmatched',
    gross_kurus      BIGINT NOT NULL DEFAULT 0,
    commission_kurus BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS match_group (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('exact','tolerant','group','manual')),
    score      BIGINT NOT NULL DEFAULT 100,   -- score in basis points; no floats
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS match_member (
    match_group_id BIGINT NOT NULL REFERENCES match_group(id),
    transaction_id BIGINT NOT NULL REFERENCES transaction(id),
    UNIQUE (transaction_id)                    -- invariant 2 at schema level
);

CREATE TABLE IF NOT EXISTS discrepancy (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id    BIGINT REFERENCES transaction(id),
    match_group_id    BIGINT REFERENCES match_group(id),
    type              TEXT NOT NULL CHECK (type IN
        ('commission','refund','partial','timing','duplicate','missing','unknown')),
    amount_delta_kurus BIGINT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'open',
    CHECK (transaction_id IS NOT NULL OR match_group_id IS NOT NULL)
    -- a discrepancy attached to nothing would silently break invariant 1
);

CREATE TABLE IF NOT EXISTS rejected_row (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES source_batch(id),
    row_no   INT NOT NULL,
    reason   TEXT NOT NULL,
    raw_line TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_log (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entity    TEXT NOT NULL,
    entity_id BIGINT NOT NULL,
    action    TEXT NOT NULL,
    at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    details   JSONB
);
