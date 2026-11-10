-- name: UpdateRule :one
-- M3 rule edit (RULE-5): full replace of the definition; rule_account rows are
-- replaced separately in the same tx (DeleteRuleAccountsForRule + InsertRuleAccount).
UPDATE rule
SET name = ?, pattern = ?, direction = ?, subcategory_id = ?, enabled = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, name, pattern, direction, subcategory_id, enabled, created_at, updated_at;
