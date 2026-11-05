-- name: ListSubcategoriesByCategory :many
SELECT id, category_id, name, color, icon, hotkey, sort_order
FROM subcategory
WHERE category_id = ?
ORDER BY sort_order, id;
