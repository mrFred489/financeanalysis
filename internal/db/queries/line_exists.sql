-- name: LineExists :one
-- Duplicate check against the unique key (IMP-6, PRD section 6). The current
-- bank format has no running balance, so balance is not part of the key.
SELECT COUNT(*) FROM statement_line
WHERE account_id = ?
  AND booking_date = ?
  AND amount_minor = ?
  AND message = ?
  AND occurrence = ?;
