-- name: ListCategories :many
-- Get all categories.
SELECT id, key, name, color FROM category ORDER BY id;
