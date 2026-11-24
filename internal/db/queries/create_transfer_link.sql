-- name: CreateTransferLink :one
-- Link an out-line and an in-line (TRF-2 auto / TRF-7 manual).
INSERT INTO transfer_link (out_line_id, in_line_id, method)
VALUES (?, ?, ?)
RETURNING *;
