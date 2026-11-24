-- name: SetLineSourceTransfer :exec
-- TRF-5: a linked line becomes source='transfer' and leaves rule/manual state.
UPDATE statement_line
SET source = 'transfer', subcategory_id = NULL, rule_id = NULL
WHERE id = ?;
