-- name: MarkActionUndone :exec
UPDATE action_log
SET undone_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;
