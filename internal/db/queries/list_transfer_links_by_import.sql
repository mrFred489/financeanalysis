-- name: ListTransferLinksByImport :many
-- Links touching any line of an import batch (IMP-13: import undo cleanup +
-- re-evaluation of the surviving counterpart line).
SELECT tl.id, tl.out_line_id, tl.in_line_id, tl.method, tl.created_at
FROM transfer_link tl
WHERE EXISTS (SELECT 1 FROM statement_line sl WHERE sl.id = tl.out_line_id AND sl.import_id = sqlc.arg(import_id))
   OR EXISTS (SELECT 1 FROM statement_line sl WHERE sl.id = tl.in_line_id AND sl.import_id = sqlc.arg(import_id));
