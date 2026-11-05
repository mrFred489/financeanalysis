-- name: CreateImportBatch :one
INSERT INTO import_batch (account_id, file_name, file_sha256, line_count, duplicate_count)
VALUES (?, ?, ?, ?, ?)
RETURNING id, account_id, file_name, file_sha256, imported_at, line_count, duplicate_count, undone_at;
