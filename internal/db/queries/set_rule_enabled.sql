-- name: SetRuleEnabled :exec
UPDATE rule
SET enabled = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;
