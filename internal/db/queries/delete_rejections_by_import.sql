-- name: DeleteRejectionsByImport :exec
-- IMP-13 cleanup: rejection rows referencing lines of an undone import. Must run
-- before the lines themselves are deleted.
DELETE FROM transfer_rejection
WHERE out_line_id IN (SELECT sl.id FROM statement_line sl WHERE sl.import_id = sqlc.arg(import_id))
   OR in_line_id IN (SELECT sl.id FROM statement_line sl WHERE sl.import_id = sqlc.arg(import_id));
