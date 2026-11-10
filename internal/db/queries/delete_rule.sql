-- name: DeleteRule :exec
-- Cascades rule_account and conflict_resolution (FK ON DELETE CASCADE).
-- Lines that pointed at the rule are re-evaluated before this runs (RULE-6).
DELETE FROM rule WHERE id = ?;
