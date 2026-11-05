-- name: ListStatementLines :many
-- Filtered, paginated statement lines (STMT-2/STMT-3). NULL params disable the filter.
-- NOTE: ORDER BY is fixed here because sqlc cannot parameterize it; the server
-- builds the ORDER BY dynamically (whitelisted columns) and reuses this row type.
SELECT
    sl.id, sl.account_id, a.name AS account_name, sl.import_id,
    sl.booking_date, sl.transaction_date, sl.value_date,
    sl.message, sl.amount_minor, sl.balance_minor, sl.currency,
    sl.bank_ref, sl.counterparty_account,
    sl.file_row, sl.seq, sl.occurrence,
    sl.subcategory_id, sc.name AS subcategory_name, sc.color AS subcategory_color,
    c.id AS category_id, c.name AS category_name,
    sl.source, sl.rule_id, sl.spread_months
FROM statement_line sl
JOIN account a ON a.id = sl.account_id
LEFT JOIN subcategory sc ON sc.id = sl.subcategory_id
LEFT JOIN category c ON c.id = sc.category_id
WHERE (sqlc.narg(from_date) IS NULL OR sl.booking_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date) IS NULL OR sl.booking_date <= sqlc.narg(to_date))
  AND (sqlc.narg(accounts) IS NULL OR sl.account_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(accounts))))
  AND (sqlc.narg(direction) IS NULL OR (sqlc.narg(direction) = 'in' AND sl.amount_minor > 0) OR (sqlc.narg(direction) = 'out' AND sl.amount_minor < 0))
  AND (sqlc.narg(amount_min) IS NULL OR sl.amount_minor >= sqlc.narg(amount_min))
  AND (sqlc.narg(amount_max) IS NULL OR sl.amount_minor <= sqlc.narg(amount_max))
  AND (sqlc.narg(message) IS NULL OR sl.message LIKE '%' || sqlc.narg(message) || '%')
  AND (sqlc.narg(categories) IS NULL OR sc.category_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(categories))))
  AND (sqlc.narg(subcategories) IS NULL OR sl.subcategory_id IN (SELECT CAST(value AS INTEGER) FROM json_each(sqlc.narg(subcategories))))
  AND (sqlc.narg(source) IS NULL OR sl.source IN (SELECT value FROM json_each(sqlc.narg(source))))
  AND (sqlc.narg(import_id) IS NULL OR sl.import_id = sqlc.narg(import_id))
  AND (sqlc.narg(spread_gt) IS NULL OR sl.spread_months > sqlc.narg(spread_gt))
ORDER BY sl.booking_date DESC, sl.seq DESC, sl.id
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);
