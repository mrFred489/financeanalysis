-- name: InsertRuleAccount :exec
INSERT INTO rule_account (rule_id, account_id) VALUES (?, ?);
