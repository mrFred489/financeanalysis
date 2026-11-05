-- name: ListSettings :many
SELECT key, value FROM setting ORDER BY key;
