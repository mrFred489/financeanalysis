-- name: ListAccountsForRule :many
SELECT account_id FROM rule_account WHERE rule_id = ? ORDER BY account_id;
