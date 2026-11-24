-- name: ListActionChangesByEntity :many
-- All change rows of one entity kind (M4-fix1: transfer_rejection rows are
-- filtered in Go via actions.DecodeRejectionID, keeping the synthetic key
-- single-sourced in the actions package).
SELECT action_id, entity, entity_id, before_json, after_json
FROM action_change
WHERE entity = ?;
