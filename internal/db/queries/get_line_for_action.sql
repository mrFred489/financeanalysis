-- name: GetLineForAction :one
-- Full statement_line row, marshaled to JSON by the caller for the
-- before_json/after_json snapshots of an action (D8).
SELECT id, account_id, import_id, booking_date, transaction_date, value_date,
       message, amount_minor, balance_minor, currency, bank_ref, counterparty_account,
       file_row, seq, occurrence, subcategory_id, source, rule_id, spread_months
FROM statement_line
WHERE id = ?;
