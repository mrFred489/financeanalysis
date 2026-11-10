-- name: ListRules :many
-- Rules with their target subcategory (RULE-8). NULL params disable the filter.
SELECT r.id, r.name, r.pattern, r.direction, r.subcategory_id, r.enabled, r.created_at, r.updated_at,
       sc.name AS subcategory_name, sc.color AS subcategory_color
FROM rule r
LEFT JOIN subcategory sc ON sc.id = r.subcategory_id
WHERE (sqlc.narg(enabled) IS NULL OR r.enabled = sqlc.narg(enabled))
  AND (sqlc.narg(subcategory_id) IS NULL OR r.subcategory_id = sqlc.narg(subcategory_id))
  AND (sqlc.narg(search) IS NULL OR r.name LIKE '%' || sqlc.narg(search) || '%' OR r.pattern LIKE '%' || sqlc.narg(search) || '%')
ORDER BY r.name, r.id;
