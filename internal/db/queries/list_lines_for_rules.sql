-- name: ListLinesForRules :many
-- All lines with the columns needed for matching (preview/diff scan every line,
-- including manual/transfer, to report the "unaffected" groups of RULE-3).
SELECT id, account_id, message, amount_minor, source, subcategory_id, rule_id
FROM statement_line
ORDER BY id;
