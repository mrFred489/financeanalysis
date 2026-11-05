-- name: CountLinesByImport :one
SELECT COUNT(*) FROM statement_line WHERE import_id = ?;
