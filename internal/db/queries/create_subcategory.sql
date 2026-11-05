-- name: CreateSubcategory :one
INSERT INTO subcategory (category_id, name, color, icon, hotkey, sort_order)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id, category_id, name, color, icon, hotkey, sort_order;
