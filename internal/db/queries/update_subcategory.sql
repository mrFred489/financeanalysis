-- name: UpdateSubcategory :one
-- Partial update: NULL params leave the column unchanged.
UPDATE subcategory SET
    category_id = COALESCE(sqlc.narg(category_id), category_id),
    name = COALESCE(sqlc.narg(name), name),
    color = COALESCE(sqlc.narg(color), color),
    icon = sqlc.narg(icon),
    hotkey = sqlc.narg(hotkey),
    sort_order = COALESCE(sqlc.narg(sort_order), sort_order)
WHERE id = sqlc.arg(id)
RETURNING id, category_id, name, color, icon, hotkey, sort_order;
