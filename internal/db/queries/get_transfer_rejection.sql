-- name: GetTransferRejection :one
SELECT out_line_id, in_line_id, created_at FROM transfer_rejection
WHERE out_line_id = ? AND in_line_id = ?;
