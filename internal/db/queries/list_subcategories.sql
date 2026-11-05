-- name: ListSubcategories :many
SELECT id, category_id, name, color, icon, hotkey, sort_order
FROM subcategory
ORDER BY category_id, sort_order, id;
