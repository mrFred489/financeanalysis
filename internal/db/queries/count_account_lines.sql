-- name: CountAccountLines :one
-- Number of statement lines on an account; >0 means the account cannot be deleted (SETUP-6).
SELECT COUNT(*) FROM statement_line WHERE account_id = ?;
