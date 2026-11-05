-- name: GetSubcategory :one
SELECT id, category_id, name, color, icon, hotkey, sort_order
FROM subcategory
WHERE id = ?;
