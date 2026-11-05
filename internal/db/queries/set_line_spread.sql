-- name: SetLineSpread :one
-- Set spread months (VIZ-7, CATZ-7). Range 1..36 is enforced by the CHECK
-- constraint; handlers validate first to return a clean 400.
UPDATE statement_line
SET spread_months = ?
WHERE id = ?
RETURNING id;
