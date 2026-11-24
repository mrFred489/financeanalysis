-- name: TransferRejectionExists :one
SELECT COUNT(*) FROM transfer_rejection WHERE out_line_id = ? AND in_line_id = ?;
