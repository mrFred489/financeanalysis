-- name: SetLineSubcategory :one
-- Manual categorization (CATZ-4): source='manual', rule cleared (D3/D4).
UPDATE statement_line
SET subcategory_id = ?, source = 'manual', rule_id = NULL
WHERE id = ?
RETURNING id;
