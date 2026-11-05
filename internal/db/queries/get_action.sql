-- name: GetAction :one
-- One action row (used by Undo/Redo to find the latest actionable entry).
SELECT id, kind, description, created_at, undone_at
FROM action_log
WHERE id = ?;

-- name: GetLatestUndoableAction :one
-- Latest action that has not been undone yet (UNDO-3 LIFO).
SELECT id, kind, description, created_at, undone_at
FROM action_log
WHERE undone_at IS NULL
ORDER BY id DESC
LIMIT 1;

-- name: GetLatestUndoneAction :one
-- Most recently undone action (redo target; undo/redo move a single pointer, so
-- redo re-applies the last undone action first, strictly LIFO).
SELECT id, kind, description, created_at, undone_at
FROM action_log
WHERE undone_at IS NOT NULL
ORDER BY undone_at DESC, id DESC
LIMIT 1;

-- name: ListActionChanges :many
-- All row snapshots of one action, in insertion order.
SELECT action_id, entity, entity_id, before_json, after_json
FROM action_change
WHERE action_id = ?
ORDER BY entity, entity_id;
