-- name: GetTransferLink :one
SELECT id, out_line_id, in_line_id, method, created_at FROM transfer_link WHERE id = ?;
