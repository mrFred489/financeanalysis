-- name: CountSubcategoryLines :one
SELECT COUNT(*) FROM statement_line WHERE subcategory_id = ?;
