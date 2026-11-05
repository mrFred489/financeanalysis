-- name: ReassignLinesToSubcategory :exec
-- Merge-delete support (CAT-4): move lines from one subcategory to another.
UPDATE statement_line SET subcategory_id = ? WHERE subcategory_id = ?;
