-- name: ListLinesForViz :many
-- Minimal line set for M5 visualization aggregation (plan docs/plans/M5-visualization.md
-- step 1/D1/D3). Only the columns the aggregator needs; no message text, no ORDER BY
-- (aggregation is done in Go). NULL params disable the corresponding filter.
-- include_transfers = 0 adds `AND sl.source != 'transfer'` (VIZ default exclusion).
SELECT
    sl.id, sl.account_id, sl.booking_date, sl.amount_minor,
    sl.subcategory_id, sl.source, sl.spread_months,
    c.key AS category_key
FROM statement_line sl
LEFT JOIN subcategory sc ON sc.id = sl.subcategory_id
LEFT JOIN category c ON c.id = sc.category_id
WHERE (sqlc.narg(from_date) IS NULL OR sl.booking_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date) IS NULL OR sl.booking_date <= sqlc.narg(to_date))
  AND (sqlc.narg(accounts) IS NULL OR sl.account_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(accounts))))
  AND (sqlc.narg(categories) IS NULL OR sc.category_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(categories))))
  AND (sqlc.narg(subcategories) IS NULL OR sl.subcategory_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(subcategories))))
  AND (sqlc.narg(include_transfers) = 1 OR sl.source != 'transfer');
