-- name: CreateRule :one
-- M3 rule create (RULE-1). Callers insert the rule_account rows in the same tx.
INSERT INTO rule (name, pattern, direction, subcategory_id, enabled)
VALUES (?, ?, ?, ?, ?)
RETURNING id, name, pattern, direction, subcategory_id, enabled, created_at, updated_at;
