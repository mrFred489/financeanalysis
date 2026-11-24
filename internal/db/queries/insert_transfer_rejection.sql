-- name: InsertTransferRejection :exec
-- Record a pair that must never be auto-matched again (TRF-6 unlink, TRF-3 dismiss).
INSERT INTO transfer_rejection (out_line_id, in_line_id) VALUES (?, ?)
ON CONFLICT (out_line_id, in_line_id) DO NOTHING;
