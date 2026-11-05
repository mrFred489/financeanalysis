-- +goose Up

-- Accounts (bank accounts the user imports statements for)
CREATE TABLE account (
    id INTEGER PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    account_number TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- Subcategories: user-defined children of exactly one category
CREATE TABLE subcategory (
    id INTEGER PRIMARY KEY,
    category_id INTEGER NOT NULL REFERENCES category(id),
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    icon TEXT,
    hotkey TEXT UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE (category_id, name)
) STRICT;

-- Import batches: one uploaded file for one account
CREATE TABLE import_batch (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES account(id),
    file_name TEXT NOT NULL,
    file_sha256 TEXT NOT NULL,
    imported_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    line_count INTEGER NOT NULL DEFAULT 0,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    undone_at TEXT
) STRICT;

-- Statement lines: one row from a bank statement
CREATE TABLE statement_line (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES account(id),
    import_id INTEGER REFERENCES import_batch(id),
    booking_date TEXT NOT NULL,
    message TEXT NOT NULL,
    amount_minor INTEGER NOT NULL,
    balance_minor INTEGER NOT NULL,
    currency TEXT NOT NULL DEFAULT 'DKK',
    file_row INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    occurrence INTEGER NOT NULL DEFAULT 1,
    subcategory_id INTEGER REFERENCES subcategory(id),
    source TEXT NOT NULL DEFAULT 'none' CHECK (source IN ('none', 'rule', 'manual', 'transfer', 'conflict')),
    rule_id INTEGER,
    spread_months INTEGER NOT NULL DEFAULT 1 CHECK (spread_months BETWEEN 1 AND 36)
) STRICT;

-- Duplicate detection key (see PRD section 6)
CREATE UNIQUE INDEX idx_statement_line_dedup
    ON statement_line (account_id, booking_date, amount_minor, balance_minor, message, occurrence);

-- Chronological listing
CREATE INDEX idx_statement_line_chrono ON statement_line (account_id, booking_date, seq);
CREATE INDEX idx_statement_line_subcategory ON statement_line (subcategory_id);
CREATE INDEX idx_statement_line_source ON statement_line (source);
CREATE INDEX idx_import_batch_account ON import_batch (account_id);

-- +goose Down

DROP TABLE IF EXISTS statement_line;
DROP TABLE IF EXISTS import_batch;
DROP TABLE IF EXISTS subcategory;
DROP TABLE IF EXISTS account;
