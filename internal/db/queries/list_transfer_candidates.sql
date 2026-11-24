-- name: ListTransferCandidates :many
-- Candidate lines for transfer detection (TRF-1): not already linked, not
-- source='transfer', non-zero amount. Rejection pairs are filtered in Go, because
-- a rejection is per pair, not per line. Ordered for the Go grouping pass.
SELECT sl.id, sl.account_id, sl.booking_date, sl.amount_minor
FROM statement_line sl
WHERE sl.source != 'transfer'
  AND sl.amount_minor != 0
  AND NOT EXISTS (SELECT 1 FROM transfer_link tl WHERE tl.out_line_id = sl.id OR tl.in_line_id = sl.id)
ORDER BY sl.booking_date, sl.amount_minor, sl.id;
