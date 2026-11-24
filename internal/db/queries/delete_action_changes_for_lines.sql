-- name: DeleteActionChangesForLines :exec
-- M4-fix1 (IMP-13): action-log cleanup on import undo. Deletes the change rows
-- of actions that reference rows deleted with the batch's statement lines:
-- statement_line snapshots and conflict_resolution rows (both keyed by line id).
-- line_ids is a JSON array of the batch's line ids (json_each).
DELETE FROM action_change
WHERE entity IN ('statement_line', 'conflict_resolution')
  AND entity_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.arg(line_ids)));
