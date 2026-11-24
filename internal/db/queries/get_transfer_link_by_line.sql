-- name: GetTransferLinkByLine :one
-- The link a line participates in, on either side (D11).
SELECT id, out_line_id, in_line_id, method, created_at FROM transfer_link
WHERE out_line_id = ? OR in_line_id = ?;
