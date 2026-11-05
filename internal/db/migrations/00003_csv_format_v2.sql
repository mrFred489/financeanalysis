-- +goose Up

-- CSV format v2 (PRD section 7.2, plan docs/plans/csv-format-v2.md): the bank's
-- real export is a fixed 16-column file with no running balance and three dates.
-- Rebuild statement_line (SQLite table rebuild, docs/migrations.md section 4;
-- the custom migrator runs this inside a transaction and no table references
-- statement_line yet, so foreign keys can stay enabled):
--   - balance_minor becomes nullable (column 5 is empty in this export)
--   - new nullable columns: transaction_date (col 7), value_date (col 9),
--     bank_ref (col 10), counterparty_account (col 3, transfers)
--   - the dedup unique index drops balance_minor

CREATE TABLE statement_line_v2 (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES account(id),
    import_id INTEGER REFERENCES import_batch(id),
    booking_date TEXT NOT NULL,
    transaction_date TEXT,
    value_date TEXT,
    message TEXT NOT NULL,
    amount_minor INTEGER NOT NULL,
    balance_minor INTEGER,
    currency TEXT NOT NULL DEFAULT 'DKK',
    bank_ref TEXT,
    counterparty_account TEXT,
    file_row INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    occurrence INTEGER NOT NULL DEFAULT 1,
    subcategory_id INTEGER REFERENCES subcategory(id),
    source TEXT NOT NULL DEFAULT 'none' CHECK(source IN ('none', 'rule', 'manual', 'transfer', 'conflict')),
    rule_id INTEGER,
    spread_months INTEGER NOT NULL DEFAULT 1 CHECK(spread_months BETWEEN 1 AND 36)
) STRICT;

INSERT INTO statement_line_v2 (
    id, account_id, import_id, booking_date, message, amount_minor, balance_minor,
    currency, file_row, seq, occurrence, subcategory_id, source, rule_id, spread_months
)
SELECT
    id, account_id, import_id, booking_date, message, amount_minor, balance_minor,
    currency, file_row, seq, occurrence, subcategory_id, source, rule_id, spread_months
FROM statement_line;

DROP TABLE statement_line;

ALTER TABLE statement_line_v2 RENAME TO statement_line;

-- Duplicate detection key without balance (PRD section 6)
CREATE UNIQUE INDEX idx_statement_line_dedup
    ON statement_line (account_id, booking_date, amount_minor, message, occurrence);

-- Chronological listing and the other indexes from 00002
CREATE INDEX idx_statement_line_chrono ON statement_line (account_id, booking_date, seq);
CREATE INDEX idx_statement_line_subcategory ON statement_line (subcategory_id);
CREATE INDEX idx_statement_line_source ON statement_line (source);

-- +goose Down

-- Dev-only rollback: rebuild the pre-v2 schema (balance NOT NULL, no v2 columns).
CREATE TABLE statement_line_v1 (
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
    source TEXT NOT NULL DEFAULT 'none' CHECK(source IN ('none', 'rule', 'manual', 'transfer', 'conflict')),
    rule_id INTEGER,
    spread_months INTEGER NOT NULL DEFAULT 1 CHECK(spread_months BETWEEN 1 AND 36)
) STRICT;

INSERT INTO statement_line_v1 (
    id, account_id, import_id, booking_date, message, amount_minor, balance_minor,
    currency, file_row, seq, occurrence, subcategory_id, source, rule_id, spread_months
)
SELECT
    id, account_id, import_id, booking_date, message, amount_minor, COALESCE(balance_minor, 0),
    currency, file_row, seq, occurrence, subcategory_id, source, rule_id, spread_months
FROM statement_line;

DROP TABLE statement_line;

ALTER TABLE statement_line_v1 RENAME TO statement_line;

CREATE UNIQUE INDEX idx_statement_line_dedup
    ON statement_line (account_id, booking_date, amount_minor, balance_minor, message, occurrence);
CREATE INDEX idx_statement_line_chrono ON statement_line (account_id, booking_date, seq);
CREATE INDEX idx_statement_line_subcategory ON statement_line (subcategory_id);
CREATE INDEX idx_statement_line_source ON statement_line (source);
