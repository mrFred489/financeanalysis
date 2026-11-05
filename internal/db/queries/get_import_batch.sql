-- name: GetImportBatch :one
SELECT id, account_id, file_name, file_sha256, imported_at, line_count, duplicate_count, undone_at
FROM import_batch
WHERE id = ?;
