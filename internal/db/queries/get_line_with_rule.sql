-- name: GetLineWithRule :one
-- Line plus its rule's name (display when source = 'rule').
SELECT sl.id, sl.account_id, sl.rule_id, r.name AS rule_name,
       sl.source, sl.subcategory_id, sl.message, sl.amount_minor
FROM statement_line sl
LEFT JOIN rule r ON r.id = sl.rule_id
WHERE sl.id = ?;
