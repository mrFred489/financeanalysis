-- name: DeleteConflictResolution :exec
-- RULE-7 undo / manual categorization clears the remembered choice (D7).
DELETE FROM conflict_resolution WHERE line_id = ?;
