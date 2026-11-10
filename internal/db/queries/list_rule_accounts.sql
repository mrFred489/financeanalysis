-- name: ListRuleAccounts :many
-- All rule_account rows, so the evaluator can load every rule's account filter in one query.
SELECT rule_id, account_id FROM rule_account ORDER BY rule_id, account_id;
