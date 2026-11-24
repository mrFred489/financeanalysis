-- name: DeleteActionChange :exec
-- M4-fix1 (IMP-13): delete one change row by its PK (action_id, entity, entity_id).
DELETE FROM action_change
WHERE action_id = ? AND entity = ? AND entity_id = ?;
