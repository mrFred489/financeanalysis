-- name: UpsertSetting :exec
-- Upsert a setting.
INSERT INTO setting (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
