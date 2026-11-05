-- name: CountStatementLines :one
-- Filtered count for pagination (STMT-3). NULL params disable the filter.
SELECT COUNT(*)
FROM statement_line sl
LEFT JOIN subcategory sc ON sc.id = sl.subcategory_id
WHERE (sqlc.narg(from_date) IS NULL OR sl.booking_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date) IS NULL OR sl.booking_date <= sqlc.narg(to_date))
  AND (sqlc.narg(accounts) IS NULL OR sl.account_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(accounts))))
  AND (sqlc.narg(direction) IS NULL OR (sqlc.narg(direction) = 'in' AND sl.amount_minor > 0) OR (sqlc.narg(direction) = 'out' AND sl.amount_minor < 0))
  AND (sqlc.narg(amount_min) IS NULL OR sl.amount_minor >= sqlc.narg(amount_min))
  AND (sqlc.narg(amount_max) IS NULL OR sl.amount_minor <= sqlc.narg(amount_max))
  AND (sqlc.narg(message) IS NULL OR sl.message LIKE '%' || sqlc.narg(message) || '%')
  AND (sqlc.narg(categories) IS NULL OR sc.category_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(categories))))
  AND (sqlc.narg(subcategories) IS NULL OR sl.subcategory_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(subcategories))))
  AND (sqlc.narg(source) IS NULL OR sl.source = sqlc.narg(source))
  AND (sqlc.narg(import_id) IS NULL OR sl.import_id = sqlc.narg(import_id))
  AND (sqlc.narg(spread_gt) IS NULL OR sl.spread_months > sqlc.narg(spread_gt));
