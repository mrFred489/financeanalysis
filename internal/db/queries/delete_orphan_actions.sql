-- name: DeleteOrphanActions :exec
-- M4-fix1 (IMP-13): prune action_log rows left with zero change rows after
-- import undo removed their changes, so they do not consume an undo step.
DELETE FROM action_log
WHERE id NOT IN (SELECT DISTINCT action_id FROM action_change);
