-- name: InsertActionChange :exec
-- One affected row of an action: whole-row JSON snapshots before/after (D8).
INSERT INTO action_change (action_id, entity, entity_id, before_json, after_json)
VALUES (?, ?, ?, ?, ?);
