-- name: ListTransferRejections :many
SELECT out_line_id, in_line_id, created_at FROM transfer_rejection;
