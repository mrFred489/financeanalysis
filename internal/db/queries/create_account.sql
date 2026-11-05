-- name: CreateAccount :one
INSERT INTO account (name, account_number, sort_order)
VALUES (?, ?, ?)
RETURNING id, name, account_number, sort_order, archived, created_at;
