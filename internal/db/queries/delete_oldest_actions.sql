-- name: DeleteOldestActions :exec
-- Keep only the newest N actions; ON DELETE CASCADE removes their changes (UNDO-3 cap).
DELETE FROM action_log
WHERE id NOT IN (SELECT id FROM action_log ORDER BY id DESC LIMIT ?);
