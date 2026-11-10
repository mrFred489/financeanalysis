-- name: DeleteRuleAccountsForRule :exec
DELETE FROM rule_account WHERE rule_id = ?;
