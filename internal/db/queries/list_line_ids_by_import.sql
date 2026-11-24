-- name: ListLineIdsByImport :many
SELECT id FROM statement_line WHERE import_id = sqlc.arg(import_id);
