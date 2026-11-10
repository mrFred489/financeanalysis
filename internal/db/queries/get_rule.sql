-- name: GetRule :one
SELECT id, name, pattern, direction, subcategory_id, enabled, created_at, updated_at
FROM rule
WHERE id = ?;
