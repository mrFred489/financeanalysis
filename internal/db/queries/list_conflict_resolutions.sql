-- name: ListConflictResolutions :many
SELECT line_id, rule_id, created_at FROM conflict_resolution ORDER BY line_id;
