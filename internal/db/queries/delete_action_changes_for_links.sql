-- name: DeleteActionChangesForLinks :exec
-- M4-fix1 (IMP-13): action-log cleanup on import undo. Deletes the
-- transfer_link change rows of links that touch the batch's lines (the links
-- themselves are removed by ON DELETE CASCADE when the lines are deleted).
-- link_ids is a JSON array of those link ids (json_each).
DELETE FROM action_change
WHERE entity = 'transfer_link'
  AND entity_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.arg(link_ids)));
