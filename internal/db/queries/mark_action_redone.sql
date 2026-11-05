-- name: MarkActionRedone :exec
UPDATE action_log
SET undone_at = NULL
WHERE id = ?;
