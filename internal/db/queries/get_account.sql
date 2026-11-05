-- name: GetAccount :one
SELECT id, name, account_number, sort_order, archived, created_at
FROM account
WHERE id = ?;
