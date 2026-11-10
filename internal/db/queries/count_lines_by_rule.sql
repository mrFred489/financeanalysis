-- name: CountLinesByRule :one
-- matchCount part 1 (D10): lines currently categorized by this rule.
SELECT COUNT(*) FROM statement_line WHERE rule_id = ? AND source = 'rule';
