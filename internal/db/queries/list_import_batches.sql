-- name: ListImportBatches :many
-- Import history (IMP-14) with account name, line date range and actual line count.
SELECT
    ib.id,
    ib.account_id,
    a.name AS account_name,
    ib.file_name,
    ib.file_sha256,
    ib.imported_at,
    ib.line_count,
    ib.duplicate_count,
    ib.undone_at,
    (SELECT MIN(sl.booking_date) FROM statement_line sl WHERE sl.import_id = ib.id) AS date_from,
    (SELECT MAX(sl.booking_date) FROM statement_line sl WHERE sl.import_id = ib.id) AS date_to,
    (SELECT COUNT(*) FROM statement_line sl WHERE sl.import_id = ib.id) AS actual_line_count
FROM import_batch ib
JOIN account a ON a.id = ib.account_id
ORDER BY ib.id DESC;
