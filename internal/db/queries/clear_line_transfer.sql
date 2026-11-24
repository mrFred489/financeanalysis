-- name: ClearLineTransfer :exec
-- TRF-6: before rule re-evaluation on unlink, the line goes back to source='none'.
UPDATE statement_line
SET source = 'none', subcategory_id = NULL, rule_id = NULL
WHERE id = ?;
