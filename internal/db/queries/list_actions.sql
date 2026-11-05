-- name: ListActions :many
-- Recent actions, latest first, with their change counts (UNDO-5; feeds the M6
-- history panel via GET /api/actions).
SELECT al.id, al.kind, al.description, al.created_at, al.undone_at,
       COUNT(ac.entity_id) AS change_count
FROM action_log al
LEFT JOIN action_change ac ON ac.action_id = al.id
GROUP BY al.id
ORDER BY al.id DESC
LIMIT ?;
