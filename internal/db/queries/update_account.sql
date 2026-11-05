-- name: UpdateAccount :one
-- Partial update: NULL params leave the column unchanged.
UPDATE account SET
    name = COALESCE(sqlc.narg(name), name),
    account_number = COALESCE(sqlc.narg(account_number), account_number),
    sort_order = COALESCE(sqlc.narg(sort_order), sort_order),
    archived = COALESCE(sqlc.narg(archived), archived)
WHERE id = sqlc.arg(id)
RETURNING id, name, account_number, sort_order, archived, created_at;
