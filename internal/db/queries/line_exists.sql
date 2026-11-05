-- name: LineExists :one
-- Duplicate check against the unique key (IMP-6, PRD section 6).
SELECT COUNT(*) FROM statement_line
WHERE account_id = ?
  AND booking_date = ?
  AND amount_minor = ?
  AND balance_minor = ?
  AND message = ?
  AND occurrence = ?;
