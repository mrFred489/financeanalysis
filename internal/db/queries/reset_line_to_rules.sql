-- name: ResetLineToRules :one
-- "Reset to rules" (D4): M2 semantics clear the manual assignment because no
-- rules exist yet; M3 upgrades this to rule re-evaluation (same action kind).
UPDATE statement_line
SET subcategory_id = NULL, source = 'none', rule_id = NULL
WHERE id = ?
RETURNING id;
