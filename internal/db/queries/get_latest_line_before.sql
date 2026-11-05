-- name: GetLatestLineBefore :one
-- Latest existing line strictly before (date, seq); used by the account sanity check (IMP-8).
SELECT id, booking_date, seq, balance_minor
FROM statement_line
WHERE account_id = ?
  AND (booking_date < ? OR (booking_date = ? AND seq < ?))
ORDER BY booking_date DESC, seq DESC
LIMIT 1;
