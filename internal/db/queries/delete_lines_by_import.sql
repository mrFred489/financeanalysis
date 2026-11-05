-- name: DeleteLinesByImport :exec
DELETE FROM statement_line WHERE import_id = ?;
