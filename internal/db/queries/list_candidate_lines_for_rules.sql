-- name: ListCandidateLinesForRules :many
-- Candidate lines for rule evaluation: everything not manual/transfer
-- (PRD 7.5 precedence -- those sources are never touched by rules).
SELECT id, account_id, message, amount_minor, source, subcategory_id, rule_id
FROM statement_line
WHERE source != 'manual' AND source != 'transfer'
ORDER BY statement_line.id;
