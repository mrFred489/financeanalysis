-- name: InsertStatementLine :one
-- M1: new lines always get source = 'none' (D5); rule/transfer evaluation is M3/M4.
-- CSV format v2 (PRD section 7.2): transaction_date/value_date come from columns
-- 7/9, balance_minor from column 5 (NULL when the export has no balance),
-- bank_ref from column 10, counterparty_account from column 3 (transfers).
INSERT INTO statement_line (
    account_id, import_id, booking_date, transaction_date, value_date,
    message, amount_minor, balance_minor, currency, bank_ref, counterparty_account,
    file_row, seq, occurrence, source
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'none')
RETURNING id;
