-- name: ListAccounts :many
-- List all accounts (including archived), ordered for display.
SELECT id, name, account_number, sort_order, archived, created_at
FROM account
ORDER BY sort_order, id;
