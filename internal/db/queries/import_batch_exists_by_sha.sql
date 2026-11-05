-- name: ImportBatchExistsBySHA :one
-- IMP-9: warn when re-uploading a file with an identical SHA-256.
SELECT COUNT(*) FROM import_batch
WHERE file_sha256 = ? AND undone_at IS NULL;
