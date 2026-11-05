-- name: InsertStatementLine :one
-- M1: new lines always get source = 'none' (D5); rule/transfer evaluation is M3/M4.
INSERT INTO statement_line (
    account_id, import_id, booking_date, message, amount_minor, balance_minor,
    currency, file_row, seq, occurrence, source
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'none')
RETURNING id;
