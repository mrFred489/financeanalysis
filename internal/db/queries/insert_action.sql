-- name: InsertAction :one
-- Record one undoable user action (M2, UNDO-2). Callers insert the action_change
-- rows in the same transaction.
INSERT INTO action_log (kind, description)
VALUES (?, ?)
RETURNING id, kind, description, created_at, undone_at;
