-- name: GetSetting :one
-- Get a setting by key.
SELECT key, value FROM setting WHERE key = ?;
