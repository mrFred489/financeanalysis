-- name: GetEarliestLineAfter :one
-- Earliest existing line strictly after (date, seq); used by the account sanity check (IMP-8).
SELECT id, booking_date, seq, balance_minor, amount_minor
FROM statement_line
WHERE account_id = ?
  AND (booking_date > ? OR (booking_date = ? AND seq > ?))
ORDER BY booking_date ASC, seq ASC
LIMIT 1;
