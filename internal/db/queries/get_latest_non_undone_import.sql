-- name: GetLatestNonUndoneImport :one
-- The import that IMP-13 undo operates on: the latest non-undone batch globally.
SELECT id, account_id, file_name, file_sha256, imported_at, line_count, duplicate_count, undone_at
FROM import_batch
WHERE undone_at IS NULL
ORDER BY id DESC
LIMIT 1;
