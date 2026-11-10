-- name: UpsertConflictResolution :one
-- RULE-7: remember the winning rule for a conflicted line.
INSERT INTO conflict_resolution (line_id, rule_id)
VALUES (?, ?)
ON CONFLICT (line_id) DO UPDATE SET rule_id = excluded.rule_id, created_at = excluded.created_at
RETURNING line_id, rule_id, created_at;
