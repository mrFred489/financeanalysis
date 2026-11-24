-- +goose Up

-- M4 transfers (PRD section 6/7.4, plan docs/plans/M4-transfers.md D1).
-- transfer_link: one detected or manually created internal transfer between two
-- of the user's own accounts. Each line can be in at most one link (UNIQUE on
-- both sides). ON DELETE CASCADE on both line references makes import undo
-- (delete the import's lines) remove the links automatically (IMP-13).
-- transfer_rejection: pairs the user unlinked or dismissed; they are never
-- auto-matched again (TRF-1/TRF-6). No FK here: rows referencing deleted lines
-- are harmless and are cleaned up explicitly on import undo.

CREATE TABLE transfer_link (
    id INTEGER PRIMARY KEY,
    out_line_id INTEGER NOT NULL UNIQUE REFERENCES statement_line(id) ON DELETE CASCADE,
    in_line_id INTEGER NOT NULL UNIQUE REFERENCES statement_line(id) ON DELETE CASCADE,
    method TEXT NOT NULL DEFAULT 'auto' CHECK (method IN ('auto', 'manual')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE TABLE transfer_rejection (
    out_line_id INTEGER NOT NULL,
    in_line_id INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (out_line_id, in_line_id)
) STRICT;

-- Partial index for the detection scan: unlinked candidates by date+amount
-- (plan step 1, risk mitigation for the 40k-line scan).
CREATE INDEX idx_statement_line_transfer_scan
    ON statement_line (booking_date, amount_minor)
    WHERE source != 'transfer';

-- +goose Down

-- Dev-only rollback.
DROP INDEX IF EXISTS idx_statement_line_transfer_scan;
DROP TABLE IF EXISTS transfer_rejection;
DROP TABLE IF EXISTS transfer_link;
