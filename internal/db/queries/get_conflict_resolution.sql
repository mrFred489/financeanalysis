-- name: GetConflictResolution :one
SELECT line_id, rule_id, created_at FROM conflict_resolution WHERE line_id = ?;
