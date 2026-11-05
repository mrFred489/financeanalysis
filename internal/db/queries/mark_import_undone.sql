-- name: MarkImportUndone :exec
UPDATE import_batch
SET undone_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;
