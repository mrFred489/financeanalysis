-- name: ListLinesByRule :many
-- Lines currently pointing at a rule (RULE-6: dropped lines are re-evaluated).
-- Only the columns the evaluator needs for matching + snapshots are selected.
SELECT id, account_id, message, amount_minor, source, subcategory_id, rule_id
FROM statement_line
WHERE rule_id = ?
ORDER BY id;
