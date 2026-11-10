-- name: ListConflictedLines :many
-- Conflicted lines with display joins (GET /api/conflicts, RULE-7).
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
WHERE sl.source = 'conflict'
ORDER BY sl.booking_date DESC, sl.seq DESC, sl.id;
