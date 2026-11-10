-- name: DeleteConflictResolutionsForRule :exec
-- Used when a rule is deleted (the FK cascade covers it, this is explicit for clarity).
DELETE FROM conflict_resolution WHERE rule_id = ?;
