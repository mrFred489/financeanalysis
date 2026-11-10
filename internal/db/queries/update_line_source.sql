-- name: UpdateLineSource :exec
-- Generic categorization write used by the rules evaluator (PRD 7.5):
-- sets subcategory_id, source and rule_id together.
UPDATE statement_line
SET subcategory_id = ?, source = ?, rule_id = ?
WHERE id = ?;
